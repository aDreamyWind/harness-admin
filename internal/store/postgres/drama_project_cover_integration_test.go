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

func TestDramaProjectCoverLifecycle(t *testing.T) {
	databaseURL := os.Getenv("HARNESS_TEST_WRITE_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("设置测试数据库后验证漫剧封面发布和选择")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	scope := drama.CoverScope{OrganizationID: uuid.NewString(), UserID: uuid.NewString(), ProjectID: uuid.NewString()}
	exec := func(query string, values ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, query, values...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO organizations (id,code,name) VALUES ($1,$2,'封面测试企业')`, scope.OrganizationID, "cover-"+scope.OrganizationID)
	defer func() {
		if _, err := db.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, scope.UserID); err != nil {
			t.Errorf("清理封面测试用户失败: %v", err)
		}
		if _, err := db.Exec(context.Background(), `DELETE FROM organizations WHERE id=$1`, scope.OrganizationID); err != nil {
			t.Errorf("清理封面测试数据失败: %v", err)
		}
	}()
	exec(`INSERT INTO users (id,organization_id,username,display_name,password_hash) VALUES ($1,$2,$3,'封面测试用户','test')`,
		scope.UserID, scope.OrganizationID, "cover-"+scope.UserID)
	repo := NewDramaRepository(db)
	project, err := repo.Create(ctx, drama.Project{
		ID: scope.ProjectID, OrganizationID: scope.OrganizationID, OwnerID: scope.UserID, Title: "雪城",
		SourceText: "雪夜的城门", SourceLength: 5, SourceMode: "upload", Style: "暗黑漫画风",
		AspectRatio: "16:9", ModelCode: "test", Status: "ready", Phase: "overview_done",
	})
	if err != nil || project.CoverURL != "" {
		t.Fatalf("无法创建无封面的项目: %+v %v", project, err)
	}
	if state, err := repo.GetProjectCoverGeneration(ctx, scope); err != nil || state.Status != drama.CoverIdle {
		t.Fatalf("未生成时未返回 idle: %+v %v", state, err)
	}
	other := scope
	other.UserID = uuid.NewString()
	if _, err := repo.GetProjectCoverGeneration(ctx, other); !errors.Is(err, drama.ErrNotFound) {
		t.Fatalf("未拒绝其他用户访问状态: %v", err)
	}
	reserve := func(previous, id string, start bool) drama.CoverGeneration {
		t.Helper()
		state, started, err := repo.ReserveProjectCoverGeneration(ctx, scope, previous, id, "painter", time.Now().Add(time.Minute))
		if err != nil || started != start {
			t.Fatalf("预留任务结果错误: %+v %v %v", state, started, err)
		}
		return state
	}
	finish := func(id, artwork string) drama.CoverGeneration {
		t.Helper()
		if err := repo.FinishProjectCoverGeneration(ctx, scope, id, drama.CoverImage{Data: []byte(artwork), MIME: "image/svg+xml"}, ""); err != nil {
			t.Fatal(err)
		}
		state, err := repo.GetProjectCoverGeneration(ctx, scope)
		if err != nil || state.Status != drama.CoverSucceeded {
			t.Fatalf("保存生成结果失败: %+v %v", state, err)
		}
		return state
	}
	first, second, third, fourth, fifth := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	if state := reserve("", first, true); state.Status != drama.CoverRunning || state.JobID != first {
		t.Fatalf("未建立任务: %+v", state)
	}
	if state := reserve("", second, false); state.JobID != first {
		t.Fatalf("并发启动没有复用同一任务: %+v", state)
	}
	if project, err := repo.Get(ctx, scope.OrganizationID, scope.UserID, scope.ProjectID); err != nil || project.CoverGenerationStatus != drama.CoverRunning {
		t.Fatalf("项目未返回生成状态: %+v %v", project, err)
	}
	if state := finish(first, "<svg>首张封面</svg>"); state.CoverID != first || state.CandidateID != "" {
		t.Fatalf("首张封面未自动采用: %+v", state)
	}
	if state := reserve("", second, false); state.JobID != first || state.CoverID != first {
		t.Fatalf("状态过期的请求意外生成了第二张: %+v", state)
	}
	projects, err := repo.List(ctx, scope.OrganizationID, scope.UserID)
	if err != nil || len(projects) != 1 || projects[0].CoverURL != drama.ProjectCoverImageURL(scope.ProjectID, first) || projects[0].CoverGenerationStatus != "" {
		t.Fatalf("项目列表缺少当前封面地址或状态错误: %+v %v", projects, err)
	}
	reserve(first, second, true)
	if state := finish(second, "<svg>候选封面</svg>"); state.CoverID != first || state.CandidateID != second {
		t.Fatalf("生成候选时覆盖了原封面: %+v", state)
	}
	if state := reserve(second, third, false); state.CoverID != first || state.CandidateID != second || state.JobID != second {
		t.Fatalf("重复启动覆盖了尚未选择的候选: %+v", state)
	}
	if _, err := repo.ReadProjectCoverImage(ctx, other, second); !errors.Is(err, drama.ErrNotFound) {
		t.Fatalf("未拒绝其他用户访问候选图片: %v", err)
	}
	if image, err := repo.ReadProjectCoverImage(ctx, scope, second); err != nil || string(image.Data) != "<svg>候选封面</svg>" {
		t.Fatalf("候选图片不可预览: %+v %v", image, err)
	}
	if state, err := repo.SelectProjectCover(ctx, scope, drama.CoverSelection{JobID: second, Choice: "current"}); err != nil || state.CoverID != first || state.CandidateID != "" {
		t.Fatalf("保留当前封面失败: %+v %v", state, err)
	}
	if _, err := repo.SelectProjectCover(ctx, scope, drama.CoverSelection{JobID: second, Choice: "candidate"}); !errors.Is(err, drama.ErrCoverConflict) {
		t.Fatalf("被丢弃的候选还可以采用: %v", err)
	}
	reserve(second, third, true)
	finish(third, "<svg>采用封面</svg>")
	for attempt := 0; attempt < 2; attempt++ {
		if state, err := repo.SelectProjectCover(ctx, scope, drama.CoverSelection{JobID: third, Choice: "candidate"}); err != nil || state.CoverID != third || state.CandidateID != "" {
			t.Fatalf("采用候选失败: %+v %v", state, err)
		}
	}
	if image, err := repo.ReadProjectCoverImage(ctx, scope, third); err != nil || string(image.Data) != "<svg>采用封面</svg>" {
		t.Fatalf("采用的封面不可读取: %+v %v", image, err)
	}
	reserve(third, fourth, true)
	exec(`UPDATE drama_project_covers SET expires_at=NOW()-interval '1 minute' WHERE project_id=$1`, scope.ProjectID)
	if state, err := repo.GetProjectCoverGeneration(ctx, scope); err != nil || state.Status != drama.CoverFailed || state.Error == "" || state.CoverID != third {
		t.Fatalf("中断任务未变为可重试或已发布封面被修改: %+v %v", state, err)
	}
	reserve(fourth, fifth, true)
	if err := repo.FinishProjectCoverGeneration(ctx, scope, fourth, drama.CoverImage{Data: []byte("过期封面")}, ""); !errors.Is(err, drama.ErrCoverConflict) {
		t.Fatalf("过期任务能够覆盖新结果: %v", err)
	}
	if err := repo.FinishProjectCoverGeneration(ctx, scope, fifth, drama.CoverImage{}, "模型调用失败"); err != nil {
		t.Fatal(err)
	}
	if state, err := repo.GetProjectCoverGeneration(ctx, scope); err != nil || state.Status != drama.CoverFailed || state.Error != "模型调用失败" || state.CoverID != third {
		t.Fatalf("未保留原图或记录失败: %+v %v", state, err)
	}
}
