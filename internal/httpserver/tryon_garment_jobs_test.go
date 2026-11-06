package httpserver

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"harness-admin/internal/tryon"
)

type tryOnGarmentJobsStub struct {
	TryOnService
	owner      tryon.Owner
	model      string
	items      []tryon.GarmentJobItem
	source     []byte
	queued     []tryon.GarmentJob
	successful tryon.GarmentJob
	failed     tryon.GarmentJob
	result     tryon.Image
	asset      tryon.Asset
}

func (s *tryOnGarmentJobsStub) StartGarmentJobs(_ context.Context, owner tryon.Owner, model string, items []tryon.GarmentJobItem, source []byte) ([]tryon.GarmentJob, error) {
	if owner != s.owner {
		return nil, tryon.ErrNotFound
	}
	if model == "queue-full" {
		return nil, tryon.ErrGarmentJobLimit
	}
	s.model, s.items, s.source = model, items, source
	return s.queued, nil
}

func (s *tryOnGarmentJobsStub) ListGarmentJobs(_ context.Context, owner tryon.Owner) ([]tryon.GarmentJob, error) {
	if owner != s.owner {
		return []tryon.GarmentJob{}, nil
	}
	return []tryon.GarmentJob{s.successful, s.failed}, nil
}

func (s *tryOnGarmentJobsStub) GarmentJobResult(_ context.Context, owner tryon.Owner, id string) (tryon.Image, error) {
	if owner != s.owner {
		return tryon.Image{}, tryon.ErrNotFound
	}
	switch id {
	case s.successful.ID:
		return s.result, nil
	case s.failed.ID:
		return tryon.Image{}, tryon.ErrGarmentJobState
	default:
		return tryon.Image{}, tryon.ErrNotFound
	}
}

func (s *tryOnGarmentJobsStub) SaveGarmentJob(ctx context.Context, owner tryon.Owner, id string) (tryon.Asset, error) {
	if _, err := s.GarmentJobResult(ctx, owner, id); err != nil {
		return tryon.Asset{}, err
	}
	return s.asset, nil
}

func (s *tryOnGarmentJobsStub) RetryGarmentJob(_ context.Context, owner tryon.Owner, id string) (tryon.GarmentJob, error) {
	if owner != s.owner {
		return tryon.GarmentJob{}, tryon.ErrNotFound
	}
	switch id {
	case s.successful.ID:
		return tryon.GarmentJob{}, tryon.ErrGarmentJobState
	case s.failed.ID:
		job := s.failed
		job.Status, job.Error = "queued", ""
		return job, nil
	default:
		return tryon.GarmentJob{}, tryon.ErrNotFound
	}
}

func TestTryOnGarmentJobsHTTP(t *testing.T) {
	var photo bytes.Buffer
	if err := png.Encode(&photo, image.NewNRGBA(image.Rect(0, 0, 80, 100))); err != nil {
		t.Fatal(err)
	}
	session := accountTestSession()
	authStub := &csrfCheckingAuthStub{&accountAuthStub{session: session}}
	makeJob := func(name, category string) tryon.GarmentJob {
		return tryon.GarmentJob{
			ID: uuid.NewString(), Name: name, Category: category, Status: "queued",
			ModelCode: "editor", CreatedAt: time.Now(),
		}
	}
	service := &tryOnGarmentJobsStub{
		owner: tryon.Owner{OrganizationID: session.User.OrganizationID, UserID: session.User.ID},
		queued: []tryon.GarmentJob{
			makeJob("蓝衬衫", "上衣"), makeJob("浅色长裤", "裤子"),
		},
		result: tryon.Image{Data: photo.Bytes(), MIME: "image/png"},
		asset:  tryon.Asset{ID: uuid.NewString(), Kind: tryon.Garment, Name: "蓝衬衫", Category: "上衣"},
	}
	service.successful, service.failed = service.queued[0], service.queued[1]
	service.successful.Status, service.failed.Status = "succeeded", "failed"
	service.failed.Error = "图片模型未返回有效结果"
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := New(Dependencies{Environment: "test", Logger: logger, TryOn: service, Auth: authStub, SessionCookie: "session"})
	send := func(method, path, contentType string, body io.Reader, auth, csrf bool, want int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "/api/v1/ai-tryon/garment-jobs"+path, body)
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
	submit := func(model, items string, want int) *httptest.ResponseRecorder {
		t.Helper()
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		file, err := writer.CreateFormFile("file", "衣物参考.png")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(photo.Bytes()); err != nil {
			t.Fatal(err)
		}
		for name, value := range map[string]string{"modelCode": model, "items": items} {
			if err := writer.WriteField(name, value); err != nil {
				t.Fatal(err)
			}
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		return send("POST", "", writer.FormDataContentType(), &body, true, true, want)
	}

	savePath, retryPath := "/"+service.successful.ID+"/save", "/"+service.failed.ID+"/retry"
	send("GET", "", "", nil, false, false, 401)
	send("POST", "", "", nil, false, false, 401)
	for _, path := range []string{"", savePath, retryPath} {
		send("POST", path, "", nil, true, false, 403)
	}
	for _, input := range []string{
		"[]",
		`[{"name":"蓝衬衫","category":"上衣","box":[null,0,1,1]}]`,
		`[{"name":"蓝衬衫","category":"上衣","box":[0,0,1,1,1]}]`,
		"[" + strings.Repeat(`{"name":"蓝衬衫","category":"上衣"},`, 8) + `{"name":"浅色长裤","category":"裤子"}]`,
	} {
		submit("editor", input, 400)
	}
	if len(service.items) != 0 {
		t.Fatal("无效候选不应传到任务服务")
	}

	const items = `[{"name":"蓝衬衫","category":"上衣","box":[0.1,0.2,0.8,0.9]},{"name":"浅色长裤","category":"裤子"}]`
	submit("queue-full", items, 409)
	queued := tryOnResponse[[]tryon.GarmentJob](t, submit("editor", items, 202))
	if len(queued) != 2 || queued[0].ID != service.queued[0].ID || queued[1].Status != "queued" {
		t.Fatalf("批量提交未返回两条排队任务: %#v", queued)
	}
	if service.model != "editor" || !bytes.Equal(service.source, photo.Bytes()) || len(service.items) != 2 ||
		service.items[0].Name != "蓝衬衫" || service.items[0].Category != "上衣" ||
		service.items[0].Box == nil || *service.items[0].Box != (tryon.Box{.1, .2, .8, .9}) ||
		service.items[1].Name != "浅色长裤" || service.items[1].Category != "裤子" || service.items[1].Box != nil {
		t.Fatalf("任务服务未接收到原图、模型和两件衣物候选: %#v", service.items)
	}
	listed := tryOnResponse[[]tryon.GarmentJob](t, send("GET", "", "", nil, true, false, 200))
	if len(listed) != 2 || listed[0].ID != queued[0].ID || listed[0].Status != "succeeded" ||
		listed[1].ID != queued[1].ID || listed[1].Status != "failed" || listed[1].Error == "" {
		t.Fatalf("衣物任务列表未返回独立状态: %#v", listed)
	}

	resultPath := "/" + service.successful.ID + "/result"
	result := send("GET", resultPath, "", nil, true, false, 200)
	if result.Header().Get("Content-Type") != "image/png" || result.Header().Get("Cache-Control") != "private, no-store" ||
		!bytes.Equal(result.Body.Bytes(), photo.Bytes()) {
		t.Fatal("单品预览应返回私有图片响应")
	}
	send("GET", "/"+service.failed.ID+"/result", "", nil, true, false, 409)
	send("POST", "/"+service.failed.ID+"/save", "", nil, true, true, 409)
	send("POST", "/"+service.successful.ID+"/retry", "", nil, true, true, 409)
	saved := tryOnResponse[tryon.Asset](t, send("POST", savePath, "", nil, true, true, 201))
	if saved.ID != service.asset.ID || saved.Kind != tryon.Garment {
		t.Fatalf("确认任务未返回衣物素材: %#v", saved)
	}
	retried := tryOnResponse[tryon.GarmentJob](t, send("POST", retryPath, "", nil, true, true, 202))
	if retried.ID != service.failed.ID || retried.Status != "queued" || retried.Error != "" {
		t.Fatalf("重试未返回重新排队的任务: %#v", retried)
	}
	authStub.session.User.ID = uuid.NewString()
	send("GET", resultPath, "", nil, true, false, 404)
	send("POST", savePath, "", nil, true, true, 404)
	send("POST", retryPath, "", nil, true, true, 404)
}
