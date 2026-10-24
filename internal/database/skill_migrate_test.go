package database

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"harness-admin/internal/identity"
	"harness-admin/internal/skilladmin"
	"harness-admin/internal/store/postgres"
)

// 在专用测试实例中验证持久化、发布及撤权；测试自行创建和销毁隔离数据库。
func TestEnterpriseSkillRepositoryLifecycle(t *testing.T) {
	adminURL := os.Getenv("HARNESS_TEST_MIGRATION_ADMIN_URL")
	if adminURL == "" {
		t.Skip("设置 HARNESS_TEST_MIGRATION_ADMIN_URL 后运行企业 Skill 数据库测试")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(context.Background())
	var suffix [6]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatal(err)
	}
	dbName := "harness_skills_" + hex.EncodeToString(suffix[:])
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{dbName}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.Exec(context.Background(), "DROP DATABASE "+pgx.Identifier{dbName}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Errorf("清理 Skill 测试数据库失败: %v", err)
		}
	}()
	dbURL := migrationTestDatabaseURL(t, adminURL, dbName)
	if err := Migrate(ctx, dbURL); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	bootstrap, err := postgres.NewIdentityRepository(pool).BootstrapAdmin(ctx, identity.BootstrapAdminInput{
		OrganizationCode: "skill-test", OrganizationName: "Skill 测试企业", Username: "admin",
		DisplayName: "管理员", PasswordHash: "test-hash",
	})
	if err != nil {
		t.Fatal(err)
	}
	actor := skilladmin.Actor{OrganizationID: bootstrap.OrganizationID, UserID: bootstrap.UserID}
	service := skilladmin.NewService(postgres.NewSkillRepository(pool), nil)
	content := []byte("---\nname: lunch-helper\ndescription: 推荐午餐\n---\n请推荐午餐。\n")
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(content))
	pack := skilladmin.Package{Name: "lunch-helper", Description: "推荐午餐",
		SourceType:     "upload",
		Frontmatter:    json.RawMessage(`{"name":"lunch-helper","description":"推荐午餐"}`),
		ManifestDigest: digest, Files: []skilladmin.File{{Path: "SKILL.md", Content: content,
			MIMEType: "text/markdown", Digest: digest, Size: len(content)}},
	}
	repo := postgres.NewSkillRepository(pool)
	asset, err := repo.ImportVersion(ctx, actor, pack)
	if err != nil {
		t.Fatal(err)
	}
	versions, err := service.Versions(ctx, actor.OrganizationID, asset.ID)
	if err != nil || len(versions) != 1 {
		t.Fatalf("版本导入失败: versions=%d err=%v", len(versions), err)
	}
	githubPack := pack
	githubPack.SourceType = "github"
	githubPack.SourceRef = "https://github.com/example/skills"
	githubPack.SourceRevision = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	githubPack.SourcePath = "skills/lunch-helper"
	githubPack.ManifestDigest = "sha256:" + fmt.Sprintf("%x", sha256.Sum256([]byte("github-version")))
	if _, err := repo.ImportVersion(ctx, actor, githubPack); err != nil {
		t.Fatal(err)
	}
	versions, err = service.Versions(ctx, actor.OrganizationID, asset.ID)
	if err != nil || len(versions) != 2 || versions[0].SourceType != "github" ||
		versions[0].SourceRef != githubPack.SourceRef || versions[0].SourceRevision != githubPack.SourceRevision ||
		versions[0].SourcePath != githubPack.SourcePath {
		t.Fatalf("GitHub 来源未持久化: versions=%#v err=%v", versions, err)
	}
	if _, err := service.Publish(ctx, actor, asset.ID, versions[1].ID, "published"); err != nil {
		t.Fatal(err)
	}
	if items, err := service.ListForUser(ctx, actor.OrganizationID, actor.UserID); err != nil || len(items) != 0 {
		t.Fatalf("未授权员工不应看到 Skill: items=%d err=%v", len(items), err)
	}
	if _, err := service.ReplaceGrants(ctx, actor, asset.ID, nil, []string{actor.UserID}); err != nil {
		t.Fatal(err)
	}
	items, err := service.ListForUser(ctx, actor.OrganizationID, actor.UserID)
	if err != nil || len(items) != 1 || items[0].Asset.Name != pack.Name {
		t.Fatalf("授权后目录不正确: items=%v err=%v", items, err)
	}
	file, err := service.ReadFile(ctx, actor.OrganizationID, actor.UserID, pack.Name, "SKILL.md")
	if err != nil || string(file.Content) != string(content) {
		t.Fatalf("授权后文件读取失败: err=%v", err)
	}
	if _, err := service.ReplaceGrants(ctx, actor, asset.ID, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReadFile(ctx, actor.OrganizationID, actor.UserID, pack.Name, "SKILL.md"); !errors.Is(err, skilladmin.ErrForbidden) {
		t.Fatalf("撤权后应立即禁止读取，实际: %v", err)
	}
	if _, err := service.Publish(ctx, actor, asset.ID, versions[1].ID, "disabled"); err != nil {
		t.Fatal(err)
	}
}
