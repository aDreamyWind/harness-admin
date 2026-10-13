package config

import "testing"

func TestLoadUsesDefaults(t *testing.T) {
	t.Setenv("APP_NAME", "")
	t.Setenv("APP_ENV", "")
	t.Setenv("REDIS_DB", "")
	t.Setenv("AUTO_MIGRATE", "")
	t.Setenv("SHUTDOWN_TIMEOUT", "")
	t.Setenv("SESSION_TTL", "")
	t.Setenv("SESSION_COOKIE_NAME", "")

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
	if cfg.SessionTTL.String() != "12h0m0s" {
		t.Fatalf("默认会话时长不正确: %s", cfg.SessionTTL)
	}
}

func TestLoadRejectsInvalidEnvironment(t *testing.T) {
	t.Setenv("APP_ENV", "unknown")
	if _, err := Load(); err == nil {
		t.Fatal("非法 APP_ENV 应返回错误")
	}
}

func TestLoadRejectsUnsafeSessionTTL(t *testing.T) {
	t.Setenv("SESSION_TTL", "5m")
	if _, err := Load(); err == nil {
		t.Fatal("过短 SESSION_TTL 应返回错误")
	}
}

func TestLoadRequiresModelCredentialKeyInProduction(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("MODEL_CREDENTIAL_MASTER_KEY", "")
	if _, err := Load(); err == nil {
		t.Fatal("生产环境缺少模型凭据主密钥应返回错误")
	}
}
