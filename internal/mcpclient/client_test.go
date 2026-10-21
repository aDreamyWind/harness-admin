package mcpclient

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"harness-admin/internal/mcpadmin"
)

func TestStreamableClientDiscoversAndCallsTool(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "test-upstream", Version: "1.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "working_directory", Description: "读取当前目录"},
		func(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "/work"}}}, nil, nil
		})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-secret" ||
			r.Header.Get("aaaa") != "header-secret" || r.URL.Query().Get("key") != "query-secret" ||
			r.URL.Path != "/company-a/mcp" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	defer upstream.Close()
	client := New([]string{"127.0.0.1"})
	service := mcpadmin.ServiceRecord{UpstreamURL: upstream.URL + "/{tenant}/mcp", Transport: "streamable_http"}
	credentials := mcpadmin.UpstreamCredentials{
		Parameters: []mcpadmin.ParameterValue{
			{Location: "header", Name: "Authorization", Value: "Bearer test-secret"},
			{Location: "header", Name: "aaaa", Value: "header-secret"},
			{Location: "query", Name: "key", Value: "query-secret"},
			{Location: "path", Name: "tenant", Value: "company-a"},
		},
	}
	tools, err := client.ListTools(context.Background(), service, credentials)
	if err != nil || len(tools) != 1 || tools[0].Name != "working_directory" {
		t.Fatalf("工具发现失败: %v, %v", tools, err)
	}
	result, err := client.CallTool(context.Background(), service, credentials, "working_directory", json.RawMessage(`{}`))
	if err != nil || !strings.Contains(string(result), "/work") {
		t.Fatalf("工具调用失败: %s, %v", result, err)
	}
}

func TestUnsafeIP(t *testing.T) {
	for _, value := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "::1"} {
		if !unsafeIP(net.ParseIP(value)) {
			t.Errorf("未阻止受保护地址 %s", value)
		}
	}
	if unsafeIP(net.ParseIP("8.8.8.8")) {
		t.Fatal("公共地址被错误阻止")
	}
	if !forbiddenIP(net.ParseIP("169.254.169.254")) || forbiddenIP(net.ParseIP("127.0.0.1")) {
		t.Fatal("链路本地地址必须始终拒绝；管理员明确允许时仍可访问本机回环地址")
	}
}
