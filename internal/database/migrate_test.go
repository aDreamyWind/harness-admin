package database

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/png"
	"net/url"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"harness-admin/internal/identity"
	"harness-admin/internal/novel"
	"harness-admin/internal/platformadmin"
	"harness-admin/internal/store/postgres"
)

const expectedMigrationVersion = 62

// 需要具备 CREATE DATABASE 权限的专用测试实例；绝不能指向生产数据库。
func TestMigrateBaselineMatchesLegacy(t *testing.T) {
	adminURL := os.Getenv("HARNESS_TEST_MIGRATION_ADMIN_URL")
	if adminURL == "" {
		t.Skip("设置 HARNESS_TEST_MIGRATION_ADMIN_URL 后运行数据库基线对比测试")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Fatalf("连接测试数据库实例失败: %v", err)
	}
	defer admin.Close(context.Background())

	var suffix [6]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("生成测试数据库名称失败: %v", err)
	}
	baseName := "harness_migrate_" + hex.EncodeToString(suffix[:])
	freshName, legacyName := baseName+"_fresh", baseName+"_legacy"
	for _, name := range []string{freshName, legacyName} {
		if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
			t.Fatalf("创建测试数据库 %s 失败: %v", name, err)
		}
		defer func(name string) {
			if _, err := admin.Exec(context.Background(), "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
				t.Errorf("清理测试数据库 %s 失败: %v", name, err)
			}
		}(name)
	}

	freshURL := migrationTestDatabaseURL(t, adminURL, freshName)
	legacyURL := migrationTestDatabaseURL(t, adminURL, legacyName)
	if err := Migrate(ctx, freshURL); err != nil {
		t.Fatalf("全新数据库执行基线失败: %v", err)
	}
	if err := Migrate(ctx, freshURL); err != nil {
		t.Fatalf("全新数据库重复迁移失败: %v", err)
	}
	freshPool, err := pgxpool.New(ctx, freshURL)
	if err != nil {
		t.Fatalf("连接基线数据库失败: %v", err)
	}
	defer freshPool.Close()
	var appliedCount int
	if err := freshPool.QueryRow(ctx, `SELECT COUNT(*) FROM goose_db_version WHERE is_applied AND version_id > 0`).Scan(&appliedCount); err != nil {
		t.Fatalf("读取基线执行次数失败: %v", err)
	}
	if expected := expectedMigrationVersion - baselineVersion + 1; appliedCount != expected {
		t.Fatalf("全新数据库执行了 %d 个迁移，预期 %d 个", appliedCount, expected)
	}

	legacyConfig, err := pgx.ParseConfig(legacyURL)
	if err != nil {
		t.Fatalf("解析历史数据库地址失败: %v", err)
	}
	legacyDB := stdlib.OpenDB(*legacyConfig)
	goose.SetBaseFS(migrationFiles)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("设置历史迁移方言失败: %v", err)
	}
	if err := goose.UpToContext(ctx, legacyDB, "migrations/legacy", 19); err != nil {
		legacyDB.Close()
		t.Fatalf("准备 19 号历史数据库失败: %v", err)
	}
	if err := legacyDB.Close(); err != nil {
		t.Fatalf("关闭历史数据库连接失败: %v", err)
	}
	if err := Migrate(ctx, legacyURL); err != nil {
		t.Fatalf("历史数据库升级到基线版本失败: %v", err)
	}

	fresh := migrationTestSnapshot(t, ctx, freshURL)
	legacy := migrationTestSnapshot(t, ctx, legacyURL)
	if !reflect.DeepEqual(fresh, legacy) {
		for i := 0; i < len(fresh) && i < len(legacy); i++ {
			if fresh[i] != legacy[i] {
				t.Fatalf("全新基线与历史迁移结果第 %d 项不同（基线 %d 项，历史 %d 项）\n基线: %s\n历史: %s", i, len(fresh), len(legacy), fresh[i], legacy[i])
			}
		}
		t.Fatalf("全新基线与历史迁移结果数量不同：基线 %d 项，历史 %d 项", len(fresh), len(legacy))
	}

	bootstrap, err := postgres.NewIdentityRepository(freshPool).BootstrapAdmin(ctx, identity.BootstrapAdminInput{
		OrganizationCode: "migration-bootstrap", OrganizationName: "迁移测试企业",
		Username: "admin", DisplayName: "管理员", PasswordHash: "test-hash",
	})
	if err != nil {
		t.Fatalf("基线数据库初始化企业管理员失败: %v", err)
	}
	migrationTestAdminPermissions(t, ctx, freshPool, bootstrap.OrganizationID)
	t.Run("小说作品保存与隔离", func(t *testing.T) {
		repo := postgres.NewNovelRepository(freshPool)
		service := novel.NewService(repo, nil, nil, nil)
		owner := novel.Owner{OrganizationID: bootstrap.OrganizationID, UserID: bootstrap.UserID}
		project, err := service.Create(ctx, owner, novel.CreateInput{Title: "潮汐档案", Brief: "守塔人听到呼救", ChapterCount: 1})
		if err != nil {
			t.Fatal(err)
		}
		updated, err := service.Apply(ctx, owner, project.ID, novel.Action{
			Kind: "confirm_outline", Version: project.Version,
			Outline: &novel.Outline{Synopsis: "追查声源", World: "孤岛", Characters: "守塔人",
				Beats: []novel.Beat{{Title: "雾笛", Goal: "发现异常信号"}}},
		})
		if err != nil || updated.Version != 2 {
			t.Fatalf("保存作品版本失败: %+v %v", updated, err)
		}
		stored, err := service.Get(ctx, owner, project.ID)
		if err != nil || !stored.Confirmed || stored.Outline == nil || stored.Outline.Beats[0].Title != "雾笛" {
			t.Fatalf("读取持久化大纲失败: %+v %v", stored, err)
		}
		stored.Title = "逆潮灯语"
		if renamed, err := repo.Save(ctx, owner, stored); err != nil || renamed.Version != 3 {
			t.Fatalf("更新作品名失败: %+v %v", renamed, err)
		}
		if _, err := repo.Save(ctx, owner, project); !errors.Is(err, novel.ErrConflict) {
			t.Fatalf("过期版本未被拒绝: %v", err)
		}
		other := novel.Owner{OrganizationID: owner.OrganizationID, UserID: "00000000-0000-0000-0000-000000000000"}
		if _, err := service.Get(ctx, other, project.ID); !errors.Is(err, novel.ErrNotFound) {
			t.Fatalf("他人的作品未隔离: %v", err)
		}
		if books, err := service.List(ctx, owner); err != nil || len(books) != 1 || books[0].ID != project.ID || books[0].Title != stored.Title {
			t.Fatalf("读取作品列表失败: %+v %v", books, err)
		}
		beats := make([]novel.Beat, 30)
		for i := range beats {
			beats[i] = novel.Beat{Title: fmt.Sprintf("续写 %d", i+1), Goal: "继续调查"}
		}
		updated, err = service.Apply(ctx, owner, project.ID, novel.Action{Kind: "append_chapters", Version: 3, Beats: beats})
		if err != nil || updated.ChapterCount != 31 || len(updated.Chapters) != 31 || updated.Outline.Beats[0].Title != "雾笛" {
			t.Fatalf("追加到 30 章以上失败: 章节数 %d, 工作区章节数 %d, 错误 %v", updated.ChapterCount, len(updated.Chapters), err)
		}
		if books, err := service.List(ctx, owner); err != nil || len(books) != 1 || books[0].ChapterCount != 31 {
			t.Fatalf("章节总数未持久化: %+v %v", books, err)
		}
		var coverImage bytes.Buffer
		if err := png.Encode(&coverImage, image.NewNRGBA(image.Rect(0, 0, 6, 9))); err != nil {
			t.Fatal(err)
		}
		firstCover, err := service.UploadCover(ctx, owner, project.ID, coverImage.Bytes())
		if err != nil || firstCover.Revision == "" || firstCover.Source != "upload" {
			t.Fatalf("初次上传封面失败: %+v %v", firstCover, err)
		}
		secondCover, err := service.UploadCover(ctx, owner, project.ID, coverImage.Bytes())
		if err != nil || secondCover.Revision == firstCover.Revision {
			t.Fatalf("替换封面未生成新地址: %+v %v", secondCover, err)
		}
		imageData, err := service.CoverContent(ctx, owner, project.ID, secondCover.Revision)
		if err != nil || imageData.MIME != "image/png" || !bytes.Equal(imageData.Data, coverImage.Bytes()) {
			t.Fatalf("读取封面失败: %q %v", imageData.MIME, err)
		}
		if _, err := service.CoverContent(ctx, other, project.ID, secondCover.Revision); !errors.Is(err, novel.ErrNotFound) {
			t.Fatalf("他人的封面未隔离: %v", err)
		}
		if _, err := service.CoverContent(ctx, owner, project.ID, firstCover.Revision); !errors.Is(err, novel.ErrNotFound) {
			t.Fatalf("已替换的封面版本仍可读取: %v", err)
		}
		if _, err := repo.SaveCover(ctx, owner, project.ID, firstCover.Revision, firstCover, imageData); !errors.Is(err, novel.ErrConflict) {
			t.Fatalf("过期封面版本未被拒绝: %v", err)
		}
		if books, err := service.List(ctx, owner); err != nil || len(books) != 1 || books[0].Cover == nil || books[0].Cover.Revision != secondCover.Revision {
			t.Fatalf("作品列表未包含当前封面: %+v %v", books, err)
		}
		final, err := service.Get(ctx, owner, project.ID)
		if err != nil || final.Version != 4 || final.Cover == nil || final.Cover.Revision != secondCover.Revision {
			t.Fatalf("保存封面改变了作品版本或详情未返回封面: %+v %v", final, err)
		}
	})
	created, err := postgres.NewPlatformAdminRepository(freshPool).CreateOrganization(ctx, platformadmin.OrganizationCreation{
		Code: "migration-platform", Name: "平台创建的测试企业",
		AdminUsername: "admin", AdminDisplayName: "管理员", AdminPasswordHash: "test-hash",
	})
	if err != nil {
		t.Fatalf("基线数据库通过平台创建企业失败: %v", err)
	}
	migrationTestAdminPermissions(t, ctx, freshPool, created.ID)
}

func migrationTestAdminPermissions(t *testing.T, ctx context.Context, pool *pgxpool.Pool, organizationID string) {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(DISTINCT p.code)
		FROM roles r
		JOIN role_permissions rp ON rp.role_id = r.id
		JOIN permissions p ON p.id = rp.permission_id
		WHERE r.organization_id = $1 AND r.code = 'enterprise_admin'
		  AND p.code IN ('model.gateway.read', 'model.gateway.write',
		                 'oidc.client.read', 'oidc.client.write',
		                 'model.policy.read', 'model.policy.write',
		                 'mcp.service.read', 'mcp.service.write',
		                 'mcp.connection.read', 'mcp.connection.write',
		                 'skill.asset.read', 'skill.asset.write')
	`, organizationID).Scan(&count); err != nil {
		t.Fatalf("检查管理员权限失败: %v", err)
	}
	if count != 12 {
		t.Errorf("企业 %s 的管理员只有 %d 项后期模块权限，预期 12 项", organizationID, count)
	}
}

func migrationTestDatabaseURL(t *testing.T, adminURL, name string) string {
	t.Helper()
	parsed, err := url.Parse(adminURL)
	if err != nil {
		t.Fatalf("解析测试数据库地址失败: %v", err)
	}
	parsed.Path = "/" + name
	return parsed.String()
}

func migrationTestSnapshot(t *testing.T, ctx context.Context, databaseURL string) []string {
	t.Helper()
	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatalf("读取迁移结果失败: %v", err)
	}
	defer conn.Close(context.Background())

	queries := []string{
		`SELECT 'table|' || c.relname || '|' || COALESCE(obj_description(c.oid), '')
		 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		 WHERE n.nspname = 'public' AND c.relkind IN ('r', 'S') AND c.relname <> 'goose_db_version' ORDER BY c.relname`,
		`SELECT 'column|' || c.relname || '.' || a.attname || '|' || format_type(a.atttypid, a.atttypmod)
		 || '|' || a.attnotnull || '|' || a.attidentity::text || '|' || COALESCE(pg_get_expr(d.adbin, d.adrelid), '')
		 || '|' || COALESCE(col_description(c.oid, a.attnum), '')
		 FROM pg_attribute a JOIN pg_class c ON c.oid = a.attrelid
		 JOIN pg_namespace n ON n.oid = c.relnamespace
		 LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
		 WHERE n.nspname = 'public' AND c.relkind = 'r' AND c.relname <> 'goose_db_version'
		 AND a.attnum > 0 AND NOT a.attisdropped ORDER BY c.relname, a.attnum`,
		`SELECT 'constraint|' || c.relname || '.' || con.conname || '|' || con.contype::text
		 || '|' || CASE WHEN con.contype = 'c' THEN '' ELSE pg_get_constraintdef(con.oid) END
		 FROM pg_constraint con JOIN pg_class c ON c.oid = con.conrelid
		 JOIN pg_namespace n ON n.oid = c.relnamespace
		 WHERE n.nspname = 'public' AND c.relname <> 'goose_db_version' ORDER BY c.relname, con.conname`,
		`SELECT 'index|' || indexname || '|' || indexdef FROM pg_indexes
		 WHERE schemaname = 'public' AND tablename <> 'goose_db_version' ORDER BY indexname`,
		`SELECT 'permission|' || id || '|' || code || '|' || name || '|' || resource || '|' || action || '|' || description
		 FROM permissions ORDER BY code`,
	}
	var snapshot []string
	for _, query := range queries {
		rows, err := conn.Query(ctx, query)
		if err != nil {
			t.Fatalf("查询数据库结构失败: %v", err)
		}
		for rows.Next() {
			var value string
			if err := rows.Scan(&value); err != nil {
				rows.Close()
				t.Fatalf("读取数据库结构失败: %v", err)
			}
			snapshot = append(snapshot, value)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			t.Fatalf("遍历数据库结构失败: %v", err)
		}
		rows.Close()
	}
	var version int64
	if err := conn.QueryRow(ctx, "SELECT max(version_id) FROM goose_db_version WHERE is_applied").Scan(&version); err != nil {
		t.Fatalf("读取迁移版本失败: %v", err)
	}
	if version != expectedMigrationVersion {
		t.Fatalf("迁移版本为 %d，预期 %d", version, expectedMigrationVersion)
	}
	return append(snapshot, fmt.Sprintf("version|%d", version))
}
