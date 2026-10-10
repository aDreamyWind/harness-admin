package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	AppName         string
	Environment     string
	HTTPAddress     string
	DatabaseURL     string
	RedisAddress    string
	RedisPassword   string
	RedisDB         int
	AutoMigrate     bool
	ShutdownTimeout time.Duration
	SessionTTL      time.Duration
	SessionCookie   string
}

func Load() (Config, error) {
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
		AppName:         value("APP_NAME", "Harness Admin"),
		Environment:     value("APP_ENV", "development"),
		HTTPAddress:     value("HTTP_ADDR", ":8080"),
		DatabaseURL:     value("DATABASE_URL", "postgres://harness:harness@localhost:55432/harness_admin?sslmode=disable"),
		RedisAddress:    value("REDIS_ADDR", "localhost:56379"),
		RedisPassword:   os.Getenv("REDIS_PASSWORD"),
		RedisDB:         redisDB,
		AutoMigrate:     autoMigrate,
		ShutdownTimeout: shutdownTimeout,
		SessionTTL:      sessionTTL,
		SessionCookie:   value("SESSION_COOKIE_NAME", "harness_admin_session"),
	}
	if cfg.SessionTTL < 15*time.Minute || cfg.SessionTTL > 30*24*time.Hour {
		return Config{}, fmt.Errorf("SESSION_TTL 必须在 15 分钟到 30 天之间")
	}

	switch cfg.Environment {
	case "development", "test", "production":
	default:
		return Config{}, fmt.Errorf("APP_ENV 必须是 development、test 或 production，当前值为 %q", cfg.Environment)
	}

	return cfg, nil
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
