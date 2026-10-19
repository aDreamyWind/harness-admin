package httpserver

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"harness-admin/internal/identity"
	"harness-admin/internal/modelgateway"
)

type csrfCheckingAuthStub struct{ *accountAuthStub }

func (*csrfCheckingAuthStub) ValidateCSRF(_ identity.Session, token string) error {
	if token != "csrf-token" {
		return identity.ErrInvalidCSRF
	}
	return nil
}

type modelPolicyServiceStub struct {
	saved   modelgateway.SavePolicyInput
	batch   modelgateway.SaveDepartmentPoliciesInput
	deleted modelgateway.DeletePolicyInput
}

func (*modelPolicyServiceStub) List(context.Context, string) ([]modelgateway.SubjectPolicy, error) {
	return []modelgateway.SubjectPolicy{}, nil
}
func (s *modelPolicyServiceStub) Save(_ context.Context, input modelgateway.SavePolicyInput) (modelgateway.SubjectPolicy, error) {
	s.saved = input
	return modelgateway.SubjectPolicy{SubjectType: input.SubjectType, SubjectID: input.SubjectID, ScopeMode: input.ScopeMode}, nil
}
func (s *modelPolicyServiceStub) SaveDepartments(_ context.Context, input modelgateway.SaveDepartmentPoliciesInput) ([]modelgateway.SubjectPolicy, error) {
	s.batch = input
	return []modelgateway.SubjectPolicy{{SubjectType: "department", SubjectID: input.SubjectIDs[0], MonthlyTokenMode: input.MonthlyTokenMode}}, nil
}
func (s *modelPolicyServiceStub) Delete(_ context.Context, input modelgateway.DeletePolicyInput) error {
	s.deleted = input
	return nil
}

func TestDepartmentModelPoliciesBatchUsesCurrentOrganization(t *testing.T) {
	policy := &modelPolicyServiceStub{}
	router := New(Dependencies{
		AppName: "Harness Admin", Environment: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth: &csrfCheckingAuthStub{&accountAuthStub{session: accountTestSession()}}, Access: accountAccessStub{}, ModelPolicy: policy,
		SessionCookie: "test_session",
	})
	path := "/api/v1/model-policies/departments"
	body := `{"subjectIds":["30000000-0000-4000-8000-000000000001"],"scopeMode":"all","requestsPerMinute":10,"monthlyTokenLimit":1000,"monthlyTokenMode":"per_user"}`
	request := httptest.NewRequest(http.MethodPut, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("批量保存缺少 CSRF 时状态码 = %d", response.Code)
	}
	request = httptest.NewRequest(http.MethodPut, path, strings.NewReader(strings.Replace(body, "30000000-0000-4000-8000-000000000001", "invalid", 1)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", "csrf-token")
	request.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("批量保存部门标识错误时状态码 = %d", response.Code)
	}
	request = httptest.NewRequest(http.MethodPut, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", "csrf-token")
	request.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || policy.batch.OrganizationID != "org-1" || policy.batch.MonthlyTokenMode != "per_user" || len(policy.batch.SubjectIDs) != 1 {
		t.Fatalf("批量部门策略保存错误: status=%d input=%#v response=%s", response.Code, policy.batch, response.Body.String())
	}
}

func TestModelPolicyWriteUsesCurrentOrganizationAndRequiresCSRF(t *testing.T) {
	policy := &modelPolicyServiceStub{}
	router := New(Dependencies{
		AppName: "Harness Admin", Environment: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth: &csrfCheckingAuthStub{&accountAuthStub{session: accountTestSession()}}, Access: accountAccessStub{}, ModelPolicy: policy,
		SessionCookie: "test_session",
	})
	path := "/api/v1/model-policies/user/30000000-0000-4000-8000-000000000001"
	request := httptest.NewRequest(http.MethodPut, path, strings.NewReader(`{"scopeMode":"selected","allowedModelCodes":["main"],"requestsPerMinute":10,"monthlyTokenLimit":1000}`))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("缺少 CSRF 时状态码 = %d", response.Code)
	}
	request = httptest.NewRequest(http.MethodPut, path, strings.NewReader(`{"scopeMode":"selected","allowedModelCodes":["main"],"requestsPerMinute":10,"monthlyTokenLimit":1000}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", "csrf-token")
	request.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || policy.saved.OrganizationID != "org-1" || policy.saved.ActorID != "user-1" || policy.saved.ScopeMode != "selected" {
		t.Fatalf("模型策略保存未使用当前企业身份: status=%d input=%#v response=%s", response.Code, policy.saved, response.Body.String())
	}
}

func TestGatewayModelsReturnsOpenAIShape(t *testing.T) {
	router := newGatewayTestRouter(&modelGatewayStub{})
	request := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	request.Header.Set("Authorization", "Bearer harness_sk_abcdefghijklmnopqrstuvwxyz0123456789")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"object":"list"`) ||
		!strings.Contains(response.Body.String(), `"id":"main"`) || strings.Contains(response.Body.String(), `"data":{"`) {
		t.Fatalf("OpenAI 模型目录格式错误: status=%d response=%s", response.Code, response.Body.String())
	}
}

func TestGatewayModelsUsesOIDCUserScope(t *testing.T) {
	provider := &oidcProviderStub{}
	router := New(Dependencies{
		AppName: "Harness Admin", Environment: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth: &accountAuthStub{session: accountTestSession()}, Access: accountAccessStub{},
		ModelGateway: &modelGatewayStub{}, GatewayForwarder: gatewayForwarderStub{}, OIDCProvider: provider,
		SessionCookie: "test_session",
	})
	request := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	request.Header.Set("Authorization", "Bearer oidc-access-token")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || provider.requiredScope != "model.invoke" || provider.accessToken != "oidc-access-token" {
		t.Fatalf("OIDC 模型目录认证错误: status=%d scope=%q token=%q response=%s", response.Code, provider.requiredScope, provider.accessToken, response.Body.String())
	}
}
