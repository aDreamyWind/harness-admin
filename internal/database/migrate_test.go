package database

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
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
	"harness-admin/internal/platformadmin"
	"harness-admin/internal/store/postgres"
)

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
	if appliedCount != 33 {
		t.Fatalf("全新数据库执行了 %d 个迁移，预期执行基线及 21～52 号增量迁移", appliedCount)
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
	if version != 52 {
		t.Fatalf("迁移版本为 %d，预期 52", version)
	}
	return append(snapshot, fmt.Sprintf("version|%d", version))
}
