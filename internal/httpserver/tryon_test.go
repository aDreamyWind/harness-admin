package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/jpeg"
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

	"github.com/google/uuid"

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
func (r *tryOnRepoFixture) CreateJob(_ context.Context, owner tryon.Owner, job tryon.Job, _ time.Time, builtinAssets []tryon.AssetImage) (tryon.Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if owner != r.owner {
		return tryon.Job{}, tryon.ErrNotFound
	}
	if r.job.Status == "running" {
		return tryon.Job{}, tryon.ErrBusy
	}
	for _, asset := range builtinAssets {
		asset.Builtin = false // PostgreSQL 只保存素材快照，标记由业务层补回。
		r.assets[asset.ID] = asset
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
	legacyKey := "harness-admin/tryon/preset/" + owner.OrganizationID + "/" + owner.UserID + "/garment-blazer.png"
	legacyID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(legacyKey)).String()
	repo.assets[legacyID] = tryon.AssetImage{
		Asset: tryon.Asset{ID: legacyID, Kind: tryon.Garment, Name: "焦糖色翻领外套", Category: "外套"},
		Image: tryon.Image{Data: photo.Bytes(), MIME: "image/png"},
	}
	assets := tryOnResponse[[]tryon.Asset](t, send("GET", "/assets", "", nil, true, false, 200))
	privateCount := 0
	legacyRetired := false
	builtin := make(map[string][]tryon.Asset)
	for _, asset := range assets {
		if asset.ID == legacyID {
			legacyRetired = asset.Builtin && asset.Retired
			continue
		}
		if asset.Builtin {
			builtin[asset.Kind] = append(builtin[asset.Kind], asset)
		} else {
			privateCount++
		}
	}
	if privateCount != 3 || pose.Kind != tryon.Pose {
		t.Fatalf("未找到已保存的三种素材: %#v", assets)
	}
	if len(builtin[tryon.Person2D]) == 0 || len(builtin[tryon.PersonReal]) == 0 || len(builtin[tryon.Garment]) != 3 || len(builtin[tryon.Pose]) < 2 {
		t.Fatalf("试衣样例素材未覆盖人物、衣物和姿态库: %#v", builtin)
	}
	oldImage := send("GET", "/assets/"+legacyID+"/content", "", nil, true, false, 200)
	if !legacyRetired || !bytes.Equal(oldImage.Body.Bytes(), photo.Bytes()) {
		t.Fatal("旧样例应标记为已下架，并保留试穿引用的图片")
	}
	for _, asset := range builtin[tryon.Garment] {
		res := send("GET", "/assets/"+asset.ID+"/content", "", nil, true, false, 200)
		if _, err := jpeg.Decode(res.Body); err != nil || res.Header().Get("Content-Type") != "image/jpeg" {
			t.Fatalf("无法读取内置衣物 %s 的商品图: %v", asset.Name, err)
		}
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

	builtinPerson := builtin[tryon.Person2D][0]
	builtinGarment := builtin[tryon.Garment][0]
	builtinPose := builtin[tryon.Pose][0]
	var expected [][]byte
	for _, asset := range []tryon.Asset{builtinPerson, builtinGarment, builtinPose} {
		ref := send("GET", "/assets/"+asset.ID+"/content", "", nil, true, false, 200)
		if _, _, err := image.DecodeConfig(bytes.NewReader(ref.Body.Bytes())); err != nil {
			t.Fatalf("内置素材 %s 无法作为图片预览: %v", asset.Name, err)
		}
		expected = append(expected, ref.Body.Bytes())
	}
	repo.done = make(chan struct{})
	builtinBody := `{"personId":"` + builtinPerson.ID + `","garmentId":"` + builtinGarment.ID + `","poseId":"` + builtinPose.ID + `","modelCode":"editor"}`
	builtinJob := tryOnResponse[tryon.Job](t, post("/jobs", builtinBody, 202))
	if builtinJob.PersonID != builtinPerson.ID || builtinJob.GarmentID != builtinGarment.ID || builtinJob.PoseID != builtinPose.ID {
		t.Fatalf("任务未保存选中的内置参考: %#v", builtinJob)
	}
	select {
	case refs := <-engine.references:
		if len(refs) != len(expected) {
			t.Fatalf("模型收到 %d 张参考图", len(refs))
		}
		for i, ref := range refs {
			if !bytes.Equal(ref.Data, expected[i]) {
				t.Fatalf("第 %d 张模型参考图与预览不一致", i+1)
			}
		}
	case <-time.After(3 * time.Second):
		t.Fatal("未发起内置素材试穿")
	}
	select {
	case <-repo.done:
	case <-time.After(3 * time.Second):
		t.Fatal("内置素材试穿未结束")
	}
	listed := tryOnResponse[[]tryon.Asset](t, send("GET", "/assets", "", nil, true, false, 200))
	if len(listed) != len(assets) {
		t.Fatalf("任务保存的快照在素材库重复展示: %d，预期 %d", len(listed), len(assets))
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
