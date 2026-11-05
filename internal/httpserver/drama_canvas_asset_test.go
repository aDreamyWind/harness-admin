package httpserver

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"harness-admin/internal/drama"
)

func (s *visualServiceStub) CreateCanvasAsset(_ context.Context, org, user, project string, in drama.CanvasAssetInput) (drama.CanvasAssetResult, error) {
	s.scope = []string{org, user, project}
	return drama.CanvasAssetResult{ID: "asset", Revision: in.Revision + 1}, s.err
}

func TestDramaCanvasAssetCreationRequiresSessionCSRFAndUsesCallerScope(t *testing.T) {
	service := &visualServiceStub{}
	session := accountTestSession()
	router := New(Dependencies{AppName: "test", Environment: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Auth: &csrfCheckingAuthStub{&accountAuthStub{session: session}}, DramaVisual: service, SessionCookie: "session"})
	call := func(auth, csrf bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/ai-drama/projects/project/canvas/assets",
			strings.NewReader(`{"kind":"character","name":"林夏","description":"巡夜人","revision":2,"x":0,"y":34}`))
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
	if res := call(false, false); res.Code != 401 || service.scope != nil {
		t.Fatalf("未登录请求未拒绝: %d", res.Code)
	}
	if res := call(true, false); res.Code != 403 || service.scope != nil {
		t.Fatalf("缺少 CSRF 的请求未拒绝: %d", res.Code)
	}
	if res := call(true, true); res.Code != 201 || !strings.Contains(res.Body.String(), `"id":"asset","revision":3`) ||
		!reflect.DeepEqual(service.scope, []string{session.User.OrganizationID, session.User.ID, "project"}) {
		t.Fatalf("创建资产响应或归属错误: %d %s %+v", res.Code, res.Body, service.scope)
	}
	service.err = drama.ErrCanvasAssetNameExists
	if res := call(true, true); res.Code != 409 || !strings.Contains(res.Body.String(), `"code":"drama_canvas_asset_name_exists"`) {
		t.Fatalf("没有返回可区分的重名冲突: %d %s", res.Code, res.Body)
	}
}
