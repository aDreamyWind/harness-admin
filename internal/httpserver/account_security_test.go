package httpserver

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"harness-admin/internal/identity"
	"harness-admin/internal/useradmin"
)

type accountAuthStub struct {
	session             identity.Session
	changePasswordInput identity.ChangePasswordInput
	sessions            []identity.UserSession
}

func (*accountAuthStub) Login(context.Context, identity.LoginInput) (identity.LoginResult, error) {
	return identity.LoginResult{}, nil
}

func (s *accountAuthStub) Authenticate(context.Context, string) (identity.Session, error) {
	return s.session, nil
}

func (*accountAuthStub) ValidateCSRF(identity.Session, string) error { return nil }
func (*accountAuthStub) Logout(context.Context, identity.Session, string, string) error {
	return nil
}

func (s *accountAuthStub) ChangePassword(_ context.Context, input identity.ChangePasswordInput) (int64, error) {
	s.changePasswordInput = input
	return 2, nil
}

func (s *accountAuthStub) ListSessions(context.Context, identity.Session) ([]identity.UserSession, error) {
	return s.sessions, nil
}

func (*accountAuthStub) RevokeManagedSession(context.Context, identity.Session, string, string, string) error {
	return nil
}

func (*accountAuthStub) RevokeOtherSessions(context.Context, identity.Session, string, string) (int64, error) {
	return 0, nil
}

type accountUserAdminStub struct {
	resetInput useradmin.ResetPasswordInput
	resetErr   error
}

func (*accountUserAdminStub) List(context.Context, useradmin.ListFilter) (useradmin.UserList, error) {
	return useradmin.UserList{}, nil
}

func (*accountUserAdminStub) Create(context.Context, useradmin.CreateInput) (useradmin.User, error) {
	return useradmin.User{}, nil
}

func (*accountUserAdminStub) ChangeStatus(context.Context, useradmin.ChangeStatusInput) (useradmin.User, error) {
	return useradmin.User{}, nil
}

func (s *accountUserAdminStub) ResetPassword(_ context.Context, input useradmin.ResetPasswordInput) error {
	s.resetInput = input
	return s.resetErr
}

type accountAccessStub struct{}

func (accountAccessStub) Allowed(context.Context, string, string) (bool, error) { return true, nil }

func TestChangePasswordHandlerUsesCurrentSession(t *testing.T) {
	auth := &accountAuthStub{session: accountTestSession()}
	router := newAccountSecurityTestRouter(auth, nil)
	request := httptest.NewRequest(http.MethodPut, "/api/v1/auth/password",
		strings.NewReader(`{"currentPassword":"123","newPassword":"456"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", "csrf-token")
	request.Header.Set("X-Request-ID", "request-1")
	request.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 %d；响应 = %s", response.Code, http.StatusOK, response.Body.String())
	}
	input := auth.changePasswordInput
	if input.Session.ID != "session-1" || input.Session.User.ID != "user-1" || input.CurrentPassword != "123" || input.NewPassword != "456" || input.RequestID != "request-1" {
		t.Fatalf("修改密码参数不完整: %#v", input)
	}
}

func TestMeHandlerReturnsCurrentPermissions(t *testing.T) {
	auth := &accountAuthStub{session: accountTestSession()}
	auth.session.User.PermissionCodes = []string{"audit.event.read", "iam.user.read"}
	router := newAccountSecurityTestRouter(auth, nil)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	request.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 %d；响应 = %s", response.Code, http.StatusOK, response.Body.String())
	}
	var body struct {
		Data struct {
			PermissionCodes []string `json:"permissionCodes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data.PermissionCodes) != 2 || body.Data.PermissionCodes[1] != "iam.user.read" {
		t.Fatalf("当前用户权限响应不正确: %s", response.Body.String())
	}
}

func TestListSessionsHandlerReturnsCurrentSessionMarker(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	auth := &accountAuthStub{
		session: accountTestSession(),
		sessions: []identity.UserSession{{
			ID: "session-1", UserAgent: "test-browser", SourceIP: "127.0.0.1",
			ExpiresAt: now.Add(time.Hour), LastSeenAt: now, CreatedAt: now.Add(-time.Minute), Current: true,
		}},
	}
	router := newAccountSecurityTestRouter(auth, nil)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/sessions", nil)
	request.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 %d；响应 = %s", response.Code, http.StatusOK, response.Body.String())
	}
	var body struct {
		Data []struct {
			ID      string `json:"id"`
			Current bool   `json:"current"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data) != 1 || body.Data[0].ID != "session-1" || !body.Data[0].Current {
		t.Fatalf("会话响应不正确: %s", response.Body.String())
	}
}

func TestResetUserPasswordHandlerUsesCurrentOrganizationAndActor(t *testing.T) {
	auth := &accountAuthStub{session: accountTestSession()}
	users := &accountUserAdminStub{}
	router := newAccountSecurityTestRouter(auth, users)
	request := httptest.NewRequest(http.MethodPut, "/api/v1/users/917e4f53-3cb0-4df2-8027-c0a49fd58d25/password",
		strings.NewReader(`{"password":"789"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", "csrf-token")
	request.Header.Set("X-Request-ID", "request-2")
	request.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 %d；响应 = %s", response.Code, http.StatusOK, response.Body.String())
	}
	input := users.resetInput
	if input.OrganizationID != "org-1" || input.ActorID != "user-1" || input.Password != "789" || input.RequestID != "request-2" {
		t.Fatalf("重置密码参数不完整: %#v", input)
	}
}

func accountTestSession() identity.Session {
	return identity.Session{
		ID:   "session-1",
		User: identity.User{ID: "user-1", OrganizationID: "org-1", Status: "active"},
	}
}

func newAccountSecurityTestRouter(auth AuthService, users UserAdminService) http.Handler {
	return New(Dependencies{
		AppName:       "Harness Admin",
		Environment:   "test",
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth:          auth,
		Access:        accountAccessStub{},
		UserAdmin:     users,
		SessionCookie: "test_session",
	})
}
