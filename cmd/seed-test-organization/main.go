package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"harness-admin/internal/config"
	"harness-admin/internal/database"
	pgstore "harness-admin/internal/store/postgres"
	"harness-admin/internal/testseed"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fail(err)
	}
	if cfg.Environment == "production" {
		fail(fmt.Errorf("生产环境禁止写入测试组织数据"))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		fail(err)
	}
	defer pool.Close()
	if cfg.AutoMigrate {
		if err := database.Migrate(ctx, cfg.DatabaseURL); err != nil {
			fail(err)
		}
	}

	service := testseed.NewService(pgstore.NewTestSeedRepository(pool))
	result, err := service.SeedOrganization(ctx, testseed.OrganizationInput{
		OrganizationCode: env("TEST_ORG_CODE", env("BOOTSTRAP_ORG_CODE", "default")),
		ActorUsername:    env("TEST_ORG_ACTOR_USERNAME", env("BOOTSTRAP_ADMIN_USERNAME", "admin")),
	})
	if err != nil {
		fail(err)
	}
	fmt.Printf("测试组织树写入成功。organization_id=%s created=%d updated=%d\n", result.OrganizationID, result.CreatedCount, result.UpdatedCount)
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
