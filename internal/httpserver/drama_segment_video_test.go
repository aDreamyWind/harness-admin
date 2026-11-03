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

	"harness-admin/internal/drama"
)

type videoTestFile struct{ *strings.Reader }

func (videoTestFile) Close() error { return nil }

type segmentVideoHTTPStub struct {
	scope     drama.SegmentVideoScope
	generated chan string
}

func (s *segmentVideoHTTPStub) SegmentVideos(_ context.Context, scope drama.SegmentVideoScope) ([]drama.SegmentVideo, error) {
	s.scope = scope
	return []drama.SegmentVideo{}, nil
}
func (s *segmentVideoHTTPStub) StartSegmentVideo(_ context.Context, scope drama.SegmentVideoScope, revision int) (drama.SegmentVideoJob, error) {
	s.scope = scope
	if revision != 2 {
		return drama.SegmentVideoJob{}, drama.ErrStoryboardConflict
	}
	return drama.SegmentVideoJob{Video: drama.SegmentVideo{ID: "video", Status: "running"}}, nil
}
func (s *segmentVideoHTTPStub) GenerateSegmentVideo(job drama.SegmentVideoJob) error {
	s.generated <- job.Video.ID
	return nil
}
func (s *segmentVideoHTTPStub) SegmentVideoContent(_ context.Context, scope drama.SegmentVideoScope, id string) (io.ReadSeekCloser, time.Time, error) {
	s.scope = scope
	if id != "video" {
		return nil, time.Time{}, drama.ErrNotFound
	}
	return videoTestFile{strings.NewReader("0123456789")}, time.Time{}, nil
}

func TestSegmentVideoRoutesEnforceSessionCSRFAndServeRanges(t *testing.T) {
	stub := &segmentVideoHTTPStub{generated: make(chan string, 1)}
	session := accountTestSession()
	router := New(Dependencies{AppName: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Auth: &csrfCheckingAuthStub{&accountAuthStub{session: session}}, DramaSegmentVideos: stub, SessionCookie: "session"})
	base := "/api/v1/ai-drama/projects/project/video-episodes/episode/storyboards/run/segments/segment/videos"
	for _, tc := range []struct {
		method, path, body string
		auth, csrf         bool
		status             int
	}{
		{"GET", base, "", false, false, 401},
		{"GET", base, "", true, false, 200},
		{"POST", base, `{"revision":2}`, true, false, 403},
		{"POST", base, `{"revision":1}`, true, true, 409},
		{"POST", base, `{"revision":2}`, true, true, 202},
		{"GET", base + "/video/content", "", true, false, 206},
		{"GET", base + "/missing/content", "", true, false, 404},
	} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		if tc.auth {
			req.AddCookie(&http.Cookie{Name: "session", Value: "token"})
		}
		if tc.csrf {
			req.Header.Set("X-CSRF-Token", "csrf-token")
		}
		if tc.status == 206 {
			req.Header.Set("Range", "bytes=2-4")
		}
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		if out.Code != tc.status {
			t.Fatalf("%s %s：%d %s", tc.method, tc.path, out.Code, out.Body.String())
		}
		if tc.status == 206 && (out.Body.String() != "234" || out.Header().Get("Content-Type") != "video/mp4") {
			t.Fatal("视频范围请求返回错误")
		}
	}
	if stub.scope.OrganizationID != session.User.OrganizationID || stub.scope.UserID != session.User.ID || stub.scope.SegmentID != "segment" || stub.scope.RunID != "run" {
		t.Fatalf("视频查询范围错误：%+v", stub.scope)
	}
	select {
	case id := <-stub.generated:
		if id != "video" {
			t.Fatal(id)
		}
	case <-time.After(time.Second):
		t.Fatal("未启动后台任务")
	}
}
