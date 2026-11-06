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
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"harness-admin/internal/httpx"
	"harness-admin/internal/tryon"
)

type tryOnRepoFixture struct {
	mu     sync.Mutex
	owner  tryon.Owner
	assets map[string]tryon.AssetImage
	outfit tryon.Outfit
	job    tryon.Job
	lease  time.Time
	result tryon.Image
	done   chan struct{}
}

func (r *tryOnRepoFixture) SaveAsset(_ context.Context, owner tryon.Owner, asset tryon.AssetImage) (tryon.Asset, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if owner != r.owner {
		return tryon.Asset{}, tryon.ErrNotFound
	}
	r.assets[asset.ID] = asset
	return asset.Asset, nil
}
func (r *tryOnRepoFixture) ListAssets(_ context.Context, owner tryon.Owner) ([]tryon.Asset, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
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
	r.mu.Lock()
	defer r.mu.Unlock()
	asset, ok := r.assets[id]
	if !ok || owner != r.owner {
		return tryon.AssetImage{}, tryon.ErrNotFound
	}
	return asset, nil
}
func (r *tryOnRepoFixture) DeleteAsset(_ context.Context, owner tryon.Owner, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	asset, ok := r.assets[id]
	if owner != r.owner || !ok || asset.Deleted {
		return tryon.ErrNotFound
	}
	if id == r.job.PersonID || slices.Contains(r.job.GarmentIDs, id) || id == r.job.PoseID {
		asset.Deleted = true
		r.assets[id] = asset
	} else {
		delete(r.assets, id)
	}
	return nil
}
func (r *tryOnRepoFixture) ListOutfits(_ context.Context, owner tryon.Owner) ([]tryon.Outfit, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if owner != r.owner || r.outfit.ID == "" {
		return []tryon.Outfit{}, nil
	}
	return []tryon.Outfit{r.outfit}, nil
}
func (r *tryOnRepoFixture) CreateOutfit(_ context.Context, owner tryon.Owner, outfit tryon.Outfit, builtinAssets []tryon.AssetImage) (tryon.Outfit, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if owner != r.owner {
		return tryon.Outfit{}, tryon.ErrNotFound
	}
	r.saveBuiltinAssets(builtinAssets)
	outfit.CreatedAt = time.Now()
	r.outfit = outfit
	return outfit, nil
}
func (r *tryOnRepoFixture) DeleteOutfit(_ context.Context, owner tryon.Owner, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if owner != r.owner || id != r.outfit.ID {
		return tryon.ErrNotFound
	}
	r.outfit = tryon.Outfit{}
	return nil
}
func (r *tryOnRepoFixture) CreateJob(_ context.Context, owner tryon.Owner, job tryon.Job, _ time.Time, builtinAssets []tryon.AssetImage) (tryon.Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if owner != r.owner {
		return tryon.Job{}, tryon.ErrNotFound
	}
	r.saveBuiltinAssets(builtinAssets)
	job.Status, job.CreatedAt = "queued", time.Now()
	r.job = job
	return job, nil
}
func (r *tryOnRepoFixture) saveBuiltinAssets(builtinAssets []tryon.AssetImage) {
	for _, asset := range builtinAssets {
		asset.Builtin = false // PostgreSQL 只保存素材快照，标记由业务层补回。
		r.assets[asset.ID] = asset
	}
}
func (r *tryOnRepoFixture) ClaimTryOnJob(_ context.Context, lease time.Time) (tryon.TryOnWork, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.job.Status != "queued" {
		return tryon.TryOnWork{}, tryon.ErrNotFound
	}
	r.job.Status, r.lease = "running", lease
	return tryon.TryOnWork{Job: r.job, Owner: r.owner, Lease: lease}, nil
}
func (r *tryOnRepoFixture) FinishTryOnJob(_ context.Context, id string, lease time.Time, im tryon.Image, failure string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if id != r.job.ID || r.job.Status != "running" || !r.lease.Equal(lease) {
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
	photo        []byte
	calls        chan tryOnEngineCall
	release      chan struct{}
	detectionErr error
}

type tryOnEngineCall struct {
	prompt string
	refs   []tryon.Image
}

func (e *tryOnEngineFixture) Models(context.Context, tryon.Owner) (tryon.Models, error) {
	return tryon.Models{
		Detection: []tryon.Model{{Code: "vision"}},
		Image:     []tryon.Model{{Code: "editor"}},
		Person:    []tryon.Model{{Code: "editor"}},
	}, nil
}
func (e *tryOnEngineFixture) Detect(_ context.Context, _ tryon.Owner, _ string, _ tryon.Image) ([]tryon.Candidate, error) {
	if e.detectionErr != nil {
		return nil, e.detectionErr
	}
	return tryon.ParseDetection(`{"items":[{"name":"蓝色衬衫","category":"上衣","box":[100,200,900,800]},{"name":"衬衫重复","category":"上衣","box":[101,200,900,800]}]}`)
}
func (e *tryOnEngineFixture) Generate(ctx context.Context, _ tryon.Owner, modelCode, operation, prompt string, refs []tryon.Image) (tryon.Image, error) {
	if modelCode != "editor" {
		return tryon.Image{}, tryon.ErrModel
	}
	if operation == "tryon_generate" {
		e.calls <- tryOnEngineCall{prompt: prompt, refs: refs}
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
	engine := &tryOnEngineFixture{photo: photo.Bytes(), calls: make(chan tryOnEngineCall, 1), release: make(chan struct{})}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	authStub := &csrfCheckingAuthStub{&accountAuthStub{session: session}}
	service := tryon.NewService(repo, engine, logger).WithTryOnQueue(repo).WithOutfits(repo)
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		service.RunTryOnJobs(t.Context())
	}()
	t.Cleanup(func() { <-workerDone })
	router := New(Dependencies{Environment: "test", Logger: logger, TryOn: service,
		Auth: authStub, SessionCookie: "session"})
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
	uploadImage := func(path string, fields map[string]string, imageData []byte, want int) *httptest.ResponseRecorder {
		t.Helper()
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		file, err := writer.CreateFormFile("file", "photo.png")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = file.Write(imageData); err != nil {
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
	upload := func(path string, fields map[string]string, want int) *httptest.ResponseRecorder {
		return uploadImage(path, fields, photo.Bytes(), want)
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
	engine.detectionErr = context.DeadlineExceeded
	timeout := upload("/garments/detect", map[string]string{"modelCode": "vision"}, http.StatusGatewayTimeout)
	engine.detectionErr = nil
	var timeoutResponse httpx.Response
	if err := json.Unmarshal(timeout.Body.Bytes(), &timeoutResponse); err != nil {
		t.Fatal(err)
	}
	if timeoutResponse.Error == nil || timeoutResponse.Error.Code != "tryon_detection_timeout" ||
		timeoutResponse.Error.Message != "衣物识别超时，请重试或切换识别模型。" {
		t.Fatalf("未返回衣物识别超时提示: %#v", timeoutResponse.Error)
	}
	upload("/assets", map[string]string{"kind": "garment", "name": "无效框", "category": "上衣", "box": "[0,0,1,1,1]"}, 400)
	garment := tryOnResponse[tryon.Asset](t, upload("/assets", map[string]string{"kind": "garment", "name": "蓝色衬衫", "category": "上衣", "box": "[0.1,0.2,0.9,0.8]"}, 201))
	pose := tryOnResponse[tryon.Asset](t, upload("/assets", map[string]string{"kind": "pose", "name": "站姿"}, 201))
	legacyRetired := make(map[string]bool)
	for _, sample := range []struct{ file, kind, name, category string }{
		{"garment-blazer.png", tryon.Garment, "焦糖色翻领外套", "外套"},
		{"person-illustration.jpg", tryon.Person2D, "短发眼镜模特", ""},
		{"person-studio.jpg", tryon.PersonReal, "短发写实模特", ""},
	} {
		key := "harness-admin/tryon/preset/" + owner.OrganizationID + "/" + owner.UserID + "/" + sample.file
		id := uuid.NewSHA1(uuid.NameSpaceOID, []byte(key)).String()
		legacyRetired[id] = false
		repo.assets[id] = tryon.AssetImage{
			Asset: tryon.Asset{ID: id, Kind: sample.kind, Name: sample.name, Category: sample.category},
			Image: tryon.Image{Data: photo.Bytes(), MIME: "image/png"},
		}
	}
	assets := tryOnResponse[[]tryon.Asset](t, send("GET", "/assets", "", nil, true, false, 200))
	privateCount := 0
	builtin := make(map[string][]tryon.Asset)
	for _, asset := range assets {
		if _, legacy := legacyRetired[asset.ID]; legacy {
			legacyRetired[asset.ID] = asset.Builtin && asset.Retired
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
	if len(builtin[tryon.Person2D]) != 6 || len(builtin[tryon.PersonReal]) != 6 || len(builtin[tryon.Garment]) != 3 || len(builtin[tryon.Pose]) < 2 {
		t.Fatalf("试衣样例素材未覆盖人物、衣物和姿态库: %#v", builtin)
	}
	for id, retired := range legacyRetired {
		oldImage := send("GET", "/assets/"+id+"/content", "", nil, true, false, 200)
		if !retired || !bytes.Equal(oldImage.Body.Bytes(), photo.Bytes()) {
			t.Fatalf("旧样例 %s 应标记为已下架，并保留试穿引用的图片", id)
		}
		send("DELETE", "/assets/"+id, "", nil, true, true, 409)
	}
	for _, kind := range []string{tryon.Person2D, tryon.PersonReal, tryon.Garment} {
		for _, asset := range builtin[kind] {
			res := send("GET", "/assets/"+asset.ID+"/content", "", nil, true, false, 200)
			if _, err := jpeg.Decode(res.Body); err != nil || res.Header().Get("Content-Type") != "image/jpeg" {
				t.Fatalf("无法读取内置素材 %s 的图片: %v", asset.Name, err)
			}
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
	if job.PoseID != "" || job.Status != "queued" || job.GarmentID != garment.ID || !slices.Equal(job.GarmentIDs, []string{garment.ID}) {
		t.Fatalf("不选姿势时任务无效: %#v", job)
	}
	select {
	case call := <-engine.calls:
		refs := call.refs
		if len(refs) != 2 || !bytes.Equal(refs[0].Data, photo.Bytes()) || bytes.Equal(refs[1].Data, photo.Bytes()) {
			t.Fatalf("模型未收到已确认的人物与衣物裁剪图")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("未发起试穿任务")
	}
	running := tryOnResponse[[]tryon.Job](t, send("GET", "/jobs", "", nil, true, false, 200))
	if len(running) != 1 || running[0].ID != job.ID || running[0].Status != "running" {
		t.Fatalf("未返回后台执行中的任务: %#v", running)
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

	garmentPath := "/assets/" + garment.ID
	send("DELETE", garmentPath, "", nil, false, false, 401)
	send("DELETE", garmentPath, "", nil, true, false, 403)
	authStub.session.User.ID = uuid.NewString()
	send("DELETE", garmentPath, "", nil, true, true, 404)
	authStub.session = session
	deleted := tryOnResponse[map[string]bool](t, send("DELETE", garmentPath, "", nil, true, true, 200))
	if !deleted["deleted"] {
		t.Fatalf("删除素材结果错误: %#v", deleted)
	}
	send("DELETE", garmentPath, "", nil, true, true, 404)
	post("/jobs", body, 404)
	afterDelete := tryOnResponse[[]tryon.Asset](t, send("GET", "/assets", "", nil, true, false, 200))
	if !slices.ContainsFunc(afterDelete, func(asset tryon.Asset) bool { return asset.ID == garment.ID && asset.Deleted }) {
		t.Fatal("试穿历史引用的素材应保留已删除标记")
	}
	send("GET", garmentPath+"/content", "", nil, true, false, 200)
	history := tryOnResponse[[]tryon.Job](t, send("GET", "/jobs", "", nil, true, false, 200))
	if len(history) != 1 || history[0].ID != job.ID || history[0].GarmentID != garment.ID {
		t.Fatalf("删除衣物后试穿记录丢失: %#v", history)
	}
	send("GET", "/jobs/"+job.ID+"/result", "", nil, true, false, 200)

	builtinPerson := builtin[tryon.Person2D][0]
	builtinGarment := builtin[tryon.Garment][0]
	builtinPose := builtin[tryon.Pose][0]
	garmentIDs := []string{builtin[tryon.Garment][1].ID, builtinGarment.ID}
	garmentJSON, err := json.Marshal(garmentIDs)
	if err != nil {
		t.Fatal(err)
	}
	send("GET", "/outfits", "", nil, false, false, 401)
	send("POST", "/outfits", "application/json", strings.NewReader("{}"), true, false, 403)
	post("/outfits", `{"name":"只有一件","garmentIds":["`+builtinGarment.ID+`"]}`, 400)
	post("/outfits", `{"name":"已删除衣物","garmentIds":["`+garment.ID+`","`+builtinGarment.ID+`"]}`, 404)
	outfit := tryOnResponse[tryon.Outfit](t, post("/outfits", `{"name":"秋日搭配","garmentIds":`+string(garmentJSON)+`}`, 201))
	if outfit.Name != "秋日搭配" || !slices.Equal(outfit.GarmentIDs, garmentIDs) || outfit.CreatedAt.IsZero() {
		t.Fatalf("套装未保存衣物选择顺序: %#v", outfit)
	}
	outfits := tryOnResponse[[]tryon.Outfit](t, send("GET", "/outfits", "", nil, true, false, 200))
	if len(outfits) != 1 || outfits[0].ID != outfit.ID || !slices.Equal(outfits[0].GarmentIDs, garmentIDs) {
		t.Fatalf("套装未出现在当前用户的列表: %#v", outfits)
	}
	outfitPath := "/outfits/" + outfit.ID
	send("DELETE", outfitPath, "", nil, true, false, 403)
	authStub.session.User.ID = uuid.NewString()
	otherOutfits := tryOnResponse[[]tryon.Outfit](t, send("GET", "/outfits", "", nil, true, false, 200))
	if len(otherOutfits) != 0 {
		t.Fatal("不能查看其他用户的套装")
	}
	send("DELETE", outfitPath, "", nil, true, true, 404)
	authStub.session = session
	removed := tryOnResponse[map[string]bool](t, send("DELETE", outfitPath, "", nil, true, true, 200))
	if !removed["deleted"] {
		t.Fatalf("删除套装结果错误: %#v", removed)
	}
	outfits = tryOnResponse[[]tryon.Outfit](t, send("GET", "/outfits", "", nil, true, false, 200))
	if len(outfits) != 0 {
		t.Fatalf("已删除的套装仍出现在列表: %#v", outfits)
	}
	send("DELETE", "/assets/"+builtinGarment.ID, "", nil, true, true, 409)
	var expected [][]byte
	for _, asset := range []tryon.Asset{builtinPerson, builtinPose} {
		ref := send("GET", "/assets/"+asset.ID+"/content", "", nil, true, false, 200)
		if _, _, err := image.DecodeConfig(bytes.NewReader(ref.Body.Bytes())); err != nil {
			t.Fatalf("内置素材 %s 无法作为图片预览: %v", asset.Name, err)
		}
		expected = append(expected, ref.Body.Bytes())
	}
	repo.done = make(chan struct{})
	post("/jobs", `{"personId":"`+builtinPerson.ID+`","garmentIds":`+string(garmentJSON)+`,"garmentId":"`+builtinGarment.ID+`","modelCode":"editor"}`, 400)
	builtinBody := `{"personId":"` + builtinPerson.ID + `","garmentIds":` + string(garmentJSON) + `,"poseId":"` + builtinPose.ID + `","modelCode":"editor"}`
	builtinJob := tryOnResponse[tryon.Job](t, post("/jobs", builtinBody, 202))
	if builtinJob.Status != "queued" || builtinJob.PersonID != builtinPerson.ID || builtinJob.GarmentID != garmentIDs[0] ||
		!slices.Equal(builtinJob.GarmentIDs, garmentIDs) || builtinJob.PoseID != builtinPose.ID {
		t.Fatalf("任务未保存选中的内置参考: %#v", builtinJob)
	}
	select {
	case call := <-engine.calls:
		refs := call.refs
		if len(refs) != 3 || !bytes.Equal(refs[0].Data, expected[0]) || !bytes.Equal(refs[2].Data, expected[1]) {
			t.Fatalf("模型应依次收到人物、衣物参考板、姿态，共 3 张图，实际 %d 张", len(refs))
		}
		if _, _, err := image.DecodeConfig(bytes.NewReader(refs[1].Data)); err != nil {
			t.Fatalf("衣物参考板无法读取: %v", err)
		}
		left := strings.Index(call.prompt, "左上格是"+builtin[tryon.Garment][1].Category)
		right := strings.Index(call.prompt, "右上格是"+builtinGarment.Category)
		if left < 0 || right <= left {
			t.Fatalf("生成提示未按选择顺序对应衣物参考板的区块: %s", call.prompt)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("未发起内置素材试穿")
	}
	select {
	case <-repo.done:
	case <-time.After(3 * time.Second):
		t.Fatal("内置素材试穿未结束")
	}
	completed := tryOnResponse[[]tryon.Job](t, send("GET", "/jobs", "", nil, true, false, 200))
	if len(completed) != 1 || completed[0].ID != builtinJob.ID || completed[0].Status != "succeeded" ||
		!slices.Equal(completed[0].GarmentIDs, garmentIDs) {
		t.Fatalf("多件试穿任务未在历史中保留衣物顺序: %#v", completed)
	}
	listed := tryOnResponse[[]tryon.Asset](t, send("GET", "/assets", "", nil, true, false, 200))
	if len(listed) != len(assets) {
		t.Fatalf("任务保存的快照在素材库重复展示: %d，预期 %d", len(listed), len(assets))
	}

	send("POST", "/assets/preview", "", nil, false, false, 401)
	send("POST", "/assets/preview", "", nil, true, false, 403)
	upload("/assets/preview", map[string]string{
		"kind": "garment", "category": "上衣", "modelCode": "editor", "box": "[null,0,1,1]",
	}, 400)
	upload("/assets/preview", map[string]string{
		"kind": "pose", "modelCode": "vision",
	}, 400)
	var generated bytes.Buffer
	if err := png.Encode(&generated, image.NewNRGBA(image.Rect(0, 0, 320, 400))); err != nil {
		t.Fatal(err)
	}
	engine.photo = generated.Bytes()
	for _, kind := range []string{tryon.Garment, tryon.Pose, tryon.Person2D, tryon.PersonReal} {
		fields := map[string]string{"kind": kind, "modelCode": "editor"}
		if kind == tryon.Garment {
			fields["name"] = candidates[0].Name
			fields["category"] = candidates[0].Category
			fields["box"] = "[0.1,0.2,0.9,0.8]"
		}
		preview := upload("/assets/preview", fields, 200)
		imageType := preview.Header().Get("Content-Type")
		info, _, err := image.DecodeConfig(bytes.NewReader(preview.Body.Bytes()))
		if err != nil || info.Width != 320 || info.Height != 400 ||
			(imageType != "image/png" && imageType != "image/jpeg") ||
			preview.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatalf("%s 预览应返回模型生成的图片及私有缓存头: %v, %v", kind, info, err)
		}
		afterPreview := tryOnResponse[[]tryon.Asset](t, send("GET", "/assets", "", nil, true, false, 200))
		if len(afterPreview) != len(listed) {
			t.Fatalf("%s 预览不应提前入库", kind)
		}
		saved := tryOnResponse[tryon.Asset](t, uploadImage("/assets", map[string]string{
			"kind": kind, "name": "确认的生成素材", "category": fields["category"],
		}, preview.Body.Bytes(), 201))
		content := send("GET", "/assets/"+saved.ID+"/content", "", nil, true, false, 200)
		if !bytes.Equal(content.Body.Bytes(), preview.Body.Bytes()) {
			t.Fatalf("%s 入库的图片应为确认过的生成预览", kind)
		}
		listed = append(listed, saved)
	}
	engine.photo = []byte("invalid model result")
	upload("/assets/preview", map[string]string{
		"kind": "pose", "modelCode": "editor",
	}, 502)
	afterFailure := tryOnResponse[[]tryon.Asset](t, send("GET", "/assets", "", nil, true, false, 200))
	if len(afterFailure) != len(listed) {
		t.Fatal("模型未生成有效预览时不应保存参考图")
	}
	posePath := "/assets/" + pose.ID
	send("DELETE", posePath, "", nil, true, true, 200)
	send("GET", posePath+"/content", "", nil, true, false, 404)
	afterPoseDelete := tryOnResponse[[]tryon.Asset](t, send("GET", "/assets", "", nil, true, false, 200))
	if slices.ContainsFunc(afterPoseDelete, func(asset tryon.Asset) bool { return asset.ID == pose.ID }) {
		t.Fatal("从未用于试穿的素材删除后不应出现在列表")
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
