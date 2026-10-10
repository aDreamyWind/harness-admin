package cache

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

type Client struct {
	raw *redis.Client
}

func Open(ctx context.Context, address, password string, database int) (*Client, error) {
	raw := redis.NewClient(&redis.Options{
		Addr:     address,
		Password: password,
		DB:       database,
	})
	if err := raw.Ping(ctx).Err(); err != nil {
		_ = raw.Close()
		return nil, fmt.Errorf("连接 Redis 失败: %w", err)
	}
	return &Client{raw: raw}, nil
}

func (c *Client) Ping(ctx context.Context) error {
	return c.raw.Ping(ctx).Err()
}

func (c *Client) Close() error {
	return c.raw.Close()
}

func (c *Client) Raw() *redis.Client {
	return c.raw
}
