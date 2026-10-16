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

	oidcmodule "harness-admin/internal/oidc"
)

type oidcAdminStub struct {
	createInput oidcmodule.CreateClientInput
	revealInput oidcmodule.RevealClientSecretInput
}

func (*oidcAdminStub) ListClients(context.Context, string) ([]oidcmodule.Client, error) {
	return []oidcmodule.Client{}, nil
}
func (s *oidcAdminStub) CreateClient(_ context.Context, input oidcmodule.CreateClientInput) (oidcmodule.CreatedClient, error) {
	s.createInput = input
	return oidcmodule.CreatedClient{Client: oidcmodule.Client{
		ID: "40000000-0000-4000-8000-000000000001", OrganizationID: input.OrganizationID,
		Name: input.Name, ClientID: "harness_client_test", ClientSecretPrefix: "harness_client_secret_example",
		ApplicationType: input.ApplicationType, TokenEndpointAuthMethod: oidcmodule.TokenEndpointAuthClientSecretBasic,
		RedirectURIs: input.RedirectURIs, AllowedScopes: input.AllowedScopes, Status: "active", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}, ClientSecret: "harness_client_secret_abcdefghijklmnopqrstuvwxyz"}, nil
}
func (*oidcAdminStub) UpdateClient(context.Context, oidcmodule.UpdateClientInput) (oidcmodule.Client, error) {
	return oidcmodule.Client{}, nil
}
func (*oidcAdminStub) ChangeClientStatus(context.Context, oidcmodule.ChangeClientStatusInput) (oidcmodule.Client, error) {
	return oidcmodule.Client{}, nil
}
func (*oidcAdminStub) RotateClientSecret(context.Context, oidcmodule.RotateClientSecretInput) (oidcmodule.CreatedClient, error) {
	return oidcmodule.CreatedClient{}, nil
}
func (s *oidcAdminStub) RevealClientSecret(_ context.Context, input oidcmodule.RevealClientSecretInput) (string, error) {
	s.revealInput = input
	return "harness_client_secret_revealed", nil
}

func TestCreateOIDCClientUsesCurrentOrganizationAndReturnsSecret(t *testing.T) {
	oidcAdmin := &oidcAdminStub{}
	router := New(Dependencies{
		AppName: "Harness Admin", Environment: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth: &accountAuthStub{session: accountTestSession()}, Access: accountAccessStub{},
		OIDCAdmin: oidcAdmin, SessionCookie: "test_session",
	})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/oidc/clients", strings.NewReader(`{
		"name":"DeepSeek Harness",
		"applicationType":"web",
		"redirectUris":["https://harness.example.com/callback"],
		"allowedScopes":["openid","profile","model.invoke"]
	}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", "csrf-token")
	request.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("状态码 = %d；响应 = %s", response.Code, response.Body.String())
	}
	if oidcAdmin.createInput.OrganizationID != "org-1" || oidcAdmin.createInput.ActorID != "user-1" {
		t.Fatalf("创建应用参数不完整: %#v", oidcAdmin.createInput)
	}
	if oidcAdmin.createInput.ApplicationType != "web" {
		t.Fatalf("应用类型未传递: %#v", oidcAdmin.createInput)
	}
	if !strings.Contains(response.Body.String(), "harness_client_secret_abcdefghijklmnopqrstuvwxyz") {
		t.Fatalf("首次创建响应未返回客户端密钥: %s", response.Body.String())
	}
}

func TestRevealOIDCClientSecretUsesCurrentOrganizationAndDisablesCache(t *testing.T) {
	oidcAdmin := &oidcAdminStub{}
	router := New(Dependencies{
		AppName: "Harness Admin", Environment: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth: &accountAuthStub{session: accountTestSession()}, Access: accountAccessStub{},
		OIDCAdmin: oidcAdmin, SessionCookie: "test_session",
	})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/oidc/clients/40000000-0000-4000-8000-000000000001/secret", nil)
	request.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"secret":"harness_client_secret_revealed"`) {
		t.Fatalf("状态码 = %d；响应 = %s", response.Code, response.Body.String())
	}
	if oidcAdmin.revealInput.OrganizationID != "org-1" || oidcAdmin.revealInput.ActorID != "user-1" {
		t.Fatalf("查看客户端密钥参数不完整: %#v", oidcAdmin.revealInput)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("敏感响应未禁止缓存: %#v", response.Header())
	}
}
