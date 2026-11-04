package cache

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"

	"harness-admin/internal/novel"
)

type NovelCoverPreviewStore struct {
	client *Client
}

func NewNovelCoverPreviewStore(client *Client) *NovelCoverPreviewStore {
	return &NovelCoverPreviewStore{client: client}
}

func (s *NovelCoverPreviewStore) Put(ctx context.Context, owner novel.Owner, id string, preview novel.CoverPreview) error {
	data, err := json.Marshal(preview)
	if err != nil {
		return err
	}
	return s.client.raw.Set(ctx, novelCoverPreviewKey(owner, id), data, 30*time.Minute).Err()
}

func (s *NovelCoverPreviewStore) Get(ctx context.Context, owner novel.Owner, id string) (novel.CoverPreview, error) {
	data, err := s.client.raw.Get(ctx, novelCoverPreviewKey(owner, id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return novel.CoverPreview{}, novel.ErrCoverPreviewExpired
	}
	if err != nil {
		return novel.CoverPreview{}, err
	}
	var preview novel.CoverPreview
	if err := json.Unmarshal(data, &preview); err != nil {
		return novel.CoverPreview{}, err
	}
	return preview, nil
}

func novelCoverPreviewKey(owner novel.Owner, id string) string {
	return "novel:cover-preview:" + owner.OrganizationID + ":" + owner.UserID + ":" + id
}
