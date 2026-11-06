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

func TestDramaRenameProjectPersistsTitle(t *testing.T) {
	url := os.Getenv("HARNESS_TEST_WRITE_DATABASE_URL")
	if url == "" {
		t.Skip("设置 HARNESS_TEST_WRITE_DATABASE_URL 后验证漫剧标题的 PostgreSQL 保存")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	config.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, `CREATE TEMP TABLE drama_projects (
		id uuid PRIMARY KEY, organization_id uuid NOT NULL, owner_id uuid NOT NULL,
		title varchar(120) NOT NULL, updated_at timestamptz NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}

	organizationID, ownerID, projectID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	originalUpdatedAt := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	if _, err := pool.Exec(ctx, `INSERT INTO drama_projects (id, organization_id, owner_id, title, updated_at)
		VALUES ($1, $2, $3, $4, $5)`, projectID, organizationID, ownerID, "原漫剧标题", originalUpdatedAt); err != nil {
		t.Fatal(err)
	}

	repo := NewDramaRepository(pool)
	newTitle := "芦苇渡"
	saved, err := repo.RenameProject(ctx, organizationID, ownerID, projectID, newTitle)
	if err != nil || saved.Title != newTitle || !saved.UpdatedAt.After(originalUpdatedAt) {
		t.Fatalf("保存中文漫剧标题失败：%+v %v", saved, err)
	}
	repeated, err := repo.RenameProject(ctx, organizationID, ownerID, projectID, newTitle)
	if err != nil || repeated.Title != newTitle || !repeated.UpdatedAt.Equal(saved.UpdatedAt) {
		t.Fatalf("重复提交相同标题应保留更新时间：%+v %v", repeated, err)
	}
	if _, err := repo.RenameProject(ctx, organizationID, uuid.NewString(), projectID, "其他用户的标题"); !errors.Is(err, drama.ErrNotFound) {
		t.Fatalf("其他用户不能修改项目标题：%v", err)
	}

	var stored drama.ProjectTitle
	if err := pool.QueryRow(ctx, `SELECT title, updated_at FROM drama_projects WHERE id=$1`, projectID).Scan(&stored.Title, &stored.UpdatedAt); err != nil {
		t.Fatal(err)
	}
	if stored.Title != newTitle || !stored.UpdatedAt.Equal(saved.UpdatedAt) {
		t.Fatalf("数据库中保存的标题或更新时间不符：%+v", stored)
	}
}

func TestDramaProjectVisualSettingsPreserveOtherFields(t *testing.T) {
	url := os.Getenv("HARNESS_TEST_WRITE_DATABASE_URL")
	if url == "" {
		t.Skip("设置 HARNESS_TEST_WRITE_DATABASE_URL 后验证漫剧画面设置的部分更新")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	config.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, `CREATE TEMP TABLE drama_projects (
		id uuid PRIMARY KEY, organization_id uuid NOT NULL, owner_id uuid NOT NULL,
		style_id varchar(80), style varchar(80) NOT NULL, aspect_ratio varchar(5) NOT NULL,
		protagonist text NOT NULL, genre text NOT NULL, synopsis text NOT NULL, background text NOT NULL, setting text NOT NULL,
		updated_at timestamptz NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	org, owner, id := uuid.NewString(), uuid.NewString(), uuid.NewString()
	if _, err := pool.Exec(ctx, `INSERT INTO drama_projects VALUES
		($1,$2,$3,NULL,'历史风格','9:16','青川','奇幻','追查故乡','沿海古城','宵禁',NOW() - INTERVAL '1 day')`, id, org, owner); err != nil {
		t.Fatal(err)
	}
	repo := NewDramaRepository(pool)
	ratio := drama.ProjectVisualSettings{AspectRatio: "16:9"}
	if _, err := repo.UpdateProjectVisualSettings(ctx, org, uuid.NewString(), id, ratio); !errors.Is(err, drama.ErrNotFound) {
		t.Fatalf("其他用户不能修改画面设置：%v", err)
	}
	saved, err := repo.UpdateProjectVisualSettings(ctx, org, owner, id, ratio)
	if err != nil || saved.AspectRatio != "16:9" || saved.UpdatedAt.IsZero() {
		t.Fatalf("保存画面比例失败：%+v %v", saved, err)
	}
	var oldStyle string
	var oldStyleID sql.NullString
	if err := pool.QueryRow(ctx, `SELECT style,style_id FROM drama_projects WHERE id=$1`, id).Scan(&oldStyle, &oldStyleID); err != nil || oldStyle != "历史风格" || oldStyleID.Valid {
		t.Fatalf("仅修改比例应保留历史风格：%q %+v %v", oldStyle, oldStyleID, err)
	}
	style := drama.ProjectVisualSettings{StyleID: "dark-concept", Style: "黑暗原画概念风"}
	saved, err = repo.UpdateProjectVisualSettings(ctx, org, owner, id, style)
	if err != nil || saved.StyleID != style.StyleID || saved.Style != style.Style || saved.AspectRatio != "" {
		t.Fatalf("保存风格失败：%+v %v", saved, err)
	}
	repeated, err := repo.UpdateProjectVisualSettings(ctx, org, owner, id, style)
	if err != nil || !repeated.UpdatedAt.Equal(saved.UpdatedAt) {
		t.Fatalf("重复提交同一风格不应刷新项目时间：%+v %v", repeated, err)
	}
	var stored [8]string
	err = pool.QueryRow(ctx, `SELECT style_id,style,aspect_ratio,protagonist,genre,synopsis,background,setting
		FROM drama_projects WHERE id=$1`, id).Scan(&stored[0], &stored[1], &stored[2], &stored[3], &stored[4], &stored[5], &stored[6], &stored[7])
	if want := [8]string{"dark-concept", "黑暗原画概念风", "16:9", "青川", "奇幻", "追查故乡", "沿海古城", "宵禁"}; err != nil || stored != want {
		t.Fatalf("风格、比例和故事字段保存异常：%v %v", stored, err)
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
