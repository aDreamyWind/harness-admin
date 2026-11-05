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

type canvasMediaRouteStub struct {
	uploadScope []string
	readScope   []string
	err         error
}

type canvasMediaReadCloser struct{ *strings.Reader }

func (canvasMediaReadCloser) Close() error { return nil }

func (s *canvasMediaRouteStub) UploadCanvasMedia(_ context.Context, org, user, project string, file io.Reader) (drama.CanvasMedia, error) {
	data, err := io.ReadAll(file)
	if err != nil {
		return drama.CanvasMedia{}, err
	}
	s.uploadScope = []string{org, user, project, string(data)}
	return drama.CanvasMedia{ID: "media-id", Kind: "audio", MIME: "audio/mpeg"}, s.err
}

func (s *canvasMediaRouteStub) ReadCanvasMedia(_ context.Context, org, user, project, id string) (drama.CanvasMediaContent, error) {
	s.readScope = []string{org, user, project, id}
	return drama.CanvasMediaContent{File: canvasMediaReadCloser{strings.NewReader("123456789")}, MIME: "audio/mpeg"}, s.err
}

func TestDramaCanvasMediaRoutesAuthorizeAndServeByteRange(t *testing.T) {
	service := &canvasMediaRouteStub{}
	session := accountTestSession()
	router := New(Dependencies{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Auth: &csrfCheckingAuthStub{&accountAuthStub{session: session}},
		DramaCanvasMedia: service, SessionCookie: "session",
	})
	const base = "/api/v1/ai-drama/projects/project/canvas/media"
	var body bytes.Buffer
	upload := multipart.NewWriter(&body)
	part, err := upload.CreateFormFile("file", "sample.mp3")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(part, "123456789"); err != nil {
		t.Fatal(err)
	}
	if err := upload.Close(); err != nil {
		t.Fatal(err)
	}
	post := func(auth, csrf bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, base, bytes.NewReader(body.Bytes()))
		req.Header.Set("Content-Type", upload.FormDataContentType())
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
	if post(false, false).Code != http.StatusUnauthorized || post(true, false).Code != http.StatusForbidden || service.uploadScope != nil {
		t.Fatal("上传没有先校验会话和 CSRF")
	}
	if out := post(true, true); out.Code != http.StatusCreated || !strings.Contains(out.Body.String(), base+"/media-id/content") {
		t.Fatalf("上传没有返回可保存的素材地址: %d %s", out.Code, out.Body)
	}
	if got := strings.Join(service.uploadScope, " "); got != session.User.OrganizationID+" "+session.User.ID+" project 123456789" {
		t.Fatalf("上传没有使用会话项目范围: %q", got)
	}
	service.err = drama.ErrCanvasMediaType
	if out := post(true, true); out.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("错误素材格式未返回 415: %d", out.Code)
	}
	service.err = drama.ErrCanvasMediaSize
	if out := post(true, true); out.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("错误素材大小未返回 413: %d", out.Code)
	}
	service.err = nil
	get := func(auth bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, base+"/media-id/content", nil)
		req.Header.Set("Range", "bytes=2-4")
		if auth {
			req.AddCookie(&http.Cookie{Name: "session", Value: "token"})
		}
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		return out
	}
	if out := get(false); out.Code != http.StatusUnauthorized || service.readScope != nil {
		t.Fatal("无需会话即可读取素材")
	}
	if out := get(true); out.Code != http.StatusPartialContent || out.Body.String() != "345" || out.Header().Get("Content-Type") != "audio/mpeg" {
		t.Fatalf("播放范围未正确读取: %d %s", out.Code, out.Body)
	}
	if got := strings.Join(service.readScope, " "); got != session.User.OrganizationID+" "+session.User.ID+" project media-id" {
		t.Fatalf("读取没有使用会话项目范围: %q", got)
	}
}
