package novel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	DraftGenerationIdle      = "idle"
	DraftGenerationRunning   = "running"
	DraftGenerationSucceeded = "succeeded"
	DraftGenerationFailed    = "failed"
	DraftGenerationLease     = 41 * time.Minute
	DraftGenerationRetention = 24 * time.Hour
	draftGenerationTimeout   = 40 * time.Minute
)

type DraftGenerationInput struct {
	Version     int    `json:"version"`
	ModelCode   string `json:"modelCode"`
	Instruction string `json:"instruction"`
}

type DraftGeneration struct {
	Status         string `json:"status"`
	Phase          string `json:"phase,omitempty"`
	JobID          string `json:"jobId,omitempty"`
	ErrorMessage   string `json:"errorMessage,omitempty"`
	WarningMessage string `json:"warningMessage,omitempty"`
	ProjectVersion int    `json:"projectVersion,omitempty"`
}

type DraftGenerationStore interface {
	StartDraft(context.Context, Owner, string, int, DraftGeneration) (DraftGeneration, bool, error)
	GetDraft(context.Context, Owner, string, int) (DraftGeneration, error)
	UpdateDraft(context.Context, Owner, string, int, DraftGeneration) (bool, error)
}

func (s *Service) WithDraftGeneration(store DraftGenerationStore) *Service {
	s.draftJobs = store
	return s
}

func (s *Service) StartDraftGeneration(ctx context.Context, owner Owner, id string, chapter int, input DraftGenerationInput) (DraftGeneration, error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return DraftGeneration{}, ErrNotFound
	}
	id = parsed.String()
	input.Instruction = strings.TrimSpace(input.Instruction)
	input.ModelCode = strings.TrimSpace(input.ModelCode)
	if input.Version < 1 || !validOptional(input.Instruction, 1200) {
		return DraftGeneration{}, ErrInvalid
	}
	if !valid(input.ModelCode, 160) {
		return DraftGeneration{}, ErrModel
	}
	if s.draftJobs == nil {
		return DraftGeneration{}, errors.New("未配置小说正文任务存储")
	}
	parent := context.WithoutCancel(ctx)
	startCtx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	p, err := s.Get(startCtx, owner, id)
	if err != nil {
		return DraftGeneration{}, err
	}
	index, err := chapterActionIndex(&p, Action{Kind: "write", Chapter: chapter})
	if err != nil {
		return DraftGeneration{}, err
	}
	current, err := s.draftJobs.GetDraft(startCtx, owner, id, chapter)
	if err != nil {
		return DraftGeneration{}, fmt.Errorf("读取小说正文任务失败: %w", err)
	}
	if current.Status == DraftGenerationRunning {
		return current, nil
	}
	if p.Version != input.Version {
		return DraftGeneration{}, ErrConflict
	}
	deadline := time.Now().Add(draftGenerationTimeout)
	job, started, err := s.draftJobs.StartDraft(startCtx, owner, id, chapter, DraftGeneration{
		Status: DraftGenerationRunning, Phase: "writing", JobID: uuid.NewString(),
	})
	if err != nil {
		return DraftGeneration{}, fmt.Errorf("创建小说正文任务失败: %w", err)
	}
	if started {
		go s.runDraftGeneration(parent, deadline, owner, id, index, input, job)
	}
	return job, nil
}

func (s *Service) GetDraftGeneration(ctx context.Context, owner Owner, id string, chapter int) (DraftGeneration, error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return DraftGeneration{}, ErrNotFound
	}
	if chapter < 1 || chapter > MaxChapters {
		return DraftGeneration{}, ErrInvalid
	}
	if s.draftJobs == nil {
		return DraftGeneration{}, errors.New("未配置小说正文任务存储")
	}
	return s.draftJobs.GetDraft(ctx, owner, parsed.String(), chapter)
}

func (s *Service) runDraftGeneration(parent context.Context, deadline time.Time, owner Owner, id string, index int, input DraftGenerationInput, job DraftGeneration) {
	ctx, cancel := context.WithDeadline(parent, deadline)
	defer cancel()
	err := s.generateAndSaveDraft(ctx, owner, id, index, input, &job)
	job.Status, job.Phase = DraftGenerationSucceeded, ""
	if err != nil {
		job.Status = DraftGenerationFailed
		job.ErrorMessage = draftJobErrorMessage(err)
		job.WarningMessage = ""
		if job.ProjectVersion > 0 && !errors.Is(err, ErrConflict) {
			job.Status, job.ErrorMessage = DraftGenerationSucceeded, ""
			job.WarningMessage = "正文已保存，摘要或审稿结果未能保存。可继续编辑或重新审稿。"
		}
		slog.WarnContext(parent, "小说正文后台任务未完成", "job_id", job.JobID, "project_id", id, "chapter", index+1, "error", err)
	}
	finishCtx, finish := context.WithTimeout(parent, 10*time.Second)
	defer finish()
	if _, err := s.draftJobs.UpdateDraft(finishCtx, owner, id, index+1, job); err != nil {
		slog.ErrorContext(parent, "保存小说正文任务状态失败", "job_id", job.JobID, "error", err)
	}
}

func (s *Service) generateAndSaveDraft(ctx context.Context, owner Owner, id string, index int, input DraftGenerationInput, job *DraftGeneration) error {
	p, err := s.Get(ctx, owner, id)
	if err != nil {
		return err
	}
	if p.Version != input.Version {
		return ErrConflict
	}
	draft, err := s.writeChapterDraft(ctx, owner, &p, index, input.ModelCode, input.Instruction, p.Chapters[index].Review)
	if err != nil {
		return err
	}
	// 先落库正文。服务在审稿中途停止时，这个标记让作者仍能手动补审。
	draft.ReviewFailed = true
	p.Chapters[index] = draft
	saved, err := s.saveDraftForJob(ctx, owner, p, index, job.JobID)
	if err != nil {
		return err
	}
	job.ProjectVersion, job.Phase = saved.Version, "reviewing"
	active, err := s.draftJobs.UpdateDraft(ctx, owner, p.ID, index+1, *job)
	if err != nil {
		return fmt.Errorf("更新小说正文任务进度失败: %w", err)
	}
	if !active {
		return errors.New("小说正文任务租约已失效")
	}
	saved.Chapters = slices.Clone(saved.Chapters)
	s.finishChapterDraft(ctx, owner, &saved, index, input.ModelCode, nil, nil)
	if saved.Chapters[index].ReviewFailed {
		job.WarningMessage = "正文已保存，摘要或审稿未完成。可继续编辑或重新审稿。"
	}
	if saved.Chapters[index] == draft {
		return nil
	}
	p, err = s.saveDraftForJob(ctx, owner, saved, index, job.JobID)
	if err != nil {
		return err
	}
	job.ProjectVersion = p.Version
	return nil
}

func (s *Service) saveDraftForJob(ctx context.Context, owner Owner, p Project, index int, jobID string) (Project, error) {
	if err := ctx.Err(); err != nil {
		return Project{}, err
	}
	current, err := s.draftJobs.GetDraft(ctx, owner, p.ID, index+1)
	if err != nil {
		return Project{}, fmt.Errorf("核对小说正文任务归属失败: %w", err)
	}
	if current.Status != DraftGenerationRunning || current.JobID != jobID {
		return Project{}, errors.New("小说正文任务租约已失效")
	}
	return s.repo.Save(ctx, owner, p)
}

func draftJobErrorMessage(err error) string {
	var draftError *DraftOutputError
	var formatError *ModelOutputFormatError
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "正文生成超时，请重新生成。"
	case errors.Is(err, ErrConflict):
		return "作品在生成期间有其他改动，请刷新后查看当前版本。"
	case errors.Is(err, ErrModel):
		return ErrModel.Error()
	case errors.As(err, &draftError):
		return draftError.Error()
	case errors.As(err, &formatError):
		return ModelOutputFormatMessage
	default:
		return "正文生成未完成，请重新生成。"
	}
}
