package httpserver

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	listQuery   modelgateway.CallQuery
	revealInput modelgateway.RevealAPIKeySecretInput
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
func (*modelGatewayStub) UpdateAPIKey(context.Context, modelgateway.UpdateAPIKeyInput) (modelgateway.APIKey, error) {
	return modelgateway.APIKey{}, nil
}
func (*modelGatewayStub) RevokeAPIKey(context.Context, modelgateway.RevokeAPIKeyInput) (modelgateway.APIKey, error) {
	return modelgateway.APIKey{}, nil
}
func (s *modelGatewayStub) RevealAPIKeySecret(_ context.Context, input modelgateway.RevealAPIKeySecretInput) (string, error) {
	s.revealInput = input
	return "harness_sk_revealed", nil
}
func (s *modelGatewayStub) Prepare(_ context.Context, credential string, body []byte) (modelgateway.PreparedRequest, error) {
	s.credential = credential
	s.requestBody = string(body)
	return modelgateway.PreparedRequest{
		Caller: modelgateway.Caller{OrganizationID: "org-1", APIKeyID: "key-1", AuthType: "api_key"},
		Route:  modelgateway.Route{ModelID: "model-1", ModelCode: "main", UpstreamModelID: "upstream-main", ProviderID: "provider-1"},
		Body:   body, Streaming: strings.Contains(string(body), `"stream":true`),
	}, nil
}
func (s *modelGatewayStub) PrepareForUser(_ context.Context, organizationID, userID string, body []byte) (modelgateway.PreparedRequest, error) {
	s.requestBody = string(body)
	return modelgateway.PreparedRequest{
		Caller: modelgateway.Caller{OrganizationID: organizationID, UserID: userID, AuthType: "user"},
		Route:  modelgateway.Route{ModelID: "model-1", ModelCode: "main", UpstreamModelID: "upstream-main", ProviderID: "provider-1"},
		Body:   body,
	}, nil
}
func (s *modelGatewayStub) RecordCall(_ context.Context, input modelgateway.RecordCallInput) error {
	s.recorded = input
	return nil
}
func (s *modelGatewayStub) ListCalls(_ context.Context, query modelgateway.CallQuery) (modelgateway.CallPage, error) {
	s.listQuery = query
	return modelgateway.CallPage{Items: []modelgateway.Call{}, Total: 21, Page: query.Page, PageSize: query.PageSize}, nil
}
func (*modelGatewayStub) ListModelsForAPIKey(context.Context, string) ([]modelgateway.AvailableModel, error) {
	return []modelgateway.AvailableModel{{Code: "main", CreatedAt: time.Unix(100, 0)}}, nil
}
func (*modelGatewayStub) ListModelsForUser(context.Context, string, string) ([]modelgateway.AvailableModel, error) {
	return []modelgateway.AvailableModel{{Code: "main", CreatedAt: time.Unix(100, 0)}}, nil
}

type gatewayForwarderStub struct{}

func (gatewayForwarderStub) Forward(context.Context, modelgateway.Route, []byte, bool) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"id":"chat-1","usage":{"prompt_tokens":3,"completion_tokens":5,"total_tokens":8}}`)),
	}, nil
}

type streamingForwarderStub struct{}

func (streamingForwarderStub) Forward(_ context.Context, _ modelgateway.Route, _ []byte, streaming bool) (*http.Response, error) {
	if !streaming {
		return nil, errors.New("期望流式转发")
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"id\":\"call-1\",\"type\":\"function\"}]}}]}\n\n" +
			"data: {\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":5,\"total_tokens\":8}}\n\n" +
			"data: [DONE]\n\n")),
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
	if gateway.createInput.RequestsPerMinute != 60 || gateway.createInput.MonthlyTokenLimit != 0 {
		t.Fatalf("默认限流与额度不正确: %#v", gateway.createInput)
	}
	if !strings.Contains(response.Body.String(), "harness_sk_abcdefghijklmnopqrstuvwxyz0123456789") {
		t.Fatalf("首次创建响应未返回完整密钥: %s", response.Body.String())
	}
}

func TestListGatewayCallsParsesFiltersAndPagination(t *testing.T) {
	gateway := &modelGatewayStub{}
	router := newGatewayTestRouter(gateway)
	startedAt := "2026-10-01T00:00:00+08:00"
	endedAt := "2026-10-13T12:00:00+08:00"
	request := httptest.NewRequest(http.MethodGet, "/api/v1/model-gateway/calls?page=2&pageSize=10&apiKeyId=30000000-0000-4000-8000-000000000001&modelCode=Main&result=failed&startedAt="+url.QueryEscape(startedAt)+"&endedAt="+url.QueryEscape(endedAt), nil)
	request.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("状态码 = %d；响应 = %s", response.Code, response.Body.String())
	}
	query := gateway.listQuery
	if query.OrganizationID != "org-1" || query.Page != 2 || query.PageSize != 10 || query.APIKeyID == "" || query.ModelCode != "Main" || query.Result != "failed" || query.StartedAt == nil || query.EndedAt == nil {
		t.Fatalf("调用记录查询参数不完整: %#v", query)
	}
	if !strings.Contains(response.Body.String(), `"total":21`) || !strings.Contains(response.Body.String(), `"page":2`) {
		t.Fatalf("分页响应不正确: %s", response.Body.String())
	}
}

func TestRevealGatewayAPIKeyUsesCurrentOrganizationAndDisablesCache(t *testing.T) {
	gateway := &modelGatewayStub{}
	router := newGatewayTestRouter(gateway)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/model-gateway/api-keys/30000000-0000-4000-8000-000000000001/secret", nil)
	request.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"secret":"harness_sk_revealed"`) {
		t.Fatalf("状态码 = %d；响应 = %s", response.Code, response.Body.String())
	}
	if gateway.revealInput.OrganizationID != "org-1" || gateway.revealInput.ActorID != "user-1" {
		t.Fatalf("查看密钥参数不完整: %#v", gateway.revealInput)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("敏感响应未禁止缓存: %#v", response.Header())
	}
}

func TestParseSSETokenUsage(t *testing.T) {
	usage, ok := parseSSETokenUsage(`data: {"id":"chat-1","usage":{"prompt_tokens":3,"completion_tokens":5,"total_tokens":8}}`)
	if !ok || usage.PromptTokens != 3 || usage.CompletionTokens != 5 || usage.TotalTokens != 8 {
		t.Fatalf("流式 Token 用量解析失败: %#v, %v", usage, ok)
	}
	if _, ok := parseSSETokenUsage("data: [DONE]"); ok {
		t.Fatal("[DONE] 不应被识别为 Token 用量")
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

func TestChatCompletionsAcceptsOIDCUserAccessToken(t *testing.T) {
	gateway := &modelGatewayStub{}
	provider := &oidcProviderStub{}
	router := New(Dependencies{
		AppName: "Harness Admin", Environment: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth: &accountAuthStub{session: accountTestSession()}, Access: accountAccessStub{},
		ModelGateway: gateway, GatewayForwarder: gatewayForwarderStub{}, OIDCProvider: provider,
		SessionCookie: "test_session",
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"main","messages":[]}`))
	request.Header.Set("Authorization", "Bearer oidc-access-token")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("状态码 = %d；响应 = %s", response.Code, response.Body.String())
	}
	if provider.accessToken != "oidc-access-token" || provider.requiredScope != "model.invoke" {
		t.Fatalf("OIDC 令牌校验参数不正确: token=%q scope=%q", provider.accessToken, provider.requiredScope)
	}
	if gateway.recorded.AuthType != "user" || gateway.recorded.UserID != "user-1" || gateway.recorded.OrganizationID != "org-1" {
		t.Fatalf("用户调用记录身份不完整: %#v", gateway.recorded)
	}
}

func TestChatCompletionsPreservesStreamingToolCallsAndUsage(t *testing.T) {
	gateway := &modelGatewayStub{}
	router := New(Dependencies{
		AppName: "Harness Admin", Environment: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth: &accountAuthStub{session: accountTestSession()}, Access: accountAccessStub{},
		ModelGateway: gateway, GatewayForwarder: streamingForwarderStub{}, SessionCookie: "test_session",
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"main","messages":[{"role":"user","content":[{"type":"text","text":"你好"},{"type":"image_url","image_url":{"url":"https://example.com/image.png"}}]}],"stream":true}`))
	request.Header.Set("Authorization", "Bearer harness_sk_abcdefghijklmnopqrstuvwxyz0123456789")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"tool_calls"`) ||
		!strings.Contains(response.Body.String(), "data: [DONE]") || gateway.recorded.TotalTokens != 8 || !gateway.recorded.Streaming {
		t.Fatalf("流式工具调用或用量转发异常: status=%d response=%s record=%#v", response.Code, response.Body.String(), gateway.recorded)
	}
	if !strings.Contains(gateway.requestBody, `"image_url"`) {
		t.Fatalf("多模态请求未传入网关: %s", gateway.requestBody)
	}
}

func newGatewayTestRouter(gateway ModelGatewayService) http.Handler {
	return New(Dependencies{
		AppName: "Harness Admin", Environment: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth: &accountAuthStub{session: accountTestSession()}, Access: accountAccessStub{},
		ModelGateway: gateway, GatewayForwarder: gatewayForwarderStub{}, SessionCookie: "test_session",
	})
}
