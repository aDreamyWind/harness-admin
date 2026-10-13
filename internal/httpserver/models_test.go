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

	"harness-admin/internal/modeladmin"
)

type modelAdminStub struct {
	createProviderInput modeladmin.CreateProviderInput
	updateProviderInput modeladmin.UpdateProviderInput
	createModelInput    modeladmin.CreateModelInput
}

func (*modelAdminStub) ListProviders(context.Context, string) ([]modeladmin.Provider, error) {
	return []modeladmin.Provider{{ID: "10000000-0000-4000-8000-000000000001", Code: "primary", Name: "主连接", CredentialConfigured: true}}, nil
}
func (s *modelAdminStub) CreateProvider(_ context.Context, input modeladmin.CreateProviderInput) (modeladmin.Provider, error) {
	s.createProviderInput = input
	return modeladmin.Provider{ID: "10000000-0000-4000-8000-000000000001", Code: input.Code, Name: input.Name, ProviderKind: input.ProviderKind, BaseURL: input.BaseURL, Status: "active", RequestTimeoutMS: input.RequestTimeoutMS, CredentialConfigured: true, LastProbeStatus: "unknown", CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil
}
func (s *modelAdminStub) UpdateProvider(_ context.Context, input modeladmin.UpdateProviderInput) (modeladmin.Provider, error) {
	s.updateProviderInput = input
	return modeladmin.Provider{}, nil
}
func (*modelAdminStub) ChangeProviderStatus(context.Context, modeladmin.ChangeProviderStatusInput) (modeladmin.Provider, error) {
	return modeladmin.Provider{}, nil
}
func (*modelAdminStub) ProbeProvider(context.Context, modeladmin.ProbeInput) (modeladmin.ProbeResult, error) {
	return modeladmin.ProbeResult{Status: "success", CheckedAt: time.Now()}, nil
}
func (*modelAdminStub) ListModels(context.Context, string) ([]modeladmin.CatalogModel, error) {
	return []modeladmin.CatalogModel{}, nil
}
func (s *modelAdminStub) CreateModel(_ context.Context, input modeladmin.CreateModelInput) (modeladmin.CatalogModel, error) {
	s.createModelInput = input
	return modeladmin.CatalogModel{ID: "20000000-0000-4000-8000-000000000001", Code: input.Code, Name: input.Name, ProviderConnectionID: input.ProviderConnectionID, Capabilities: input.Capabilities}, nil
}
func (*modelAdminStub) UpdateModel(context.Context, modeladmin.UpdateModelInput) (modeladmin.CatalogModel, error) {
	return modeladmin.CatalogModel{}, nil
}
func (*modelAdminStub) ChangeModelStatus(context.Context, modeladmin.ChangeModelStatusInput) (modeladmin.CatalogModel, error) {
	return modeladmin.CatalogModel{}, nil
}

func TestCreateModelProviderUsesCurrentOrganizationAndHidesCredential(t *testing.T) {
	models := &modelAdminStub{}
	router := newModelTestRouter(models)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/model-providers", strings.NewReader(`{
		"code":"primary","name":"主连接","providerKind":"deepseek",
		"baseUrl":"https://api.deepseek.com","apiKey":"top-secret","requestTimeoutMs":15000
	}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", "csrf-token")
	request.Header.Set("X-Request-ID", "request-model-provider")
	request.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("状态码 = %d；响应 = %s", response.Code, response.Body.String())
	}
	if models.createProviderInput.OrganizationID != "org-1" || models.createProviderInput.ActorID != "user-1" || models.createProviderInput.ProviderKind != "deepseek" || models.createProviderInput.APIKey != "top-secret" {
		t.Fatalf("创建供应商参数不完整: %#v", models.createProviderInput)
	}
	if strings.Contains(response.Body.String(), "top-secret") || strings.Contains(response.Body.String(), "apiKey") {
		t.Fatalf("响应泄露了供应商凭据: %s", response.Body.String())
	}
}

func TestCreateModelUsesCurrentOrganization(t *testing.T) {
	models := &modelAdminStub{}
	router := newModelTestRouter(models)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/models", strings.NewReader(`{
		"providerConnectionId":"10000000-0000-4000-8000-000000000001","code":"main_model",
		"upstreamModelId":"gpt-compatible","name":"主模型","modelType":"chat","capabilities":["text","tools"]
	}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", "csrf-token")
	request.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("状态码 = %d；响应 = %s", response.Code, response.Body.String())
	}
	if models.createModelInput.OrganizationID != "org-1" || models.createModelInput.ActorID != "user-1" || models.createModelInput.Code != "main_model" {
		t.Fatalf("创建模型参数不完整: %#v", models.createModelInput)
	}
}

func newModelTestRouter(models ModelAdminService) http.Handler {
	return New(Dependencies{
		AppName: "Harness Admin", Environment: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth: &accountAuthStub{session: accountTestSession()}, Access: accountAccessStub{}, ModelAdmin: models, SessionCookie: "test_session",
	})
}
