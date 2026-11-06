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
	MaxGarmentBatch       = 8
	MaxPendingGarmentJobs = 16
	garmentWorkers        = 2
	garmentPollInterval   = 3 * time.Second
	garmentLease          = jobTimeout + time.Minute
	garmentRetention      = 7 * 24 * time.Hour
)

var (
	ErrGarmentJobState = errors.New("当前任务状态无法执行此操作")
	ErrGarmentJobLimit = errors.New("待生成的衣物已达上限，请等部分任务完成后再提交")
)

type GarmentJob struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Category  string    `json:"category"`
	ModelCode string    `json:"modelCode"`
	Status    string    `json:"status"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

type GarmentJobItem struct {
	Name     string
	Category string
	Box      *Box
}

type GarmentWork struct {
	Job    GarmentJob
	Owner  Owner
	Box    *Box
	Source Image
	Lease  time.Time
}

type GarmentJobRepository interface {
	CreateGarmentJobs(context.Context, Owner, string, []GarmentJobItem, Image) ([]GarmentJob, error)
	ClaimGarmentJob(context.Context, time.Time) (GarmentWork, error)
	FinishGarmentJob(context.Context, string, time.Time, Image, string) error
	ListGarmentJobs(context.Context, Owner) ([]GarmentJob, error)
	GarmentJobResult(context.Context, Owner, string) (Image, error)
	SaveGarmentJob(context.Context, Owner, string) (Asset, error)
	RetryGarmentJob(context.Context, Owner, string) (GarmentJob, error)
	PruneGarmentJobs(context.Context, time.Time) error
}

func (s *Service) WithGarmentJobs(repo GarmentJobRepository) *Service {
	s.garmentJobs = repo
	s.garmentWake = make(chan struct{}, garmentWorkers)
	return s
}

func (s *Service) StartGarmentJobs(ctx context.Context, owner Owner, model string, items []GarmentJobItem, data []byte) ([]GarmentJob, error) {
	if len(items) < 1 || len(items) > MaxGarmentBatch {
		return nil, invalid("每批请选择 1 至 8 件衣物")
	}
	items = slices.Clone(items)
	for i := range items {
		item := &items[i]
		meta, err := assetFields(Garment, item.Name, item.Category)
		if err != nil {
			return nil, err
		}
		item.Name = meta.Name
		if item.Box != nil && !item.Box.Valid() {
			return nil, invalid("请框选有效的衣物区域")
		}
	}
	if !validModelCode(model) {
		return nil, ErrModel
	}
	models, err := s.engine.Models(ctx, owner)
	if err != nil {
		return nil, err
	}
	if !slices.ContainsFunc(models.Image, func(item Model) bool { return item.Code == model }) {
		return nil, ErrModel
	}
	source, err := prepareImage(data, nil)
	if err != nil {
		return nil, err
	}
	jobs, err := s.garmentJobs.CreateGarmentJobs(ctx, owner, model, items, source)
	if err == nil {
		s.wakeGarmentWorkers(len(jobs))
	}
	return jobs, err
}

func (s *Service) ListGarmentJobs(ctx context.Context, owner Owner) ([]GarmentJob, error) {
	return s.garmentJobs.ListGarmentJobs(ctx, owner)
}

func (s *Service) GarmentJobResult(ctx context.Context, owner Owner, id string) (Image, error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return Image{}, ErrNotFound
	}
	return s.garmentJobs.GarmentJobResult(ctx, owner, parsed.String())
}

func (s *Service) SaveGarmentJob(ctx context.Context, owner Owner, id string) (Asset, error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return Asset{}, ErrNotFound
	}
	return s.garmentJobs.SaveGarmentJob(ctx, owner, parsed.String())
}

func (s *Service) RetryGarmentJob(ctx context.Context, owner Owner, id string) (GarmentJob, error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return GarmentJob{}, ErrNotFound
	}
	job, err := s.garmentJobs.RetryGarmentJob(ctx, owner, parsed.String())
	if err == nil {
		s.wakeGarmentWorkers(1)
	}
	return job, err
}

func (s *Service) wakeGarmentWorkers(n int) {
	for range min(n, garmentWorkers) {
		select {
		case s.garmentWake <- struct{}{}:
		default:
			return
		}
	}
}

// RunGarmentJobs 由应用启动。任务在数据库排队，关闭上传窗口或刷新页面不会取消。
func (s *Service) RunGarmentJobs(ctx context.Context) {
	var workers sync.WaitGroup
	for range garmentWorkers {
		workers.Go(func() { s.runGarmentWorker(ctx) })
	}
	defer workers.Wait()

	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for ctx.Err() == nil {
		pruneCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		err := s.garmentJobs.PruneGarmentJobs(pruneCtx, time.Now().Add(-garmentRetention))
		cancel()
		if err != nil && ctx.Err() == nil {
			s.logger.Error("清理过期衣物生成任务失败", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) runGarmentWorker(ctx context.Context) {
	for ctx.Err() == nil {
		claimCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		work, err := s.garmentJobs.ClaimGarmentJob(claimCtx, time.Now().Add(garmentLease))
		cancel()
		if err == nil {
			s.generateGarment(ctx, work)
			continue
		}
		if !errors.Is(err, ErrNotFound) && ctx.Err() == nil {
			s.logger.Error("领取衣物生成任务失败", "error", err)
		}

		timer := time.NewTimer(garmentPollInterval)
		select {
		case <-ctx.Done():
		case <-s.garmentWake:
		case <-timer.C:
		}
		timer.Stop()
	}
}

func (s *Service) generateGarment(ctx context.Context, work GarmentWork) {
	job := work.Job
	result, err := s.PreviewAsset(ctx, work.Owner, AssetPreviewInput{
		Kind: Garment, Name: job.Name, Category: job.Category, ModelCode: job.ModelCode, Box: work.Box,
	}, work.Source.Data)
	failure := ""
	if err != nil {
		failure = ErrInference.Error()
		if errors.Is(err, ErrModel) {
			failure = ErrModel.Error()
		} else if ctx.Err() != nil {
			failure = "服务已停止，本次生成中断，可重试"
		}
		s.logger.Warn("衣物生成失败", "job_id", job.ID, "error", err)
	}
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := s.garmentJobs.FinishGarmentJob(finishCtx, job.ID, work.Lease, result, failure); err != nil {
		s.logger.Error("保存衣物生成结果失败", "job_id", job.ID, "error", err)
	}
}
