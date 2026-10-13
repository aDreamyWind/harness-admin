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

	"harness-admin/internal/modelgateway"
)

type modelGatewayStub struct {
	createInput modelgateway.CreateAPIKeyInput
	credential  string
	requestBody string
	recorded    modelgateway.RecordCallInput
}

func (*modelGatewayStub) ListAPIKeys(context.Context, string) ([]modelgateway.APIKey, error) {
	return []modelgateway.APIKey{}, nil
}
func (s *modelGatewayStub) CreateAPIKey(_ context.Context, input modelgateway.CreateAPIKeyInput) (modelgateway.CreatedAPIKey, error) {
	s.createInput = input
	return modelgateway.CreatedAPIKey{
		APIKey: modelgateway.APIKey{ID: "30000000-0000-4000-8000-000000000001", OrganizationID: input.OrganizationID, Name: input.Name, KeyPrefix: "harness_sk_abcdefg", Status: "active", CreatedAt: time.Now()},
		Secret: "harness_sk_abcdefghijklmnopqrstuvwxyz0123456789",
	}, nil
}
func (*modelGatewayStub) RevokeAPIKey(context.Context, modelgateway.RevokeAPIKeyInput) (modelgateway.APIKey, error) {
	return modelgateway.APIKey{}, nil
}
func (s *modelGatewayStub) Prepare(_ context.Context, credential string, body []byte) (modelgateway.PreparedRequest, error) {
	s.credential = credential
	s.requestBody = string(body)
	return modelgateway.PreparedRequest{
		Caller: modelgateway.Caller{OrganizationID: "org-1", APIKeyID: "key-1"},
		Route:  modelgateway.Route{ModelID: "model-1", ModelCode: "main", UpstreamModelID: "upstream-main", ProviderID: "provider-1"},
		Body:   body,
	}, nil
}
func (s *modelGatewayStub) RecordCall(_ context.Context, input modelgateway.RecordCallInput) error {
	s.recorded = input
	return nil
}
func (*modelGatewayStub) ListCalls(context.Context, string, int) ([]modelgateway.Call, error) {
	return []modelgateway.Call{}, nil
}

type gatewayForwarderStub struct{}

func (gatewayForwarderStub) Forward(context.Context, modelgateway.Route, []byte, bool) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"id":"chat-1","usage":{"prompt_tokens":3,"completion_tokens":5,"total_tokens":8}}`)),
	}, nil
}

func TestCreateGatewayAPIKeyReturnsSecretAndUsesCurrentOrganization(t *testing.T) {
	gateway := &modelGatewayStub{}
	router := newGatewayTestRouter(gateway)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/model-gateway/api-keys", strings.NewReader(`{"name":"生产调用"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", "csrf-token")
	request.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("状态码 = %d；响应 = %s", response.Code, response.Body.String())
	}
	if gateway.createInput.OrganizationID != "org-1" || gateway.createInput.ActorID != "user-1" {
		t.Fatalf("创建密钥参数不完整: %#v", gateway.createInput)
	}
	if !strings.Contains(response.Body.String(), "harness_sk_abcdefghijklmnopqrstuvwxyz0123456789") {
		t.Fatalf("首次创建响应未返回完整密钥: %s", response.Body.String())
	}
}

func TestChatCompletionsProxiesOpenAIResponseAndRecordsUsage(t *testing.T) {
	gateway := &modelGatewayStub{}
	router := newGatewayTestRouter(gateway)
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"main","messages":[]}`))
	request.Header.Set("Authorization", "Bearer harness_sk_abcdefghijklmnopqrstuvwxyz0123456789")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("状态码 = %d；响应 = %s", response.Code, response.Body.String())
	}
	if gateway.credential != "harness_sk_abcdefghijklmnopqrstuvwxyz0123456789" {
		t.Fatalf("网关未读取 Bearer 调用密钥: %q", gateway.credential)
	}
	if gateway.recorded.Result != "success" || gateway.recorded.TotalTokens != 8 || gateway.recorded.ModelCode != "main" {
		t.Fatalf("调用记录不完整: %#v", gateway.recorded)
	}
}

func newGatewayTestRouter(gateway ModelGatewayService) http.Handler {
	return New(Dependencies{
		AppName: "Harness Admin", Environment: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth: &accountAuthStub{session: accountTestSession()}, Access: accountAccessStub{},
		ModelGateway: gateway, GatewayForwarder: gatewayForwarderStub{}, SessionCookie: "test_session",
	})
}
