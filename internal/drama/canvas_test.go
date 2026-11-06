package drama

import (
	"context"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
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

func TestAssetImageUsesCanvasReferenceSnapshotAndOriginalJob(t *testing.T) {
	project, parent, child, actor := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	parentImage, actorImage := uuid.NewString(), uuid.NewString()
	upload, generated, uploadFile, resultFile := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	visual := &assetCanvasVisualStub{parentImage: parentImage, canvas: Canvas{
		Revision: 7,
		Forms: []AssetForm{
			{ID: parent, AssetName: "林夏", CurrentImageID: uuid.NewString()},
			{ID: child, AssetName: "林夏", ParentID: parent},
			{ID: actor, AssetName: "守门人", ParentID: uuid.NewString(), Name: "雨夜服饰", CurrentImageID: actorImage},
		},
		Extras: &CanvasExtras{
			Cards: []CanvasCard{
				{ID: upload, Kind: "image", Title: "服装", Content: canvasGenerationResultURL(project, uploadFile)},
				{ID: generated, Kind: "image", Title: "灯塔"},
			},
			Links: []CanvasLink{{Source: upload, Target: child}, {Source: parent, Target: child},
				{Source: actor, Target: child}, {Source: generated, Target: child}},
		},
	}}
	jobs := &canvasGenerationJobsStub{jobs: map[string]CanvasGenerationJob{generated: {ResultMediaID: resultFile}}}
	png := []byte("\x89PNG\r\n\x1a\n")
	media := &canvasGenerationFilesStub{referenceID: uploadFile, otherReferenceID: resultFile, data: png}
	engine := &canvasGenerationEngineStub{started: make(chan CanvasGenerationRequest, 1), proceed: make(chan struct{}), data: png,
		models: []CanvasGenerationModel{{Code: "painter", Kind: "image", MaxReferences: 4}}}
	close(engine.proceed)
	service := NewService(&fakeRepository{settings: Settings{ImageModelCode: "painter"}}, &fakeWriter{}).
		WithVisuals(visual, &visualGeneratorStub{err: errors.New("不应使用仅支持单张父图的引擎")}, &assetCanvasFilesStub{}).
		WithCanvasGeneration(jobs, engine).WithCanvasMedia(media)
	ctx := context.Background()
	if _, err := service.StartImage(ctx, "org", "user", project, child, ImageInput{CanvasRevision: new(6)}); !errors.Is(err, ErrCanvasConflict) || visual.calls != 0 {
		t.Fatalf("未阻止使用过期连线的资产生图：%v", err)
	}
	job, err := service.StartImage(ctx, "org", "user", project, child, ImageInput{CanvasRevision: new(7), Prompt: "换衣"})
	if err != nil || job.Status != "queued" {
		t.Fatalf("资产生图未入队：%+v %v", job, err)
	}
	visual.canvas.Extras.Links = nil
	if err := service.GenerateImage(job); err != nil {
		t.Fatal(err)
	}
	request := awaitCanvasGenerationTest(t, engine.started)
	if request.JobID != job.ID || !reflect.DeepEqual(request.ReferenceIDs, []string{parent, upload, actor, generated}) ||
		!reflect.DeepEqual(request.ReferenceImages, []string{imageDataURI("image/png", []byte(parentImage)), imageDataURI("image/png", png), imageDataURI("image/png", []byte(actorImage)), imageDataURI("image/png", png)}) ||
		!strings.Contains(request.Prompt, "图1（Image 1）：林夏\n图2（Image 2）：服装\n图3（Image 3）：守门人 · 雨夜服饰\n图4（Image 4）：灯塔") {
		t.Fatalf("未按保存的入边、图片版本和参考图顺序生成：%+v", request)
	}
	if visual.job.Status != "succeeded" || visual.job.FormID != child || visual.job.StorageKey != "image.png" {
		t.Fatalf("结果未保存到原形象任务：%+v", visual.job)
	}
}

type assetCanvasVisualStub struct {
	visualRepoStub
	canvas      Canvas
	parentImage string
}

func (s *assetCanvasVisualStub) ReadCanvas(context.Context, string, string, string) (Canvas, error) {
	return s.canvas, nil
}

func (s *assetCanvasVisualStub) QueueImage(_ context.Context, org, user, project, form, model string, in ImageInput) (ImageJob, error) {
	s.calls++
	return ImageJob{ID: uuid.NewString(), OrganizationID: org, UserID: user, ProjectID: project, FormID: form,
		ModelCode: model, AspectRatio: "9:16", Prompt: in.Prompt + in.ReferenceGuide, ReferenceID: s.parentImage, Status: "queued"}, nil
}

func (s *assetCanvasVisualStub) ReadImage(_ context.Context, _, _, _, id string) (ImageJob, error) {
	return ImageJob{StorageKey: id}, nil
}

type assetCanvasFilesStub struct{ visualFilesStub }

func (*assetCanvasFilesStub) Get(_ context.Context, key string) ([]byte, string, error) {
	return []byte(key), "image/png", nil
}
