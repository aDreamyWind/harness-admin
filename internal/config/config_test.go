package config

import "testing"

func TestLoadUsesDefaults(t *testing.T) {
	t.Setenv("APP_NAME", "")
	t.Setenv("APP_ENV", "")
	t.Setenv("REDIS_DB", "")
	t.Setenv("AUTO_MIGRATE", "")
	t.Setenv("SHUTDOWN_TIMEOUT", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() 返回错误: %v", err)
	}
	if cfg.AppName != "Harness Admin" {
		t.Fatalf("默认应用名不正确: %q", cfg.AppName)
	}
	if cfg.ShutdownTimeout.String() != "10s" {
		t.Fatalf("默认停止超时不正确: %s", cfg.ShutdownTimeout)
	}
}

func TestLoadRejectsInvalidEnvironment(t *testing.T) {
	t.Setenv("APP_ENV", "unknown")
	if _, err := Load(); err == nil {
		t.Fatal("非法 APP_ENV 应返回错误")
	}
}
