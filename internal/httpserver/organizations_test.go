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

	"harness-admin/internal/organizationadmin"
)

type organizationAdminStub struct {
	updateInput organizationadmin.UpdateInput
}

func (*organizationAdminStub) ListActive(context.Context) ([]organizationadmin.Organization, error) {
	return []organizationadmin.Organization{{Code: "default", Name: "默认企业", Status: "active"}}, nil
}

func (*organizationAdminStub) Get(_ context.Context, organizationID string) (organizationadmin.Organization, error) {
	return organizationadmin.Organization{
		ID: organizationID, Code: "default", Name: "默认企业", Status: "active", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}, nil
}

func (s *organizationAdminStub) Update(_ context.Context, input organizationadmin.UpdateInput) (organizationadmin.Organization, error) {
	s.updateInput = input
	return organizationadmin.Organization{
		ID: input.OrganizationID, Code: "default", Name: input.Name, Status: "active", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}, nil
}

func TestGetOrganizationHandlerUsesCurrentOrganization(t *testing.T) {
	auth := &accountAuthStub{session: accountTestSession()}
	organizations := &organizationAdminStub{}
	router := newOrganizationTestRouter(auth, organizations)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/organization", nil)
	request.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 %d；响应 = %s", response.Code, http.StatusOK, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"code":"default"`) {
		t.Fatalf("企业响应不正确: %s", response.Body.String())
	}
}

func TestListLoginOrganizationsHandlerIsPublic(t *testing.T) {
	router := newOrganizationTestRouter(&accountAuthStub{}, &organizationAdminStub{})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/organizations", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"name":"默认企业"`) {
		t.Fatalf("可登录企业响应不正确: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestUpdateOrganizationHandlerUsesCurrentOrganizationAndActor(t *testing.T) {
	auth := &accountAuthStub{session: accountTestSession()}
	organizations := &organizationAdminStub{}
	router := newOrganizationTestRouter(auth, organizations)
	request := httptest.NewRequest(http.MethodPut, "/api/v1/organization", strings.NewReader(`{"name":"星河科技"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", "csrf-token")
	request.Header.Set("X-Request-ID", "request-organization")
	request.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 %d；响应 = %s", response.Code, http.StatusOK, response.Body.String())
	}
	input := organizations.updateInput
	if input.OrganizationID != "org-1" || input.ActorID != "user-1" || input.Name != "星河科技" || input.RequestID != "request-organization" {
		t.Fatalf("修改企业资料参数不完整: %#v", input)
	}
}

func newOrganizationTestRouter(auth AuthService, organizations OrganizationAdminService) http.Handler {
	return New(Dependencies{
		AppName:           "Harness Admin",
		Environment:       "test",
		Logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth:              auth,
		Access:            accountAccessStub{},
		OrganizationAdmin: organizations,
		SessionCookie:     "test_session",
	})
}
