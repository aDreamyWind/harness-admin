package postgres_test

import (
	"context"
	"errors"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"harness-admin/internal/database"
	"harness-admin/internal/novel"
	"harness-admin/internal/store/postgres"
)

func TestNovelStoryMemoryDurability(t *testing.T) {
	adminURL := os.Getenv("HARNESS_TEST_MIGRATION_ADMIN_URL")
	if adminURL == "" {
		t.Skip("设置专用 PostgreSQL 测试实例后验证故事档案迁移、租约与写回")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(context.Background())
	name := "harness_novel_memory_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	identifier := pgx.Identifier{name}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+identifier); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.Exec(context.Background(), "DROP DATABASE "+identifier+" WITH (FORCE)"); err != nil {
			t.Errorf("清理故事档案测试库失败: %v", err)
		}
	}()
	parsed, err := url.Parse(adminURL)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Path = "/" + name
	dsn := parsed.String()
	if err := database.Migrate(ctx, dsn); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	owner := novel.Owner{OrganizationID: uuid.NewString(), UserID: uuid.NewString()}
	other := novel.Owner{OrganizationID: owner.OrganizationID, UserID: uuid.NewString()}
	exec(`INSERT INTO organizations (id, code, name) VALUES ($1, 'test-memory', '故事档案测试企业')`, owner.OrganizationID)
	exec(`INSERT INTO users (id, organization_id, username, display_name, password_hash)
		VALUES ($1,$3,'novel-owner','测试作者','不可用密码'), ($2,$3,'other-owner','其他作者','不可用密码')`, owner.UserID, other.UserID, owner.OrganizationID)
	repo := postgres.NewNovelRepository(pool)
	create := func(title string) novel.Project {
		t.Helper()
		project, err := repo.Create(ctx, owner, novel.Project{
			Book: novel.Book{ID: uuid.NewString(), Title: title, ChapterCount: 2}, Brief: "测试故事", Version: 1,
			Workspace: novel.Workspace{Chapters: []novel.Chapter{{Body: "黎青佩戴铜牌。", Final: true}, {Body: "河渡水位上涨。"}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		return project
	}
	project, unrelated := create("旧作品"), create("无关作品")
	save := func(p novel.Project) novel.Project {
		t.Helper()
		result, err := repo.Save(ctx, owner, p)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	status := func(id string, expected ...string) []novel.MemoryChapter {
		t.Helper()
		rows, err := repo.ListMemory(ctx, owner, id)
		var actual []string
		for _, row := range rows {
			actual = append(actual, row.Status)
		}
		if err != nil || !slices.Equal(actual, expected) {
			t.Fatalf("故事档案状态 %v，预期 %v: %v", actual, expected, err)
		}
		return rows
	}
	claim := func(number int) novel.MemoryJob {
		t.Helper()
		job, ok, err := repo.ClaimMemory(ctx)
		if err != nil || !ok || job.Chapter != number || job.Owner != owner || job.ProjectID != project.ID || job.LeaseID == "" {
			t.Fatalf("无法领取第 %d 章的当前任务: %v", number, err)
		}
		return job
	}
	project.Title = "普通改名"
	project = save(project)
	status(project.ID, "missing")
	if _, claimed, err := repo.ClaimMemory(ctx); err != nil || claimed {
		t.Fatalf("普通保存触发历史定稿回填: %v", err)
	}
	project.Chapters[1].Final = true
	project.ModelCode = "test-writer"
	project = save(project)
	status(project.ID, "queued", "queued")
	status(unrelated.ID, "missing")
	first := claim(1)
	if first.Body != "黎青佩戴铜牌。" || first.ModelCode != "test-writer" {
		t.Fatal("领取未读取定稿正文和登记模型")
	}
	second := claim(2)
	if err := repo.FinishMemory(ctx, second, nil, true); err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE novel_memory_jobs SET lease_expires_at=NOW()-INTERVAL '1 second' WHERE id=$1`, first.ID)
	reclaimed := claim(1)
	if reclaimed.ID != first.ID || reclaimed.LeaseID == first.LeaseID {
		t.Fatal("过期任务未使用新的领取凭证")
	}
	if err := repo.FinishMemory(ctx, first, nil, false); !errors.Is(err, novel.ErrConflict) {
		t.Fatalf("旧领取者可写回: %v", err)
	}
	facts := []novel.MemoryFact{{Kind: "clue", Name: "铜牌", Field: "holder", Value: "黎青", Related: "黎青", Quote: first.Body, Chapter: 1}}
	if err := repo.FinishMemory(ctx, reclaimed, facts, false); err != nil {
		t.Fatal(err)
	}
	status(project.ID, "succeeded", "failed")
	if _, claimed, err := repo.ClaimMemory(ctx); err != nil || claimed {
		t.Fatalf("失败任务未等手工重试就被领取: %v", err)
	}
	if err := repo.EnqueueMemory(ctx, owner, project.ID); err != nil {
		t.Fatal(err)
	}
	if rows := status(project.ID, "succeeded", "queued"); !slices.Equal(rows[0].Facts, facts) {
		t.Fatal("手工补齐覆盖了成功提取的事实")
	}
	retried := claim(2)
	if retried.ID == second.ID {
		t.Fatal("重试失败任务未更换任务标识")
	}
	project.Chapters[1].Final = false
	project = save(project)
	status(project.ID, "succeeded")
	if err := repo.FinishMemory(ctx, retried, nil, false); !errors.Is(err, novel.ErrConflict) {
		t.Fatalf("撤回定稿后仍能写回: %v", err)
	}
	project.Chapters[0].Body = "黎青佩戴玉牌。"
	project = save(project)
	if rows := status(project.ID, "queued"); len(rows[0].Facts) != 0 {
		t.Fatal("更改定稿后仍能读到旧证据")
	}
	if job := claim(1); job.ID == reclaimed.ID || job.Body != project.Chapters[0].Body {
		t.Fatal("更改定稿后未按新正文重新登记任务")
	}
	if err := repo.EnqueueMemory(ctx, other, project.ID); !errors.Is(err, novel.ErrNotFound) {
		t.Fatalf("其他作者可以补齐作品: %v", err)
	}
	if _, err := repo.ListMemory(ctx, other, project.ID); !errors.Is(err, novel.ErrNotFound) {
		t.Fatalf("其他作者可以读取作品证据: %v", err)
	}
	if _, err := repo.MemoryDecisions(ctx, other, project.ID); !errors.Is(err, novel.ErrNotFound) {
		t.Fatalf("其他作者可以读取作品取舍: %v", err)
	}
	conflict := strings.Repeat("a", 64)
	if err := repo.SaveMemoryDecision(ctx, other, project.ID, conflict, 0); !errors.Is(err, novel.ErrNotFound) {
		t.Fatalf("其他作者可以处置作品证据: %v", err)
	}
	results := make(chan error, 2)
	for choice := range 2 {
		go func() { results <- repo.SaveMemoryDecision(ctx, owner, project.ID, conflict, choice) }()
	}
	a, b := <-results, <-results
	if !((a == nil && errors.Is(b, novel.ErrConflict)) || (b == nil && errors.Is(a, novel.ErrConflict))) {
		t.Fatalf("并发取舍没有保护首次成功的选项: %v / %v", a, b)
	}
	decisions, err := repo.MemoryDecisions(ctx, owner, project.ID)
	choice, found := decisions[conflict]
	if err != nil || !found || len(decisions) != 1 {
		t.Fatalf("未保存唯一的有效决定: %v", err)
	}
	if err := repo.SaveMemoryDecision(ctx, owner, project.ID, conflict, choice); err != nil {
		t.Fatalf("重复提交相同决定失败: %v", err)
	}
}
