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
	pgstore "harness-admin/internal/store/postgres"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fail(err)
	}

	password := os.Getenv("BOOTSTRAP_ADMIN_PASSWORD")
	if password == "" {
		fail(fmt.Errorf("请通过 BOOTSTRAP_ADMIN_PASSWORD 设置初始管理员密码，长度至少 %d 位", identity.MinimumPasswordLength))
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

	repository := pgstore.NewIdentityRepository(pool)
	result, err := repository.BootstrapAdmin(ctx, identity.BootstrapAdminInput{
		OrganizationCode: env("BOOTSTRAP_ORG_CODE", "default"),
		OrganizationName: env("BOOTSTRAP_ORG_NAME", "默认企业"),
		Username:         env("BOOTSTRAP_ADMIN_USERNAME", "admin"),
		DisplayName:      env("BOOTSTRAP_ADMIN_DISPLAY_NAME", "企业管理员"),
		Email:            strings.TrimSpace(os.Getenv("BOOTSTRAP_ADMIN_EMAIL")),
		PasswordHash:     passwordHash,
	})
	if err != nil {
		fail(err)
	}
	if !result.Created {
		fmt.Printf("管理员已存在，未修改密码。organization_id=%s user_id=%s\n", result.OrganizationID, result.UserID)
		return
	}
	fmt.Printf("管理员创建成功。organization_id=%s user_id=%s\n", result.OrganizationID, result.UserID)
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
