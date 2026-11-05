package drama

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
)

var ErrCanvasAssetNameExists = errors.New("项目中已有同名的同类资产")

type CanvasAssetInput struct {
	Kind        string   `json:"kind"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	X           *float64 `json:"x"`
	Y           *float64 `json:"y"`
	Revision    int      `json:"revision"`
}

type CanvasAssetResult struct {
	ID       string `json:"id"`
	Revision int    `json:"revision"`
}

type canvasAssetRepository interface {
	CreateCanvasAsset(context.Context, string, string, string, string, CanvasAssetInput) (CanvasAssetResult, error)
}

func (s *Service) CreateCanvasAsset(ctx context.Context, org, user, project string, in CanvasAssetInput) (CanvasAssetResult, error) {
	projectID, err := uuid.Parse(project)
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	if err != nil || (in.Kind != "character" && in.Kind != "scene" && in.Kind != "prop") ||
		!validCanvasText(in.Name, 80) || !validCanvasText(in.Description, 10000) ||
		in.Revision < 1 || in.X == nil || in.Y == nil || !finiteCanvas(*in.X) || !finiteCanvas(*in.Y) {
		return CanvasAssetResult{}, ErrInvalidInput
	}
	repo, ok := s.visual.(canvasAssetRepository)
	if !ok {
		return CanvasAssetResult{}, errors.New("未配置漫剧画布资产写入")
	}
	return repo.CreateCanvasAsset(ctx, org, user, projectID.String(), uuid.NewString(), in)
}
