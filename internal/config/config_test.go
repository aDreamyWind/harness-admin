package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadUsesDefaults(t *testing.T) {
	t.Setenv("APP_NAME", "")
	t.Setenv("APP_ENV", "")
	t.Setenv("REDIS_DB", "")
	t.Setenv("AUTO_MIGRATE", "")
	t.Setenv("SHUTDOWN_TIMEOUT", "")
	t.Setenv("SESSION_TTL", "")
	t.Setenv("SESSION_COOKIE_NAME", "")
	t.Setenv("OIDC_SIGNING_PRIVATE_KEY", "")
	t.Setenv("OIDC_SIGNING_PRIVATE_KEY_FILE", "")

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

func TestLoadReadsOIDCSigningPrivateKeyFromFile(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "oidc-signing-private-key.pem")
	const privateKey = "-----BEGIN PRIVATE KEY-----\ntest-key\n-----END PRIVATE KEY-----\n"
	if err := os.WriteFile(keyPath, []byte(privateKey), 0o600); err != nil {
		t.Fatalf("写入测试私钥失败: %v", err)
	}
	t.Setenv("OIDC_SIGNING_PRIVATE_KEY", "")
	t.Setenv("OIDC_SIGNING_PRIVATE_KEY_FILE", keyPath)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() 返回错误: %v", err)
	}
	if cfg.OIDCSigningPrivateKey != privateKey {
		t.Fatalf("未从文件加载 OIDC 签名私钥: %q", cfg.OIDCSigningPrivateKey)
	}
}

func TestLoadRejectsMultipleOIDCSigningPrivateKeySources(t *testing.T) {
	t.Setenv("OIDC_SIGNING_PRIVATE_KEY", "inline-key")
	t.Setenv("OIDC_SIGNING_PRIVATE_KEY_FILE", "key.pem")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "只能配置一个") {
		t.Fatalf("同时配置两种 OIDC 私钥来源应返回明确错误，实际为: %v", err)
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
