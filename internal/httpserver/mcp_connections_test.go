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

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"harness-admin/internal/mcpadmin"
	"harness-admin/internal/mcpconnection"
	"harness-admin/internal/oidc"
)

type mcpConnectionsStub struct {
	route         mcpconnection.GatewayConnection
	credential    string
	recorded      mcpconnection.CallRecord
	userScopeUsed string
	healthOrg     string
}

func (*mcpConnectionsStub) List(context.Context, string) ([]mcpconnection.Connection, error) {
	return nil, nil
}
func (*mcpConnectionsStub) Create(context.Context, mcpconnection.CreateInput) (mcpconnection.CreatedConnection, error) {
	return mcpconnection.CreatedConnection{}, nil
}
func (*mcpConnectionsStub) ReplaceGrants(context.Context, mcpconnection.GrantsInput) (mcpconnection.Connection, error) {
	return mcpconnection.Connection{}, nil
}
func (*mcpConnectionsStub) ChangeStatus(context.Context, mcpconnection.Actor, string, string) (mcpconnection.Connection, error) {
	return mcpconnection.Connection{}, nil
}
func (*mcpConnectionsStub) RevealSecret(context.Context, mcpconnection.Actor, string) (string, error) {
	return "", nil
}
func (*mcpConnectionsStub) RevealUpstreamAuthorization(context.Context, mcpconnection.Actor, string) (string, error) {
	return "", nil
}
func (*mcpConnectionsStub) RevealUpstreamCredentials(context.Context, mcpconnection.Actor, string) (mcpadmin.UpstreamCredentials, error) {
	return mcpadmin.UpstreamCredentials{}, nil
}
func (*mcpConnectionsStub) RotateSecret(context.Context, mcpconnection.Actor, string) (string, error) {
	return "rotated-secret", nil
}
func (*mcpConnectionsStub) UpdateUpstreamCredentials(context.Context, mcpconnection.Actor, string, mcpadmin.UpstreamCredentials) (mcpconnection.Connection, error) {
	return mcpconnection.Connection{}, nil
}
func (s *mcpConnectionsStub) Lookup(_ context.Context, _ string) (mcpconnection.GatewayConnection, error) {
	return s.route, nil
}
func (s *mcpConnectionsStub) CheckHealth(_ context.Context, organizationID, id string, _ mcpconnection.HealthProber) (mcpconnection.Health, error) {
	s.healthOrg = organizationID
	return mcpconnection.Health{ConnectionID: id, Status: "healthy", Message: "MCP 握手和工具列表正常"}, nil
}
func (s *mcpConnectionsStub) AuthorizeDeveloper(_ context.Context, _ mcpconnection.GatewayConnection, credential string) (mcpconnection.Caller, error) {
	s.credential = credential
	if credential != "application-secret" {
		return mcpconnection.Caller{}, mcpconnection.ErrUnauthorized
	}
	return mcpconnection.Caller{}, nil
}
func (*mcpConnectionsStub) AuthorizeUser(_ context.Context, _ mcpconnection.GatewayConnection, _, _ string) (mcpconnection.Caller, error) {
	return mcpconnection.Caller{UserID: "user-1", DepartmentID: "department-1"}, nil
}
func (*mcpConnectionsStub) ListForUser(context.Context, string, string) ([]mcpconnection.Connection, error) {
	return []mcpconnection.Connection{{Code: "test_mcp", Name: "测试连接", ServiceName: "测试服务"}}, nil
}
func (s *mcpConnectionsStub) RecordCall(_ context.Context, record mcpconnection.CallRecord) error {
	s.recorded = record
	return nil
}
func (*mcpConnectionsStub) ListUsage(context.Context, string) ([]mcpconnection.Usage, error) {
	return nil, nil
}

type mcpToolCallerStub struct {
	credentials mcpadmin.UpstreamCredentials
	name        string
	calls       int
}

func (s *mcpToolCallerStub) CallTool(_ context.Context, _ mcpadmin.ServiceRecord, credentials mcpadmin.UpstreamCredentials, name string, _ json.RawMessage) (json.RawMessage, error) {
	s.credentials, s.name = credentials, name
	s.calls++
	return json.RawMessage(`{"content":[{"type":"text","text":"ok"}]}`), nil
}
func (*mcpToolCallerStub) ListTools(context.Context, mcpadmin.ServiceRecord, mcpadmin.UpstreamCredentials) ([]mcpadmin.Tool, error) {
	return []mcpadmin.Tool{{Name: "test_tool"}}, nil
}

func TestMCPConnectionHealthRequiresSessionAndUsesCurrentOrganization(t *testing.T) {
	connections := &mcpConnectionsStub{}
	router := New(Dependencies{AppName: "Harness Admin", Environment: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth: &accountAuthStub{session: accountTestSession()}, Access: accountAccessStub{},
		MCPConnections: connections, MCPToolCaller: &mcpToolCallerStub{}, OIDCProvider: &oidcProviderStub{},
		SessionCookie: "test_session"})
	id := "00000000-0000-4000-8000-000000000001"
	request := httptest.NewRequest(http.MethodGet, "/api/v1/mcp-connections/"+id+"/health", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("未登录查询健康状态应被拒绝: %d", response.Code)
	}
	request = httptest.NewRequest(http.MethodGet, "/api/v1/mcp-connections/"+id+"/health", nil)
	request.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || connections.healthOrg != "org-1" || !strings.Contains(response.Body.String(), `"status":"healthy"`) {
		t.Fatalf("连接健康查询未限定当前企业: status=%d org=%s body=%s", response.Code, connections.healthOrg, response.Body.String())
	}
}

type bearerTransport struct {
	token string
}

func (t bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", "Bearer "+t.token)
	return http.DefaultTransport.RoundTrip(clone)
}

func TestMCPGatewayAuthenticatesAndRecordsDeveloperToolCall(t *testing.T) {
	connections := &mcpConnectionsStub{route: mcpconnection.GatewayConnection{
		Connection:     mcpconnection.Connection{ID: "connection-1", Name: "测试连接", Code: "test_mcp", Purpose: "developer", AuthMode: "bearer", Status: "active"},
		OrganizationID: "org-1", ServiceStatus: "active", UpstreamURL: "https://upstream.example/mcp",
		Transport: "streamable_http", UpstreamAuthorization: "Bearer upstream-secret",
		ServiceParameters: []mcpadmin.ParameterValue{
			{Location: "header", Name: "aaaa", Value: "service-secret"},
			{Location: "header", Name: "X-API-Key", Value: "service-old-key"},
		},
		UpstreamHeaderName: "X-API-Key", UpstreamHeaderValue: "header-secret",
		UpstreamQueryName: "key", UpstreamQueryValue: "query-secret",
		Tools: []mcpadmin.Tool{{Name: "working_directory", Description: "查看工作目录", InputSchema: json.RawMessage(`{"type":"object"}`)}},
	}}
	caller := &mcpToolCallerStub{}
	router := New(Dependencies{
		AppName: "Harness Admin", Environment: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth: &accountAuthStub{session: accountTestSession()}, Access: accountAccessStub{},
		MCPConnections: connections, MCPToolCaller: caller, OIDCProvider: &oidcProviderStub{},
		SessionCookie: "test_session",
	})
	server := httptest.NewServer(router)
	defer server.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "gateway-test-client", Version: "1.0"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:             server.URL + "/mcp/test_mcp",
		HTTPClient:           &http.Client{Transport: bearerTransport{token: "application-secret"}},
		DisableStandaloneSSE: true, MaxRetries: -1,
	}, nil)
	if err != nil {
		t.Fatalf("连接 MCP 网关失败: %v", err)
	}
	defer session.Close()
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil || len(tools.Tools) != 1 || tools.Tools[0].Name != "working_directory" {
		t.Fatalf("网关工具目录不正确: %#v, %v", tools, err)
	}
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "working_directory", Arguments: map[string]any{}})
	actual := make(map[string]string)
	for _, parameter := range caller.credentials.AllParameters() {
		actual[parameter.Location+":"+parameter.Name] = parameter.Value
	}
	if err != nil || result.IsError || actual["header:Authorization"] != "Bearer upstream-secret" ||
		actual["header:aaaa"] != "service-secret" || actual["header:X-API-Key"] != "header-secret" ||
		actual["query:key"] != "query-secret" ||
		caller.name != "working_directory" || connections.recorded.Result != "success" ||
		connections.recorded.UserID != "" {
		t.Fatalf("网关调用或统计不正确: result=%#v err=%v record=%#v", result, err, connections.recorded)
	}
}

func TestMCPGatewayRejectsInvalidDeveloperCredential(t *testing.T) {
	connections := &mcpConnectionsStub{route: mcpconnection.GatewayConnection{
		Connection:    mcpconnection.Connection{ID: "connection-1", Code: "test_mcp", Purpose: "developer", AuthMode: "bearer", Status: "active"},
		ServiceStatus: "active",
	}}
	router := New(Dependencies{
		AppName: "Harness Admin", Environment: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth: &accountAuthStub{session: accountTestSession()}, Access: accountAccessStub{},
		MCPConnections: connections, MCPToolCaller: &mcpToolCallerStub{}, OIDCProvider: &oidcProviderStub{},
	})
	request := httptest.NewRequest(http.MethodPost, "/mcp/test_mcp", nil)
	request.Header.Set("Authorization", "Bearer wrong")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || connections.recorded.ToolName != "" {
		t.Fatalf("错误凭据未拒绝: status=%d body=%s", response.Code, response.Body.String())
	}
}

type mcpEmployeeProviderStub struct{ oidcProviderStub }

func (*mcpEmployeeProviderStub) AuthenticateAccessToken(_ context.Context, token, scope string) (oidc.Subject, error) {
	if scope != "mcp.invoke" || token == "no-scope" || token == "" {
		return oidc.Subject{}, oidc.ErrInvalidToken
	}
	if token == "allowed" {
		return oidc.Subject{ID: "user-1", OrganizationID: "org-1"}, nil
	}
	if token == "other" {
		return oidc.Subject{ID: "user-2", OrganizationID: "org-1"}, nil
	}
	return oidc.Subject{}, oidc.ErrInvalidToken
}

type mcpEmployeeConnectionsStub struct {
	*mcpConnectionsStub
	grantActive bool
}

func (s *mcpEmployeeConnectionsStub) AuthorizeUser(_ context.Context, _ mcpconnection.GatewayConnection, organizationID, userID string) (mcpconnection.Caller, error) {
	if organizationID != "org-1" || userID != "user-1" || !s.grantActive {
		return mcpconnection.Caller{}, mcpconnection.ErrForbidden
	}
	return mcpconnection.Caller{UserID: userID, DepartmentID: "department-1"}, nil
}

func (s *mcpEmployeeConnectionsStub) ListForUser(_ context.Context, organizationID, userID string) ([]mcpconnection.Connection, error) {
	if organizationID != "org-1" || userID != "user-1" || !s.grantActive {
		return nil, nil
	}
	return []mcpconnection.Connection{{Code: "employee_mcp", Name: "员工测试连接", ServiceName: "托管 MCP"}}, nil
}

func TestMCPEmployeeGatewayAndDirectoryEnforceScopeGrantAndRevocation(t *testing.T) {
	connections := &mcpEmployeeConnectionsStub{
		mcpConnectionsStub: &mcpConnectionsStub{route: mcpconnection.GatewayConnection{
			Connection: mcpconnection.Connection{ID: "connection-1", Code: "employee_mcp", Name: "员工测试连接",
				Purpose: "harness", AuthMode: "oidc", Status: "active"},
			OrganizationID: "org-1", ServiceStatus: "active", Transport: "stdio", SourceType: "github",
			DeploymentStatus: "running", Tools: []mcpadmin.Tool{{Name: "read_only_tool", InputSchema: json.RawMessage(`{"type":"object"}`)}},
		}},
		grantActive: true,
	}
	toolCaller := &mcpToolCallerStub{}
	router := New(Dependencies{
		AppName: "Harness Admin", Environment: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth: &accountAuthStub{session: accountTestSession()}, Access: accountAccessStub{},
		MCPConnections: connections, MCPToolCaller: toolCaller, OIDCProvider: &mcpEmployeeProviderStub{},
	})
	server := httptest.NewServer(router)
	defer server.Close()
	directory := func(token string) (int, string) {
		request, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/mcp/connections", nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+token)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		return response.StatusCode, string(body)
	}
	status, body := directory("no-scope")
	if status != http.StatusUnauthorized {
		t.Fatalf("缺少 mcp.invoke 的目录请求未拒绝: %d %s", status, body)
	}
	status, body = directory("allowed")
	if status != http.StatusOK || !strings.Contains(body, `"code":"employee_mcp"`) ||
		!strings.Contains(body, `"url":"https://id.example.com/mcp/employee_mcp"`) {
		t.Fatalf("获授权员工目录不正确: %d %s", status, body)
	}
	status, body = directory("other")
	if status != http.StatusOK || strings.Contains(body, "employee_mcp") {
		t.Fatalf("未授权员工看到了连接: %d %s", status, body)
	}
	gatewayStatus := func(token, payload string) int {
		request, err := http.NewRequest(http.MethodPost, server.URL+"/mcp/employee_mcp", strings.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json, text/event-stream")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		_, _ = io.Copy(io.Discard, response.Body)
		return response.StatusCode
	}
	listPayload := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`
	callPayload := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"read_only_tool","arguments":{}}}`
	if got := gatewayStatus("no-scope", listPayload); got != http.StatusUnauthorized {
		t.Fatalf("缺少 mcp.invoke 的网关请求未拒绝: %d", got)
	}
	if got := gatewayStatus("other", callPayload); got != http.StatusForbidden || toolCaller.calls != 0 {
		t.Fatalf("未授权员工的网关请求未拒绝: %d", got)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "employee-test", Version: "1.0"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint: server.URL + "/mcp/employee_mcp", HTTPClient: &http.Client{Transport: bearerTransport{token: "allowed"}},
		DisableStandaloneSSE: true, MaxRetries: -1,
	}, nil)
	if err != nil {
		t.Fatalf("获授权员工连接失败: %v", err)
	}
	defer session.Close()
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil || len(tools.Tools) != 1 || tools.Tools[0].Name != "read_only_tool" {
		t.Fatalf("员工工具目录不正确: %#v, %v", tools, err)
	}
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "read_only_tool", Arguments: map[string]any{}})
	if err != nil || result.IsError || connections.recorded.UserID != "user-1" || connections.recorded.DepartmentID != "department-1" {
		t.Fatalf("员工调用或统计身份不正确: result=%#v err=%v record=%#v", result, err, connections.recorded)
	}
	connections.grantActive = false
	status, body = directory("allowed")
	if status != http.StatusOK || strings.Contains(body, "employee_mcp") {
		t.Fatalf("撤权后目录仍可见: %d %s", status, body)
	}
	if got := gatewayStatus("allowed", callPayload); got != http.StatusForbidden || toolCaller.calls != 1 {
		t.Fatalf("撤权后网关仍可调用: %d", got)
	}
}
