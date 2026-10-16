package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	AppName               string
	Environment           string
	HTTPAddress           string
	DatabaseURL           string
	RedisAddress          string
	RedisPassword         string
	RedisDB               int
	AutoMigrate           bool
	ShutdownTimeout       time.Duration
	SessionTTL            time.Duration
	SessionCookie         string
	PlatformSessionCookie string
	ModelCredentialKey    string
	ModelPrivateHosts     []string
	OIDCIssuer            string
	OIDCSigningPrivateKey string
	ConsoleURL            string
}

func Load() (Config, error) {
	oidcSigningPrivateKey, err := oidcSigningPrivateKeyValue()
	if err != nil {
		return Config{}, err
	}

	redisDB, err := intValue("REDIS_DB", 0)
	if err != nil {
		return Config{}, err
	}

	autoMigrate, err := boolValue("AUTO_MIGRATE", true)
	if err != nil {
		return Config{}, err
	}

	shutdownTimeout, err := durationValue("SHUTDOWN_TIMEOUT", 10*time.Second)
	if err != nil {
		return Config{}, err
	}
	sessionTTL, err := durationValue("SESSION_TTL", 12*time.Hour)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		AppName:               value("APP_NAME", "Harness Admin"),
		Environment:           value("APP_ENV", "development"),
		HTTPAddress:           value("HTTP_ADDR", ":8080"),
		DatabaseURL:           value("DATABASE_URL", "postgres://harness:harness@localhost:55432/harness_admin?sslmode=disable"),
		RedisAddress:          value("REDIS_ADDR", "localhost:56379"),
		RedisPassword:         os.Getenv("REDIS_PASSWORD"),
		RedisDB:               redisDB,
		AutoMigrate:           autoMigrate,
		ShutdownTimeout:       shutdownTimeout,
		SessionTTL:            sessionTTL,
		SessionCookie:         value("SESSION_COOKIE_NAME", "harness_admin_session"),
		PlatformSessionCookie: value("PLATFORM_SESSION_COOKIE_NAME", "harness_platform_session"),
		ModelCredentialKey:    value("MODEL_CREDENTIAL_MASTER_KEY", "aGFybmVzcy1hZG1pbi1kZXYtbW9kZWwta2V5LTAwMDE="),
		ModelPrivateHosts:     csvValue("MODEL_PROVIDER_PRIVATE_HOSTS"),
		OIDCIssuer:            strings.TrimRight(value("OIDC_ISSUER", "http://localhost:8080"), "/"),
		OIDCSigningPrivateKey: oidcSigningPrivateKey,
		ConsoleURL:            strings.TrimRight(value("CONSOLE_URL", "http://localhost:5173"), "/"),
	}
	if cfg.SessionTTL < 15*time.Minute || cfg.SessionTTL > 30*24*time.Hour {
		return Config{}, fmt.Errorf("SESSION_TTL 必须在 15 分钟到 30 天之间")
	}

	switch cfg.Environment {
	case "development", "test", "production":
	default:
		return Config{}, fmt.Errorf("APP_ENV 必须是 development、test 或 production，当前值为 %q", cfg.Environment)
	}
	if cfg.Environment == "production" && os.Getenv("MODEL_CREDENTIAL_MASTER_KEY") == "" {
		return Config{}, fmt.Errorf("production 环境必须配置 MODEL_CREDENTIAL_MASTER_KEY")
	}
	issuerURL, issuerErr := url.Parse(cfg.OIDCIssuer)
	if issuerErr != nil || !issuerURL.IsAbs() || issuerURL.Host == "" {
		return Config{}, fmt.Errorf("OIDC_ISSUER 必须是完整地址")
	}
	if cfg.Environment == "production" && issuerURL.Scheme != "https" {
		return Config{}, fmt.Errorf("production 环境的 OIDC_ISSUER 必须使用 HTTPS")
	}
	if cfg.Environment == "production" && strings.TrimSpace(cfg.OIDCSigningPrivateKey) == "" {
		return Config{}, fmt.Errorf("production 环境必须配置 OIDC_SIGNING_PRIVATE_KEY 或 OIDC_SIGNING_PRIVATE_KEY_FILE")
	}
	consoleURL, consoleErr := url.Parse(cfg.ConsoleURL)
	if consoleErr != nil || !consoleURL.IsAbs() || consoleURL.Host == "" || consoleURL.RawQuery != "" || consoleURL.Fragment != "" {
		return Config{}, fmt.Errorf("CONSOLE_URL 必须是完整且不包含查询参数的地址")
	}
	if cfg.Environment == "production" && consoleURL.Scheme != "https" {
		return Config{}, fmt.Errorf("production 环境的 CONSOLE_URL 必须使用 HTTPS")
	}

	return cfg, nil
}

func oidcSigningPrivateKeyValue() (string, error) {
	inlineValue := strings.TrimSpace(os.Getenv("OIDC_SIGNING_PRIVATE_KEY"))
	filePath := strings.TrimSpace(os.Getenv("OIDC_SIGNING_PRIVATE_KEY_FILE"))
	if inlineValue != "" && filePath != "" {
		return "", fmt.Errorf("OIDC_SIGNING_PRIVATE_KEY 和 OIDC_SIGNING_PRIVATE_KEY_FILE 只能配置一个")
	}
	if inlineValue != "" {
		return inlineValue, nil
	}
	if filePath == "" {
		return "", nil
	}

	contents, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("读取 OIDC 签名私钥文件 %q 失败: %w", filePath, err)
	}
	if strings.TrimSpace(string(contents)) == "" {
		return "", fmt.Errorf("OIDC 签名私钥文件 %q 不能为空", filePath)
	}
	return string(contents), nil
}

func csvValue(key string) []string {
	values := make([]string, 0)
	for _, item := range strings.Split(os.Getenv(key), ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			values = append(values, item)
		}
	}
	return values
}

func value(key, fallback string) string {
	if result := os.Getenv(key); result != "" {
		return result
	}
	return fallback
}

func intValue(key string, fallback int) (int, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	result, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s 必须是整数: %w", key, err)
	}
	return result, nil
}

func boolValue(key string, fallback bool) (bool, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	result, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s 必须是布尔值: %w", key, err)
	}
	return result, nil
}

func durationValue(key string, fallback time.Duration) (time.Duration, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	result, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s 必须是合法时长: %w", key, err)
	}
	return result, nil
}
