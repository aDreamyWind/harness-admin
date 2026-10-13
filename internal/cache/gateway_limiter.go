package cache

import (
	"context"
	"time"
)

type GatewayRequestLimiter struct {
	client *Client
	now    func() time.Time
}

func NewGatewayRequestLimiter(client *Client) *GatewayRequestLimiter {
	return &GatewayRequestLimiter{client: client, now: time.Now}
}

func (l *GatewayRequestLimiter) Allow(ctx context.Context, apiKeyID string, maximum int) (bool, error) {
	if maximum <= 0 {
		return true, nil
	}
	minute := l.now().UTC().Truncate(time.Minute)
	key := "gateway:rpm:" + apiKeyID + ":" + minute.Format("200601021504")
	count, err := l.client.raw.Incr(ctx, key).Result()
	if err != nil {
		return false, err
	}
	if count == 1 {
		if err := l.client.raw.Expire(ctx, key, 2*time.Minute).Err(); err != nil {
			return false, err
		}
	}
	return count <= int64(maximum), nil
}
