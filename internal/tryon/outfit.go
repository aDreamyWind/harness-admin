package tryon

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const MaxTryOnGarments = 4

type Outfit struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	GarmentIDs []string  `json:"garmentIds"`
	CreatedAt  time.Time `json:"createdAt"`
}

type OutfitInput struct {
	Name       string   `json:"name"`
	GarmentIDs []string `json:"garmentIds"`
}

type OutfitRepository interface {
	ListOutfits(context.Context, Owner) ([]Outfit, error)
	CreateOutfit(context.Context, Owner, Outfit, []AssetImage) (Outfit, error)
	DeleteOutfit(context.Context, Owner, string) error
}

func (s *Service) WithOutfits(repo OutfitRepository) *Service {
	s.outfits = repo
	return s
}

func (s *Service) ListOutfits(ctx context.Context, owner Owner) ([]Outfit, error) {
	return s.outfits.ListOutfits(ctx, owner)
}

func (s *Service) CreateOutfit(ctx context.Context, owner Owner, input OutfitInput) (Outfit, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" || utf8.RuneCountInString(name) > 80 || strings.ContainsRune(name, 0) {
		return Outfit{}, invalid("请填写 1 至 80 字的套装名称")
	}
	ids, err := normalizeGarmentIDs(input.GarmentIDs, 2)
	if err != nil {
		return Outfit{}, err
	}
	garments, err := s.loadGarments(ctx, owner, ids)
	if err != nil {
		return Outfit{}, err
	}
	return s.outfits.CreateOutfit(ctx, owner, Outfit{ID: uuid.NewString(), Name: name, GarmentIDs: ids}, builtinSnapshots(garments))
}

func (s *Service) DeleteOutfit(ctx context.Context, owner Owner, id string) error {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return ErrNotFound
	}
	return s.outfits.DeleteOutfit(ctx, owner, parsed.String())
}

func normalizeGarmentIDs(ids []string, minimum int) ([]string, error) {
	if len(ids) < minimum || len(ids) > MaxTryOnGarments {
		return nil, invalid(fmt.Sprintf("请选择 %d 至 %d 件衣物", minimum, MaxTryOnGarments))
	}
	normalized := make([]string, 0, len(ids))
	seen := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		parsed, err := uuid.Parse(id)
		if err != nil {
			return nil, ErrNotFound
		}
		if seen[parsed] {
			return nil, invalid("请勿重复选择同一件衣物")
		}
		seen[parsed] = true
		normalized = append(normalized, parsed.String())
	}
	return normalized, nil
}

func (s *Service) loadGarments(ctx context.Context, owner Owner, ids []string) ([]AssetImage, error) {
	items := make([]AssetImage, 0, len(ids))
	for _, id := range ids {
		asset, err := s.Asset(ctx, owner, id)
		if err != nil {
			return nil, err
		}
		if asset.Deleted {
			return nil, ErrNotFound
		}
		if asset.Kind != Garment {
			return nil, invalid("请选择衣物库中的单品")
		}
		items = append(items, asset)
	}
	return items, nil
}

func builtinSnapshots(assets []AssetImage) []AssetImage {
	var snapshots []AssetImage
	for _, asset := range assets {
		if asset.Builtin {
			snapshots = append(snapshots, asset)
		}
	}
	return snapshots
}
