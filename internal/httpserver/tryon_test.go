package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"harness-admin/internal/tryon"
)

type tryOnRepoFixture struct {
	mu     sync.Mutex
	owner  tryon.Owner
	assets map[string]tryon.AssetImage
	job    tryon.Job
	result tryon.Image
	done   chan struct{}
}

func (r *tryOnRepoFixture) SaveAsset(_ context.Context, owner tryon.Owner, asset tryon.AssetImage) (tryon.Asset, error) {
	if owner != r.owner {
		return tryon.Asset{}, tryon.ErrNotFound
	}
	r.assets[asset.ID] = asset
	return asset.Asset, nil
}
func (r *tryOnRepoFixture) ListAssets(_ context.Context, owner tryon.Owner) ([]tryon.Asset, error) {
	if owner != r.owner {
		return nil, tryon.ErrNotFound
	}
	items := make([]tryon.Asset, 0, len(r.assets))
	for _, a := range r.assets {
		items = append(items, a.Asset)
	}
	return items, nil
}
func (r *tryOnRepoFixture) Asset(_ context.Context, owner tryon.Owner, id string) (tryon.AssetImage, error) {
	asset, ok := r.assets[id]
	if !ok || owner != r.owner {
		return tryon.AssetImage{}, tryon.ErrNotFound
	}
	return asset, nil
}
func (r *tryOnRepoFixture) CreateJob(_ context.Context, owner tryon.Owner, job tryon.Job, _ time.Time) (tryon.Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if owner != r.owner {
		return tryon.Job{}, tryon.ErrNotFound
	}
	if r.job.Status == "running" {
		return tryon.Job{}, tryon.ErrBusy
	}
	job.Status = "running"
	r.job = job
	return job, nil
}
func (r *tryOnRepoFixture) FinishJob(_ context.Context, owner tryon.Owner, id string, im tryon.Image, failure string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if owner != r.owner || id != r.job.ID {
		return tryon.ErrNotFound
	}
	r.job.Status, r.job.Error, r.result = "succeeded", failure, im
	if failure != "" {
		r.job.Status = "failed"
	}
	close(r.done)
	return nil
}
func (r *tryOnRepoFixture) ListJobs(_ context.Context, owner tryon.Owner) ([]tryon.Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if owner != r.owner {
		return nil, tryon.ErrNotFound
	}
	return []tryon.Job{r.job}, nil
}
func (r *tryOnRepoFixture) Result(_ context.Context, owner tryon.Owner, id string) (tryon.Image, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if owner != r.owner || id != r.job.ID || r.job.Status != "succeeded" {
		return tryon.Image{}, tryon.ErrNotFound
	}
	return r.result, nil
}

type tryOnEngineFixture struct {
	photo      []byte
	references chan []tryon.Image
	release    chan struct{}
}

func (e *tryOnEngineFixture) Models(context.Context, tryon.Owner) (tryon.Models, error) {
	return tryon.Models{Detection: []tryon.Model{{Code: "vision"}}, Image: []tryon.Model{{Code: "editor"}}}, nil
}
func (e *tryOnEngineFixture) Detect(_ context.Context, _ tryon.Owner, _ string, _ tryon.Image) ([]tryon.Candidate, error) {
	return tryon.ParseDetection(`{"items":[{"name":"蓝色衬衫","category":"上衣","box":[100,200,900,800]},{"name":"衬衫重复","category":"上衣","box":[101,200,900,800]}]}`)
}
func (e *tryOnEngineFixture) Generate(ctx context.Context, _ tryon.Owner, _, operation, _ string, refs []tryon.Image) (tryon.Image, error) {
	if operation == "tryon_generate" {
		e.references <- refs
		select {
		case <-e.release:
		case <-ctx.Done():
			return tryon.Image{}, ctx.Err()
		}
	}
	return tryon.Image{Data: e.photo, MIME: "image/png"}, nil
}

func TestTryOnHTTPFlow(t *testing.T) {
	var photo bytes.Buffer
	if err := png.Encode(&photo, image.NewNRGBA(image.Rect(0, 0, 100, 120))); err != nil {
		t.Fatal(err)
	}
	session := accountTestSession()
	owner := tryon.Owner{OrganizationID: session.User.OrganizationID, UserID: session.User.ID}
	repo := &tryOnRepoFixture{owner: owner, assets: make(map[string]tryon.AssetImage), done: make(chan struct{})}
	engine := &tryOnEngineFixture{photo: photo.Bytes(), references: make(chan []tryon.Image, 1), release: make(chan struct{})}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := New(Dependencies{Environment: "test", Logger: logger, TryOn: tryon.NewService(repo, engine, logger),
		Auth: &csrfCheckingAuthStub{&accountAuthStub{session: session}}, SessionCookie: "session"})
	send := func(method, path, contentType string, body io.Reader, auth, csrf bool, want int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "/api/v1/ai-tryon"+path, body)
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		if auth {
			req.AddCookie(&http.Cookie{Name: "session", Value: "token"})
		}
		if csrf {
			req.Header.Set("X-CSRF-Token", "csrf-token")
		}
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		if res.Code != want {
			t.Fatalf("%s %s: %d，预期 %d，响应 %s", method, path, res.Code, want, res.Body.String())
		}
		return res
	}
	post := func(path, body string, want int) *httptest.ResponseRecorder {
		return send("POST", path, "application/json", strings.NewReader(body), true, true, want)
	}
	upload := func(path string, fields map[string]string, want int) *httptest.ResponseRecorder {
		t.Helper()
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		file, err := writer.CreateFormFile("file", "photo.png")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = file.Write(photo.Bytes()); err != nil {
			t.Fatal(err)
		}
		for k, v := range fields {
			if err := writer.WriteField(k, v); err != nil {
				t.Fatal(err)
			}
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		return send("POST", path, writer.FormDataContentType(), &body, true, true, want)
	}
	send("GET", "/models", "", nil, false, false, 401)
	send("GET", "/models", "", nil, true, false, 200)
	send("POST", "/assets", "", nil, true, false, 403)
	send("POST", "/jobs", "application/json", strings.NewReader("{}"), true, false, 403)

	person := tryOnResponse[tryon.Asset](t, post("/people/generate", `{"kind":"person_2d","name":"插画模特","description":"复古漫画线条","modelCode":"editor"}`, 201))
	candidates := tryOnResponse[[]tryon.Candidate](t, upload("/garments/detect", map[string]string{"modelCode": "vision"}, 200))
	if len(candidates) != 1 || candidates[0].Box != (tryon.Box{.1, .2, .9, .8}) {
		t.Fatalf("识别框未正确归一化去重: %#v", candidates)
	}
	upload("/assets", map[string]string{"kind": "garment", "name": "无效框", "category": "上衣", "box": "[0,0,1,1,1]"}, 400)
	garment := tryOnResponse[tryon.Asset](t, upload("/assets", map[string]string{"kind": "garment", "name": "蓝色衬衫", "category": "上衣", "box": "[0.1,0.2,0.9,0.8]"}, 201))
	pose := tryOnResponse[tryon.Asset](t, upload("/assets", map[string]string{"kind": "pose", "name": "站姿"}, 201))
	assets := tryOnResponse[[]tryon.Asset](t, send("GET", "/assets", "", nil, true, false, 200))
	if len(assets) != 3 || pose.Kind != tryon.Pose {
		t.Fatalf("未找到已保存的三种素材: %#v", assets)
	}
	cropped := send("GET", "/assets/"+garment.ID+"/content", "", nil, true, false, 200)
	info, err := png.DecodeConfig(cropped.Body)
	if err != nil || info.Width != 80 || info.Height != 72 || cropped.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("裁剪结果尺寸或访问头错误: %v, %v", info, err)
	}
	post("/jobs", `{"personId":"`+garment.ID+`","garmentId":"`+person.ID+`","modelCode":"editor"}`, 400)
	post("/jobs", `{"personId":"`+person.ID+`","garmentId":"`+garment.ID+`","poseId":"`+garment.ID+`","modelCode":"editor"}`, 400)
	body := `{"personId":"` + person.ID + `","garmentId":"` + garment.ID + `","modelCode":"editor"}`
	job := tryOnResponse[tryon.Job](t, post("/jobs", body, 202))
	if job.PoseID != "" || job.Status != "running" {
		t.Fatalf("不选姿势时任务无效: %#v", job)
	}
	post("/jobs", body, 409)
	select {
	case refs := <-engine.references:
		if len(refs) != 2 || !bytes.Equal(refs[0].Data, photo.Bytes()) || bytes.Equal(refs[1].Data, photo.Bytes()) {
			t.Fatalf("模型未收到已确认的人物与衣物裁剪图")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("未发起试穿任务")
	}
	send("GET", "/jobs/"+job.ID+"/result", "", nil, true, false, 404)
	close(engine.release)
	select {
	case <-repo.done:
	case <-time.After(3 * time.Second):
		t.Fatal("试穿任务未结束")
	}
	jobs := tryOnResponse[[]tryon.Job](t, send("GET", "/jobs", "", nil, true, false, 200))
	if len(jobs) != 1 || jobs[0].Status != "succeeded" {
		t.Fatalf("任务状态错误: %#v", jobs)
	}
	res := send("GET", "/jobs/"+job.ID+"/result", "", nil, true, false, 200)
	if !bytes.Equal(res.Body.Bytes(), photo.Bytes()) {
		t.Fatal("输出图片未保存")
	}
}

func tryOnResponse[T any](t *testing.T, res *httptest.ResponseRecorder) T {
	t.Helper()
	var envelope struct {
		Data T `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Data
}
