package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"harness-admin/internal/config"
	"harness-admin/internal/database"
	"harness-admin/internal/identity"
	"harness-admin/internal/platformadmin"
	pgstore "harness-admin/internal/store/postgres"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fail(err)
	}
	password := strings.TrimSpace(os.Getenv("BOOTSTRAP_PLATFORM_ADMIN_PASSWORD"))
	if password == "" {
		fail(fmt.Errorf("请通过 BOOTSTRAP_PLATFORM_ADMIN_PASSWORD 设置平台管理员密码，长度至少 %d 位", identity.MinimumPasswordLength))
	}
	passwordHash, err := identity.HashPassword(password)
	if err != nil {
		fail(err)
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
	repository := pgstore.NewPlatformAdminRepository(pool)
	result, err := repository.BootstrapPlatformAdmin(ctx, platformadmin.BootstrapInput{
		Username:     env("BOOTSTRAP_PLATFORM_ADMIN_USERNAME", "platform-admin"),
		DisplayName:  env("BOOTSTRAP_PLATFORM_ADMIN_DISPLAY_NAME", "平台管理员"),
		PasswordHash: passwordHash,
	})
	if err != nil {
		fail(err)
	}
	if result.Created {
		fmt.Printf("平台管理员创建成功。platform_admin_id=%s\n", result.AdminID)
		return
	}
	fmt.Printf("平台管理员已存在，未修改密码。platform_admin_id=%s\n", result.AdminID)
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
