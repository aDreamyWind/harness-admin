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

type storyboardHTTPStub struct {
	scope     []string
	err       error
	generated chan bool
}

func (s *storyboardHTTPStub) Storyboards(_ context.Context, o, u, p, e, r string) (drama.StoryboardPage, error) {
	s.scope = []string{o, u, p, e, r}
	return drama.StoryboardPage{Runs: []drama.StoryboardRun{}, Forms: []drama.AssetForm{}}, s.err
}
func (s *storyboardHTTPStub) StartStoryboard(_ context.Context, o, u, p, e string, resume bool) (drama.StoryboardJob, error) {
	s.scope = []string{o, u, p, e}
	return drama.StoryboardJob{Run: drama.StoryboardRun{ID: "run"}}, s.err
}
func (s *storyboardHTTPStub) StartStoryboardPacing(_ context.Context, o, u, p, e, r string, in drama.StoryboardPacingRequest) (drama.StoryboardJob, error) {
	s.scope = []string{o, u, p, e, r, in.SegmentID}
	return drama.StoryboardJob{Run: drama.StoryboardRun{ID: "optimized-run"}}, s.err
}
func (s *storyboardHTTPStub) GenerateStoryboard(drama.StoryboardJob) error {
	s.generated <- true
	return nil
}
func (s *storyboardHTTPStub) EditStoryboard(_ context.Context, o, u, p, e, r string, in drama.StoryboardEdit) (drama.StoryboardRun, error) {
	s.scope = []string{o, u, p, e, r}
	return drama.StoryboardRun{ID: r}, s.err
}
func (s *storyboardHTTPStub) ConfirmStoryboard(_ context.Context, o, u, p, e, r string, revision int) error {
	s.scope = []string{o, u, p, e, r}
	return s.err
}
func TestStoryboardRoutesSessionCSRFAndScope(t *testing.T) {
	s := &storyboardHTTPStub{generated: make(chan bool, 4)}
	session := accountTestSession()
	router := New(Dependencies{AppName: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Auth: &csrfCheckingAuthStub{&accountAuthStub{session: session}}, DramaStoryboards: s, SessionCookie: "session"})
	base := "/api/v1/ai-drama/projects/b6e6da1c-7c27-4f72-babf-34a7c4e19617/video-episodes/3a364763-e0df-4cb4-9c55-88ab0a11ff94/storyboards"
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{{"GET", base, "", 200}, {"POST", base, `{"resume":true}`, 202}, {"PUT", base + "/run", `{"revision":1,"segments":[]}`, 200}, {"POST", base + "/run/confirm", `{"revision":1}`, 200}, {"POST", base + "/run/optimize-pacing", `{"revision":1,"segmentId":"3a364763-e0df-4cb4-9c55-88ab0a11ff94"}`, 202}} {
		call := func(auth, csrf bool) *httptest.ResponseRecorder {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
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
		if out := call(false, false); out.Code != 401 {
			t.Fatal("未登录未拒绝", out.Code)
		}
		if tc.method != "GET" {
			if out := call(true, false); out.Code != 403 {
				t.Fatal("缺少CSRF未拒绝", out.Code)
			}
		}
		if out := call(true, true); out.Code != tc.status {
			t.Fatal(out.Code, out.Body.String())
		}
		if s.scope[0] != session.User.OrganizationID || s.scope[1] != session.User.ID {
			t.Fatal("身份范围不正确")
		}
		s.err = drama.ErrStoryboardConflict
		if out := call(true, true); out.Code != 409 {
			t.Fatal("冲突状态错误", out.Code)
		}
		s.err = nil
	}
	<-s.generated
}
