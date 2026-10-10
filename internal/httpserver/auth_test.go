package httpserver

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestAuthCookiesUseSeparateSecurityBoundaries(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	expiresAt := time.Now().Add(time.Hour)
	deps := Dependencies{Environment: "production", SessionCookie: "admin_session"}

	setSessionCookie(ctx, deps, "session-token", expiresAt)
	setCSRFCookie(ctx, deps, "csrf-token", expiresAt)

	cookies := recorder.Result().Cookies()
	if len(cookies) != 2 {
		t.Fatalf("Cookie 数量 = %d，期望 2", len(cookies))
	}
	sessionCookie := cookies[0]
	if sessionCookie.Name != "admin_session" || sessionCookie.Value != "session-token" {
		t.Fatalf("会话 Cookie 不正确: %#v", sessionCookie)
	}
	if !sessionCookie.HttpOnly || !sessionCookie.Secure || sessionCookie.Path != "/api" {
		t.Fatalf("会话 Cookie 安全属性不正确: %#v", sessionCookie)
	}

	csrfCookie := cookies[1]
	if csrfCookie.Name != csrfCookieName || csrfCookie.Value != "csrf-token" {
		t.Fatalf("CSRF Cookie 不正确: %#v", csrfCookie)
	}
	if csrfCookie.HttpOnly || !csrfCookie.Secure || csrfCookie.Path != "/" {
		t.Fatalf("CSRF Cookie 安全属性不正确: %#v", csrfCookie)
	}
}

func TestClearAuthCookies(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	deps := Dependencies{Environment: "development", SessionCookie: "admin_session"}

	clearSessionCookie(ctx, deps)
	clearCSRFCookie(ctx, deps)

	for _, cookie := range recorder.Result().Cookies() {
		if cookie.MaxAge >= 0 {
			t.Fatalf("Cookie %q 未被标记为删除: %#v", cookie.Name, cookie)
		}
	}
}
