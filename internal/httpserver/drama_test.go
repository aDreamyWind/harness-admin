package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"harness-admin/internal/drama"
)

type dramaServiceStub struct {
	videoScope    []string
	videoError    error
	created       drama.CreateInput
	savedSettings drama.Settings
	started       chan struct{}
}

func (s *dramaServiceStub) AvailableModels(context.Context, string, string) ([]drama.Model, error) {
	return []drama.Model{{Code: "writer", Name: "剧本模型", ModelType: "chat"}}, nil
}
func (s *dramaServiceStub) GetSettings(context.Context, string, string) (drama.Settings, error) {
	return drama.Settings{TextModelCode: "writer"}, nil
}
func (s *dramaServiceStub) SaveSettings(_ context.Context, input drama.Settings) (drama.Settings, error) {
	s.savedSettings = input
	return input, nil
}
func (s *dramaServiceStub) Create(_ context.Context, input drama.CreateInput) (drama.Project, error) {
	s.created = input
	return drama.Project{ID: "b6e6da1c-7c27-4f72-babf-34a7c4e19617", OrganizationID: input.OrganizationID, OwnerID: input.OwnerID, Title: input.Title, SourceText: input.SourceText}, nil
}
func (s *dramaServiceStub) Retry(_ context.Context, organizationID, ownerID, id string) (drama.Project, error) {
	return drama.Project{ID: id, OrganizationID: organizationID, OwnerID: ownerID, Status: "queued", SourceText: "已保存原文"}, nil
}
func (s *dramaServiceStub) ReanalyzeOverview(_ context.Context, organizationID, ownerID, id string) (drama.Project, error) {
	return drama.Project{ID: id, OrganizationID: organizationID, OwnerID: ownerID, Status: "queued", SourceText: "已保存原文"}, nil
}
func (s *dramaServiceStub) List(context.Context, string, string) ([]drama.Project, error) {
	return []drama.Project{}, nil
}
func (s *dramaServiceStub) Get(context.Context, string, string, string) (drama.Project, error) {
	return drama.Project{Title: "测试漫剧"}, nil
}
func (s *dramaServiceStub) GetSource(context.Context, string, string, string) (string, error) {
	return "完整原文", nil
}
func (s *dramaServiceStub) SourcePage(_ context.Context, _, _, _ string, start, limit int) (drama.SourceRange, error) {
	return drama.SourceRange{StartOffset: start, EndOffset: start + 4, TotalLength: 4, Text: "完整原文"}, nil
}
func (s *dramaServiceStub) ListBatches(context.Context, string, string, string) ([]drama.Batch, error) {
	return []drama.Batch{}, nil
}
func (s *dramaServiceStub) SuggestBatch(context.Context, string, string, string, drama.BatchSelection) (drama.BatchSuggestion, error) {
	return drama.BatchSuggestion{StartOffset: 0, EndOffset: 4, Boundary: "end"}, nil
}
func (s *dramaServiceStub) CreateBatch(_ context.Context, _, _, _ string, input drama.BatchSelection) (drama.Batch, error) {
	return drama.Batch{ID: "3a364763-e0df-4cb4-9c55-88ab0a11ff94", SelectionMode: input.Mode, StartOffset: 0, EndOffset: 4}, nil
}
func (s *dramaServiceStub) UpdateBatchRange(context.Context, string, string, string, string, drama.BatchRangeChange) (drama.Batch, error) {
	return drama.Batch{ID: "3a364763-e0df-4cb4-9c55-88ab0a11ff94", Boundary: "manual"}, nil
}
func (s *dramaServiceStub) StartBatchAnalysis(context.Context, string, string, string, string) (drama.BatchAnalysisJob, error) {
	return drama.BatchAnalysisJob{Batch: drama.Batch{ID: "3a364763-e0df-4cb4-9c55-88ab0a11ff94", Status: "running"}}, nil
}
func (s *dramaServiceStub) AnalyzeBatch(drama.BatchAnalysisJob) error { return nil }
func (s *dramaServiceStub) BatchProgress(context.Context, string, string, string, string) (drama.BatchProgress, error) {
	return drama.BatchProgress{ReviewIssues: []drama.ReviewIssue{}, Steps: []drama.BatchStep{{Stage: "art", ChunkNumber: 1, ChunkTotal: 2, Status: "completed"}}}, nil
}
func (s *dramaServiceStub) BatchResult(context.Context, string, string, string, string) (drama.BatchResult, error) {
	return drama.BatchResult{}, nil
}
func (s *dramaServiceStub) ProjectAssets(context.Context, string, string, string, string, string) ([]drama.Asset, error) {
	return []drama.Asset{}, nil
}
func (s *dramaServiceStub) AddEpisodeAsset(context.Context, string, string, string, string, string, string) (drama.EpisodeAsset, error) {
	return drama.EpisodeAsset{}, nil
}
func (s *dramaServiceStub) RemoveEpisodeAsset(context.Context, string, string, string, string, string) error {
	return nil
}
func (s *dramaServiceStub) UpdateOverview(_ context.Context, _, _, _ string, overview json.RawMessage) (drama.Project, error) {
	return drama.Project{Overview: overview}, nil
}
func (s *dramaServiceStub) Analyze(drama.Project) error { s.started <- struct{}{}; return nil }

func TestDramaCreateRequiresSessionAndCSRF(t *testing.T) {
	service := &dramaServiceStub{started: make(chan struct{}, 1)}
	router := New(Dependencies{AppName: "Harness Admin", Environment: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth: &csrfCheckingAuthStub{&accountAuthStub{session: accountTestSession()}}, Drama: service, SessionCookie: "test_session"})
	body := []byte(`{"title":"测试漫剧","sourceText":"很长的原文","sourceMode":"upload","style":"国风","aspectRatio":"9:16"}`)
	call := func(cookie, csrf bool) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/ai-drama/projects", bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		if cookie {
			request.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
		}
		if csrf {
			request.Header.Set("X-CSRF-Token", "csrf-token")
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	if response := call(false, false); response.Code != http.StatusUnauthorized {
		t.Fatalf("未登录仍可创建: %d", response.Code)
	}
	if response := call(true, false); response.Code != http.StatusForbidden {
		t.Fatalf("缺少 CSRF 仍可创建: %d", response.Code)
	}
	if response := call(true, true); response.Code != http.StatusCreated {
		t.Fatalf("创建失败: %d %s", response.Code, response.Body.String())
	}
	if service.created.OrganizationID != "org-1" || service.created.OwnerID == "" || service.created.SourceText != "很长的原文" {
		t.Fatalf("创建时未绑定登录用户或原文丢失: %+v", service.created)
	}
	<-service.started
}

func TestDramaSourceRequiresSession(t *testing.T) {
	service := &dramaServiceStub{started: make(chan struct{}, 1)}
	router := New(Dependencies{AppName: "Harness Admin", Environment: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth: &csrfCheckingAuthStub{&accountAuthStub{session: accountTestSession()}}, Drama: service, SessionCookie: "test_session"})
	path := "/api/v1/ai-drama/projects/b6e6da1c-7c27-4f72-babf-34a7c4e19617/source"
	request := httptest.NewRequest(http.MethodGet, path, nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("未登录仍可读取原文: %d", response.Code)
	}
	request = httptest.NewRequest(http.MethodGet, path, nil)
	request.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte("完整原文")) {
		t.Fatalf("已登录读取失败: %d %s", response.Code, response.Body.String())
	}
}

func TestDramaSettingsRequiresSessionAndCSRF(t *testing.T) {
	service := &dramaServiceStub{started: make(chan struct{}, 1)}
	router := New(Dependencies{AppName: "Harness Admin", Environment: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth: &csrfCheckingAuthStub{&accountAuthStub{session: accountTestSession()}}, Drama: service, SessionCookie: "test_session"})
	body := []byte(`{"textModelCode":"writer","imageModelCode":"","videoModelCode":""}`)
	request := httptest.NewRequest(http.MethodPut, "/api/v1/ai-drama/settings", bytes.NewReader(body))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("未登录仍可保存设置: %d", response.Code)
	}
	request = httptest.NewRequest(http.MethodPut, "/api/v1/ai-drama/settings", bytes.NewReader(body))
	request.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("缺少 CSRF 仍可保存设置: %d", response.Code)
	}
	request = httptest.NewRequest(http.MethodPut, "/api/v1/ai-drama/settings", bytes.NewReader(body))
	request.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
	request.Header.Set("X-CSRF-Token", "csrf-token")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || service.savedSettings.OrganizationID != "org-1" || service.savedSettings.UserID == "" {
		t.Fatalf("用户设置保存失败或未绑定当前用户: %d %+v", response.Code, service.savedSettings)
	}
}

func TestDramaRetryRequiresSessionAndCSRF(t *testing.T) {
	service := &dramaServiceStub{started: make(chan struct{}, 1)}
	router := New(Dependencies{AppName: "Harness Admin", Environment: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth: &csrfCheckingAuthStub{&accountAuthStub{session: accountTestSession()}}, Drama: service, SessionCookie: "test_session"})
	path := "/api/v1/ai-drama/projects/b6e6da1c-7c27-4f72-babf-34a7c4e19617/retry"
	call := func(cookie, csrf bool) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, path, nil)
		if cookie {
			request.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
		}
		if csrf {
			request.Header.Set("X-CSRF-Token", "csrf-token")
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	if response := call(false, false); response.Code != http.StatusUnauthorized {
		t.Fatalf("未登录仍可重试: %d", response.Code)
	}
	if response := call(true, false); response.Code != http.StatusForbidden {
		t.Fatalf("缺少 CSRF 仍可重试: %d", response.Code)
	}
	if response := call(true, true); response.Code != http.StatusAccepted {
		t.Fatalf("重新解析失败: %d %s", response.Code, response.Body.String())
	}
	<-service.started
}

func TestDramaBatchEndpointsRequireSessionAndCSRF(t *testing.T) {
	service := &dramaServiceStub{started: make(chan struct{}, 1)}
	router := New(Dependencies{AppName: "Harness Admin", Environment: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth: &csrfCheckingAuthStub{&accountAuthStub{session: accountTestSession()}}, Drama: service, SessionCookie: "test_session"})
	base := "/api/v1/ai-drama/projects/b6e6da1c-7c27-4f72-babf-34a7c4e19617"
	call := func(method, path, body string, cookie, csrf bool) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		request.Header.Set("Content-Type", "application/json")
		if cookie {
			request.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
		}
		if csrf {
			request.Header.Set("X-CSRF-Token", "csrf-token")
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	if got := call(http.MethodGet, base+"/source/range?start=0&limit=12000", "", false, false); got.Code != http.StatusUnauthorized {
		t.Fatalf("未登录仍可读取原文: %d", got.Code)
	}
	if got := call(http.MethodGet, base+"/source/range?start=bad", "", true, false); got.Code != http.StatusBadRequest {
		t.Fatalf("无效分页参数未拒绝: %d", got.Code)
	}
	if got := call(http.MethodGet, base+"/source/range?start=0&limit=12000", "", true, false); got.Code != http.StatusOK {
		t.Fatalf("分页读取失败: %d", got.Code)
	}
	if got := call(http.MethodPost, base+"/batches", `{"mode":"beginning"}`, true, false); got.Code != http.StatusForbidden {
		t.Fatalf("缺少 CSRF 仍可创建批次: %d", got.Code)
	}
	if got := call(http.MethodPost, base+"/batches", `{"mode":"beginning"}`, true, true); got.Code != http.StatusCreated {
		t.Fatalf("创建批次失败: %d %s", got.Code, got.Body.String())
	}
	namePath := base + "/batches/3a364763-e0df-4cb4-9c55-88ab0a11ff94/name"
	if got := call(http.MethodPatch, namePath, `{"name":"新名称"}`, false, false); got.Code != 401 {
		t.Fatal("未登录不能改名")
	}
	if got := call(http.MethodPatch, namePath, `{"name":"新名称"}`, true, false); got.Code != 403 {
		t.Fatal("改名必须验证CSRF")
	}
	if got := call(http.MethodPatch, namePath, `{"name":"新名称"}`, true, true); got.Code != 200 || !bytes.Contains(got.Body.Bytes(), []byte("新名称")) {
		t.Fatal("批次名称接口未返回新名称")
	}
	if got := call(http.MethodPatch, base+"/batches/3a364763-e0df-4cb4-9c55-88ab0a11ff94/range", `{"startOffset":0,"endOffset":4,"revision":1}`, true, false); got.Code != http.StatusForbidden {
		t.Fatalf("缺少 CSRF 仍可改批次: %d", got.Code)
	}
}

func TestDramaBatchAnalysisAndAssetRoutes(t *testing.T) {
	service := &dramaServiceStub{started: make(chan struct{}, 1)}
	router := New(Dependencies{AppName: "Harness Admin", Environment: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Auth: &csrfCheckingAuthStub{&accountAuthStub{session: accountTestSession()}}, Drama: service, SessionCookie: "test_session"})
	base := "/api/v1/ai-drama/projects/b6e6da1c-7c27-4f72-babf-34a7c4e19617"
	call := func(method, path, body string, session, csrf bool) int {
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		if session {
			req.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
		}
		if csrf {
			req.Header.Set("X-CSRF-Token", "csrf-token")
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response.Code
	}
	batch := base + "/batches/3a364763-e0df-4cb4-9c55-88ab0a11ff94"
	if got := call(http.MethodPost, batch+"/analyze", "", false, false); got != 401 {
		t.Fatalf("未登录解析状态 %d", got)
	}
	if got := call(http.MethodPost, batch+"/analyze", "", true, false); got != 403 {
		t.Fatalf("无 CSRF 解析状态 %d", got)
	}
	if got := call(http.MethodPost, batch+"/analyze", "", true, true); got != 202 {
		t.Fatalf("解析状态 %d", got)
	}
	if got := call(http.MethodPost, batch+"/resume", "", false, false); got != 401 {
		t.Fatalf("未登录续跑状态 %d", got)
	}
	if got := call(http.MethodPost, batch+"/resume", "", true, false); got != 403 {
		t.Fatalf("无CSRF续跑状态 %d", got)
	}
	if got := call(http.MethodPost, batch+"/resume", "", true, true); got != 202 {
		t.Fatalf("续跑状态 %d", got)
	}
	if got := call(http.MethodGet, batch+"/events", "", false, false); got != 401 {
		t.Fatalf("未登录事件流状态 %d", got)
	}
	if got := call(http.MethodGet, batch+"/events?after=-1", "", true, false); got != 400 {
		t.Fatalf("无效事件游标状态 %d", got)
	}
	if got := call(http.MethodGet, batch+"/events", "", true, false); got != 200 {
		t.Fatalf("事件流状态 %d", got)
	}
	if got := call(http.MethodGet, batch+"/result", "", true, false); got != 200 {
		t.Fatalf("结果状态 %d", got)
	}
	if got := call(http.MethodGet, batch+"/progress", "", false, false); got != 401 {
		t.Fatalf("未登录仍可读取解析进度: %d", got)
	}
	if got := call(http.MethodGet, batch+"/progress", "", true, false); got != 200 {
		t.Fatalf("进度状态 %d", got)
	}
	if got := call(http.MethodGet, base+"/assets?kind=scene&q=test", "", true, false); got != 200 {
		t.Fatalf("资产搜索状态 %d", got)
	}
	episode := base + "/episodes/3a364763-e0df-4cb4-9c55-88ab0a11ff94/assets"
	if got := call(http.MethodPost, episode, `{"assetId":"3a364763-e0df-4cb4-9c55-88ab0a11ff94"}`, true, false); got != 403 {
		t.Fatalf("关联无 CSRF 状态 %d", got)
	}
	if got := call(http.MethodPost, episode, `{"assetId":"3a364763-e0df-4cb4-9c55-88ab0a11ff94"}`, true, true); got != 201 {
		t.Fatalf("关联状态 %d", got)
	}
	if got := call(http.MethodDelete, episode+"/3a364763-e0df-4cb4-9c55-88ab0a11ff94", "", true, true); got != 200 {
		t.Fatalf("移除状态 %d", got)
	}
}

func TestDramaReanalyzeOverviewRequiresSessionAndCSRF(t *testing.T) {
	service := &dramaServiceStub{started: make(chan struct{}, 1)}
	router := New(Dependencies{AppName: "Harness Admin", Environment: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth: &csrfCheckingAuthStub{&accountAuthStub{session: accountTestSession()}}, Drama: service, SessionCookie: "test_session"})
	path := "/api/v1/ai-drama/projects/b6e6da1c-7c27-4f72-babf-34a7c4e19617/reanalyze-overview"
	call := func(cookie, csrf bool) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, path, nil)
		if cookie {
			request.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
		}
		if csrf {
			request.Header.Set("X-CSRF-Token", "csrf-token")
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	if response := call(false, false); response.Code != http.StatusUnauthorized {
		t.Fatalf("未登录仍可重新解析: %d", response.Code)
	}
	if response := call(true, false); response.Code != http.StatusForbidden {
		t.Fatalf("缺少 CSRF 仍可重新解析: %d", response.Code)
	}
	if response := call(true, true); response.Code != http.StatusAccepted {
		t.Fatalf("重新解析未排队: %d %s", response.Code, response.Body.String())
	}
	<-service.started
}

func (s *dramaServiceStub) ResumeBatchAnalysis(context.Context, string, string, string, string) (drama.BatchAnalysisJob, error) {
	return drama.BatchAnalysisJob{Batch: drama.Batch{Status: "running"}}, nil
}
func (s *dramaServiceStub) BatchEvents(context.Context, string, string, string, string, int64) (drama.BatchEventPage, error) {
	return drama.BatchEventPage{Status: "failed", Events: []drama.BatchEvent{{ID: 1, Stage: "review", Kind: "failed", Message: "审校失败"}}}, nil
}

func (s *dramaServiceStub) RenameBatch(_ context.Context, _, _, project, batch, name string) (drama.Batch, error) {
	return drama.Batch{ID: batch, ProjectID: project, Name: name, NameManuallyEdited: true}, nil
}
