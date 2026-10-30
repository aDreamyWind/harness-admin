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
	job   ImageJob
	calls int
}

func (r *visualRepoStub) SaveCanvasLayout(context.Context, string, string, string, CanvasLayout) (int, error) {
	r.calls++
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
