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

	"harness-admin/internal/mcpadmin"
)

type mcpAdminStub struct {
	created  mcpadmin.CreateInput
	updated  mcpadmin.UpdateInput
	saved    mcpadmin.UpdateCredentialsInput
	revealed mcpadmin.Actor
}

func (*mcpAdminStub) List(context.Context, string) ([]mcpadmin.ServiceRecord, error) {
	return []mcpadmin.ServiceRecord{}, nil
}
func (s *mcpAdminStub) Create(_ context.Context, input mcpadmin.CreateInput) (mcpadmin.ServiceRecord, error) {
	s.created = input
	return mcpadmin.ServiceRecord{ID: "10000000-0000-4000-8000-000000000001", Code: input.Code, Name: input.Name,
		Transport: input.Transport, UpstreamURL: input.UpstreamURL, Status: "active"}, nil
}
func (s *mcpAdminStub) Update(_ context.Context, input mcpadmin.UpdateInput) (mcpadmin.ServiceRecord, error) {
	s.updated = input
	return mcpadmin.ServiceRecord{ID: input.ServiceID, Name: input.Name, Description: input.Description,
		Transport: input.Transport, UpstreamURL: input.UpstreamURL, Parameters: input.Parameters}, nil
}
func (s *mcpAdminStub) RevealCredentials(_ context.Context, actor mcpadmin.Actor, _ string) (mcpadmin.UpstreamCredentials, error) {
	s.revealed = actor
	return mcpadmin.UpstreamCredentials{Parameters: []mcpadmin.ParameterValue{{Location: "header", Name: "X-API-Key", Value: "saved-secret"}}}, nil
}
func (s *mcpAdminStub) UpdateCredentials(_ context.Context, input mcpadmin.UpdateCredentialsInput) (mcpadmin.ServiceRecord, error) {
	s.saved = input
	return mcpadmin.ServiceRecord{}, nil
}
func (*mcpAdminStub) ChangeStatus(context.Context, mcpadmin.ChangeStatusInput) (mcpadmin.ServiceRecord, error) {
	return mcpadmin.ServiceRecord{}, nil
}
func (*mcpAdminStub) Discover(context.Context, mcpadmin.DiscoverInput) ([]mcpadmin.Tool, error) {
	return []mcpadmin.Tool{}, nil
}
func (*mcpAdminStub) ListTools(context.Context, string, string) ([]mcpadmin.Tool, error) {
	return []mcpadmin.Tool{}, nil
}
func (*mcpAdminStub) Test(context.Context, mcpadmin.TestInput) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

func TestCreateMCPServiceUsesCurrentOrganizationAndCSRF(t *testing.T) {
	service := &mcpAdminStub{}
	router := New(Dependencies{
		AppName: "Harness Admin", Environment: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth:   &csrfCheckingAuthStub{&accountAuthStub{session: accountTestSession()}},
		Access: accountAccessStub{}, MCPAdmin: service, SessionCookie: "test_session",
	})
	body := `{"name":"内部工具","description":"部门工具","transport":"streamable_http","upstreamUrl":"https://example.com/mcp","parameters":[{"location":"header","name":"X-API-Key","description":"每条连接独立填写","required":true}]}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/mcp-services", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("缺少 CSRF 时状态码 = %d", response.Code)
	}
	request = httptest.NewRequest(http.MethodPost, "/api/v1/mcp-services", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", "csrf-token")
	request.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || service.created.OrganizationID != "org-1" ||
		service.created.UserID != "user-1" || service.created.Description != "部门工具" ||
		len(service.created.Parameters) != 1 || service.created.Parameters[0].Description != "每条连接独立填写" ||
		len(service.created.Credentials.Parameters) != 0 {
		t.Fatalf("创建请求未按当前企业处理: status=%d input=%#v response=%s", response.Code, service.created, response.Body.String())
	}
}

func TestUpdateMCPServiceUsesCurrentOrganizationAndCSRF(t *testing.T) {
	service := &mcpAdminStub{}
	router := New(Dependencies{
		AppName: "Harness Admin", Environment: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth:   &csrfCheckingAuthStub{&accountAuthStub{session: accountTestSession()}},
		Access: accountAccessStub{}, MCPAdmin: service, SessionCookie: "test_session",
	})
	path := "/api/v1/mcp-services/10000000-0000-4000-8000-000000000001"
	body := `{"name":"更新后的服务","description":"更新说明","transport":"sse","upstreamUrl":"https://example.com/sse","parameters":[{"location":"query","name":"key","description":"应用独立密钥","required":true}]}`
	request := httptest.NewRequest(http.MethodPut, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("修改服务缺少 CSRF 时状态码 = %d", response.Code)
	}
	request = httptest.NewRequest(http.MethodPut, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", "csrf-token")
	request.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || service.updated.OrganizationID != "org-1" ||
		service.updated.ServiceID != "10000000-0000-4000-8000-000000000001" ||
		service.updated.Parameters[0].Description != "应用独立密钥" {
		t.Fatalf("修改服务接口失败: status=%d input=%#v body=%s", response.Code, service.updated, response.Body.String())
	}
}

func TestMCPServiceCredentialsRoutesUseSessionAndCSRF(t *testing.T) {
	service := &mcpAdminStub{}
	router := New(Dependencies{
		AppName: "Harness Admin", Environment: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth:   &csrfCheckingAuthStub{&accountAuthStub{session: accountTestSession()}},
		Access: accountAccessStub{}, MCPAdmin: service, SessionCookie: "test_session",
	})
	path := "/api/v1/mcp-services/10000000-0000-4000-8000-000000000001/credentials"
	get := httptest.NewRequest(http.MethodGet, path, nil)
	get.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, get)
	if response.Code != http.StatusOK || service.revealed.OrganizationID != "org-1" ||
		!strings.Contains(response.Body.String(), "saved-secret") {
		t.Fatalf("查看服务凭证失败: status=%d actor=%#v body=%s", response.Code, service.revealed, response.Body.String())
	}
	put := httptest.NewRequest(http.MethodPut, path, strings.NewReader(`{"parameters":[{"location":"header","name":"X-API-Key","value":"new-secret"}]}`))
	put.Header.Set("Content-Type", "application/json")
	put.Header.Set("X-CSRF-Token", "csrf-token")
	put.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
	response = httptest.NewRecorder()
	router.ServeHTTP(response, put)
	if response.Code != http.StatusOK || service.saved.OrganizationID != "org-1" ||
		len(service.saved.Credentials.Parameters) != 1 || service.saved.Credentials.Parameters[0].Value != "new-secret" {
		t.Fatalf("保存服务凭证失败: status=%d input=%#v body=%s", response.Code, service.saved, response.Body.String())
	}
}
