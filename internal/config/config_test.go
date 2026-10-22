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

func TestLoadKeepsMCPPrivateHostsSeparate(t *testing.T) {
	t.Setenv("MODEL_PROVIDER_PRIVATE_HOSTS", "model.internal")
	t.Setenv("MCP_PRIVATE_HOSTS", "mcp.internal, 127.0.0.1")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.ModelPrivateHosts) != 1 || cfg.ModelPrivateHosts[0] != "model.internal" ||
		len(cfg.MCPPrivateHosts) != 2 || cfg.MCPPrivateHosts[0] != "mcp.internal" || cfg.MCPPrivateHosts[1] != "127.0.0.1" {
		t.Fatalf("模型与 MCP 私有主机白名单不应混用: %#v, %#v", cfg.ModelPrivateHosts, cfg.MCPPrivateHosts)
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

func TestMCPDeploymentDisabledByDefaultAndBuildNetworkValidated(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("MCP_DEPLOY_ENABLED", "")
	t.Setenv("MCP_BUILD_NETWORK", "")
	cfg, err := Load()
	if err != nil || cfg.MCPDeployEnabled || cfg.MCPBuildNetwork != "none" {
		t.Fatalf("源码部署默认值错误: %+v %v", cfg, err)
	}
	t.Setenv("MCP_BUILD_NETWORK", "default")
	if cfg, err := Load(); err != nil || cfg.MCPBuildNetwork != "default" {
		t.Fatalf("应允许 BuildKit 默认构建网络: %+v %v", cfg, err)
	}
	t.Setenv("MCP_BUILD_NETWORK", "bridge")
	if _, err := Load(); err == nil {
		t.Fatal("BuildKit 不支持 bridge 构建网络")
	}
	t.Setenv("MCP_BUILD_NETWORK", "host")
	if _, err := Load(); err == nil {
		t.Fatal("不应允许 host 构建网络")
	}
}

func TestProductionMCPDeploymentRequiresPinnedImages(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("MODEL_CREDENTIAL_MASTER_KEY", "key-present")
	t.Setenv("OIDC_SIGNING_PRIVATE_KEY", "key-present")
	t.Setenv("OIDC_SIGNING_PRIVATE_KEY_FILE", "")
	t.Setenv("OIDC_ISSUER", "https://admin.example.com")
	t.Setenv("CONSOLE_URL", "https://admin.example.com")
	t.Setenv("MCP_DEPLOY_ENABLED", "true")
	t.Setenv("MCP_NODE_IMAGE", "node:22-alpine")
	t.Setenv("MCP_PYTHON_IMAGE", "python:3.12-slim")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("生产托管部署未要求固定镜像摘要: %v", err)
	}
}
