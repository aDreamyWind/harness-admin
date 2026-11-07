package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"harness-admin/internal/novel"
)

type novelHTTPStub struct {
	owner              novel.Owner
	createInput        novel.CreateInput
	refineInput        novel.RefineIdeaInput
	refineResult       *novel.RefineIdeaResult
	refineError        error
	planInput          novel.ChapterPlanInput
	actionInput        novel.Action
	actionResult       *novel.Project
	actionError        error
	coverInput         novel.GenerateCoverInput
	cover              novel.Cover
	coverData          []byte
	coverGeneration    novel.CoverGeneration
	coverGenerationErr error
	draftInput         novel.DraftGenerationInput
	draftProject       string
	draftChapter       int
	draftGeneration    novel.DraftGeneration
	draftGenerationErr error
}

func (s *novelHTTPStub) List(_ context.Context, owner novel.Owner) ([]novel.Book, error) {
	s.owner = owner
	if s.cover.Revision != "" {
		return []novel.Book{{ID: "book", Cover: &s.cover}}, nil
	}
	return []novel.Book{}, nil
}
func (s *novelHTTPStub) Models(_ context.Context, owner novel.Owner) ([]novel.Model, error) {
	s.owner = owner
	return []novel.Model{}, nil
}
func (s *novelHTTPStub) RefineIdea(_ context.Context, owner novel.Owner, input novel.RefineIdeaInput) (novel.RefineIdeaResult, error) {
	s.owner = owner
	s.refineInput = input
	if s.refineError != nil {
		return novel.RefineIdeaResult{}, s.refineError
	}
	if s.refineResult != nil {
		return *s.refineResult, nil
	}
	return novel.RefineIdeaResult{Brief: "一名守塔人追查午夜呼救的来历"}, nil
}
func (s *novelHTTPStub) Create(_ context.Context, owner novel.Owner, input novel.CreateInput) (novel.Project, error) {
	s.owner = owner
	s.createInput = input
	return novel.Project{Book: novel.Book{Title: input.Title}, Workspace: novel.Workspace{StoryMode: input.StoryMode}}, nil
}

func TestNovelCreateBindsIdeaModeWithoutTitle(t *testing.T) {
	stub := &novelHTTPStub{}
	router := New(Dependencies{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth:   &csrfCheckingAuthStub{&accountAuthStub{session: accountTestSession()}},
		Novel:  stub, SessionCookie: "test_session",
	})
	for _, test := range []struct {
		name string
		body string
		want novel.CreateInput
	}{
		{
			name: "普通小说",
			body: `{"brief":"守塔人听到呼救","chapterCount":3,"suggestTitle":true}`,
			want: novel.CreateInput{Brief: "守塔人听到呼救", ChapterCount: 3, SuggestTitle: true},
		},
		{
			name: "长篇连载",
			body: `{"brief":"守塔人听到呼救","storyMode":"serial"}`,
			want: novel.CreateInput{Brief: "守塔人听到呼救", StoryMode: novel.StoryModeSerial},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/v1/ai-novel/projects", strings.NewReader(test.body))
			request.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
			request.Header.Set("X-CSRF-Token", "csrf-token")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusCreated || stub.createInput != test.want {
				t.Fatalf("未接收灵感创作参数，响应 %d，参数 %+v", recorder.Code, stub.createInput)
			}
			if hasStoryMode := strings.Contains(recorder.Body.String(), `"storyMode":"serial"`); hasStoryMode != (test.want.StoryMode == novel.StoryModeSerial) {
				t.Fatalf("小说模式响应错误: %s", recorder.Body)
			}
		})
	}
}
func (s *novelHTTPStub) Get(_ context.Context, owner novel.Owner, _ string) (novel.Project, error) {
	s.owner = owner
	return novel.Project{}, novel.ErrNotFound
}
func (s *novelHTTPStub) PlanChapters(_ context.Context, owner novel.Owner, _ string, input novel.ChapterPlanInput) (novel.ChapterPlan, error) {
	s.owner, s.planInput = owner, input
	return novel.ChapterPlan{Beats: []novel.Beat{{Title: "余波", Goal: "追踪新证人"}}}, nil
}

func (s *novelHTTPStub) StartDraftGeneration(_ context.Context, owner novel.Owner, project string, chapter int, input novel.DraftGenerationInput) (novel.DraftGeneration, error) {
	s.draftInput = input
	s.owner, s.draftProject, s.draftChapter = owner, project, chapter
	return s.draftGeneration, s.draftGenerationErr
}

func (s *novelHTTPStub) GetDraftGeneration(_ context.Context, owner novel.Owner, project string, chapter int) (novel.DraftGeneration, error) {
	s.owner, s.draftProject, s.draftChapter = owner, project, chapter
	return s.draftGeneration, s.draftGenerationErr
}

func (s *novelHTTPStub) Apply(_ context.Context, owner novel.Owner, _ string, input novel.Action) (novel.Project, error) {
	s.owner, s.actionInput = owner, input
	if s.actionError != nil {
		return novel.Project{}, s.actionError
	}
	if s.actionResult != nil {
		return *s.actionResult, nil
	}
	return novel.Project{}, novel.ErrConflict
}

func TestNovelDraftGenerationRoutes(t *testing.T) {
	stub := &novelHTTPStub{}
	router := New(Dependencies{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth:   &csrfCheckingAuthStub{&accountAuthStub{session: accountTestSession()}},
		Novel:  stub, SessionCookie: "test_session",
	})
	const project = "2ebf4214-5527-407d-92c0-a9694d1f0926"
	const chapterPath = "/api/v1/ai-novel/projects/" + project + "/chapters/"
	const path = chapterPath + "2/draft-generation"
	const body = `{"version":6,"modelCode":"writer","instruction":"加强悬疑感","ownerId":"other"}`
	send := func(method, path, body string, session, csrf bool) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		if session {
			request.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
		}
		if csrf {
			request.Header.Set("X-CSRF-Token", "csrf-token")
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	for _, tt := range []struct {
		name, method, path, body string
		session, csrf            bool
		status                   int
	}{
		{"匿名查询", http.MethodGet, path, "", false, false, http.StatusUnauthorized},
		{"匿名提交", http.MethodPost, path, body, false, false, http.StatusUnauthorized},
		{"缺少 CSRF", http.MethodPost, path, body, true, false, http.StatusForbidden},
		{"请求格式无效", http.MethodPost, path, "{", true, true, http.StatusBadRequest},
		{"章节不是整数", http.MethodGet, chapterPath + "2.5/draft-generation", "", true, false, http.StatusBadRequest},
		{"章节超出范围", http.MethodPost, chapterPath + "501/draft-generation", body, true, true, http.StatusBadRequest},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if response := send(tt.method, tt.path, tt.body, tt.session, tt.csrf); response.Code != tt.status {
				t.Fatalf("响应状态 %d，预期 %d: %s", response.Code, tt.status, response.Body)
			}
		})
	}
	if stub.draftProject != "" {
		t.Fatal("无效请求不应进入生成服务")
	}
	assertGeneration := func(response *httptest.ResponseRecorder, status int, want map[string]any) {
		t.Helper()
		if response.Code != status || response.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatalf("任务状态响应或缓存头有误: %d %+v %s", response.Code, response.Header(), response.Body)
		}
		var result struct{ Data map[string]any }
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || !reflect.DeepEqual(result.Data, want) {
			t.Fatalf("任务状态响应有误: %s (%v)", response.Body, err)
		}
	}
	stub.draftGeneration = novel.DraftGeneration{Status: "idle"}
	assertGeneration(send(http.MethodGet, path, "", true, false), http.StatusOK, map[string]any{"status": "idle"})
	stub.draftGeneration = novel.DraftGeneration{Status: "running", Phase: "writing", JobID: "new-job"}
	assertGeneration(send(http.MethodPost, path, body, true, true), http.StatusAccepted, map[string]any{
		"status": "running", "phase": "writing", "jobId": "new-job",
	})
	if stub.owner != (novel.Owner{OrganizationID: "org-1", UserID: "user-1"}) || stub.draftProject != project || stub.draftChapter != 2 ||
		stub.draftInput != (novel.DraftGenerationInput{Version: 6, ModelCode: "writer", Instruction: "加强悬疑感"}) {
		t.Fatalf("生成请求未使用会话身份、路径或提交参数: %+v %q %d %+v", stub.owner, stub.draftProject, stub.draftChapter, stub.draftInput)
	}
	stub.draftGeneration = novel.DraftGeneration{Status: "running", Phase: "reviewing", JobID: "new-job", ProjectVersion: 7}
	assertGeneration(send(http.MethodGet, path, "", true, false), http.StatusOK, map[string]any{
		"status": "running", "phase": "reviewing", "jobId": "new-job", "projectVersion": float64(7),
	})
	stub.draftGeneration = novel.DraftGeneration{Status: "succeeded", JobID: "new-job", ProjectVersion: 7, WarningMessage: "正文已保存，审稿未完成。"}
	assertGeneration(send(http.MethodGet, path, "", true, false), http.StatusOK, map[string]any{
		"status": "succeeded", "jobId": "new-job", "projectVersion": float64(7), "warningMessage": "正文已保存，审稿未完成。",
	})
	stub.draftGenerationErr = novel.ErrConflict
	if response := send(http.MethodPost, path, body, true, true); response.Code != http.StatusConflict {
		t.Fatalf("版本冲突未返回 409: %d %s", response.Code, response.Body)
	}
	stub.draftGenerationErr = novel.ErrNotFound
	if response := send(http.MethodGet, path, "", true, false); response.Code != http.StatusNotFound {
		t.Fatalf("不可访问作品未返回 404: %d %s", response.Code, response.Body)
	}
}

func TestNovelOutlineCheckAction(t *testing.T) {
	outline := &novel.Outline{
		Synopsis: "村庄寻找新的水源", World: "旱灾仍在持续", Characters: "阿宁负责救灾",
		Beats: []novel.Beat{{Title: "初见旱情", Goal: "阿宁当日种树解除旱情"}},
	}
	issue := novel.OutlineCheckIssue{Chapter: 1, Message: "树苗无法解除当日旱情", Quote: outline.Beats[0].Goal}
	stub := &novelHTTPStub{actionResult: &novel.Project{Workspace: novel.Workspace{
		Outline:      outline,
		OutlineCheck: &novel.OutlineCheck{Status: novel.OutlineCheckNeedsReview, Issues: []novel.OutlineCheckIssue{issue}},
	}}}
	router := New(Dependencies{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth:   &csrfCheckingAuthStub{&accountAuthStub{session: accountTestSession()}},
		Novel:  stub, SessionCookie: "test_session",
	})
	for _, test := range []struct {
		name      string
		body      string
		withEdits bool
	}{
		{
			name: "检查保存的大纲",
			body: `{"kind":"check_outline","version":2,"modelCode":"writer"}`,
		},
		{
			name:      "检查未保存的修改",
			body:      `{"kind":"check_outline","version":2,"modelCode":"writer","outline":{"synopsis":"村庄寻找新的水源","world":"旱灾仍在持续","characters":"阿宁负责救灾","beats":[{"title":"初见旱情","goal":"阿宁当日种树解除旱情"}]}}`,
			withEdits: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/v1/ai-novel/projects/book/actions", strings.NewReader(test.body))
			request.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
			request.Header.Set("X-CSRF-Token", "csrf-token")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("检查大纲响应有误: %d %s", response.Code, response.Body)
			}
			action := stub.actionInput
			if action.Kind != "check_outline" || action.ModelCode != "writer" || action.Version != 2 ||
				(action.Outline != nil) != test.withEdits {
				t.Fatalf("检查操作未正确传入: %+v", action)
			}
			if test.withEdits && (len(action.Outline.Beats) != 1 || action.Outline.Beats[0] != outline.Beats[0]) {
				t.Fatalf("未传入修改后的章纲: %+v", action.Outline)
			}
			var output struct {
				Data struct {
					OutlineCheck *novel.OutlineCheck `json:"outlineCheck"`
				} `json:"data"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &output); err != nil {
				t.Fatal(err)
			}
			check := output.Data.OutlineCheck
			if check == nil || check.Status != novel.OutlineCheckNeedsReview || check.Repaired || len(check.Issues) != 1 || check.Issues[0] != issue {
				t.Fatalf("检查结果有误: %s", response.Body)
			}
		})
	}
}

func TestNovelReopenActionsAndLargeOutline(t *testing.T) {
	stub := &novelHTTPStub{actionResult: &novel.Project{Version: 2}}
	router := New(Dependencies{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth:   &csrfCheckingAuthStub{&accountAuthStub{session: accountTestSession()}},
		Novel:  stub, SessionCookie: "test_session",
	})
	outline := &novel.Outline{Synopsis: "寻找证据", World: "孤岛", Characters: "林静守塔", Beats: make([]novel.Beat, novel.MaxChapters)}
	for i := range outline.Beats {
		outline.Beats[i] = novel.Beat{Title: "查访", Goal: strings.Repeat("继续查访", 300)}
	}
	for _, input := range []novel.Action{
		{Kind: "reopen_outline", Version: 1},
		{Kind: "reopen_chapter", Version: 2, Chapter: 2},
		{Kind: "confirm_outline", Version: 3, Outline: outline},
	} {
		t.Run(input.Kind, func(t *testing.T) {
			body, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodPost, "/api/v1/ai-novel/projects/book/actions", bytes.NewReader(body))
			request.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
			request.Header.Set("X-CSRF-Token", "csrf-token")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusOK || !reflect.DeepEqual(stub.actionInput, input) {
				t.Fatalf("未接收修改操作: %d %s", response.Code, response.Body)
			}
		})
	}
}

func TestNovelOutlineGenerationReportsChapterCountWithoutExposingProviderErrors(t *testing.T) {
	stub := &novelHTTPStub{}
	router := New(Dependencies{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth:   &csrfCheckingAuthStub{&accountAuthStub{session: accountTestSession()}},
		Novel:  stub, SessionCookie: "test_session",
	})
	for _, test := range []struct {
		name    string
		err     error
		message string
	}{
		{"章数不符", &novel.SerialOpeningCountError{InitialCount: 9, ActualCount: 10, TargetCount: 8},
			"模型首次生成 9 章，调整后返回 10 章，预期 8 章"},
		{"供应商错误", fmt.Errorf("%w: 供应商内部信息", novel.ErrGeneration), novel.ErrGeneration.Error()},
	} {
		t.Run(test.name, func(t *testing.T) {
			stub.actionError = test.err
			request := httptest.NewRequest(http.MethodPost, "/api/v1/ai-novel/projects/book/actions",
				strings.NewReader(`{"kind":"generate_outline","version":1,"modelCode":"writer"}`))
			request.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
			request.Header.Set("X-CSRF-Token", "csrf-token")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if body := response.Body.String(); response.Code != http.StatusBadGateway ||
				!strings.Contains(body, test.message) || strings.Contains(body, "供应商内部信息") {
				t.Fatalf("模型生成错误响应有误: %d %s", response.Code, body)
			}
		})
	}
}

func (s *novelHTTPStub) CoverModels(_ context.Context, owner novel.Owner) ([]novel.Model, error) {
	s.owner = owner
	return []novel.Model{{Code: "art", Name: "测试图片模型"}}, nil
}

func (s *novelHTTPStub) CoverContent(_ context.Context, owner novel.Owner, id, revision string) (novel.CoverImage, error) {
	s.owner = owner
	if id != "book" || (s.cover.Revision != revision && !(revision == "upload-revision" && s.coverData != nil)) {
		return novel.CoverImage{}, novel.ErrNotFound
	}
	return novel.CoverImage{MIME: "image/png", Data: s.coverData}, nil
}

func (s *novelHTTPStub) ListCoverHistory(_ context.Context, owner novel.Owner, id string) ([]novel.CoverHistoryItem, error) {
	s.owner = owner
	if id != "book" {
		return nil, novel.ErrNotFound
	}
	return []novel.CoverHistoryItem{{Revision: "upload-revision", Source: "upload", Current: s.cover.Revision == "upload-revision"}}, nil
}

func (s *novelHTTPStub) ApplyCoverHistory(_ context.Context, owner novel.Owner, id, revision string) (novel.Cover, error) {
	s.owner = owner
	if id != "book" || revision != "upload-revision" {
		return novel.Cover{}, novel.ErrNotFound
	}
	s.cover = novel.Cover{Revision: revision, Source: "upload"}
	return s.cover, nil
}

func (s *novelHTTPStub) UploadCover(_ context.Context, owner novel.Owner, _ string, data []byte) (novel.Cover, error) {
	s.owner, s.coverData = owner, data
	s.cover = novel.Cover{Revision: "upload-revision", Source: "upload"}
	return s.cover, nil
}

func (s *novelHTTPStub) GenerateCover(_ context.Context, owner novel.Owner, _ string, input novel.GenerateCoverInput) (string, error) {
	s.owner, s.coverInput = owner, input
	return "generated-revision", nil
}

func (s *novelHTTPStub) StartCoverGeneration(ctx context.Context, owner novel.Owner, id string, input novel.GenerateCoverInput) (novel.CoverGeneration, error) {
	s.coverInput = input
	return s.GetCoverGeneration(ctx, owner, id)
}

func (s *novelHTTPStub) GetCoverGeneration(_ context.Context, owner novel.Owner, id string) (novel.CoverGeneration, error) {
	s.owner = owner
	if id != "book" {
		return novel.CoverGeneration{}, novel.ErrNotFound
	}
	return s.coverGeneration, s.coverGenerationErr
}

func (s *novelHTTPStub) CoverPreviewContent(_ context.Context, owner novel.Owner, _ string, token string) (novel.CoverImage, error) {
	s.owner = owner
	if token != "generated-revision" {
		return novel.CoverImage{}, novel.ErrCoverPreviewExpired
	}
	return novel.CoverImage{MIME: "image/jpeg", Data: []byte("候选封面")}, nil
}

func (s *novelHTTPStub) ApplyCoverPreview(_ context.Context, owner novel.Owner, _ string, token string) (novel.Cover, error) {
	s.owner = owner
	switch token {
	case "stale-revision":
		return novel.Cover{}, novel.ErrCoverConflict
	case "generated-revision":
	default:
		return novel.Cover{}, novel.ErrCoverPreviewExpired
	}
	s.cover = novel.Cover{Revision: "generated-revision", Source: "ai"}
	return s.cover, nil
}

func TestNovelRoutesRequireSessionAndUseSessionOwner(t *testing.T) {
	stub := &novelHTTPStub{}
	router := New(Dependencies{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth:   &csrfCheckingAuthStub{&accountAuthStub{session: accountTestSession()}},
		Novel:  stub, SessionCookie: "test_session",
	})
	for _, test := range []struct {
		method, path, body string
		session, csrf      bool
		status             int
	}{
		{"GET", "/projects", "", false, false, 401},
		{"GET", "/projects", "", true, false, 200},
		{"GET", "/models", "", true, false, 200},
		{"GET", "/cover-models", "", false, false, 401},
		{"GET", "/cover-models", "", true, false, 200},
		{"POST", "/ideas/refine", "{}", false, false, 401},
		{"POST", "/ideas/refine", "{}", true, false, 403},
		{"POST", "/ideas/refine", "{", true, true, 400},
		{"POST", "/ideas/refine", `{"brief":"守塔人听到呼救","instruction":"加强悬疑感","modelCode":"writer","organizationId":"other","ownerId":"other"}`, true, true, 200},
		{"GET", "/projects/other-book", "", true, false, 404},
		{"POST", "/projects", "{}", true, false, 403},
		{"POST", "/projects", "{", true, true, 400},
		{"POST", "/projects", `{"title":"潮汐档案","organizationId":"other","ownerId":"other"}`, true, true, 201},
		{"POST", "/projects/book/chapter-plan", "{}", false, false, 401},
		{"POST", "/projects/book/chapter-plan", "{}", true, false, 403},
		{"POST", "/projects/book/chapter-plan", `{"version":3,"count":1,"modelCode":"writer","instruction":"换到港口","ownerId":"other"}`, true, true, 200},
		{"POST", "/projects/book/actions", "{}", true, false, 403},
		{"POST", "/projects/book/actions", `{"kind":"finalize","version":1}`, true, true, 409},
	} {
		t.Run(test.method+test.path, func(t *testing.T) {
			stub.owner = novel.Owner{}
			request := httptest.NewRequest(test.method, "/api/v1/ai-novel"+test.path, strings.NewReader(test.body))
			if test.session {
				request.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
			}
			if test.csrf {
				request.Header.Set("X-CSRF-Token", "csrf-token")
			}
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			if recorder.Code != test.status {
				t.Fatalf("响应 %d，预期 %d: %s", recorder.Code, test.status, recorder.Body)
			}
			if test.status != 401 && test.status != 403 && test.status != 400 && stub.owner != (novel.Owner{OrganizationID: "org-1", UserID: "user-1"}) {
				t.Fatalf("未使用会话中的组织和作者: %+v", stub.owner)
			}
			if test.path == "/ideas/refine" && test.status == http.StatusOK {
				want := novel.RefineIdeaInput{Brief: "守塔人听到呼救", Instruction: "加强悬疑感", ModelCode: "writer"}
				if !reflect.DeepEqual(stub.refineInput, want) || !strings.Contains(recorder.Body.String(), `"brief":"一名守塔人追查午夜呼救的来历"`) {
					t.Fatalf("灵感优化请求或预览响应错误: %+v, %s", stub.refineInput, recorder.Body)
				}
			}
			if test.path == "/projects/book/chapter-plan" && test.status == http.StatusOK {
				want := novel.ChapterPlanInput{Version: 3, Count: 1, ModelCode: "writer", Instruction: "换到港口"}
				if stub.planInput != want || !strings.Contains(recorder.Body.String(), `"beats":[{"title":"余波","goal":"追踪新证人"}]`) {
					t.Fatalf("续章规划请求或预览响应错误: %+v, %s", stub.planInput, recorder.Body)
				}
			}
		})
	}
}

func TestNovelRefineIdeaBindsPreviousSuggestionsAndReportsFailures(t *testing.T) {
	stub := &novelHTTPStub{refineResult: &novel.RefineIdeaResult{
		Brief: "守塔人陆徊借观潮录查明午夜呼救的来历", ProtagonistName: "陆徊", SystemName: "观潮录",
	}}
	router := New(Dependencies{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth:   &csrfCheckingAuthStub{&accountAuthStub{session: accountTestSession()}},
		Novel:  stub, SessionCookie: "test_session",
	})
	refine := func() *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/ai-novel/ideas/refine", strings.NewReader(
			`{"brief":"守塔人听到呼救","modelCode":"writer","previousSuggestions":["守塔人贺岩误以为是潮声","旧版在涨潮时开启剧情"]}`))
		request.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
		request.Header.Set("X-CSRF-Token", "csrf-token")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		return recorder
	}
	recorder := refine()
	if recorder.Code != http.StatusOK {
		t.Fatalf("响应 %d，预期 200: %s", recorder.Code, recorder.Body)
	}
	want := novel.RefineIdeaInput{
		Brief: "守塔人听到呼救", ModelCode: "writer",
		PreviousSuggestions: []string{"守塔人贺岩误以为是潮声", "旧版在涨潮时开启剧情"},
	}
	if !reflect.DeepEqual(stub.refineInput, want) {
		t.Fatalf("未绑定历史优化片段，收到 %+v", stub.refineInput)
	}
	for _, field := range []string{`"brief":"守塔人陆徊借观潮录查明午夜呼救的来历"`, `"protagonistName":"陆徊"`, `"systemName":"观潮录"`} {
		if !strings.Contains(recorder.Body.String(), field) {
			t.Fatalf("灵感优化响应缺少 %s: %s", field, recorder.Body)
		}
	}

	const modelDetail = "只供内部排查的模型结果"
	for _, test := range []struct {
		name    string
		err     error
		message string
	}{
		{"优化结果重复", fmt.Errorf("%w: %s", novel.ErrIdeaRepetition, modelDetail), novel.ErrIdeaRepetition.Error()},
		{"优化结果格式错误", fmt.Errorf("灵感优化: %w", &novel.IdeaOutputError{Err: fmt.Errorf("JSON 解析失败: %s", modelDetail)}),
			"模型返回的灵感格式有误，已自动重试。请再生成一次。"},
		{"供应商内容审核中止", fmt.Errorf("%w: %w: %s", novel.ErrGeneration, novel.ErrContentInspection, modelDetail),
			"本次生成被模型供应商的内容审核中止。"},
		{"其他生成错误", fmt.Errorf("%w: %s", novel.ErrGeneration, modelDetail), novel.ErrGeneration.Error()},
	} {
		t.Run(test.name, func(t *testing.T) {
			stub.refineError = test.err
			recorder := refine()
			if body := recorder.Body.String(); recorder.Code != http.StatusBadGateway ||
				!strings.Contains(body, `"code":"novel_generation_failed"`) || !strings.Contains(body, test.message) ||
				strings.Contains(body, modelDetail) {
				t.Fatalf("优化结果错误响应有误: %d %s", recorder.Code, body)
			}
		})
	}
}

func TestNovelCoverRoutesProtectAndVersionImages(t *testing.T) {
	stub := &novelHTTPStub{}
	router := New(Dependencies{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth:   &csrfCheckingAuthStub{&accountAuthStub{session: accountTestSession()}},
		Novel:  stub, SessionCookie: "test_session",
	})
	send := func(method, path string, body io.Reader, contentType string, session, csrf bool) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, "/api/v1/ai-novel"+path, body)
		request.Header.Set("Content-Type", contentType)
		if session {
			request.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
		}
		if csrf {
			request.Header.Set("X-CSRF-Token", "csrf-token")
		}
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		return recorder
	}
	const path = "/projects/book/cover"
	var form bytes.Buffer
	writer := multipart.NewWriter(&form)
	part, err := writer.CreateFormFile("file", "cover.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("封面数据")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if response := send("POST", path, bytes.NewReader(form.Bytes()), writer.FormDataContentType(), true, false); response.Code != http.StatusForbidden {
		t.Fatalf("未拒绝缺少 CSRF 的上传: %d", response.Code)
	}
	if response := send("POST", path, strings.NewReader("{}"), "application/json", true, true); response.Code != http.StatusBadRequest {
		t.Fatalf("未拒绝错误的上传请求格式: %d", response.Code)
	}
	if response := send("POST", path, bytes.NewReader(form.Bytes()), writer.FormDataContentType(), true, true); response.Code != http.StatusOK ||
		!strings.Contains(response.Body.String(), path+"?v=upload-revision") || string(stub.coverData) != "封面数据" {
		t.Fatalf("未收到上传文件及图片地址: %d %s", response.Code, response.Body)
	}
	if response := send("GET", path+"?v=upload-revision", nil, "", false, false); response.Code != http.StatusUnauthorized {
		t.Fatalf("未拒绝匿名下载: %d", response.Code)
	}
	if response := send("GET", path+"?v=upload-revision", nil, "", true, false); response.Code != http.StatusOK ||
		response.Header().Get("Content-Type") != "image/png" || response.Header().Get("Cache-Control") != "private, no-store" ||
		response.Header().Get("X-Content-Type-Options") != "nosniff" || response.Body.String() != "封面数据" {
		t.Fatalf("下载内容或响应头错误: %d %+v %q", response.Code, response.Header(), response.Body)
	}
	input := `{"modelCode":"art","styleId":"ink-wash","instruction":"夜晚的灯塔","ownerId":"other"}`
	if response := send("POST", path+"/generate", strings.NewReader(input), "application/json", true, false); response.Code != http.StatusForbidden {
		t.Fatalf("未拒绝缺少 CSRF 的生成请求: %d", response.Code)
	}
	if response := send("POST", path+"/generate", strings.NewReader(input), "application/json", true, true); response.Code != http.StatusOK ||
		!strings.Contains(response.Body.String(), path+"/previews/generated-revision") ||
		!strings.Contains(response.Body.String(), `"token":"generated-revision"`) ||
		response.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("生成请求未返回新预览: %d %s", response.Code, response.Body)
	}
	if stub.coverInput != (novel.GenerateCoverInput{ModelCode: "art", StyleID: "ink-wash", Instruction: "夜晚的灯塔"}) ||
		stub.owner != (novel.Owner{OrganizationID: "org-1", UserID: "user-1"}) {
		t.Fatalf("生成参数或会话身份错误: %+v %+v", stub.coverInput, stub.owner)
	}
	if response := send("GET", path+"?v=upload-revision", nil, "", true, false); response.Code != http.StatusOK {
		t.Fatalf("生成预览后原封面应可读: %d", response.Code)
	}
	if response := send("GET", path+"/previews/generated-revision", nil, "", false, false); response.Code != http.StatusUnauthorized {
		t.Fatalf("未拒绝匿名下载预览: %d", response.Code)
	}
	if response := send("GET", path+"/previews/generated-revision", nil, "", true, false); response.Code != http.StatusOK ||
		response.Header().Get("Content-Type") != "image/jpeg" || response.Header().Get("Cache-Control") != "private, no-store" ||
		response.Header().Get("X-Content-Type-Options") != "nosniff" || response.Body.String() != "候选封面" {
		t.Fatalf("预览图片或响应头错误: %d %+v %q", response.Code, response.Header(), response.Body)
	}
	if response := send("POST", path+"/apply", strings.NewReader(`{"token":"expired"}`), "application/json", true, true); response.Code != http.StatusNotFound ||
		!strings.Contains(response.Body.String(), `"novel_cover_preview_expired"`) {
		t.Fatalf("过期的候选未返回对应错误: %d %s", response.Code, response.Body)
	}
	if response := send("POST", path+"/apply", strings.NewReader(`{"token":"stale-revision"}`), "application/json", true, true); response.Code != http.StatusConflict ||
		!strings.Contains(response.Body.String(), `"novel_cover_conflict"`) {
		t.Fatalf("封面版本冲突未返回对应错误: %d %s", response.Code, response.Body)
	}
	const apply = `{"token":"generated-revision"}`
	if response := send("POST", path+"/apply", strings.NewReader(apply), "application/json", true, false); response.Code != http.StatusForbidden {
		t.Fatalf("未拒绝缺少 CSRF 的确认: %d", response.Code)
	}
	if response := send("POST", path+"/apply", strings.NewReader(apply), "application/json", true, true); response.Code != http.StatusOK ||
		!strings.Contains(response.Body.String(), path+"?v=generated-revision") || !strings.Contains(response.Body.String(), `"source":"ai"`) {
		t.Fatalf("应用未返回新的封面: %d %s", response.Code, response.Body)
	}
	if response := send("GET", path+"?v=upload-revision", nil, "", true, false); response.Code != http.StatusOK || response.Body.String() != "封面数据" {
		t.Fatalf("已更换版本应返回历史图片: %d %s", response.Code, response.Body)
	}
	if response := send("GET", "/projects", nil, "", true, false); response.Code != http.StatusOK ||
		!strings.Contains(response.Body.String(), path+"?v=generated-revision") || strings.Contains(response.Body.String(), `"revision"`) {
		t.Fatalf("作品列表未使用新封面地址: %d %s", response.Code, response.Body)
	}
	const history = path + "/history"
	if response := send("GET", history, nil, "", false, false); response.Code != http.StatusUnauthorized {
		t.Fatalf("未拒绝匿名查看历史: %d", response.Code)
	}
	if response := send("GET", history, nil, "", true, false); response.Code != http.StatusOK ||
		!strings.Contains(response.Body.String(), path+"?v=upload-revision") || !strings.Contains(response.Body.String(), `"current":false`) ||
		response.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("历史列表未返回对应版本地址: %d %s", response.Code, response.Body)
	}
	const restore = `{"revision":"upload-revision","ownerId":"other"}`
	if response := send("POST", history+"/apply", strings.NewReader(restore), "application/json", true, false); response.Code != http.StatusForbidden {
		t.Fatalf("未拒绝缺少 CSRF 的恢复: %d", response.Code)
	}
	if response := send("POST", history+"/apply", strings.NewReader(restore), "application/json", true, true); response.Code != http.StatusOK ||
		!strings.Contains(response.Body.String(), path+"?v=upload-revision") || !strings.Contains(response.Body.String(), `"source":"upload"`) ||
		stub.owner != (novel.Owner{OrganizationID: "org-1", UserID: "user-1"}) {
		t.Fatalf("恢复历史封面未使用会话身份及指定版本: %d %s", response.Code, response.Body)
	}
}

func TestNovelCoverGenerationRoutes(t *testing.T) {
	stub := &novelHTTPStub{}
	router := New(Dependencies{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth:   &csrfCheckingAuthStub{&accountAuthStub{session: accountTestSession()}},
		Novel:  stub, SessionCookie: "test_session",
	})
	const path = "/api/v1/ai-novel/projects/book/cover/generation"
	const input = `{"modelCode":"art","styleId":"ink-wash","instruction":"夜晚的灯塔","ownerId":"other"}`
	send := func(method, path, body string, session, csrf bool) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		if session {
			request.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
		}
		if csrf {
			request.Header.Set("X-CSRF-Token", "csrf-token")
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	for _, tt := range []struct {
		name, method, path, body string
		session, csrf            bool
		status                   int
	}{
		{"匿名查询", http.MethodGet, path, "", false, false, http.StatusUnauthorized},
		{"匿名提交", http.MethodPost, path, input, false, false, http.StatusUnauthorized},
		{"缺少 CSRF", http.MethodPost, path, input, true, false, http.StatusForbidden},
		{"请求格式无效", http.MethodPost, path, "{", true, true, http.StatusBadRequest},
		{"其他作品", http.MethodGet, strings.Replace(path, "/book/", "/other-book/", 1), "", true, false, http.StatusNotFound},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if response := send(tt.method, tt.path, tt.body, tt.session, tt.csrf); response.Code != tt.status {
				t.Fatalf("响应状态 %d，期望 %d: %s", response.Code, tt.status, response.Body)
			}
		})
	}
	assertGeneration := func(response *httptest.ResponseRecorder, status int, want map[string]any) {
		t.Helper()
		if response.Code != status || response.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatalf("生成状态响应或缓存头不符: %d %+v %s", response.Code, response.Header(), response.Body)
		}
		var result struct{ Data map[string]any }
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || !reflect.DeepEqual(result.Data, want) {
			t.Fatalf("生成状态响应错误: %s (%v)", response.Body, err)
		}
	}
	stub.coverGeneration.Status = novel.CoverGenerationIdle
	assertGeneration(send(http.MethodGet, path, "", true, false), http.StatusOK, map[string]any{"status": "idle"})
	stub.coverGeneration = novel.CoverGeneration{
		Status: novel.CoverGenerationRunning, JobID: "new-job", ModelCode: "art", StyleID: "ink-wash", Instruction: "夜晚的灯塔",
	}
	running := map[string]any{
		"status": "running", "jobId": "new-job", "modelCode": "art", "styleId": "ink-wash", "instruction": "夜晚的灯塔",
	}
	assertGeneration(send(http.MethodPost, path, input, true, true), http.StatusAccepted, running)
	if stub.coverInput != (novel.GenerateCoverInput{ModelCode: "art", StyleID: "ink-wash", Instruction: "夜晚的灯塔"}) ||
		stub.owner != (novel.Owner{OrganizationID: "org-1", UserID: "user-1"}) {
		t.Fatalf("任务未使用请求参数和会话身份: %+v %+v", stub.coverInput, stub.owner)
	}
	assertGeneration(send(http.MethodGet, path, "", true, false), http.StatusOK, running)
	preview := map[string]any{"token": "generated-revision", "previewUrl": "/api/v1/ai-novel/projects/book/cover/previews/generated-revision"}
	stub.coverGeneration = novel.CoverGeneration{
		Status: novel.CoverGenerationSucceeded, JobID: "new-job", PreviewToken: "generated-revision",
	}
	assertGeneration(send(http.MethodGet, path, "", true, false), http.StatusOK, map[string]any{
		"status": "succeeded", "jobId": "new-job", "preview": preview,
	})
	stub.coverGeneration = novel.CoverGeneration{
		Status: novel.CoverGenerationFailed, JobID: "next-job", PreviewToken: "generated-revision", ErrorMessage: "封面生成失败，请重试",
	}
	assertGeneration(send(http.MethodGet, path, "", true, false), http.StatusOK, map[string]any{
		"status": "failed", "jobId": "next-job", "errorMessage": "封面生成失败，请重试", "preview": preview,
	})
	stub.coverGenerationErr = novel.ErrCoverStyle
	if response := send(http.MethodPost, path, input, true, true); response.Code != http.StatusBadRequest {
		t.Fatalf("无效风格未返回参数错误: %d %s", response.Code, response.Body)
	}
}
