package database

import (
	"context"
	"embed"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql migrations/legacy/*.sql
var migrationFiles embed.FS

const baselineVersion = 20

func Migrate(ctx context.Context, databaseURL string) error {
	config, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return fmt.Errorf("解析迁移数据库连接失败: %w", err)
	}

	db := stdlib.OpenDB(*config)
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("迁移前检查数据库失败: %w", err)
	}

	goose.SetBaseFS(migrationFiles)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("设置迁移方言失败: %w", err)
	}
	version, err := goose.GetDBVersionContext(ctx, db)
	if err != nil {
		return fmt.Errorf("读取当前迁移版本失败: %w", err)
	}
	if version > 0 && version < baselineVersion {
		if err := goose.UpContext(ctx, db, "migrations/legacy"); err != nil {
			return fmt.Errorf("升级历史数据库迁移失败: %w", err)
		}
	}
	if err := goose.UpContext(ctx, db, "migrations"); err != nil {
		return fmt.Errorf("执行数据库迁移失败: %w", err)
	}
	return nil
}
