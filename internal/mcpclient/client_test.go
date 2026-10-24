package mcpclient

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
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

func TestStreamableClientReadsResourcesAndPrompts(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "content-upstream", Version: "1.0"}, nil)
	server.AddResource(&mcp.Resource{URI: "test://menu", Name: "menu", MIMEType: "text/plain"},
		func(_ context.Context, _ *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: "test://menu", Text: "午餐菜单"}}}, nil
		})
	server.AddResourceTemplate(&mcp.ResourceTemplate{URITemplate: "test://menu/{day}", Name: "daily-menu"},
		func(_ context.Context, request *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: request.Params.URI, Text: "今日菜单"}}}, nil
		})
	server.AddPrompt(&mcp.Prompt{Name: "lunch", Arguments: []*mcp.PromptArgument{{Name: "person", Required: true}}},
		func(_ context.Context, request *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			return &mcp.GetPromptResult{Messages: []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: "给" + request.Params.Arguments["person"] + "推荐午餐"}}}}, nil
		})
	upstream := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil))
	defer upstream.Close()
	client := New([]string{"127.0.0.1"})
	service := mcpadmin.ServiceRecord{UpstreamURL: upstream.URL, Transport: "streamable_http"}
	resources, templates, err := client.ListResources(context.Background(), service, mcpadmin.UpstreamCredentials{})
	if err != nil || len(resources) != 1 || resources[0].URI != "test://menu" || len(templates) != 1 {
		t.Fatalf("资源目录不正确: %#v, %#v, %v", resources, templates, err)
	}
	content, err := client.ReadResource(context.Background(), service, mcpadmin.UpstreamCredentials{}, "test://menu")
	if err != nil || len(content.Contents) != 1 || content.Contents[0].Text != "午餐菜单" {
		t.Fatalf("资源读取不正确: %#v, %v", content, err)
	}
	prompts, err := client.ListPrompts(context.Background(), service, mcpadmin.UpstreamCredentials{})
	if err != nil || len(prompts) != 1 || prompts[0].Name != "lunch" {
		t.Fatalf("提示词目录不正确: %#v, %v", prompts, err)
	}
	prompt, err := client.GetPrompt(context.Background(), service, mcpadmin.UpstreamCredentials{}, "lunch", map[string]string{"person": "小明"})
	if err != nil || len(prompt.Messages) != 1 {
		t.Fatalf("提示词读取不正确: %#v, %v", prompt, err)
	}
}

func TestStreamableClientReadsSkillsExtension(t *testing.T) {
	capabilities := &mcp.ServerCapabilities{Resources: &mcp.ResourceCapabilities{}}
	capabilities.AddExtension(SkillsExtension, nil)
	server := mcp.NewServer(&mcp.Implementation{Name: "skills-upstream", Version: "1.0"},
		&mcp.ServerOptions{Capabilities: capabilities})
	content := "---\nname: lunch-helper\ndescription: 推荐午餐\n---\n请给出午餐建议。\n"
	uri := "skill://lunch-helper/SKILL.md"
	server.AddResource(&mcp.Resource{URI: uri, Name: "lunch-helper", MIMEType: "text/markdown"},
		func(_ context.Context, _ *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: uri, Text: content}}}, nil
		})
	entry := SkillEntry{URI: uri, Frontmatter: map[string]any{"name": "lunch-helper", "description": "推荐午餐"},
		Resources: json.RawMessage(fmt.Sprintf(`[{"uri":%q,"digest":%q,"size":%d}]`, uri,
			fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(content))), len(content)))}
	if err := mcp.AddReceivingCustomMethod(server, "skills/list",
		func(_ context.Context, _ *mcp.ServerSession, _ *ListSkillsParams) (*ListSkillsResult, error) {
			return &ListSkillsResult{ResultType: "complete", Skills: []SkillEntry{entry}, TTLMS: 300000, CacheScope: "public"}, nil
		}); err != nil {
		t.Fatal(err)
	}
	if err := mcp.AddReceivingCustomMethod(server, "skills/get",
		func(_ context.Context, _ *mcp.ServerSession, _ *GetSkillParams) (*GetSkillResult, error) {
			return &GetSkillResult{ResultType: "complete", Skill: entry, TTLMS: 300000, CacheScope: "public"}, nil
		}); err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil))
	defer upstream.Close()
	client := New([]string{"127.0.0.1"})
	service := mcpadmin.ServiceRecord{UpstreamURL: upstream.URL, Transport: "streamable_http"}
	credentials := mcpadmin.UpstreamCredentials{}
	supported, err := client.SupportsSkills(context.Background(), service, credentials)
	if err != nil || !supported {
		t.Fatalf("未发现 Skills 扩展: %v, %v", supported, err)
	}
	listed, err := client.ListSkills(context.Background(), service, credentials, "")
	if err != nil || len(listed.Skills) != 1 || listed.Skills[0].URI != uri {
		t.Fatalf("读取 Skills 目录失败: %#v, %v", listed, err)
	}
	got, err := client.GetSkill(context.Background(), service, credentials, uri)
	if err != nil || got.Skill.URI != uri {
		t.Fatalf("读取 Skill 元数据失败: %#v, %v", got, err)
	}
	read, err := client.ReadResource(context.Background(), service, credentials, uri)
	if err != nil || len(read.Contents) != 1 || read.Contents[0].Text != content {
		t.Fatalf("读取 Skill 文件失败: %#v, %v", read, err)
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
