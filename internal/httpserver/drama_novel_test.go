package httpserver

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"harness-admin/internal/novel"
)

type dramaNovelHTTPStub struct {
	novelHTTPStub
	project novel.Project
}

func (s *dramaNovelHTTPStub) Get(_ context.Context, owner novel.Owner, id string) (novel.Project, error) {
	s.owner = owner
	if owner != (novel.Owner{OrganizationID: "org-1", UserID: "user-1"}) || id != s.project.ID {
		return novel.Project{}, novel.ErrNotFound
	}
	return s.project, nil
}

func TestDramaCreateFromNovelUsesOwnedFinalizedSnapshot(t *testing.T) {
	const id = "2ebf4214-5527-407d-92c0-a9694d1f0926"
	novels := &dramaNovelHTTPStub{project: novel.Project{
		Book: novel.Book{ID: id, Title: "雾海拾音", ChapterCount: 4},
		Workspace: novel.Workspace{
			Outline: &novel.Outline{Beats: []novel.Beat{{Title: "岸边"}, {Title: "过海"}, {Title: "灯塔"}, {Title: "天光"}}},
			Chapters: []novel.Chapter{
				{Body: "未选中的定稿正文", Final: true},
				{Body: "船夫开船。\n潮水涨起。", Summary: "未导出的章节摘要", Final: true},
				{Body: "灯塔亮了。", Final: true},
				{Body: "尚未定稿的正文"},
			},
		},
	}}
	dramaService := &dramaServiceStub{started: make(chan struct{}, 1)}
	router := New(Dependencies{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth:   &csrfCheckingAuthStub{&accountAuthStub{session: accountTestSession()}},
		Drama:  dramaService, Novel: novels, SessionCookie: "test_session",
	})
	const createPath = "/api/v1/ai-drama/projects/from-novel"
	const validBody = `{"novelId":"` + id + `","startChapter":2,"endChapter":3,"aspectRatio":"9:16","style":"国风","sourceText":"伪造原文","ownerId":"other"}`
	call := func(method, path, body string, session, csrf bool) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
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
	for _, tc := range []struct {
		name          string
		body          string
		session, csrf bool
		status        int
	}{
		{"未登录", validBody, false, false, http.StatusUnauthorized},
		{"缺少 CSRF", validBody, true, false, http.StatusForbidden},
		{"他人的作品", strings.Replace(validBody, id, "544be6bf-fd12-48a2-aa93-f20b8cd16cab", 1), true, true, http.StatusNotFound},
		{"逆序章节", strings.Replace(validBody, `"startChapter":2`, `"startChapter":4`, 1), true, true, http.StatusBadRequest},
		{"包含草稿", strings.Replace(validBody, `"endChapter":3`, `"endChapter":4`, 1), true, true, http.StatusConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := call(http.MethodPost, createPath, tc.body, tc.session, tc.csrf)
			if res.Code != tc.status || dramaService.created.SourceText != "" {
				t.Fatalf("拒绝请求时写入了项目或返回状态错误: %d %s", res.Code, res.Body.String())
			}
		})
	}
	chaptersPath := "/api/v1/ai-novel/projects/" + id + "/finalized-chapters"
	if res := call(http.MethodGet, chaptersPath, "", false, false); res.Code != http.StatusUnauthorized {
		t.Fatalf("未登录仍可读取章节: %d", res.Code)
	}
	res := call(http.MethodGet, chaptersPath, "", true, false)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `"data":[{"number":1,"title":"岸边"},{"number":2,"title":"过海"},{"number":3,"title":"灯塔"}]`) {
		t.Fatalf("章节接口应只返回定稿元数据: %d %s", res.Code, res.Body.String())
	}
	res = call(http.MethodPost, createPath, validBody, true, true)
	if res.Code != http.StatusCreated {
		t.Fatalf("从小说创建失败: %d %s", res.Code, res.Body.String())
	}
	created := dramaService.created
	if created.OrganizationID != "org-1" || created.OwnerID != "user-1" || created.Title != "雾海拾音" ||
		created.SourceName != "《雾海拾音》第 2 至 3 章" || created.SourceMode != "upload" ||
		created.SourceText != "第2章 过海\n\n船夫开船。\n潮水涨起。\n\n第3章 灯塔\n\n灯塔亮了。" {
		t.Fatalf("未按会话与定稿范围保存原文快照: %+v", created)
	}
	select {
	case <-dramaService.started:
	case <-time.After(time.Second):
		t.Fatal("创建后未启动概览解析")
	}
}
