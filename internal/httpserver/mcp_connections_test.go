package httpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"harness-admin/internal/mcpadmin"
	"harness-admin/internal/mcpclient"
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

type mcpContentCallerStub struct{ mcpToolCallerStub }

type mcpSkillCallerStub struct{ mcpContentCallerStub }

func (*mcpSkillCallerStub) SupportsSkills(context.Context, mcpadmin.ServiceRecord, mcpadmin.UpstreamCredentials) (bool, error) {
	return true, nil
}
func (*mcpSkillCallerStub) ListSkills(context.Context, mcpadmin.ServiceRecord, mcpadmin.UpstreamCredentials, string) (*mcpclient.ListSkillsResult, error) {
	return &mcpclient.ListSkillsResult{ResultType: "complete", TTLMS: 300000, CacheScope: "public", Skills: []mcpclient.SkillEntry{{
		URI: "skill://lunch-helper/SKILL.md", Frontmatter: map[string]any{"name": "lunch-helper", "description": "推荐午餐"},
		Resources: json.RawMessage(`"dynamic"`),
	}}}, nil
}
func (*mcpSkillCallerStub) GetSkill(_ context.Context, _ mcpadmin.ServiceRecord, _ mcpadmin.UpstreamCredentials, uri string) (*mcpclient.GetSkillResult, error) {
	return &mcpclient.GetSkillResult{ResultType: "complete", TTLMS: 300000, CacheScope: "public", Skill: mcpclient.SkillEntry{
		URI: uri, Frontmatter: map[string]any{"name": "lunch-helper", "description": "推荐午餐"}, Resources: json.RawMessage(`"dynamic"`),
	}}, nil
}

func (*mcpContentCallerStub) ListResources(context.Context, mcpadmin.ServiceRecord, mcpadmin.UpstreamCredentials) ([]*mcp.Resource, []*mcp.ResourceTemplate, error) {
	return []*mcp.Resource{{URI: "test://menu", Name: "menu", MIMEType: "text/plain"}},
		[]*mcp.ResourceTemplate{{URITemplate: "test://menu/{day}", Name: "daily-menu"}}, nil
}
func (*mcpContentCallerStub) ReadResource(_ context.Context, _ mcpadmin.ServiceRecord, _ mcpadmin.UpstreamCredentials, uri string) (*mcp.ReadResourceResult, error) {
	return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: uri, Text: "午餐菜单"}}}, nil
}
func (*mcpContentCallerStub) ListPrompts(context.Context, mcpadmin.ServiceRecord, mcpadmin.UpstreamCredentials) ([]*mcp.Prompt, error) {
	return []*mcp.Prompt{{Name: "lunch", Arguments: []*mcp.PromptArgument{{Name: "person", Required: true}}}}, nil
}
func (*mcpContentCallerStub) GetPrompt(_ context.Context, _ mcpadmin.ServiceRecord, _ mcpadmin.UpstreamCredentials, _ string, args map[string]string) (*mcp.GetPromptResult, error) {
	return &mcp.GetPromptResult{Messages: []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: "给" + args["person"] + "推荐午餐"}}}}, nil
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

func TestMCPEmployeeLegacySSERequiresGrantOnEachMessage(t *testing.T) {
	connections := &mcpEmployeeConnectionsStub{
		mcpConnectionsStub: &mcpConnectionsStub{route: mcpconnection.GatewayConnection{
			Connection: mcpconnection.Connection{ID: "connection-1", Code: "employee_mcp", Name: "员工测试连接",
				Purpose: "harness", AuthMode: "oidc", Status: "active"},
			OrganizationID: "org-1", ServiceStatus: "active", Transport: "stdio", SourceType: "github",
			DeploymentStatus: "running", Tools: []mcpadmin.Tool{{Name: "read_only_tool", InputSchema: json.RawMessage(`{"type":"object"}`)}},
		}}, grantActive: true,
	}
	toolCaller := &mcpToolCallerStub{}
	router := New(Dependencies{
		AppName: "Harness Admin", Environment: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth: &accountAuthStub{session: accountTestSession()}, Access: accountAccessStub{},
		MCPConnections: connections, MCPToolCaller: toolCaller, OIDCProvider: &mcpEmployeeProviderStub{},
	})
	server := httptest.NewServer(router)
	defer server.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "sse-test", Version: "1.0"}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := client.Connect(ctx, &mcp.SSEClientTransport{
		Endpoint:   server.URL + "/mcp/employee_mcp/sse",
		HTTPClient: &http.Client{Transport: bearerTransport{token: "allowed"}},
	}, nil)
	if err != nil {
		t.Fatalf("连接旧版 SSE 网关失败: %v", err)
	}
	defer session.Close()
	tools, err := session.ListTools(ctx, nil)
	if err != nil || len(tools.Tools) != 1 {
		t.Fatalf("SSE 工具目录不正确: %#v, %v", tools, err)
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "read_only_tool", Arguments: map[string]any{}})
	if err != nil || result.IsError || toolCaller.calls != 1 {
		t.Fatalf("SSE 工具调用失败: %#v, %v, calls=%d", result, err, toolCaller.calls)
	}
	connections.grantActive = false
	callCtx, stop := context.WithTimeout(context.Background(), 2*time.Second)
	defer stop()
	_, err = session.CallTool(callCtx, &mcp.CallToolParams{Name: "read_only_tool", Arguments: map[string]any{}})
	if err == nil || toolCaller.calls != 1 {
		t.Fatalf("撤权后旧版 SSE 会话仍可调用: err=%v, calls=%d", err, toolCaller.calls)
	}
}

func TestMCPGatewayProxiesResourcesAndPrompts(t *testing.T) {
	connections := &mcpEmployeeConnectionsStub{
		mcpConnectionsStub: &mcpConnectionsStub{route: mcpconnection.GatewayConnection{
			Connection: mcpconnection.Connection{ID: "connection-1", Code: "employee_mcp", Name: "员工测试连接",
				Purpose: "harness", AuthMode: "oidc", Status: "active"},
			OrganizationID: "org-1", ServiceStatus: "active", Transport: "stdio", SourceType: "github", DeploymentStatus: "running",
		}}, grantActive: true,
	}
	router := New(Dependencies{
		AppName: "Harness Admin", Environment: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth: &accountAuthStub{session: accountTestSession()}, Access: accountAccessStub{},
		MCPConnections: connections, MCPToolCaller: &mcpContentCallerStub{}, OIDCProvider: &mcpEmployeeProviderStub{},
	})
	server := httptest.NewServer(router)
	defer server.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "content-gateway-test", Version: "1.0"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint: server.URL + "/mcp/employee_mcp", HTTPClient: &http.Client{Transport: bearerTransport{token: "allowed"}},
		DisableStandaloneSSE: true, MaxRetries: -1,
	}, nil)
	if err != nil {
		t.Fatalf("连接资源网关失败: %v", err)
	}
	defer session.Close()
	resources, err := session.ListResources(context.Background(), nil)
	if err != nil || len(resources.Resources) != 1 || resources.Resources[0].URI != "test://menu" {
		t.Fatalf("资源列表不正确: %#v, %v", resources, err)
	}
	templates, err := session.ListResourceTemplates(context.Background(), nil)
	if err != nil || len(templates.ResourceTemplates) != 1 {
		t.Fatalf("资源模板列表不正确: %#v, %v", templates, err)
	}
	content, err := session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: "test://menu"})
	if err != nil || len(content.Contents) != 1 || content.Contents[0].Text != "午餐菜单" {
		t.Fatalf("资源读取不正确: %#v, %v", content, err)
	}
	prompts, err := session.ListPrompts(context.Background(), nil)
	if err != nil || len(prompts.Prompts) != 1 || prompts.Prompts[0].Name != "lunch" {
		t.Fatalf("提示词列表不正确: %#v, %v", prompts, err)
	}
	prompt, err := session.GetPrompt(context.Background(), &mcp.GetPromptParams{Name: "lunch", Arguments: map[string]string{"person": "小明"}})
	if err != nil || len(prompt.Messages) != 1 {
		t.Fatalf("提示词读取不正确: %#v, %v", prompt, err)
	}
	connections.grantActive = false
	_, err = session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: "test://menu"})
	if err == nil {
		t.Fatal("撤权后资源仍可读取")
	}
}

func TestMCPGatewayProxiesSkillsExtension(t *testing.T) {
	connections := &mcpEmployeeConnectionsStub{
		mcpConnectionsStub: &mcpConnectionsStub{route: mcpconnection.GatewayConnection{
			Connection: mcpconnection.Connection{ID: "connection-1", Code: "employee_mcp", Name: "员工测试连接",
				Purpose: "harness", AuthMode: "oidc", Status: "active"},
			OrganizationID: "org-1", ServiceStatus: "active", Transport: "stdio", SourceType: "github", DeploymentStatus: "running",
		}}, grantActive: true,
	}
	router := New(Dependencies{
		AppName: "Harness Admin", Environment: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth: &accountAuthStub{session: accountTestSession()}, Access: accountAccessStub{},
		MCPConnections: connections, MCPToolCaller: &mcpSkillCallerStub{}, OIDCProvider: &mcpEmployeeProviderStub{},
	})
	server := httptest.NewServer(router)
	defer server.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "skill-gateway-test", Version: "1.0"}, nil)
	if err := mcp.AddSendingCustomMethod[*mcpclient.ListSkillsParams, *mcpclient.ListSkillsResult](client, "skills/list"); err != nil {
		t.Fatal(err)
	}
	if err := mcp.AddSendingCustomMethod[*mcpclient.GetSkillParams, *mcpclient.GetSkillResult](client, "skills/get"); err != nil {
		t.Fatal(err)
	}
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint: server.URL + "/mcp/employee_mcp", HTTPClient: &http.Client{Transport: bearerTransport{token: "allowed"}},
		DisableStandaloneSSE: true, MaxRetries: -1,
	}, nil)
	if err != nil {
		t.Fatalf("连接 Skills 网关失败: %v", err)
	}
	defer session.Close()
	if _, ok := session.InitializeResult().Capabilities.Extensions[mcpclient.SkillsExtension]; !ok {
		t.Fatal("网关未声明 Skills 扩展")
	}
	listed, err := mcp.CallCustomMethod[*mcpclient.ListSkillsParams, *mcpclient.ListSkillsResult](context.Background(), session, "skills/list", &mcpclient.ListSkillsParams{})
	if err != nil || len(listed.Skills) != 1 {
		t.Fatalf("Skills 目录不正确: %#v, %v", listed, err)
	}
	got, err := mcp.CallCustomMethod[*mcpclient.GetSkillParams, *mcpclient.GetSkillResult](context.Background(), session, "skills/get", &mcpclient.GetSkillParams{URI: "skill://lunch-helper/SKILL.md"})
	if err != nil || got.Skill.URI != "skill://lunch-helper/SKILL.md" {
		t.Fatalf("Skill 元数据不正确: %#v, %v", got, err)
	}
	content, err := session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: "skill://lunch-helper/SKILL.md"})
	if err != nil || len(content.Contents) != 1 {
		t.Fatalf("Skill 文件不可读取: %#v, %v", content, err)
	}
	connections.grantActive = false
	_, err = session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: "skill://lunch-helper/SKILL.md"})
	if err == nil {
		t.Fatal("撤权后仍可读取 Skill 文件")
	}
}

func TestMCPGatewayWithRealSSEUpstreamResourcesAndPrompts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	const resourceURI = "test://sse-menu"
	upstreamServer := mcp.NewServer(&mcp.Implementation{Name: "SSE 上游验收", Version: "1.0"},
		&mcp.ServerOptions{})
	upstreamServer.AddResource(&mcp.Resource{URI: resourceURI, Name: "sse-menu", MIMEType: "text/plain"},
		func(_ context.Context, _ *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: resourceURI, Text: "SSE 上游菜单"}}}, nil
		})
	upstreamServer.AddPrompt(&mcp.Prompt{Name: "hello", Arguments: []*mcp.PromptArgument{{Name: "name", Required: true}}},
		func(_ context.Context, request *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			return &mcp.GetPromptResult{Messages: []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: "你好，" + request.Params.Arguments["name"]}}}}, nil
		})
	upstreamHandler := mcp.NewSSEHandler(func(*http.Request) *mcp.Server { return upstreamServer }, nil)
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer upstream-test" {
			http.Error(writer, "missing upstream credential", http.StatusUnauthorized)
			return
		}
		upstreamHandler.ServeHTTP(writer, request)
	}))
	defer upstream.Close()
	connections := &mcpEmployeeConnectionsStub{
		mcpConnectionsStub: &mcpConnectionsStub{route: mcpconnection.GatewayConnection{
			Connection: mcpconnection.Connection{ID: "connection-1", Code: "employee_mcp", Name: "SSE 测试连接",
				Purpose: "harness", AuthMode: "oidc", Status: "active"},
			OrganizationID: "org-1", ServiceStatus: "active", Transport: "sse", UpstreamURL: upstream.URL,
			UpstreamAuthorization: "Bearer upstream-test",
		}}, grantActive: true,
	}
	router := New(Dependencies{
		AppName: "Harness Admin", Environment: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth: &accountAuthStub{session: accountTestSession()}, Access: accountAccessStub{},
		MCPConnections: connections, MCPToolCaller: mcpclient.New([]string{"127.0.0.1"}), OIDCProvider: &mcpEmployeeProviderStub{},
	})
	gateway := httptest.NewServer(router)
	defer gateway.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "gateway-sse-upstream-test", Version: "1.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: gateway.URL + "/mcp/employee_mcp", HTTPClient: &http.Client{Transport: bearerTransport{token: "allowed"}},
		DisableStandaloneSSE: true, MaxRetries: -1,
	}, nil)
	if err != nil {
		t.Fatalf("经 Admin 网关连接 SSE 上游失败: %v", err)
	}
	defer session.Close()
	resources, err := session.ListResources(ctx, nil)
	if err != nil || len(resources.Resources) != 1 || resources.Resources[0].URI != resourceURI {
		t.Fatalf("SSE 上游资源目录未透传: %#v, %v", resources, err)
	}
	content, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: resourceURI})
	if err != nil || len(content.Contents) != 1 || content.Contents[0].Text != "SSE 上游菜单" {
		t.Fatalf("SSE 上游资源内容未透传: %#v, %v", content, err)
	}
	prompts, err := session.ListPrompts(ctx, nil)
	if err != nil || len(prompts.Prompts) != 1 || prompts.Prompts[0].Name != "hello" {
		t.Fatalf("SSE 上游提示词目录未透传: %#v, %v", prompts, err)
	}
	prompt, err := session.GetPrompt(ctx, &mcp.GetPromptParams{Name: "hello", Arguments: map[string]string{"name": "小明"}})
	if err != nil || len(prompt.Messages) != 1 || prompt.Messages[0].Content.(*mcp.TextContent).Text != "你好，小明" {
		t.Fatalf("SSE 上游提示词内容未透传: %#v, %v", prompt, err)
	}
	legacyClient := mcp.NewClient(&mcp.Implementation{Name: "legacy-sse-gateway-test", Version: "1.0"}, nil)
	legacySession, err := legacyClient.Connect(ctx, &mcp.SSEClientTransport{
		Endpoint: gateway.URL + "/mcp/employee_mcp/sse", HTTPClient: &http.Client{Transport: bearerTransport{token: "allowed"}},
	}, nil)
	if err != nil {
		t.Fatalf("旧版 SSE 客户端连接 Admin 网关失败: %v", err)
	}
	defer legacySession.Close()
	legacyResources, err := legacySession.ListResources(ctx, nil)
	if err != nil || len(legacyResources.Resources) != 1 || legacyResources.Resources[0].URI != resourceURI {
		t.Fatalf("旧版 SSE 客户端未读取到真实上游资源: %#v, %v", legacyResources, err)
	}
	legacyPrompt, err := legacySession.GetPrompt(ctx, &mcp.GetPromptParams{Name: "hello", Arguments: map[string]string{"name": "小红"}})
	if err != nil || len(legacyPrompt.Messages) != 1 || legacyPrompt.Messages[0].Content.(*mcp.TextContent).Text != "你好，小红" {
		t.Fatalf("旧版 SSE 客户端未读取到真实上游提示词: %#v, %v", legacyPrompt, err)
	}
	connections.grantActive = false
	if _, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: resourceURI}); err == nil {
		t.Fatal("撤销员工授权后仍可经 SSE 上游读取资源")
	}
	if _, err := legacySession.ReadResource(ctx, &mcp.ReadResourceParams{URI: resourceURI}); err == nil {
		t.Fatal("撤销员工授权后旧版 SSE 会话仍可读取资源")
	}
}

func TestMCPGatewayWithRealStreamableUpstreamSkills(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	const skillURI = "skill://lunch-helper/SKILL.md"
	capabilities := &mcp.ServerCapabilities{Resources: &mcp.ResourceCapabilities{}}
	capabilities.AddExtension(mcpclient.SkillsExtension, nil)
	upstreamServer := mcp.NewServer(&mcp.Implementation{Name: "Skills 上游验收", Version: "1.0"},
		&mcp.ServerOptions{Capabilities: capabilities})
	upstreamServer.AddResource(&mcp.Resource{URI: skillURI, Name: "lunch-helper", MIMEType: "text/markdown"},
		func(_ context.Context, _ *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: skillURI, Text: "---\nname: lunch-helper\ndescription: 推荐午餐\n---\n选择午餐。\n"}}}, nil
		})
	skill := mcpclient.SkillEntry{URI: skillURI, Frontmatter: map[string]any{"name": "lunch-helper", "description": "推荐午餐"},
		Resources: json.RawMessage(`"dynamic"`)}
	if err := mcp.AddReceivingCustomMethod(upstreamServer, "skills/list",
		func(_ context.Context, _ *mcp.ServerSession, _ *mcpclient.ListSkillsParams) (*mcpclient.ListSkillsResult, error) {
			return &mcpclient.ListSkillsResult{ResultType: "complete", Skills: []mcpclient.SkillEntry{skill}, TTLMS: 300000, CacheScope: "public"}, nil
		}); err != nil {
		t.Fatal(err)
	}
	if err := mcp.AddReceivingCustomMethod(upstreamServer, "skills/get",
		func(_ context.Context, _ *mcp.ServerSession, _ *mcpclient.GetSkillParams) (*mcpclient.GetSkillResult, error) {
			return &mcpclient.GetSkillResult{ResultType: "complete", Skill: skill, TTLMS: 300000, CacheScope: "public"}, nil
		}); err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return upstreamServer },
		&mcp.StreamableHTTPOptions{Stateless: true}))
	defer upstream.Close()
	connections := &mcpEmployeeConnectionsStub{
		mcpConnectionsStub: &mcpConnectionsStub{route: mcpconnection.GatewayConnection{
			Connection: mcpconnection.Connection{ID: "connection-1", Code: "employee_mcp", Name: "Skills 测试连接",
				Purpose: "harness", AuthMode: "oidc", Status: "active"},
			OrganizationID: "org-1", ServiceStatus: "active", Transport: "streamable_http", UpstreamURL: upstream.URL,
		}}, grantActive: true,
	}
	router := New(Dependencies{
		AppName: "Harness Admin", Environment: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth: &accountAuthStub{session: accountTestSession()}, Access: accountAccessStub{},
		MCPConnections: connections, MCPToolCaller: mcpclient.New([]string{"127.0.0.1"}), OIDCProvider: &mcpEmployeeProviderStub{},
	})
	gateway := httptest.NewServer(router)
	defer gateway.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "gateway-streamable-skill-test", Version: "1.0"}, nil)
	if err := mcp.AddSendingCustomMethod[*mcpclient.ListSkillsParams, *mcpclient.ListSkillsResult](client, "skills/list"); err != nil {
		t.Fatal(err)
	}
	if err := mcp.AddSendingCustomMethod[*mcpclient.GetSkillParams, *mcpclient.GetSkillResult](client, "skills/get"); err != nil {
		t.Fatal(err)
	}
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: gateway.URL + "/mcp/employee_mcp", HTTPClient: &http.Client{Transport: bearerTransport{token: "allowed"}},
		DisableStandaloneSSE: true, MaxRetries: -1,
	}, nil)
	if err != nil {
		t.Fatalf("经 Admin 网关连接 Skills 上游失败: %v", err)
	}
	defer session.Close()
	if _, ok := session.InitializeResult().Capabilities.Extensions[mcpclient.SkillsExtension]; !ok {
		t.Fatal("真实上游的 Skills 扩展未通过 Admin 网关声明")
	}
	listed, err := mcp.CallCustomMethod[*mcpclient.ListSkillsParams, *mcpclient.ListSkillsResult](ctx, session, "skills/list", &mcpclient.ListSkillsParams{})
	if err != nil || len(listed.Skills) != 1 || listed.Skills[0].URI != skillURI {
		t.Fatalf("真实上游 Skill 目录未透传: %#v, %v", listed, err)
	}
	got, err := mcp.CallCustomMethod[*mcpclient.GetSkillParams, *mcpclient.GetSkillResult](ctx, session, "skills/get", &mcpclient.GetSkillParams{URI: skillURI})
	if err != nil || got.Skill.URI != skillURI {
		t.Fatalf("真实上游 Skill 元数据未透传: %#v, %v", got, err)
	}
	file, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: skillURI})
	if err != nil || len(file.Contents) != 1 || !strings.Contains(file.Contents[0].Text, "选择午餐") {
		t.Fatalf("真实上游 Skill 文件未透传: %#v, %v", file, err)
	}
	connections.grantActive = false
	if _, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: skillURI}); err == nil {
		t.Fatal("撤权后仍可读取真实上游 Skill 文件")
	}
}

// This fixture implements the legacy HTTP+SSE wire directly because the Go
// SDK's SSE handler rejects custom MCP methods before dispatching them.
func newSkillsSSEUpstream(t *testing.T, skillURI string) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	sessions := make(map[string]chan []byte)
	nextID := 0
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer upstream-test" {
			http.Error(writer, "missing upstream credential", http.StatusUnauthorized)
			return
		}
		switch request.Method {
		case http.MethodGet:
			writer.Header().Set("Content-Type", "text/event-stream")
			writer.Header().Set("Cache-Control", "no-cache")
			flusher, ok := writer.(http.Flusher)
			if !ok {
				t.Error("SSE fixture lacks a flusher")
				return
			}
			mu.Lock()
			nextID++
			id := fmt.Sprint(nextID)
			messages := make(chan []byte, 8)
			sessions[id] = messages
			mu.Unlock()
			defer func() { mu.Lock(); delete(sessions, id); mu.Unlock() }()
			fmt.Fprintf(writer, "event: endpoint\ndata: /messages?sessionId=%s\n\n", id)
			flusher.Flush()
			for {
				select {
				case payload := <-messages:
					fmt.Fprintf(writer, "event: message\ndata: %s\n\n", payload)
					flusher.Flush()
				case <-request.Context().Done():
					return
				}
			}
		case http.MethodPost:
			id := request.URL.Query().Get("sessionId")
			mu.Lock()
			messages := sessions[id]
			mu.Unlock()
			if messages == nil {
				http.Error(writer, "unknown session", http.StatusNotFound)
				return
			}
			var call struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
			}
			if err := json.NewDecoder(io.LimitReader(request.Body, 1<<20)).Decode(&call); err != nil {
				http.Error(writer, "invalid JSON-RPC", http.StatusBadRequest)
				return
			}
			if len(call.ID) == 0 {
				writer.WriteHeader(http.StatusAccepted)
				return
			}
			entry := mcpclient.SkillEntry{URI: skillURI,
				Frontmatter: map[string]any{"name": "lunch-helper", "description": "推荐午餐"},
				Resources:   json.RawMessage(`"dynamic"`)}
			var result any
			switch call.Method {
			case "initialize":
				var params struct {
					ProtocolVersion string `json:"protocolVersion"`
				}
				if err := json.Unmarshal(call.Params, &params); err != nil {
					t.Error(err)
				}
				result = map[string]any{"protocolVersion": params.ProtocolVersion,
					"capabilities": map[string]any{"resources": map[string]any{},
						"extensions": map[string]any{mcpclient.SkillsExtension: map[string]any{}}},
					"serverInfo": map[string]any{"name": "compatible-sse-skills", "version": "1.0"}}
			case "ping":
				result = map[string]any{}
			case "skills/list":
				result = mcpclient.ListSkillsResult{ResultType: "complete", Skills: []mcpclient.SkillEntry{entry}, TTLMS: 300000, CacheScope: "public"}
			case "skills/get":
				result = mcpclient.GetSkillResult{ResultType: "complete", Skill: entry, TTLMS: 300000, CacheScope: "public"}
			case "resources/list":
				result = map[string]any{"resources": []any{map[string]any{"uri": skillURI, "name": "lunch-helper", "mimeType": "text/markdown"}}}
			case "resources/read":
				result = map[string]any{"contents": []any{map[string]any{"uri": skillURI, "mimeType": "text/markdown", "text": "# 推荐午餐\n"}}}
			case "resources/templates/list":
				result = map[string]any{"resourceTemplates": []any{}}
			default:
				writer.WriteHeader(http.StatusAccepted)
				messages <- []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"method not found"}}`, call.ID))
				return
			}
			payload, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": call.ID, "result": result})
			if err != nil {
				t.Error(err)
				http.Error(writer, "marshal failed", http.StatusInternalServerError)
				return
			}
			writer.WriteHeader(http.StatusAccepted)
			messages <- payload
		default:
			writer.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	return httptest.NewServer(handler)
}

func TestMCPGatewayWithCompatibleSSEUpstreamSkills(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	const skillURI = "skill://lunch-helper/SKILL.md"
	upstream := newSkillsSSEUpstream(t, skillURI)
	defer upstream.Close()
	connections := &mcpEmployeeConnectionsStub{
		mcpConnectionsStub: &mcpConnectionsStub{route: mcpconnection.GatewayConnection{
			Connection: mcpconnection.Connection{ID: "connection-1", Code: "employee_mcp", Name: "SSE Skills 测试连接",
				Purpose: "harness", AuthMode: "oidc", Status: "active"},
			OrganizationID: "org-1", ServiceStatus: "active", Transport: "sse", UpstreamURL: upstream.URL,
			UpstreamAuthorization: "Bearer upstream-test",
		}}, grantActive: true,
	}
	router := New(Dependencies{AppName: "Harness Admin", Environment: "test",
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Auth: &accountAuthStub{session: accountTestSession()},
		Access: accountAccessStub{}, MCPConnections: connections, MCPToolCaller: mcpclient.New([]string{"127.0.0.1"}),
		OIDCProvider: &mcpEmployeeProviderStub{}})
	gateway := httptest.NewServer(router)
	defer gateway.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "sse-skills-gateway-test", Version: "1.0"}, nil)
	if err := mcp.AddSendingCustomMethod[*mcpclient.ListSkillsParams, *mcpclient.ListSkillsResult](client, "skills/list"); err != nil {
		t.Fatal(err)
	}
	if err := mcp.AddSendingCustomMethod[*mcpclient.GetSkillParams, *mcpclient.GetSkillResult](client, "skills/get"); err != nil {
		t.Fatal(err)
	}
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: gateway.URL + "/mcp/employee_mcp",
		HTTPClient: &http.Client{Transport: bearerTransport{token: "allowed"}}, DisableStandaloneSSE: true, MaxRetries: -1}, nil)
	if err != nil {
		t.Fatalf("连接 SSE Skills 上游失败: %v", err)
	}
	defer session.Close()
	if _, ok := session.InitializeResult().Capabilities.Extensions[mcpclient.SkillsExtension]; !ok {
		t.Fatal("SSE Skills 扩展未声明")
	}
	listed, err := mcp.CallCustomMethod[*mcpclient.ListSkillsParams, *mcpclient.ListSkillsResult](ctx, session, "skills/list", &mcpclient.ListSkillsParams{})
	if err != nil || len(listed.Skills) != 1 || listed.Skills[0].URI != skillURI {
		t.Fatalf("SSE Skills 目录未透传: %#v, %v", listed, err)
	}
	got, err := mcp.CallCustomMethod[*mcpclient.GetSkillParams, *mcpclient.GetSkillResult](ctx, session, "skills/get", &mcpclient.GetSkillParams{URI: skillURI})
	if err != nil || got.Skill.URI != skillURI {
		t.Fatalf("SSE Skill 元数据未透传: %#v, %v", got, err)
	}
	file, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: skillURI})
	if err != nil || len(file.Contents) != 1 || file.Contents[0].Text != "# 推荐午餐\n" {
		t.Fatalf("SSE Skill 文件未透传: %#v, %v", file, err)
	}
	legacyClient := mcp.NewClient(&mcp.Implementation{Name: "legacy-sse-skills-gateway-test", Version: "1.0"}, nil)
	if err := mcp.AddSendingCustomMethod[*mcpclient.ListSkillsParams, *mcpclient.ListSkillsResult](legacyClient, "skills/list"); err != nil {
		t.Fatal(err)
	}
	legacySession, err := legacyClient.Connect(ctx, &mcp.SSEClientTransport{
		Endpoint: gateway.URL + "/mcp/employee_mcp/sse", HTTPClient: &http.Client{Transport: bearerTransport{token: "allowed"}},
	}, nil)
	if err != nil {
		t.Fatalf("旧版 SSE 客户端连接 Skills 网关失败: %v", err)
	}
	defer legacySession.Close()
	legacyList, err := mcp.CallCustomMethod[*mcpclient.ListSkillsParams, *mcpclient.ListSkillsResult](ctx, legacySession, "skills/list", &mcpclient.ListSkillsParams{})
	if err != nil || len(legacyList.Skills) != 1 || legacyList.Skills[0].URI != skillURI {
		t.Fatalf("旧版 SSE 客户端未读取到 Skill 目录: %#v, %v", legacyList, err)
	}
	connections.grantActive = false
	if _, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: skillURI}); err == nil {
		t.Fatal("撤权后仍可读取 SSE Skill 文件")
	}
	if _, err := legacySession.ReadResource(ctx, &mcp.ReadResourceParams{URI: skillURI}); err == nil {
		t.Fatal("撤权后旧版 SSE 客户端仍可读取 Skill 文件")
	}
}
