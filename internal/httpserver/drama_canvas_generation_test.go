package httpserver

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"harness-admin/internal/drama"
)

type canvasGenerationRouteStub struct {
	scope []string
	input drama.CanvasGenerationInput
	job   *drama.CanvasGenerationJob
	err   error
}

func (s *canvasGenerationRouteStub) ListCanvasGenerationModels(_ context.Context, org, user string) ([]drama.CanvasGenerationModel, error) {
	s.scope = []string{org, user}
	return []drama.CanvasGenerationModel{
		{Code: "image", Kind: "image", MaxReferences: 3, Resolutions: []string{"1K", "2K"}},
		{Code: "video", Kind: "video", MaxReferences: 5, Resolutions: []string{"720P", "1080P"}},
	}, s.err
}

func (s *canvasGenerationRouteStub) ListCanvasGenerations(_ context.Context, org, user, project string) ([]drama.CanvasGenerationJob, error) {
	s.scope = []string{org, user, project}
	return nil, s.err
}

func (s *canvasGenerationRouteStub) GetCanvasGeneration(_ context.Context, org, user, project, node string) (*drama.CanvasGenerationJob, error) {
	s.scope = []string{org, user, project, node}
	return s.job, s.err
}

func (s *canvasGenerationRouteStub) StartCanvasGeneration(_ context.Context, org, user, project, node string, in drama.CanvasGenerationInput) (drama.CanvasGenerationJob, error) {
	s.scope = []string{org, user, project, node}
	s.input = in
	return drama.CanvasGenerationJob{ID: "new-job", NodeID: node, Status: "running"}, s.err
}

func TestDramaCanvasGenerationRoutes(t *testing.T) {
	const canvasPath = "/api/v1/ai-drama/projects/project/canvas"
	const nodePath = canvasPath + "/nodes/node/generation"
	const modelPath = "/api/v1/ai-drama/canvas/generation-models"
	const input = `{"modelCode":"image","revision":7,"prompt":"镜头向前","styleId":"ink-wash","aspectRatio":"9:16","resolution":"2K"}`
	service := &canvasGenerationRouteStub{}
	session := accountTestSession()
	router := New(Dependencies{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Auth: &csrfCheckingAuthStub{&accountAuthStub{session: session}},
		DramaCanvasGeneration: service, SessionCookie: "session",
	})
	send := func(method, path, body string, auth, csrf bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
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
	if send(http.MethodPost, nodePath, input, false, false).Code != http.StatusUnauthorized ||
		send(http.MethodPost, nodePath, input, true, false).Code != http.StatusForbidden ||
		send(http.MethodPost, nodePath, `{`, true, true).Code != http.StatusBadRequest ||
		send(http.MethodPost, nodePath, `{"modelCode":"video"}`, true, true).Code != http.StatusBadRequest || service.scope != nil {
		t.Fatal("未拦截缺少会话、缺少 CSRF 或格式错误的生成请求")
	}
	for _, path := range []string{modelPath, canvasPath + "/generations", nodePath} {
		if out := send(http.MethodGet, path, "", false, false); out.Code != http.StatusUnauthorized || service.scope != nil {
			t.Fatalf("允许无会话读取 %s: %d", path, out.Code)
		}
	}
	if out := send(http.MethodPost, nodePath, input, true, true); out.Code != http.StatusAccepted || !strings.Contains(out.Body.String(), `"status":"running"`) {
		t.Fatalf("生成提交未返回后台任务: %d %s", out.Code, out.Body)
	}
	actor := []string{session.User.OrganizationID, session.User.ID}
	nodeScope := []string{actor[0], actor[1], "project", "node"}
	if !slices.Equal(service.scope, nodeScope) || service.input != (drama.CanvasGenerationInput{ModelCode: "image", Revision: 7, Prompt: "镜头向前", StyleID: "ink-wash", AspectRatio: "9:16", Resolution: "2K"}) {
		t.Fatalf("生成提交范围或参数不正确: %v %+v", service.scope, service.input)
	}
	const videoInput = `{"modelCode":"video","revision":7,"prompt":"镜头向前","resolution":"1080P","duration":5}`
	if out := send(http.MethodPost, nodePath, videoInput, true, true); out.Code != http.StatusAccepted ||
		service.input != (drama.CanvasGenerationInput{ModelCode: "video", Revision: 7, Prompt: "镜头向前", Resolution: "1080P", Duration: 5}) {
		t.Fatalf("视频生成参数未正确传入服务: %d %s %+v", out.Code, out.Body, service.input)
	}
	service.err = drama.ErrCanvasConflict
	if out := send(http.MethodPost, nodePath, input, true, true); out.Code != http.StatusConflict {
		t.Fatalf("过期画布版本未返回 409: %d", out.Code)
	}
	service.err = drama.ErrNotFound
	if out := send(http.MethodGet, nodePath, "", true, false); out.Code != http.StatusNotFound {
		t.Fatalf("不可读项目未返回 404: %d", out.Code)
	}
	service.err = nil
	if out := send(http.MethodGet, nodePath, "", true, false); out.Code != http.StatusOK || out.Body.String() != `{"data":null}` || !slices.Equal(service.scope, nodeScope) {
		t.Fatalf("无任务节点未返回空状态或查询范围错误: %d %s %v", out.Code, out.Body, service.scope)
	}
	result := canvasPath + "/media/stable-media/content"
	service.job = &drama.CanvasGenerationJob{ID: "finished-job", Status: "succeeded", ResultURL: result, ResultMediaID: "hidden"}
	out := send(http.MethodGet, nodePath, "", true, false)
	var envelope struct{ Data drama.CanvasGenerationJob }
	err := json.Unmarshal(out.Body.Bytes(), &envelope)
	if out.Code != http.StatusOK || err != nil || envelope.Data.ResultURL != result || strings.Contains(out.Body.String(), "hidden") ||
		out.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("任务结果未返回受控素材地址或缓存策略错误: %d %s %v", out.Code, out.Body, err)
	}
	if out := send(http.MethodGet, canvasPath+"/generations", "", true, false); out.Code != http.StatusOK || out.Body.String() != `{"data":[]}` || !slices.Equal(service.scope, nodeScope[:3]) {
		t.Fatalf("项目任务查询结果或范围错误: %d %s %v", out.Code, out.Body, service.scope)
	}
	if out := send(http.MethodGet, modelPath, "", true, false); out.Code != http.StatusOK || !strings.Contains(out.Body.String(), `"maxReferences":3`) ||
		!strings.Contains(out.Body.String(), `"resolutions":["1K","2K"]`) || !strings.Contains(out.Body.String(), `"resolutions":["720P","1080P"]`) || !slices.Equal(service.scope, actor) {
		t.Fatalf("模型查询结果或范围错误: %d %s %v", out.Code, out.Body, service.scope)
	}
}
