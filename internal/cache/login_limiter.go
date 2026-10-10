package cache

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

type LoginLimiter struct {
	client  *Client
	maximum int64
	window  time.Duration
}

func NewLoginLimiter(client *Client, maximum int64, window time.Duration) *LoginLimiter {
	return &LoginLimiter{client: client, maximum: maximum, window: window}
}

func (l *LoginLimiter) Allow(ctx context.Context, key string) (bool, error) {
	count, err := l.client.raw.Get(ctx, key).Int64()
	if err != nil && !errors.Is(err, redis.Nil) {
		return false, err
	}
	return count < l.maximum, nil
}

func (l *LoginLimiter) RecordFailure(ctx context.Context, key string) error {
	count, err := l.client.raw.Incr(ctx, key).Result()
	if err != nil {
		return err
	}
	if count == 1 {
		return l.client.raw.Expire(ctx, key, l.window).Err()
	}
	return nil
}

func (l *LoginLimiter) Clear(ctx context.Context, key string) error {
	return l.client.raw.Del(ctx, key).Err()
}
