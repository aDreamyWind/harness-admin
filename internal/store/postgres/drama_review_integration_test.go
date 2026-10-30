package postgres

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"harness-admin/internal/drama"
)

func TestDramaReviewHistoryIsolatedByRunAndResumable(t *testing.T) {
	url := os.Getenv("HARNESS_TEST_WRITE_DATABASE_URL")
	if url == "" {
		t.Skip("设置数据库地址后验证审校记录")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var org, owner string
	if err = pool.QueryRow(ctx, `SELECT organization_id,id FROM users WHERE organization_id IS NOT NULL LIMIT 1`).Scan(&org, &owner); err != nil {
		t.Fatal(err)
	}
	id := uuid.NewString()
	repo := NewDramaRepository(pool)
	_, err = repo.Create(ctx, drama.Project{ID: id, OrganizationID: org, OwnerID: owner, Title: "累计审校临时测试", SourceText: "张三走入竹林", SourceLength: 6, SourceMode: "upload", Style: "国风", AspectRatio: "9:16", ModelCode: "writer", Status: "ready", Phase: "overview_done"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM drama_projects WHERE id=$1`, id); err != nil {
			t.Errorf("清理临时项目失败: %v", err)
		}
	}()
	batch, err := repo.CreateBatch(ctx, org, owner, drama.Batch{ID: uuid.NewString(), ProjectID: id, EndOffset: 6, SelectionMode: "beginning", Boundary: "end"})
	if err != nil {
		t.Fatal(err)
	}
	batch, err = repo.QueueBatchAnalysis(ctx, org, owner, id, batch.ID, drama.BatchRunOptions{ContextText: `{"version":3}`})
	if err != nil {
		t.Fatal(err)
	}
	job := drama.BatchAnalysisJob{Batch: batch, OrganizationID: org, OwnerID: owner}
	issue := drama.ReviewIssue{ID: uuid.NewString(), RunID: batch.AnalysisRunID, EpisodeNumber: 1, ContextHash: strings.Repeat("a", 64), Key: strings.Repeat("b", 64), Category: "fact", Severity: "blocking", Message: "动作方向有误", SourceQuote: "走入", ScriptQuote: "走出", SourceStart: 2, SourceEnd: 4, Status: "open"}
	if err = repo.SaveReviewIssues(ctx, job, []drama.ReviewIssue{issue}); err != nil {
		t.Fatal(err)
	}
	issue.Status = "resolved"
	if err = repo.SaveReviewIssues(ctx, job, []drama.ReviewIssue{issue}); err != nil {
		t.Fatal(err)
	}
	history, err := repo.ListReviewIssues(ctx, org, owner, id, batch.ID)
	if err != nil || len(history) != 1 || history[0].Status != "resolved" || history[0].SourceStart != 2 {
		t.Fatalf("审校记录必须幂等并保留依据: %+v %v", history, err)
	}
	if _, err = repo.ListReviewIssues(ctx, org, uuid.NewString(), id, batch.ID); !errors.Is(err, drama.ErrNotFound) {
		t.Fatal("其他用户不能读取审校问题")
	}
	wrong := job
	wrong.Batch.AnalysisRunID = uuid.NewString()
	if err = repo.SaveReviewIssues(ctx, wrong, []drama.ReviewIssue{issue}); !errors.Is(err, drama.ErrBatchConflict) {
		t.Fatal("其他运行不能写入本轮问题")
	}
	step := drama.BatchStep{Stage: "director", ChunkNumber: 1, ChunkTotal: 1, InputHash: strings.Repeat("c", 64), ResultText: `{"episodes":[]}`, DurationMS: 3000}
	if err = repo.StartBatchStep(ctx, org, owner, id, batch.ID, batch.AnalysisRevision, step); err != nil {
		t.Fatal(err)
	}
	if err = repo.CompleteBatchStep(ctx, org, owner, id, batch.ID, batch.AnalysisRevision, step); err != nil {
		t.Fatal(err)
	}
	if err = repo.StartBatchStep(ctx, org, owner, id, batch.ID, batch.AnalysisRevision, step); err != nil {
		t.Fatal(err)
	}
	steps, err := repo.ListBatchSteps(ctx, org, owner, id, batch.ID)
	if err != nil || steps[0].ResultText != step.ResultText || steps[0].InputHash != step.InputHash {
		t.Fatal("重试不能先清空上次完成的正文")
	}
	if err = repo.SaveRepairedEpisode(ctx, job, 1, `{"episodes":[{"scriptText":"张三走入竹林"}]}`, step.InputHash); err != nil {
		t.Fatal(err)
	}
	steps, err = repo.ListBatchSteps(ctx, org, owner, id, batch.ID)
	if err != nil || steps[0].Status != "completed" || !strings.Contains(steps[0].ResultText, "走入") {
		t.Fatal("局部修正必须更新可恢复的导演检查点")
	}
	if err = repo.FailBatchAnalysis(ctx, org, owner, id, batch.ID, batch.AnalysisRevision, "测试中断"); err != nil {
		t.Fatal(err)
	}
	resumed, err := repo.QueueBatchAnalysis(ctx, org, owner, id, batch.ID, drama.BatchRunOptions{Resume: true, ContextText: `{"version":3}`})
	if err != nil || resumed.AnalysisRunID != batch.AnalysisRunID {
		t.Fatal("继续解析必须保留运行标识")
	}
	if err = repo.FailBatchAnalysis(ctx, org, owner, id, batch.ID, batch.AnalysisRevision, "测试重启"); err != nil {
		t.Fatal(err)
	}
	restarted, err := repo.QueueBatchAnalysis(ctx, org, owner, id, batch.ID, drama.BatchRunOptions{ContextText: `{"version":3}`})
	if err != nil || restarted.AnalysisRunID == batch.AnalysisRunID {
		t.Fatal("重新解析必须创建独立运行")
	}
	history, err = repo.ListReviewIssues(ctx, org, owner, id, batch.ID)
	if err != nil || len(history) != 0 {
		t.Fatal("新运行不能沿用历史问题")
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM drama_review_issues WHERE batch_id=$1`, batch.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("历史审校记录不能随重新解析删除")
	}
}
