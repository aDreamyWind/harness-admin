package drama

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"math"
	"testing"
)

type visualRepoStub struct {
	VisualRepository
	job    ImageJob
	layout CanvasLayout
	calls  int
}

func (r *visualRepoStub) SaveCanvasLayout(_ context.Context, _, _, _ string, layout CanvasLayout) (int, error) {
	r.calls++
	r.layout = layout
	return 2, nil
}
func (r *visualRepoStub) QueueImage(_ context.Context, _, _, _, _, _ string, _ ImageInput) (ImageJob, error) {
	r.calls++
	return ImageJob{ID: uuid.NewString()}, nil
}
func (r *visualRepoStub) ClaimImage(context.Context, string) (bool, error) {
	r.calls++
	return true, nil
}
func (r *visualRepoStub) FinishImage(_ context.Context, in ImageJob) error { r.job = in; return nil }

type visualGeneratorStub struct{ err error }

func (g *visualGeneratorStub) Generate(context.Context, ImageRequest) ([]byte, string, error) {
	return []byte("image"), "image/png", g.err
}

type visualFilesStub struct{ ImageFiles }

func (*visualFilesStub) Put(context.Context, string, []byte, string) (string, error) {
	return "image.png", nil
}
func TestCanvasRejectsInvalidCoordinatesAndDuplicateNodes(t *testing.T) {
	repo := &visualRepoStub{}
	s := &Service{visual: repo}
	id := uuid.NewString()
	for _, in := range []CanvasLayout{{Revision: 1, Zoom: 0}, {Revision: 1, Zoom: math.NaN()}, {Revision: 1, Zoom: 1, X: math.Inf(1)}, {Revision: 1, Zoom: 1, Positions: []CanvasPosition{{ID: id}, {ID: id}}}} {
		if _, err := s.SaveCanvas(context.Background(), "o", "u", id, in); !errors.Is(err, ErrInvalidInput) {
			t.Fatal("参数未拒绝")
		}
	}
	if repo.calls != 0 {
		t.Fatal("校验失败仍访问存储")
	}
}

func TestCanvasExtrasValidateMediaAndConnections(t *testing.T) {
	repo := &visualRepoStub{}
	s := &Service{visual: repo}
	project, cardID, formID, mediaID := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	valid := CanvasCard{ID: cardID, Kind: "image", Title: " 导入素材 ", Content: "/api/v1/ai-drama/projects/" + project + "/canvas/media/" + mediaID + "/content"}
	link := CanvasLink{ID: "xy-edge__" + cardID + "-" + formID, Source: cardID, Target: formID}
	for _, extras := range []CanvasExtras{
		{Cards: []CanvasCard{valid, valid}},
		{Cards: []CanvasCard{{ID: cardID, Kind: "image", Title: "无效地址", Content: "javascript:alert(1)"}}},
		{Cards: []CanvasCard{{ID: cardID, Kind: "image", Title: "跨项目素材", Content: "/api/v1/ai-drama/projects/" + uuid.NewString() + "/canvas/media/" + mediaID + "/content"}}},
		{Links: []CanvasLink{{ID: "self", Source: formID, Target: formID}}},
		{Links: []CanvasLink{link, {ID: "duplicate", Source: cardID, Target: formID}}},
	} {
		if _, err := s.SaveCanvas(context.Background(), "o", "u", project, CanvasLayout{Revision: 1, Zoom: 1, Extras: &extras}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("无效画布素材应拒绝: %v", err)
		}
	}
	if repo.calls != 0 {
		t.Fatal("校验失败后仍保存了画布")
	}
	extras := CanvasExtras{Cards: []CanvasCard{valid,
		{ID: uuid.NewString(), Kind: "text", Title: "新文本"},
		{ID: uuid.NewString(), Kind: "audio", Title: "新音频"},
	}, Links: []CanvasLink{link}}
	if _, err := s.SaveCanvas(context.Background(), "o", "u", project, CanvasLayout{Revision: 1, Zoom: 1, Extras: &extras}); err != nil {
		t.Fatal(err)
	}
	if repo.layout.Extras == nil || repo.layout.Extras.Hidden == nil || repo.layout.Extras.Cards[0].Title != "导入素材" || extras.Cards[0].Title != valid.Title {
		t.Fatalf("素材规范化异常: %+v", repo.layout.Extras)
	}
}

func TestImageFailureIsPersistedAndModelSettingRequired(t *testing.T) {
	repo := &visualRepoStub{}
	s := NewService(&fakeRepository{}, nil).WithVisuals(repo, &visualGeneratorStub{err: &ImageFailure{Message: "图片模型响应超时，请重试。", Cause: errors.New("PRIVATE_STYLE_TEMPLATE")}}, &visualFilesStub{})
	id := uuid.NewString()
	if _, err := s.StartImage(context.Background(), "o", "u", id, id, ImageInput{}); !errors.Is(err, ErrImageModelMissing) {
		t.Fatal(err)
	}
	if repo.calls != 0 {
		t.Fatal("未配置模型不应创建任务")
	}
	if err := s.GenerateImage(ImageJob{ID: id}); err == nil || repo.job.Status != "failed" || repo.job.Error != "图片模型响应超时，请重试。" {
		t.Fatalf("失败任务未保存 %+v %v", repo.job, err)
	}
}
