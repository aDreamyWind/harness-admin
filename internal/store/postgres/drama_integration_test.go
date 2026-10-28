package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"harness-admin/internal/drama"
)

func TestDramaSaveAnalysisSQLAcceptsAllStages(t *testing.T) {
	url := os.Getenv("HARNESS_TEST_WRITE_DATABASE_URL")
	if url == "" {
		t.Skip("设置 HARNESS_TEST_WRITE_DATABASE_URL 后验证 PostgreSQL 解析保存语句")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repository := NewDramaRepository(pool)
	missingID := "00000000-0000-0000-0000-000000000000"
	for _, item := range []struct {
		name  string
		phase string
		data  json.RawMessage
	}{
		{name: "开始故事解析", phase: "story"},
		{name: "保存故事概览", phase: "story", data: json.RawMessage(`{"synopsis":"测试"}`)},
		{name: "保存资产", phase: "assets", data: json.RawMessage(`{"characters":[],"scenes":[],"props":[]}`)},
		{name: "保存分集", phase: "episodes", data: json.RawMessage(`{"episodes":[]}`)},
		{name: "完成解析", phase: "complete"},
	} {
		t.Run(item.name, func(t *testing.T) {
			err := repository.SaveAnalysis(ctx, missingID, missingID, missingID, "running", item.phase, item.data)
			if !errors.Is(err, drama.ErrNotFound) {
				t.Fatalf("预期语句成功执行且未找到项目，实际错误: %v", err)
			}
		})
	}
}

func TestDramaOverviewAndRetryQueriesAcceptParameters(t *testing.T) {
	url := os.Getenv("HARNESS_TEST_WRITE_DATABASE_URL")
	if url == "" {
		t.Skip("设置 HARNESS_TEST_WRITE_DATABASE_URL 后验证 PostgreSQL 漫剧更新语句")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repository := NewDramaRepository(pool)
	missingID := "00000000-0000-0000-0000-000000000000"
	_, err = repository.UpdateOverview(ctx, missingID, missingID, missingID,
		json.RawMessage(`{"videoStyle":"国风水墨","aspectRatio":"9:16"}`))
	if !errors.Is(err, drama.ErrNotFound) {
		t.Fatalf("概览更新语句未正确执行: %v", err)
	}
	_, err = repository.QueueOverviewReparse(ctx, missingID, missingID, missingID, "writer")
	if !errors.Is(err, drama.ErrNotFound) {
		t.Fatalf("概览重新解析更新语句未正确执行: %v", err)
	}
	_, err = repository.RetryFailed(ctx, missingID, missingID, missingID, "writer")
	if !errors.Is(err, drama.ErrNotFound) {
		t.Fatalf("重试更新语句未正确执行: %v", err)
	}
}

func TestDramaOverviewColumnsReplaceWithoutMerge(t *testing.T) {
	url := os.Getenv("HARNESS_TEST_WRITE_DATABASE_URL")
	if url == "" {
		t.Skip("设置 HARNESS_TEST_WRITE_DATABASE_URL 后验证概览字段覆盖")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	var organizationID, ownerID string
	if err := tx.QueryRow(ctx, `SELECT o.id, u.id FROM organizations o CROSS JOIN users u LIMIT 1`).Scan(&organizationID, &ownerID); err != nil {
		t.Skipf("测试数据库没有企业或用户: %v", err)
	}
	id := uuid.NewString()
	_, err = tx.Exec(ctx, `INSERT INTO drama_projects
		(id, organization_id, owner_id, title, source_text, source_length, source_mode, style, aspect_ratio, model_code, status, phase,
		 protagonist, genre, synopsis, background, setting)
		VALUES ($1,$2,$3,'覆盖测试','原文',2,'upload','国风','9:16','writer','ready','overview_done',
		'旧主角','旧类型','旧梗概','旧背景','旧设定')`, id, organizationID, ownerID)
	if err != nil {
		t.Fatal(err)
	}
	newResult := sql.NullString{String: `{"protagonist":"新主角","genre":"新类型","synopsis":"新梗概","background":"新背景"}`, Valid: true}
	if _, err := tx.Exec(ctx, saveDramaAnalysisSQL, organizationID, ownerID, id, "running", "story", newResult); err != nil {
		t.Fatal(err)
	}
	item, err := scanDrama(tx.QueryRow(ctx, `SELECT `+dramaColumns+` FROM drama_projects WHERE id=$1`, id))
	if err != nil {
		t.Fatal(err)
	}
	var overview map[string]string
	if err := json.Unmarshal(item.Overview, &overview); err != nil {
		t.Fatal(err)
	}
	if overview["protagonist"] != "新主角" || overview["synopsis"] != "新梗概" || overview["setting"] != "" {
		t.Fatalf("新概览应完整覆盖旧字段: %+v", overview)
	}
}
