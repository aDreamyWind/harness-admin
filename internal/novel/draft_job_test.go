package novel

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type draftJobMemory struct {
	mu       sync.Mutex
	job      DraftGeneration
	finished chan DraftGeneration
}

func (m *draftJobMemory) StartDraft(_ context.Context, _ Owner, _ string, _ int, job DraftGeneration) (DraftGeneration, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.job.Status == DraftGenerationRunning {
		return m.job, false, nil
	}
	m.job = job
	return job, true, nil
}

func (m *draftJobMemory) GetDraft(context.Context, Owner, string, int) (DraftGeneration, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.job, nil
}

func (m *draftJobMemory) UpdateDraft(_ context.Context, _ Owner, _ string, _ int, job DraftGeneration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.job.JobID != job.JobID || m.job.Status != DraftGenerationRunning {
		return false, nil
	}
	m.job = job
	if job.Status != DraftGenerationRunning {
		m.finished <- job
	}
	return true, nil
}

type draftJobWriter func(context.Context, string) (string, error)

func (draftJobWriter) Models(context.Context, Owner) ([]Model, error) { return nil, nil }

func (w draftJobWriter) Complete(ctx context.Context, _ Owner, _, prompt string) (string, error) {
	return w(ctx, prompt)
}

func TestDraftGenerationContinuesAfterRefreshAndCheckpointsBody(t *testing.T) {
	for _, tc := range []struct {
		name       string
		edit       bool
		wantStatus string
		wantBody   string
	}{
		{"审稿失败保留正文", false, DraftGenerationSucceeded, "写好的正文"},
		{"迟到的审稿保留作者修改", true, DraftGenerationFailed, "作者手动修改后的正文"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			allowWriting, allowReview := make(chan struct{}), make(chan struct{})
			reviewStarted := make(chan struct{})
			store := &draftJobMemory{finished: make(chan DraftGeneration, 1)}
			writer := draftJobWriter(func(callCtx context.Context, prompt string) (string, error) {
				if strings.HasPrefix(prompt, reviewPrompt) {
					close(reviewStarted)
					select {
					case <-allowReview:
						if tc.edit {
							return `{"review":[]}`, nil
						}
						return "", errors.New("模拟审稿失败")
					case <-ctx.Done():
						return "", ctx.Err()
					}
				}
				select {
				case <-callCtx.Done():
					return "", callCtx.Err()
				case <-allowWriting:
					return `{"body":"写好的正文","summary":"连续性摘要"}`, nil
				case <-ctx.Done():
					return "", ctx.Err()
				}
			})
			owner := Owner{OrganizationID: "org", UserID: "author"}
			repo := &memoryRepository{}
			service := NewService(repo, writer, nil, nil).WithDraftGeneration(store)
			p, err := service.Create(ctx, owner, CreateInput{Title: "潮汐档案", Brief: "守塔人听到呼救", ChapterCount: 1})
			if err != nil {
				t.Fatal(err)
			}
			p.Outline = &Outline{Beats: []Beat{{Title: "第一章", Goal: "确认呼救位置"}}}
			p.Confirmed = true
			p, err = repo.Save(ctx, owner, p)
			if err != nil {
				t.Fatal(err)
			}
			input := DraftGenerationInput{Version: p.Version, ModelCode: "writer", Instruction: "从雨夜开始"}
			requestCtx, cancel := context.WithCancel(ctx)
			first, err := service.StartDraftGeneration(requestCtx, owner, p.ID, 1, input)
			cancel()
			if err != nil || first.Status != DraftGenerationRunning || first.Phase != "writing" || first.JobID == "" {
				t.Fatalf("没有返回可恢复的后台任务: %+v，%v", first, err)
			}
			duplicate, err := service.StartDraftGeneration(ctx, owner, p.ID, 1, input)
			if err != nil || duplicate.JobID != first.JobID {
				t.Fatalf("重复提交应继续查看同一任务: %+v，%v", duplicate, err)
			}
			close(allowWriting)
			select {
			case <-reviewStarted:
			case <-ctx.Done():
				t.Fatal("浏览器取消后后台未完成写稿")
			}
			checkpoint, err := service.Get(ctx, owner, p.ID)
			if err != nil || checkpoint.Chapters[0].Body != "写好的正文" || !checkpoint.Chapters[0].ReviewFailed {
				t.Fatalf("审稿前未持久化正文: %+v，%v", checkpoint.Chapters, err)
			}
			rejoined, err := service.GetDraftGeneration(ctx, owner, p.ID, 1)
			if err != nil || rejoined.Phase != "reviewing" || rejoined.JobID != first.JobID || rejoined.ProjectVersion != checkpoint.Version {
				t.Fatalf("刷新后未能重连审稿阶段: %+v，%v", rejoined, err)
			}
			if tc.edit {
				_, err = service.Apply(ctx, owner, p.ID, Action{
					Kind: "save_draft", Version: checkpoint.Version, Chapter: 1, Body: tc.wantBody, Summary: "作者摘要",
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			close(allowReview)
			select {
			case job := <-store.finished:
				if job.Status != tc.wantStatus || !tc.edit && job.WarningMessage == "" || tc.edit && job.ErrorMessage == "" {
					t.Fatalf("后台任务完成状态不正确: %+v", job)
				}
			case <-ctx.Done():
				t.Fatal("后台任务结果未保存")
			}
			current, err := service.Get(ctx, owner, p.ID)
			if err != nil || current.Chapters[0].Body != tc.wantBody {
				t.Fatalf("刷新后草稿丢失或被覆盖: %+v，%v", current.Chapters, err)
			}
		})
	}
}
