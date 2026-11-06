package novel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

const (
	CoverGenerationIdle        = "idle"
	CoverGenerationRunning     = "running"
	CoverGenerationSucceeded   = "succeeded"
	CoverGenerationFailed      = "failed"
	CoverGenerationInterrupted = "封面生成中断，请重新生成"
	CoverGenerationLease       = 11 * time.Minute
	CoverGenerationRetention   = 24 * time.Hour
	coverGenerationTimeout     = 10 * time.Minute
)

type CoverGeneration struct {
	Status              string `json:"status"`
	JobID               string `json:"jobId,omitempty"`
	PreviewToken        string `json:"previewToken,omitempty"`
	PreviewBaseRevision string `json:"previewBaseRevision,omitempty"`
	ErrorMessage        string `json:"errorMessage,omitempty"`
	ModelCode           string `json:"modelCode,omitempty"`
	StyleID             string `json:"styleId,omitempty"`
	Instruction         string `json:"instruction,omitempty"`
}

type CoverGenerationStore interface {
	StartGeneration(context.Context, Owner, string, CoverGeneration) (CoverGeneration, bool, error)
	GetGeneration(context.Context, Owner, string) (CoverGeneration, error)
	FinishGeneration(context.Context, Owner, string, CoverGeneration, *CoverPreview) error
}

func (s *Service) WithCoverGeneration(store CoverGenerationStore) *Service {
	s.coverJobs = store
	return s
}

func (s *Service) StartCoverGeneration(ctx context.Context, owner Owner, id string, input GenerateCoverInput) (CoverGeneration, error) {
	if s.coverJobs == nil {
		return CoverGeneration{}, errors.New("未配置封面生成任务存储")
	}
	parent := context.WithoutCancel(ctx)
	startCtx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	project, prompt, err := s.prepareCoverGeneration(startCtx, owner, id, &input)
	if err != nil {
		return CoverGeneration{}, err
	}
	job, started, err := s.coverJobs.StartGeneration(startCtx, owner, project.ID, CoverGeneration{
		JobID: uuid.NewString(), Status: CoverGenerationRunning, ModelCode: input.ModelCode,
		StyleID: input.StyleID, Instruction: input.Instruction,
	})
	if err != nil {
		return CoverGeneration{}, fmt.Errorf("创建小说封面生成任务失败: %w", err)
	}
	revision := coverRevision(project)
	if started {
		go s.runCoverGeneration(parent, owner, project.ID, job, revision, prompt)
	}
	return job.forCoverRevision(revision), nil
}

func (s *Service) GetCoverGeneration(ctx context.Context, owner Owner, id string) (CoverGeneration, error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return CoverGeneration{}, ErrNotFound
	}
	id = parsed.String()
	revision, err := s.repo.GetCoverRevision(ctx, owner, id)
	if err != nil {
		return CoverGeneration{}, err
	}
	if s.coverJobs == nil {
		return CoverGeneration{}, errors.New("未配置封面生成任务存储")
	}
	job, err := s.coverJobs.GetGeneration(ctx, owner, id)
	if err != nil {
		return CoverGeneration{}, err
	}
	if job.Status == CoverGenerationIdle && s.coverPreviews != nil {
		// 兼容上线前保存在旧缓存格式中的候选。正常轮询仅读取任务元数据。
		preview, err := s.coverPreviews.Get(ctx, owner, id)
		if err == nil {
			job.Status, job.PreviewToken, job.PreviewBaseRevision = CoverGenerationSucceeded, preview.Token, preview.PreviousRevision
		} else if !errors.Is(err, ErrCoverPreviewExpired) {
			return CoverGeneration{}, err
		}
	}
	return job.forCoverRevision(revision), nil
}

func (job CoverGeneration) forCoverRevision(revision string) CoverGeneration {
	if job.PreviewToken == revision || job.PreviewBaseRevision != revision {
		job.PreviewToken, job.PreviewBaseRevision = "", ""
	}
	if job.Status == CoverGenerationSucceeded && job.PreviewToken == "" {
		job.Status = CoverGenerationIdle
	}
	return job
}

func (s *Service) runCoverGeneration(parent context.Context, owner Owner, id string, job CoverGeneration, revision, prompt string) {
	ctx, cancel := context.WithTimeout(parent, coverGenerationTimeout)
	defer cancel()
	preview, err := s.generateCoverPreview(ctx, owner, job.ModelCode, prompt, revision, job.JobID)
	if err == nil {
		archiveCtx, archiveCancel := context.WithTimeout(parent, 10*time.Second)
		err = s.archiveCoverPreview(archiveCtx, owner, id, preview)
		archiveCancel()
	}
	job.Status = CoverGenerationSucceeded
	var completed *CoverPreview
	if err == nil {
		completed = &preview
	} else {
		job.Status = CoverGenerationFailed
		job.ErrorMessage = ErrCoverGeneration.Error()
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			job.ErrorMessage = "封面生成超时，请重新生成"
		} else if errors.Is(err, ErrCoverModel) {
			job.ErrorMessage = ErrCoverModel.Error()
		}
		slog.WarnContext(parent, "小说封面生成失败", "job_id", job.JobID, "error", err)
	}
	saveCtx, saveCancel := context.WithTimeout(parent, 10*time.Second)
	defer saveCancel()
	if err := s.coverJobs.FinishGeneration(saveCtx, owner, id, job, completed); err != nil {
		slog.ErrorContext(parent, "保存小说封面生成状态失败", "job_id", job.JobID, "error", err)
	}
}

func (s *Service) awaitCoverGeneration(ctx context.Context, owner Owner, id string, input GenerateCoverInput) (string, error) {
	job, err := s.StartCoverGeneration(ctx, owner, id, input)
	if err != nil {
		return "", err
	}
	jobID := job.JobID
	// 保持旧同步入口的响应格式，同时和后台入口共用同一份任务。
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for job.Status == CoverGenerationRunning {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
			job, err = s.GetCoverGeneration(ctx, owner, id)
			if err != nil {
				return "", err
			}
			if job.JobID != jobID {
				return "", ErrCoverConflict
			}
		}
	}
	if job.Status == CoverGenerationSucceeded {
		return job.PreviewToken, nil
	}
	if job.ErrorMessage == ErrCoverModel.Error() {
		return "", ErrCoverModel
	}
	return "", ErrCoverGeneration
}
