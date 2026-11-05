package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"harness-admin/internal/drama"
)

func TestDramaCanvasGenerationJobLifecycle(t *testing.T) {
	databaseURL := os.Getenv("HARNESS_TEST_WRITE_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("设置测试数据库后验证画布生成任务")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var scope drama.CanvasGenerationScope
	if err := db.QueryRow(ctx, `SELECT organization_id,id FROM users WHERE organization_id IS NOT NULL LIMIT 1`).Scan(&scope.OrganizationID, &scope.UserID); err != nil {
		t.Fatal(err)
	}
	scope.ProjectID, scope.NodeID = uuid.NewString(), uuid.NewString()
	repo := NewDramaRepository(db)
	_, err = repo.Create(ctx, drama.Project{
		ID: scope.ProjectID, OrganizationID: scope.OrganizationID, OwnerID: scope.UserID, Title: "临时画布任务验证",
		SourceText: "角色来到城门", SourceLength: 6, SourceMode: "upload", StyleID: "dark-concept", Style: "黑暗原画概念风",
		AspectRatio: "16:9", ModelCode: "test", Status: "ready", Phase: "overview_done",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := db.Exec(context.Background(), `DELETE FROM drama_projects WHERE id=$1`, scope.ProjectID); err != nil {
			t.Errorf("清理测试项目失败: %v", err)
		}
	}()
	board, err := repo.ReadCanvas(ctx, scope.OrganizationID, scope.UserID, scope.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	if job, err := repo.ReadCanvasGeneration(ctx, scope); job != nil || err != nil {
		t.Fatalf("没有任务时应返回空值: %+v %v", job, err)
	}
	if jobs, err := repo.ListCanvasGenerations(ctx, scope); len(jobs) != 0 || err != nil {
		t.Fatalf("项目没有任务时应返回空列表: %+v %v", jobs, err)
	}
	other := scope
	other.UserID = uuid.NewString()
	if _, err := repo.ListCanvasGenerations(ctx, other); !errors.Is(err, drama.ErrNotFound) {
		t.Fatalf("未拒绝其他用户读取项目任务: %v", err)
	}
	reserve := func(id string, revision int) (drama.CanvasGenerationJob, bool, error) {
		return repo.ReserveCanvasGeneration(ctx, scope, revision,
			drama.CanvasGenerationJob{ID: id, Kind: "image", ModelCode: "test"}, time.Now().Add(time.Minute))
	}
	firstID, secondID, thirdID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	if _, _, err := reserve(firstID, board.Revision-1); !errors.Is(err, drama.ErrCanvasConflict) {
		t.Fatalf("未拒绝过期版本的生成提交: %v", err)
	}
	if job, started, err := reserve(firstID, board.Revision); err != nil || !started || job.ID != firstID || job.Status != "running" || job.NodeID != scope.NodeID {
		t.Fatalf("提交画布任务失败: %+v %v %v", job, started, err)
	}
	if job, started, err := reserve(secondID, board.Revision); err != nil || started || job.ID != firstID {
		t.Fatalf("重复提交没有复用现有任务: %+v %v %v", job, started, err)
	}
	if err := repo.FinishCanvasGeneration(ctx, other, firstID, ""); !errors.Is(err, drama.ErrCanvasConflict) {
		t.Fatalf("其他用户可以完成项目任务: %v", err)
	}
	if err := repo.FinishCanvasGeneration(ctx, scope, firstID, ""); err != nil {
		t.Fatal(err)
	}
	if jobs, err := repo.ListCanvasGenerations(ctx, scope); err != nil || len(jobs) != 1 || jobs[0].Status != "succeeded" ||
		jobs[0].ResultMediaID != firstID || jobs[0].FinishedAt == nil {
		t.Fatalf("任务完成后未记录结果: %+v %v", jobs, err)
	}
	if job, started, err := reserve(secondID, board.Revision); err != nil || !started || job.ResultMediaID != firstID {
		t.Fatalf("重试没有保留最近成功素材: %+v %v %v", job, started, err)
	}
	if _, err := db.Exec(ctx, `UPDATE drama_canvas_generation_jobs SET expires_at=NOW()-interval '1 minute' WHERE id=$1`, secondID); err != nil {
		t.Fatal(err)
	}
	if job, err := repo.ReadCanvasGeneration(ctx, scope); err != nil || job == nil || job.Status != "failed" ||
		job.ErrorMessage == "" || job.FinishedAt == nil || job.ResultMediaID != firstID {
		t.Fatalf("过期任务未转为可重试且保留素材的失败状态: %+v %v", job, err)
	}
	if job, started, err := reserve(thirdID, board.Revision); err != nil || !started || job.ID != thirdID {
		t.Fatalf("过期任务无法重试: %+v %v %v", job, started, err)
	}
	if err := repo.FinishCanvasGeneration(ctx, scope, secondID, ""); !errors.Is(err, drama.ErrCanvasConflict) {
		t.Fatalf("过期任务可以覆盖新任务: %v", err)
	}
	if err := repo.FinishCanvasGeneration(ctx, scope, thirdID, "模型失败"); err != nil {
		t.Fatal(err)
	}
	if job, err := repo.ReadCanvasGeneration(ctx, scope); err != nil || job == nil || job.ID != thirdID ||
		job.Status != "failed" || job.ErrorMessage != "模型失败" || job.ResultMediaID != firstID {
		t.Fatalf("失败任务未保留最近成功素材: %+v %v", job, err)
	}
}
