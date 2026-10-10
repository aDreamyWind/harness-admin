package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"harness-admin/internal/identity"
)

type LoginLimiter struct {
	client  *Client
	maximum int64
	window  time.Duration
}

func NewLoginLimiter(client *Client, maximum int64, window time.Duration) *LoginLimiter {
	return &LoginLimiter{client: client, maximum: maximum, window: window}
}

func (l *LoginLimiter) Allow(ctx context.Context, organizationCode, username string) (bool, error) {
	key := loginLimitKey(organizationCode, username)
	count, err := l.client.raw.Get(ctx, key).Int64()
	if err != nil && !errors.Is(err, redis.Nil) {
		return false, err
	}
	return count < l.maximum, nil
}

func (l *LoginLimiter) RecordFailure(ctx context.Context, organizationCode, username string) (identity.LoginFailure, error) {
	key := loginLimitKey(organizationCode, username)
	count, err := l.client.raw.Incr(ctx, key).Result()
	if err != nil {
		return identity.LoginFailure{}, err
	}
	if count == 1 {
		if err := l.client.raw.Expire(ctx, key, l.window).Err(); err != nil {
			return identity.LoginFailure{}, err
		}
	}
	return identity.LoginFailure{Attempts: count, LimitReached: count >= l.maximum}, nil
}

func (l *LoginLimiter) Clear(ctx context.Context, organizationCode, username string) error {
	return l.client.raw.Del(ctx, loginLimitKey(organizationCode, username)).Err()
}

func loginLimitKey(organizationCode, username string) string {
	value := strings.ToLower(strings.TrimSpace(organizationCode)) + "\x00" + strings.ToLower(strings.TrimSpace(username))
	sum := sha256.Sum256([]byte(value))
	return "auth:login:" + hex.EncodeToString(sum[:])
}
