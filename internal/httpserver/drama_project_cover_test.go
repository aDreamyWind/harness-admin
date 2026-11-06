package httpserver

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"harness-admin/internal/drama"
)

type dramaCoverHTTPStub struct {
	scope     drama.CoverScope
	model     string
	selection drama.CoverSelection
	err       error
}

func (s *dramaCoverHTTPStub) GetProjectCoverGeneration(_ context.Context, scope drama.CoverScope) (drama.CoverGeneration, error) {
	s.scope = scope
	return drama.CoverGeneration{Status: drama.CoverIdle}, s.err
}

func (s *dramaCoverHTTPStub) StartProjectCoverGeneration(_ context.Context, scope drama.CoverScope, model string) (drama.CoverGeneration, error) {
	s.scope, s.model = scope, model
	return drama.CoverGeneration{Status: drama.CoverRunning, JobID: "generation"}, s.err
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
	for _, endpoint := range []string{"generation", "images/revision"} {
		if response := send(http.MethodGet, endpoint, "", false, false); response.Code != http.StatusUnauthorized {
			t.Fatalf("允许未登录访问 %s: %d", endpoint, response.Code)
		}
	}
	for _, endpoint := range []string{"generation", "selection"} {
		if response := send(http.MethodPost, endpoint, "{}", true, false); response.Code != http.StatusForbidden {
			t.Fatalf("允许缺少 CSRF 访问 %s: %d", endpoint, response.Code)
		}
	}
	if response := send(http.MethodPost, "generation", "", true, true); response.Code != http.StatusAccepted || service.model != "" {
		t.Fatalf("缺省模型启动请求错误: %d %s", response.Code, response.Body)
	}
	if response := send(http.MethodPost, "generation", `{"modelCode":"painter"}`, true, true); response.Code != http.StatusAccepted || service.model != "painter" {
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
	service.err = drama.ErrCoverConflict
	if response := send(http.MethodPost, "selection", `{"jobId":"generation","choice":"candidate"}`, true, true); response.Code != http.StatusConflict ||
		service.selection != (drama.CoverSelection{JobID: "generation", Choice: "candidate"}) {
		t.Fatalf("过期选择没有被拒绝或参数错误: %d %s", response.Code, response.Body)
	}
}
