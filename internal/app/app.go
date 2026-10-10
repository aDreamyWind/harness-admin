package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"harness-admin/internal/cache"
	"harness-admin/internal/config"
	"harness-admin/internal/database"
	"harness-admin/internal/httpserver"
)

type Application struct {
	Server *http.Server
	DB     *pgxpool.Pool
	Redis  *cache.Client
}

func New(ctx context.Context, cfg config.Config, logger *slog.Logger) (*Application, error) {
	startupCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	db, err := database.Open(startupCtx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}

	if cfg.AutoMigrate {
		if err := database.Migrate(startupCtx, cfg.DatabaseURL); err != nil {
			db.Close()
			return nil, err
		}
		logger.Info("数据库迁移已完成")
	}

	redisClient, err := cache.Open(startupCtx, cfg.RedisAddress, cfg.RedisPassword, cfg.RedisDB)
	if err != nil {
		db.Close()
		return nil, err
	}

	handler := httpserver.New(httpserver.Dependencies{
		AppName:     cfg.AppName,
		Environment: cfg.Environment,
		Logger:      logger,
		Database:    db,
		Cache:       redisClient,
	})

	return &Application{
		Server: &http.Server{
			Addr:              cfg.HTTPAddress,
			Handler:           handler,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       30 * time.Second,
			WriteTimeout:      30 * time.Second,
			IdleTimeout:       60 * time.Second,
		},
		DB:    db,
		Redis: redisClient,
	}, nil
}

func (a *Application) Close() {
	if a.Redis != nil {
		if err := a.Redis.Close(); err != nil {
			fmt.Printf("关闭 Redis 连接失败: %v\n", err)
		}
	}
	if a.DB != nil {
		a.DB.Close()
	}
}
