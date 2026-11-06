package tryon

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	MaxPendingTryOnJobs = 8
	tryOnWorkers        = 2
	tryOnPollInterval   = 3 * time.Second
	tryOnLease          = jobTimeout + time.Minute
)

type TryOnWork struct {
	Job   Job
	Owner Owner
	Lease time.Time
}

type TryOnQueueRepository interface {
	ClaimTryOnJob(context.Context, time.Time) (TryOnWork, error)
	FinishTryOnJob(context.Context, string, time.Time, Image, string) error
}

func (s *Service) WithTryOnQueue(repo TryOnQueueRepository) *Service {
	s.tryOnQueue = repo
	s.tryOnWake = make(chan struct{}, tryOnWorkers)
	return s
}

func (s *Service) Start(ctx context.Context, owner Owner, in JobInput) (Job, error) {
	if !validModelCode(in.ModelCode) {
		return Job{}, ErrModel
	}
	if in.GarmentIDs == nil && in.GarmentID != "" {
		in.GarmentIDs = []string{in.GarmentID}
	}
	ids, err := normalizeGarmentIDs(in.GarmentIDs, 1)
	if err != nil {
		return Job{}, err
	}
	if in.GarmentID != "" {
		first, err := uuid.Parse(in.GarmentID)
		if err != nil || first.String() != ids[0] {
			return Job{}, invalid("单件衣物标识与衣物列表不一致")
		}
	}

	person, err := s.Asset(ctx, owner, in.PersonID)
	if err != nil {
		return Job{}, err
	}
	if person.Deleted {
		return Job{}, ErrNotFound
	}
	if !isPerson(person.Kind) {
		return Job{}, invalid("请选择人物参考图")
	}
	garments, err := s.loadGarments(ctx, owner, ids)
	if err != nil {
		return Job{}, err
	}
	assets := append([]AssetImage{person}, garments...)
	if in.PoseID != "" {
		pose, err := s.Asset(ctx, owner, in.PoseID)
		if err != nil {
			return Job{}, err
		}
		if pose.Deleted {
			return Job{}, ErrNotFound
		}
		if pose.Kind != Pose {
			return Job{}, invalid("请选择姿势参考图")
		}
		assets = append(assets, pose)
		in.PoseID = pose.ID
	}
	models, err := s.engine.Models(ctx, owner)
	if err != nil {
		return Job{}, err
	}
	if !slices.ContainsFunc(models.Image, func(model Model) bool { return model.Code == in.ModelCode }) {
		return Job{}, ErrModel
	}
	job, err := s.repo.CreateJob(ctx, owner, Job{
		ID: uuid.NewString(), PersonID: person.ID, GarmentID: ids[0], GarmentIDs: ids,
		PoseID: in.PoseID, ModelCode: in.ModelCode,
	}, time.Now(), builtinSnapshots(assets))
	if err != nil {
		return Job{}, err
	}
	select {
	case s.tryOnWake <- struct{}{}:
	default:
	}
	return job, nil
}

func (s *Service) RunTryOnJobs(ctx context.Context) {
	var workers sync.WaitGroup
	for range tryOnWorkers {
		workers.Go(func() { s.runTryOnWorker(ctx) })
	}
	workers.Wait()
}

func (s *Service) runTryOnWorker(ctx context.Context) {
	for ctx.Err() == nil {
		claimCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		work, err := s.tryOnQueue.ClaimTryOnJob(claimCtx, time.Now().Add(tryOnLease))
		cancel()
		if err == nil {
			s.runTryOnJob(ctx, work)
			continue
		}
		if !errors.Is(err, ErrNotFound) && ctx.Err() == nil {
			s.logger.Error("领取试穿任务失败", "error", err)
		}
		timer := time.NewTimer(tryOnPollInterval)
		select {
		case <-ctx.Done():
		case <-s.tryOnWake:
		case <-timer.C:
		}
		timer.Stop()
	}
}

func (s *Service) runTryOnJob(parent context.Context, work TryOnWork) {
	ctx, cancel := context.WithTimeout(parent, jobTimeout)
	defer cancel()
	result, err := s.generateTryOn(ctx, work)
	message := ""
	if err != nil {
		message = ErrInference.Error()
		switch {
		case errors.Is(err, ErrModel):
			message = ErrModel.Error()
		case errors.Is(err, ErrNotFound):
			message = "找不到本次试穿的参考素材，请重新选择"
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			message = "试穿生成超时，请重试"
		case parent.Err() != nil:
			message = "服务已停止，本次试穿中断，可重新生成"
		}
		result = Image{}
		s.logger.Warn("AI 试穿生成失败", "job_id", work.Job.ID, "error", err)
	}
	saveCtx, saveCancel := context.WithTimeout(context.WithoutCancel(parent), 10*time.Second)
	defer saveCancel()
	if err := s.tryOnQueue.FinishTryOnJob(saveCtx, work.Job.ID, work.Lease, result, message); err != nil {
		s.logger.Error("保存 AI 试穿任务状态失败", "job_id", work.Job.ID, "error", err)
	}
}

func (s *Service) generateTryOn(ctx context.Context, work TryOnWork) (Image, error) {
	job := work.Job
	person, err := s.repo.Asset(ctx, work.Owner, job.PersonID)
	if err != nil {
		return Image{}, err
	}
	ids := job.GarmentIDs
	if len(ids) == 0 {
		ids = []string{job.GarmentID}
	}
	garments := make([]AssetImage, 0, len(ids))
	for _, id := range ids {
		// 即使素材随后被移出库，已提交任务仍使用原来的快照。
		garment, err := s.repo.Asset(ctx, work.Owner, id)
		if err != nil {
			return Image{}, err
		}
		garments = append(garments, garment)
	}
	garmentRef, err := garmentReference(garments)
	if err != nil {
		return Image{}, err
	}
	refs := []Image{person.Image, garmentRef}
	if job.PoseID != "" {
		pose, err := s.repo.Asset(ctx, work.Owner, job.PoseID)
		if err != nil {
			return Image{}, err
		}
		refs = append(refs, pose.Image)
	}
	prompt := outfitTryOnPrompt(person.Kind, garments, job.PoseID != "")
	result, err := s.engine.Generate(ctx, work.Owner, job.ModelCode, "tryon_generate", prompt, refs)
	if err != nil {
		return Image{}, err
	}
	mime, err := imageInfo(result.Data, MaxResultBytes)
	if err != nil {
		return Image{}, err
	}
	result.MIME = mime
	return result, nil
}
