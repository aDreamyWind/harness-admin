package novel

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"sync"
	"testing"
	"time"
)

type coverGenerationMemory struct {
	testCoverPreviews
	mu       sync.Mutex
	job      CoverGeneration
	finished chan CoverGeneration
}

func (s *coverGenerationMemory) StartGeneration(_ context.Context, _ Owner, _ string, job CoverGeneration) (CoverGeneration, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.job.Status == CoverGenerationRunning {
		return s.job, false, nil
	}
	job.PreviewToken, job.PreviewBaseRevision = s.job.PreviewToken, s.job.PreviewBaseRevision
	s.job = job
	return job, true, nil
}

func (s *coverGenerationMemory) GetGeneration(context.Context, Owner, string) (CoverGeneration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.job, nil
}

func (s *coverGenerationMemory) FinishGeneration(ctx context.Context, owner Owner, id string, job CoverGeneration, preview *CoverPreview) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if preview != nil {
		if err := s.Put(ctx, owner, id, *preview); err != nil {
			return err
		}
		job.PreviewToken, job.PreviewBaseRevision = preview.Token, preview.PreviousRevision
	}
	s.job = job
	s.finished <- job
	return nil
}

type coverGeneratorFunc func(context.Context, Owner, string, string) ([]byte, error)

func (coverGeneratorFunc) CoverModels(context.Context, Owner) ([]Model, error) { return nil, nil }

func (f coverGeneratorFunc) GenerateCover(ctx context.Context, owner Owner, model, prompt string) ([]byte, error) {
	return f(ctx, owner, model, prompt)
}

func TestCoverGenerationContinuesAfterRequestAndRecoversCandidate(t *testing.T) {
	ctx := context.Background()
	owner := Owner{OrganizationID: "org", UserID: "author"}
	repo := &memoryRepository{}
	store := &coverGenerationMemory{job: CoverGeneration{Status: CoverGenerationIdle}, finished: make(chan CoverGeneration, 2)}
	var imageBytes bytes.Buffer
	if err := png.Encode(&imageBytes, image.NewNRGBA(image.Rect(0, 0, 8, 12))); err != nil {
		t.Fatal(err)
	}
	results := make(chan error)
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	model := coverGeneratorFunc(func(ctx context.Context, _ Owner, _, _ string) ([]byte, error) {
		select {
		case <-stop:
			return nil, context.Canceled
		case <-ctx.Done():
			return nil, ctx.Err()
		case err := <-results:
			return imageBytes.Bytes(), err
		}
	})
	s := NewService(repo, nil, model, store).WithCoverGeneration(store)
	p, err := s.Create(ctx, owner, CreateInput{Title: "潮汐档案", Brief: "守塔人听到呼救", ChapterCount: 1})
	if err != nil {
		t.Fatal(err)
	}
	status := func() CoverGeneration {
		t.Helper()
		job, err := s.GetCoverGeneration(ctx, owner, p.ID)
		if err != nil {
			t.Fatal(err)
		}
		return job
	}
	finish := func(result error) CoverGeneration {
		t.Helper()
		deadline := time.NewTimer(3 * time.Second)
		defer deadline.Stop()
		select {
		case results <- result:
		case <-deadline.C:
			t.Fatal("模型调用没有在后台运行")
		}
		select {
		case job := <-store.finished:
			return job
		case <-deadline.C:
			t.Fatal("后台任务结果未保存")
			return CoverGeneration{}
		}
	}
	if job := status(); job.Status != CoverGenerationIdle {
		t.Fatalf("新作品的封面状态应为空: %+v", job)
	}
	requestCtx, cancel := context.WithCancel(ctx)
	first, err := s.StartCoverGeneration(requestCtx, owner, p.ID, GenerateCoverInput{ModelCode: " art ", Instruction: " 冷色调 "})
	cancel()
	if err != nil || first.Status != CoverGenerationRunning || first.JobID == "" || first.ModelCode != "art" || first.Instruction != "冷色调" {
		t.Fatalf("后台任务应先返回可恢复的状态: %+v %v", first, err)
	}
	if job := status(); job != first {
		t.Fatalf("离开后应查到同一个任务: %+v", job)
	}
	other := Owner{OrganizationID: owner.OrganizationID, UserID: "another"}
	if _, err := s.GetCoverGeneration(ctx, other, p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("其他作者不应看到任务: %v", err)
	}
	if duplicate, err := s.StartCoverGeneration(ctx, owner, p.ID, GenerateCoverInput{ModelCode: "another-model"}); err != nil || duplicate != first {
		t.Fatalf("同一作品的并发请求应复用正在生成的任务: %+v %v", duplicate, err)
	}
	if done := finish(nil); done.Status != CoverGenerationSucceeded || done.PreviewToken != first.JobID || status() != done {
		t.Fatalf("页面请求取消后应能恢复已生成的预览: %+v", done)
	}
	if repo.project.Cover != nil {
		t.Fatal("生成候选时提前替换了正式封面")
	}
	if history, err := s.ListCoverHistory(ctx, owner, p.ID); err != nil || len(history) != 1 || history[0].Revision != first.JobID || history[0].Current {
		t.Fatalf("后台出图完成后应归档未采用的候选: %+v %v", history, err)
	}
	if preview, err := s.CoverPreviewContent(ctx, owner, p.ID, first.JobID); err != nil || !bytes.Equal(preview.Data, imageBytes.Bytes()) {
		t.Fatalf("重新打开后无法读取候选图片: %v", err)
	}
	second, err := s.StartCoverGeneration(ctx, owner, p.ID, GenerateCoverInput{ModelCode: "art", Instruction: "保留构图"})
	if err != nil || second.JobID == first.JobID || second.PreviewToken != first.JobID {
		t.Fatalf("重新生成时应保留已有候选: %+v %v", second, err)
	}
	if failed := finish(errors.New("供应商内部细节")); failed.Status != CoverGenerationFailed || failed.PreviewToken != first.JobID ||
		failed.ErrorMessage != ErrCoverGeneration.Error() || status() != failed {
		t.Fatalf("失败应保留已有候选并返回用户可读的提示: %+v", failed)
	}
	if cover, err := s.ApplyCoverPreview(ctx, owner, p.ID, first.JobID); err != nil || cover.Revision != first.JobID {
		t.Fatalf("用户仍应能手动确认已有候选: %+v %v", cover, err)
	}
	if job := status(); job.PreviewToken != "" {
		t.Fatalf("已应用的候选不应再次显示: %+v", job)
	}
}
