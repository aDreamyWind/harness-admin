package drama

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCanvasGenerationPlansDirectInputsInLinkOrder(t *testing.T) {
	canvas := Canvas{
		Forms: []AssetForm{
			{ID: "character", AssetName: "角色", CurrentImageID: "generated-character"},
			{ID: "hidden", AssetName: "隐藏角色", CurrentImageID: "hidden-character"},
			{ID: "unfinished", AssetName: "未出图"},
		},
		Extras: &CanvasExtras{
			Hidden: []string{"hidden"},
			Cards: []CanvasCard{
				{ID: "video", Kind: "video"},
				{ID: "cover", Kind: "image", Title: "封面", Content: "https://media.example.com/cover.png"},
				{ID: "text-1", Kind: "text", Title: "第一段", Content: "从巷口走出。"},
				{ID: "text-2", Kind: "text", Title: "第二段", Content: " 镜头对准灯笼。 "},
				{ID: "upstream", Kind: "text", Title: "间接来源", Content: "不应进入模型。"},
			},
			Links: []CanvasLink{
				{Source: "text-2", Target: "video"},
				{Source: "cover", Target: "video"},
				{Source: "text-1", Target: "video"},
				{Source: "hidden", Target: "video"},
				{Source: "unfinished", Target: "video"},
				{Source: "character", Target: "video"},
				{Source: "cover", Target: "video"},
				{Source: "upstream", Target: "cover"},
			},
		},
	}
	input := CanvasGenerationInput{ModelCode: " test-video ", Prompt: "黄昏场景。"}
	request, refs, err := planCanvasGeneration(canvas, "video", input)
	if err != nil {
		t.Fatal(err)
	}
	wantPrompt := "黄昏场景。\n\n参考文案「第二段」\n镜头对准灯笼。\n\n参考文案「第一段」\n从巷口走出。"
	if request.Prompt != wantPrompt || request.Kind != "video" || request.ModelCode != "test-video" || request.AspectRatio != "16:9" || request.Duration != 5 {
		t.Fatalf("生成参数错误：%+v", request)
	}
	wantRefs := []canvasGenerationSource{
		{id: "cover", title: "封面", url: "https://media.example.com/cover.png"},
		{id: "character", title: "角色", imageID: "generated-character"},
	}
	if !reflect.DeepEqual(refs, wantRefs) {
		t.Fatalf("参考图错误：%+v", refs)
	}
	models := []CanvasGenerationModel{{Code: "test-video", Kind: "video", MinReferences: 1, MaxReferences: 2, MinDuration: 2, MaxDuration: 15}}
	if err := validateCanvasGenerationModel(models, request, len(refs)); err != nil {
		t.Fatal(err)
	}
	if err := validateCanvasGenerationModel(models, request, 3); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("模型参考图数量越界应被拒绝：%v", err)
	}
	request.Duration = 16
	if err := validateCanvasGenerationModel(models, request, len(refs)); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("模型时长越界应被拒绝：%v", err)
	}
}

func TestCanvasGenerationRetainsReferenceAndRunsAfterRequestCancellation(t *testing.T) {
	project, source, target, savedResult := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	scope := CanvasGenerationScope{OrganizationID: "org", UserID: "user", ProjectID: project, NodeID: target}
	visual := &canvasGenerationVisualStub{canvas: Canvas{Revision: 7, Extras: &CanvasExtras{
		Cards: []CanvasCard{
			{ID: source, Kind: "image", Title: "已生成的图片"},
			{ID: target, Kind: "image", Title: "下一张"},
		},
		Links: []CanvasLink{{Source: source, Target: target}},
	}}}
	jobs := &canvasGenerationJobsStub{
		jobs: map[string]CanvasGenerationJob{
			source: {ID: uuid.NewString(), NodeID: source, Status: "failed", ResultMediaID: savedResult},
		},
		finished: make(chan CanvasGenerationJob, 1),
	}
	png := []byte("\x89PNG\r\n\x1a\n")
	media := &canvasGenerationFilesStub{referenceID: savedResult, data: png, stored: make(chan string, 1)}
	engine := &canvasGenerationEngineStub{started: make(chan CanvasGenerationRequest, 1), proceed: make(chan struct{}), data: png}
	s := (&Service{visual: visual}).WithCanvasGeneration(jobs, engine).WithCanvasMedia(media)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, err := s.StartCanvasGeneration(ctx, scope.OrganizationID, scope.UserID, project, target, CanvasGenerationInput{ModelCode: "test-image", Revision: 6}); !errors.Is(err, ErrCanvasConflict) {
		t.Fatalf("未拦截过期画布的生成请求：%v", err)
	}
	job, err := s.StartCanvasGeneration(ctx, scope.OrganizationID, scope.UserID, project, target, CanvasGenerationInput{ModelCode: "test-image", Revision: 7})
	if err != nil || job.Status != "running" {
		t.Fatalf("提交生成失败：%+v %v", job, err)
	}
	cancel()
	request := awaitCanvasGenerationTest(t, engine.started)
	if request.JobID != job.ID || request.Prompt == "" || !reflect.DeepEqual(request.ReferenceIDs, []string{source}) ||
		!reflect.DeepEqual(request.ReferenceImages, []string{imageDataURI("image/png", png)}) {
		t.Fatalf("没有使用参考节点上次生成的图片：%+v", request)
	}
	duplicate, err := s.StartCanvasGeneration(context.Background(), scope.OrganizationID, scope.UserID, project, target, CanvasGenerationInput{ModelCode: "test-image", Revision: 7})
	if err != nil || duplicate.ID != job.ID {
		t.Fatalf("并行请求未复用已有任务：%+v %v", duplicate, err)
	}
	close(engine.proceed)
	if stored := awaitCanvasGenerationTest(t, media.stored); stored != job.ID {
		t.Fatalf("生成文件没有绑定当前任务：%s", stored)
	}
	if finished := awaitCanvasGenerationTest(t, jobs.finished); finished.ID != job.ID || finished.Status != "succeeded" {
		t.Fatalf("后台任务未在请求断开后完成：%+v", finished)
	}
	result, err := s.GetCanvasGeneration(context.Background(), scope.OrganizationID, scope.UserID, project, target)
	if err != nil || result == nil || result.ResultURL != canvasGenerationResultURL(project, job.ID) {
		t.Fatalf("未返回项目内生成结果：%+v %v", result, err)
	}
}

func TestCanvasGenerationReferenceURLRestrictions(t *testing.T) {
	project, media := uuid.NewString(), uuid.NewString()
	if id, ok := canvasMediaReferenceID(project, canvasGenerationResultURL(project, media)); !ok || id != media {
		t.Fatal("项目内生成的图片应可引用")
	}
	if _, ok := canvasMediaReferenceID(project, canvasGenerationResultURL(uuid.NewString(), media)); ok {
		t.Fatal("跨项目图片不应按项目素材读取")
	}
	for _, reference := range []string{"http://images.example.com/ref.png", "https://127.0.0.1/ref", "https://192.168.0.1/ref", "https://host.local/ref", "https://user@public.example/ref"} {
		if validCanvasExternalImage(reference) {
			t.Fatalf("应拒绝参考链接 %q", reference)
		}
	}
	if !validCanvasExternalImage("https://images.example.com/ref.png?revision=3") {
		t.Fatal("HTTPS 公开链接应可作为图片引用")
	}
}

type canvasGenerationVisualStub struct {
	VisualRepository
	canvas Canvas
}

func (s *canvasGenerationVisualStub) ReadCanvas(context.Context, string, string, string) (Canvas, error) {
	return s.canvas, nil
}

type canvasGenerationJobsStub struct {
	CanvasGenerationRepository
	mu       sync.Mutex
	jobs     map[string]CanvasGenerationJob
	finished chan CanvasGenerationJob
}

func (s *canvasGenerationJobsStub) ReadCanvasGeneration(_ context.Context, scope CanvasGenerationScope) (*CanvasGenerationJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[scope.NodeID]
	if !ok {
		return nil, nil
	}
	return &job, nil
}

func (s *canvasGenerationJobsStub) ReserveCanvasGeneration(_ context.Context, scope CanvasGenerationScope, revision int, job CanvasGenerationJob, _ time.Time) (CanvasGenerationJob, bool, error) {
	if revision != 7 {
		return CanvasGenerationJob{}, false, ErrCanvasConflict
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if current := s.jobs[scope.NodeID]; current.Status == "running" {
		return current, false, nil
	}
	s.jobs[scope.NodeID] = job
	return job, true, nil
}

func (s *canvasGenerationJobsStub) FinishCanvasGeneration(_ context.Context, scope CanvasGenerationScope, id, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	job := s.jobs[scope.NodeID]
	if job.ID != id {
		return ErrCanvasConflict
	}
	job.Status, job.ErrorMessage = "failed", message
	if message == "" {
		job.Status, job.ResultMediaID = "succeeded", id
	}
	s.jobs[scope.NodeID] = job
	s.finished <- job
	return nil
}

type canvasGenerationReadCloser struct{ *bytes.Reader }

func (canvasGenerationReadCloser) Close() error { return nil }

type canvasGenerationFilesStub struct {
	referenceID string
	data        []byte
	stored      chan string
}

func (s *canvasGenerationFilesStub) OpenCanvasMedia(_ context.Context, _, id string) (io.ReadSeekCloser, time.Time, error) {
	if id != s.referenceID {
		return nil, time.Time{}, ErrNotFound
	}
	return canvasGenerationReadCloser{bytes.NewReader(s.data)}, time.Time{}, nil
}

func (s *canvasGenerationFilesStub) PutCanvasMedia(ctx context.Context, _, id string, r io.Reader, _ int64) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	if !bytes.Equal(data, s.data) {
		return ErrCanvasMediaType
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.stored <- id
	return nil
}

type canvasGenerationEngineStub struct {
	started chan CanvasGenerationRequest
	proceed chan struct{}
	data    []byte
}

func (s *canvasGenerationEngineStub) CanvasGenerationModels(context.Context, string, string) ([]CanvasGenerationModel, error) {
	return []CanvasGenerationModel{{Code: "test-image", Kind: "image", MaxReferences: 2}}, nil
}

func (s *canvasGenerationEngineStub) GenerateCanvasMedia(ctx context.Context, request CanvasGenerationRequest) ([]byte, string, error) {
	s.started <- request
	select {
	case <-s.proceed:
		return s.data, "image/png", ctx.Err()
	case <-ctx.Done():
		return nil, "", ctx.Err()
	}
}

func awaitCanvasGenerationTest[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(2 * time.Second):
		t.Fatal("等待后台生成任务超时")
		var zero T
		return zero
	}
}
