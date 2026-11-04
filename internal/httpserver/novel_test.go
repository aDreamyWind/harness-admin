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

	"harness-admin/internal/novel"
)

type novelHTTPStub struct {
	owner       novel.Owner
	createInput novel.CreateInput
	refineInput novel.RefineIdeaInput
	planInput   novel.ChapterPlanInput
	coverInput  novel.GenerateCoverInput
	cover       novel.Cover
	coverData   []byte
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
	return novel.RefineIdeaResult{Brief: "一名守塔人追查午夜呼救的来历"}, nil
}
func (s *novelHTTPStub) Create(_ context.Context, owner novel.Owner, input novel.CreateInput) (novel.Project, error) {
	s.owner = owner
	s.createInput = input
	return novel.Project{Book: novel.Book{Title: input.Title}}, nil
}

func TestNovelCreateBindsIdeaModeWithoutTitle(t *testing.T) {
	stub := &novelHTTPStub{}
	router := New(Dependencies{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth:   &csrfCheckingAuthStub{&accountAuthStub{session: accountTestSession()}},
		Novel:  stub, SessionCookie: "test_session",
	})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/ai-novel/projects", strings.NewReader(`{"brief":"守塔人听到呼救","chapterCount":3,"suggestTitle":true}`))
	request.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
	request.Header.Set("X-CSRF-Token", "csrf-token")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated || stub.createInput != (novel.CreateInput{Brief: "守塔人听到呼救", ChapterCount: 3, SuggestTitle: true}) {
		t.Fatalf("未接收灵感创作参数，响应 %d，参数 %+v", recorder.Code, stub.createInput)
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
func (s *novelHTTPStub) Apply(_ context.Context, owner novel.Owner, _ string, _ novel.Action) (novel.Project, error) {
	s.owner = owner
	return novel.Project{}, novel.ErrConflict
}

func (s *novelHTTPStub) CoverModels(_ context.Context, owner novel.Owner) ([]novel.Model, error) {
	s.owner = owner
	return []novel.Model{{Code: "art", Name: "测试图片模型"}}, nil
}

func (s *novelHTTPStub) CoverContent(_ context.Context, owner novel.Owner, id, revision string) (novel.CoverImage, error) {
	s.owner = owner
	if id != "book" || s.cover.Revision != revision {
		return novel.CoverImage{}, novel.ErrNotFound
	}
	return novel.CoverImage{MIME: "image/png", Data: s.coverData}, nil
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
				if stub.refineInput != want || !strings.Contains(recorder.Body.String(), `"brief":"一名守塔人追查午夜呼救的来历"`) {
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
	input := `{"modelCode":"art","instruction":"夜晚的灯塔","ownerId":"other"}`
	if response := send("POST", path+"/generate", strings.NewReader(input), "application/json", true, false); response.Code != http.StatusForbidden {
		t.Fatalf("未拒绝缺少 CSRF 的生成请求: %d", response.Code)
	}
	if response := send("POST", path+"/generate", strings.NewReader(input), "application/json", true, true); response.Code != http.StatusOK ||
		!strings.Contains(response.Body.String(), path+"/previews/generated-revision") ||
		!strings.Contains(response.Body.String(), `"token":"generated-revision"`) ||
		response.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("生成请求未返回新预览: %d %s", response.Code, response.Body)
	}
	if stub.coverInput != (novel.GenerateCoverInput{ModelCode: "art", Instruction: "夜晚的灯塔"}) ||
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
	if response := send("GET", path+"?v=upload-revision", nil, "", true, false); response.Code != http.StatusNotFound {
		t.Fatalf("已更换版本不应返回新的图片: %d", response.Code)
	}
	if response := send("GET", "/projects", nil, "", true, false); response.Code != http.StatusOK ||
		!strings.Contains(response.Body.String(), path+"?v=generated-revision") || strings.Contains(response.Body.String(), `"revision"`) {
		t.Fatalf("作品列表未使用新封面地址: %d %s", response.Code, response.Body)
	}
}
