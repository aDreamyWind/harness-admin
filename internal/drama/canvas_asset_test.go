package drama

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/google/uuid"
)

type canvasAssetRepositoryStub struct {
	VisualRepository
	scope []string
	input CanvasAssetInput
}

func (r *canvasAssetRepositoryStub) CreateCanvasAsset(_ context.Context, org, user, project, id string, in CanvasAssetInput) (CanvasAssetResult, error) {
	r.scope = []string{org, user, project}
	r.input = in
	return CanvasAssetResult{ID: id, Revision: in.Revision + 1}, nil
}

func TestCreateCanvasAssetValidatesAndNormalizesInput(t *testing.T) {
	ctx := context.Background()
	repo := &canvasAssetRepositoryStub{}
	service := &Service{visual: repo}
	project := uuid.NewString()
	valid := CanvasAssetInput{Kind: "scene", Name: " 城门 ", Description: " 雨后的石门 ", X: new(0.0), Y: new(-14.5), Revision: 1}
	for _, change := range []func(*CanvasAssetInput){
		func(in *CanvasAssetInput) { in.Kind = "text" },
		func(in *CanvasAssetInput) { in.Name = "  " },
		func(in *CanvasAssetInput) { in.Description = "\x00" },
		func(in *CanvasAssetInput) { in.X = nil },
		func(in *CanvasAssetInput) { in.Y = new(math.NaN()) },
		func(in *CanvasAssetInput) { in.Revision = 0 },
	} {
		bad := valid
		change(&bad)
		if _, err := service.CreateCanvasAsset(ctx, "org", "user", project, bad); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("未拒绝无效画布资产 %+v: %v", bad, err)
		}
	}
	if repo.scope != nil {
		t.Fatal("无效输入访问了资产存储")
	}
	result, err := service.CreateCanvasAsset(ctx, "org", "user", project, valid)
	if err != nil || result.Revision != 2 || !canonicalCanvasID(result.ID) || repo.input.Name != "城门" || repo.input.Description != "雨后的石门" ||
		repo.scope[0] != "org" || repo.scope[1] != "user" || repo.scope[2] != project {
		t.Fatalf("创建参数或结果错误: %+v %+v %v", result, repo.input, err)
	}
}
