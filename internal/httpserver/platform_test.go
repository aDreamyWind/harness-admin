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
	"time"

	"harness-admin/internal/platformadmin"
)

type platformServiceStub struct {
	createInput platformadmin.CreateOrganizationInput
}

func (s *platformServiceStub) Login(context.Context, platformadmin.LoginInput) (platformadmin.LoginResult, error) {
	return platformadmin.LoginResult{
		Admin: platformadmin.Admin{ID: "platform-1", Username: "platform-admin", DisplayName: "平台管理员"},
		Token: "session-token", CSRFToken: "csrf-token", ExpiresAt: time.Now().Add(time.Hour),
	}, nil
}
func (s *platformServiceStub) Authenticate(context.Context, string) (platformadmin.Session, error) {
	return platformadmin.Session{ID: "session-1", Admin: platformadmin.Admin{ID: "platform-1", Username: "platform-admin", DisplayName: "平台管理员"}}, nil
}
func (s *platformServiceStub) ValidateCSRF(platformadmin.Session, string) error { return nil }
func (s *platformServiceStub) Logout(context.Context, platformadmin.Session, string, string) error {
	return nil
}
func (s *platformServiceStub) ListOrganizations(context.Context, platformadmin.Session) ([]platformadmin.Organization, error) {
	return []platformadmin.Organization{{ID: "org-1", Code: "default", Name: "默认企业", Status: "active"}}, nil
}
func (s *platformServiceStub) CreateOrganization(_ context.Context, input platformadmin.CreateOrganizationInput) (platformadmin.Organization, error) {
	s.createInput = input
	return platformadmin.Organization{ID: "org-2", Code: input.Code, Name: input.Name, Status: "active", AdminUsername: input.AdminUsername, UserCount: 1}, nil
}
func (s *platformServiceStub) UpdateOrganizationStatus(context.Context, platformadmin.Session, string, string, string, string) (platformadmin.Organization, error) {
	return platformadmin.Organization{}, nil
}

func TestPlatformLoginSetsSeparateCookies(t *testing.T) {
	service := &platformServiceStub{}
	router := New(Dependencies{Environment: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), PlatformAdmin: service, PlatformSessionCookie: "platform_session"})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/platform/auth/login", bytes.NewBufferString(`{"username":"platform-admin","password":"123"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，响应 = %s", response.Code, response.Body.String())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 2 || cookies[0].Name != "platform_session" || cookies[1].Name != platformCSRFCookieName {
		t.Fatalf("平台 Cookie 不正确: %#v", cookies)
	}
}

func TestPlatformCreateOrganizationCreatesInitialAdmin(t *testing.T) {
	service := &platformServiceStub{}
	router := New(Dependencies{Environment: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), PlatformAdmin: service, PlatformSessionCookie: "platform_session"})
	body, _ := json.Marshal(map[string]string{
		"code": "example", "name": "示例企业", "adminUsername": "admin", "adminDisplayName": "企业管理员", "adminEmail": "", "adminPassword": "123",
	})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/platform/organizations", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", "csrf-token")
	request.AddCookie(&http.Cookie{Name: "platform_session", Value: "session-token"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("状态码 = %d，响应 = %s", response.Code, response.Body.String())
	}
	if service.createInput.AdminUsername != "admin" || service.createInput.AdminPassword != "123" || service.createInput.Session.Admin.ID != "platform-1" {
		t.Fatalf("创建参数不正确: %#v", service.createInput)
	}
}
