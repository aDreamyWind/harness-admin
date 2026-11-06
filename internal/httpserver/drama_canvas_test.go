package httpserver

import (
	"context"
	"harness-admin/internal/drama"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type visualServiceStub struct {
	scope     []string
	err       error
	generated chan string
	extras    *drama.CanvasExtras
	image     drama.ImageInput
}

func (s *visualServiceStub) Canvas(_ context.Context, o, u, p string) (drama.Canvas, error) {
	s.scope = []string{o, u, p}
	return drama.Canvas{Forms: []drama.AssetForm{}, Revision: 1, Zoom: 0.6, Extras: s.extras}, s.err
}
func (s *visualServiceStub) SaveCanvas(_ context.Context, o, u, p string, layout drama.CanvasLayout) (int, error) {
	s.scope = []string{o, u, p}
	if s.err == nil && layout.Extras != nil {
		s.extras = layout.Extras
	}
	return 2, s.err
}

func TestDramaCanvasExtrasWire(t *testing.T) {
	service := &visualServiceStub{}
	router := New(Dependencies{AppName: "test", Environment: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Auth: &csrfCheckingAuthStub{&accountAuthStub{session: accountTestSession()}}, DramaVisual: service, SessionCookie: "session"})
	call := func(method, suffix, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "/api/v1/ai-drama/projects/project/canvas"+suffix, strings.NewReader(body))
		req.AddCookie(&http.Cookie{Name: "session", Value: "token"})
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", "csrf-token")
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	if res := call(http.MethodGet, "", ""); res.Code != 200 || strings.Contains(res.Body.String(), `"extras"`) {
		t.Fatalf("未保存过的素材应省略: %s", res.Body)
	}
	if res := call(http.MethodPut, "/layout", `{"revision":1,"zoom":1,"extras":{"hidden":[],"cards":[{"id":"素材编号","kind":"text","title":"记录","content":"画布内容","position":{"x":8,"y":9}}],"links":[]}}`); res.Code != 200 || service.extras == nil || len(service.extras.Cards) != 1 || service.extras.Cards[0].Position.X != 8 {
		t.Fatalf("素材请求未绑定: %d %s", res.Code, res.Body)
	}
	if res := call(http.MethodGet, "", ""); res.Code != 200 || !strings.Contains(res.Body.String(), `"extras"`) || !strings.Contains(res.Body.String(), `"content":"画布内容"`) {
		t.Fatalf("读取画布素材未返回: %d %s", res.Code, res.Body)
	}
}
func (s *visualServiceStub) EditForm(_ context.Context, o, u, p, f string, _ drama.FormEdit) error {
	s.scope = []string{o, u, p, f}
	return s.err
}
func (s *visualServiceStub) DeriveForm(_ context.Context, o, u, p, f string) (string, error) {
	s.scope = []string{o, u, p, f}
	return "child", s.err
}
func (s *visualServiceStub) StartImage(_ context.Context, o, u, p, f string, in drama.ImageInput) (drama.ImageJob, error) {
	s.scope = []string{o, u, p, f}
	s.image = in
	return drama.ImageJob{ID: "job", Status: "queued", Prompt: "PRIVATE_STYLE_TEMPLATE"}, s.err
}
func (s *visualServiceStub) GenerateImage(job drama.ImageJob) error {
	s.generated <- job.ID
	return nil
}
func (s *visualServiceStub) ImageContent(_ context.Context, o, u, p, id string) ([]byte, string, error) {
	s.scope = []string{o, u, p, id}
	return []byte("PNG"), "image/png", s.err
}
func TestDramaCanvasRoutesRequireSessionCSRFAndUseCallerScope(t *testing.T) {
	service := &visualServiceStub{generated: make(chan string, 1)}
	session := accountTestSession()
	router := New(Dependencies{AppName: "test", Environment: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Auth: &csrfCheckingAuthStub{&accountAuthStub{session: session}}, DramaVisual: service, SessionCookie: "session"})
	base := "/api/v1/ai-drama/projects/project"
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{{"GET", "/canvas", "", 200}, {"PUT", "/canvas/layout", `{"revision":1,"zoom":1}`, 200}, {"PUT", "/forms/form", `{"revision":1}`, 200}, {"POST", "/forms/form/derive", "", 201}, {"POST", "/forms/form/images", `{"canvasRevision":5,"prompt":"调整光线"}`, 202}, {"GET", "/images/image/content", "", 200}} {
		call := func(auth, csrf bool) *httptest.ResponseRecorder {
			req := httptest.NewRequest(tc.method, base+tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			if auth {
				req.AddCookie(&http.Cookie{Name: "session", Value: "token"})
			}
			if csrf {
				req.Header.Set("X-CSRF-Token", "csrf-token")
			}
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)
			return res
		}
		if res := call(false, false); res.Code != 401 {
			t.Fatalf("缺少会话未拒绝 %s: %d", tc.path, res.Code)
		}
		if tc.method != "GET" {
			if res := call(true, false); res.Code != 403 {
				t.Fatalf("缺少CSRF未拒绝 %s: %d", tc.path, res.Code)
			}
		}
		if res := call(true, true); strings.Contains(res.Body.String(), "PRIVATE_STYLE_TEMPLATE") {
			t.Fatal("任务响应泄露私有提示词")
		} else if res.Code != tc.status {
			t.Fatalf("%s: %d %s", tc.path, res.Code, res.Body)
		}
		if len(service.scope) < 3 || service.scope[0] != session.User.OrganizationID || service.scope[1] != session.User.ID {
			t.Fatal("未使用当前会话范围")
		}
		service.err = drama.ErrNotFound
		if res := call(true, true); res.Code != 404 {
			t.Fatal("越权访问应返回404")
		}
		service.err = nil
	}
	<-service.generated
	if service.image.CanvasRevision == nil || *service.image.CanvasRevision != 5 || service.image.Prompt != "调整光线" {
		t.Fatalf("资产生图未传递画布版本与提示词：%+v", service.image)
	}
}
