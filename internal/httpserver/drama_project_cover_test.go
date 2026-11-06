package httpserver

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"harness-admin/internal/drama"
)

type dramaCoverHTTPStub struct {
	scope     drama.CoverScope
	input     drama.CoverGenerationInput
	selection drama.CoverSelection
	applied   string
	uploaded  string
	err       error
}

func (s *dramaCoverHTTPStub) GetProjectCoverGeneration(_ context.Context, scope drama.CoverScope) (drama.CoverGeneration, error) {
	s.scope = scope
	return drama.CoverGeneration{Status: drama.CoverIdle}, s.err
}

func (s *dramaCoverHTTPStub) StartProjectCoverGeneration(_ context.Context, scope drama.CoverScope, input drama.CoverGenerationInput) (drama.CoverGeneration, error) {
	s.scope, s.input = scope, input
	return drama.CoverGeneration{Status: drama.CoverRunning, JobID: "generation"}, s.err
}

func (s *dramaCoverHTTPStub) ListProjectCoverHistory(_ context.Context, scope drama.CoverScope) ([]drama.CoverRevision, error) {
	s.scope = scope
	return []drama.CoverRevision{{Revision: "old-cover", Source: "upload", Current: true}}, s.err
}

func (s *dramaCoverHTTPStub) ApplyProjectCoverHistory(_ context.Context, scope drama.CoverScope, revision string) (drama.CoverGeneration, error) {
	s.scope, s.applied = scope, revision
	return drama.CoverGeneration{Status: drama.CoverIdle}, s.err
}

func (s *dramaCoverHTTPStub) UploadProjectCover(_ context.Context, scope drama.CoverScope, data []byte) (drama.CoverGeneration, error) {
	s.scope, s.uploaded = scope, string(data)
	return drama.CoverGeneration{Status: drama.CoverIdle}, s.err
}

func (s *dramaCoverHTTPStub) SelectProjectCover(_ context.Context, scope drama.CoverScope, input drama.CoverSelection) (drama.CoverGeneration, error) {
	s.scope, s.selection = scope, input
	return drama.CoverGeneration{Status: drama.CoverSucceeded}, s.err
}

func (s *dramaCoverHTTPStub) ProjectCoverImage(_ context.Context, scope drama.CoverScope, revision string) (drama.CoverImage, error) {
	s.scope = scope
	return drama.CoverImage{MIME: "image/svg+xml", Data: []byte(`<svg><text>` + revision + `</text></svg>`)}, s.err
}

func TestDramaProjectCoverRoutes(t *testing.T) {
	session := accountTestSession()
	service := &dramaCoverHTTPStub{}
	router := New(Dependencies{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Auth: &csrfCheckingAuthStub{&accountAuthStub{session: session}},
		DramaProjectCover: service, SessionCookie: "session",
	})
	send := func(method, endpoint, body string, auth, csrf bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/v1/ai-drama/projects/project/cover/"+endpoint, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if auth {
			req.AddCookie(&http.Cookie{Name: "session", Value: "token"})
		}
		if csrf {
			req.Header.Set("X-CSRF-Token", "csrf-token")
		}
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		return out
	}
	for _, endpoint := range []string{"generation", "images/revision", "history"} {
		if response := send(http.MethodGet, endpoint, "", false, false); response.Code != http.StatusUnauthorized {
			t.Fatalf("允许未登录访问 %s: %d", endpoint, response.Code)
		}
	}
	for _, endpoint := range []string{"generation", "selection", "history/apply", "upload"} {
		if response := send(http.MethodPost, endpoint, "{}", true, false); response.Code != http.StatusForbidden {
			t.Fatalf("允许缺少 CSRF 访问 %s: %d", endpoint, response.Code)
		}
	}
	if response := send(http.MethodPost, "generation", "", true, true); response.Code != http.StatusAccepted || service.input != (drama.CoverGenerationInput{}) {
		t.Fatalf("缺省模型启动请求错误: %d %s", response.Code, response.Body)
	}
	if response := send(http.MethodPost, "generation", `{"modelCode":"painter","instruction":"突出火光"}`, true, true); response.Code != http.StatusAccepted ||
		service.input != (drama.CoverGenerationInput{ModelCode: "painter", Instruction: "突出火光"}) {
		t.Fatalf("启动请求错误: %d %s", response.Code, response.Body)
	}
	if service.scope != (drama.CoverScope{OrganizationID: session.User.OrganizationID, UserID: session.User.ID, ProjectID: "project"}) {
		t.Fatalf("封面接口未使用会话作用域: %+v", service.scope)
	}
	if response := send(http.MethodGet, "images/revision", "", true, false); response.Code != http.StatusOK ||
		response.Header().Get("Content-Type") != "image/svg+xml" || response.Header().Get("X-Content-Type-Options") != "nosniff" ||
		!strings.Contains(response.Header().Get("Content-Security-Policy"), "sandbox") || response.Header().Get("Cache-Control") != "private, no-store" ||
		!strings.Contains(response.Body.String(), "revision") {
		t.Fatalf("封面图片或响应防护头错误: %d %v %s", response.Code, response.Header(), response.Body)
	}
	if response := send(http.MethodGet, "history", "", true, false); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"revision":"old-cover"`) ||
		response.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("未返回历史封面: %d %s", response.Code, response.Body)
	}
	if response := send(http.MethodPost, "history/apply", `{"revision":"old-cover"}`, true, true); response.Code != http.StatusOK || service.applied != "old-cover" {
		t.Fatalf("未选择请求中的历史封面: %d %s", response.Code, response.Body)
	}
	if response := send(http.MethodPost, "upload", `{}`, true, true); response.Code != http.StatusBadRequest {
		t.Fatalf("未拒绝非 multipart 图片上传: %d %s", response.Code, response.Body)
	}
	var body bytes.Buffer
	upload := multipart.NewWriter(&body)
	file, err := upload.CreateFormFile("file", "cover.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(file, "上传的封面"); err != nil {
		t.Fatal(err)
	}
	if err := upload.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/ai-drama/projects/project/cover/upload", &body)
	request.Header.Set("Content-Type", upload.FormDataContentType())
	request.Header.Set("X-CSRF-Token", "csrf-token")
	request.AddCookie(&http.Cookie{Name: "session", Value: "token"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || service.uploaded != "上传的封面" {
		t.Fatalf("未传递上传的封面: %d %s", response.Code, response.Body)
	}
	service.err = drama.ErrCoverConflict
	if response := send(http.MethodPost, "selection", `{"jobId":"generation","choice":"candidate"}`, true, true); response.Code != http.StatusConflict ||
		service.selection != (drama.CoverSelection{JobID: "generation", Choice: "candidate"}) {
		t.Fatalf("过期选择没有被拒绝或参数错误: %d %s", response.Code, response.Body)
	}
}
