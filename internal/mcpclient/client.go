package mcpclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/yosida95/uritemplate/v3"

	"harness-admin/internal/mcpadmin"
)

type Client struct {
	allowPrivate map[string]bool
	resolver     *net.Resolver
}

func New(allowPrivateHosts []string) *Client {
	allowed := make(map[string]bool)
	for _, host := range allowPrivateHosts {
		host = strings.ToLower(strings.TrimSpace(host))
		if host != "" {
			allowed[host] = true
		}
	}
	return &Client{allowPrivate: allowed, resolver: net.DefaultResolver}
}

func (c *Client) ListTools(ctx context.Context, service mcpadmin.ServiceRecord, credentials mcpadmin.UpstreamCredentials) ([]mcpadmin.Tool, error) {
	session, err := c.connect(ctx, service, credentials)
	if err != nil {
		return nil, err
	}
	defer session.Close()
	items := make([]mcpadmin.Tool, 0)
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			return nil, errors.New("读取上游工具列表失败")
		}
		schema, err := json.Marshal(tool.InputSchema)
		if err != nil || len(schema) > 128*1024 {
			return nil, errors.New("上游工具参数结构无效或过大")
		}
		var schemaObject map[string]any
		if err := json.Unmarshal(schema, &schemaObject); err != nil || schemaObject["type"] != "object" {
			return nil, errors.New("上游工具输入结构不是 JSON 对象")
		}
		items = append(items, mcpadmin.Tool{
			Name: tool.Name, Description: tool.Description,
			InputSchema: schema, DiscoveredAt: time.Now().UTC(),
		})
		if len(items) > 500 {
			return nil, errors.New("上游工具数量超过限制")
		}
	}
	return items, nil
}

func (c *Client) CallTool(ctx context.Context, service mcpadmin.ServiceRecord, credentials mcpadmin.UpstreamCredentials, name string, arguments json.RawMessage) (json.RawMessage, error) {
	session, err := c.connect(ctx, service, credentials)
	if err != nil {
		return nil, err
	}
	defer session.Close()
	var args map[string]any
	if err := json.Unmarshal(arguments, &args); err != nil {
		return nil, mcpadmin.ErrInvalidInput
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return nil, errors.New("上游工具调用失败")
	}
	raw, err := json.Marshal(result)
	if err != nil || len(raw) > 2*1024*1024 {
		return nil, errors.New("上游工具返回数据无效或过大")
	}
	return raw, nil
}

// ListResources returns the upstream resource catalog without copying resource content.
// An upstream server that does not advertise resources has an empty catalog.
func (c *Client) ListResources(ctx context.Context, service mcpadmin.ServiceRecord, credentials mcpadmin.UpstreamCredentials) ([]*mcp.Resource, []*mcp.ResourceTemplate, error) {
	session, err := c.connect(ctx, service, credentials)
	if err != nil {
		return nil, nil, err
	}
	defer session.Close()
	initial := session.InitializeResult()
	if initial == nil || initial.Capabilities == nil || initial.Capabilities.Resources == nil {
		return nil, nil, nil
	}
	resources := make([]*mcp.Resource, 0)
	for item, err := range session.Resources(ctx, nil) {
		if err != nil || item == nil || !validResourceURI(item.URI) || len(resources) >= 500 {
			return nil, nil, errors.New("上游资源目录无效或过大")
		}
		resources = append(resources, item)
	}
	templates := make([]*mcp.ResourceTemplate, 0)
	for item, err := range session.ResourceTemplates(ctx, nil) {
		var rpcErr *jsonrpc.Error
		if errors.As(err, &rpcErr) && rpcErr.Code == jsonrpc.CodeMethodNotFound {
			break
		}
		if err != nil || item == nil || !validResourceTemplate(item.URITemplate) || len(templates) >= 500 {
			return nil, nil, errors.New("上游资源模板目录无效或过大")
		}
		templates = append(templates, item)
	}
	return resources, templates, nil
}

func (c *Client) ReadResource(ctx context.Context, service mcpadmin.ServiceRecord, credentials mcpadmin.UpstreamCredentials, uri string) (*mcp.ReadResourceResult, error) {
	if !validResourceURI(uri) {
		return nil, mcpadmin.ErrInvalidInput
	}
	session, err := c.connect(ctx, service, credentials)
	if err != nil {
		return nil, err
	}
	defer session.Close()
	result, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: uri})
	if err != nil {
		return nil, errors.New("读取上游资源失败")
	}
	raw, err := json.Marshal(result)
	limit := 2 * 1024 * 1024
	if strings.HasPrefix(uri, "skill://") {
		limit = 24 * 1024 * 1024
	}
	if err != nil || len(raw) > limit {
		return nil, errors.New("上游资源内容无效或过大")
	}
	return result, nil
}

// ListPrompts returns prompt definitions, not rendered messages.
func (c *Client) ListPrompts(ctx context.Context, service mcpadmin.ServiceRecord, credentials mcpadmin.UpstreamCredentials) ([]*mcp.Prompt, error) {
	session, err := c.connect(ctx, service, credentials)
	if err != nil {
		return nil, err
	}
	defer session.Close()
	initial := session.InitializeResult()
	if initial == nil || initial.Capabilities == nil || initial.Capabilities.Prompts == nil {
		return nil, nil
	}
	items := make([]*mcp.Prompt, 0)
	for item, err := range session.Prompts(ctx, nil) {
		if err != nil || item == nil || item.Name == "" || len(item.Name) > 256 || len(items) >= 500 {
			return nil, errors.New("上游提示词目录无效或过大")
		}
		items = append(items, item)
	}
	return items, nil
}

func (c *Client) GetPrompt(ctx context.Context, service mcpadmin.ServiceRecord, credentials mcpadmin.UpstreamCredentials, name string, args map[string]string) (*mcp.GetPromptResult, error) {
	if name == "" || len(name) > 256 || len(args) > 100 {
		return nil, mcpadmin.ErrInvalidInput
	}
	session, err := c.connect(ctx, service, credentials)
	if err != nil {
		return nil, err
	}
	defer session.Close()
	result, err := session.GetPrompt(ctx, &mcp.GetPromptParams{Name: name, Arguments: args})
	if err != nil {
		return nil, errors.New("获取上游提示词失败")
	}
	raw, err := json.Marshal(result)
	if err != nil || len(raw) > 2*1024*1024 {
		return nil, errors.New("上游提示词内容无效或过大")
	}
	return result, nil
}

func validResourceURI(value string) bool {
	if value == "" || len(value) > 2048 {
		return false
	}
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme != ""
}

func validResourceTemplate(value string) bool {
	if value == "" || len(value) > 2048 {
		return false
	}
	if _, err := uritemplate.New(value); err != nil {
		return false
	}
	colon := strings.IndexByte(value, ':')
	return colon > 0 && validResourceURI(value[:colon]+":/")
}

func (c *Client) connect(ctx context.Context, service mcpadmin.ServiceRecord, credentials mcpadmin.UpstreamCredentials) (*mcp.ClientSession, error) {
	if !mcpadmin.ValidUpstreamCredentials(credentials) {
		return nil, mcpadmin.ErrInvalidInput
	}
	if service.Transport == "stdio" {
		if service.DeploymentStatus != "running" || service.SourceType == "remote" ||
			service.ContainerName != "hadmin-mcp-"+service.ID || len(credentials.AllParameters()) != 0 ||
			service.EntryFile == "" || path.Clean(service.EntryFile) != service.EntryFile || strings.HasPrefix(service.EntryFile, "/") ||
			strings.HasPrefix(service.EntryFile, "../") {
			return nil, mcpadmin.ErrInvalidInput
		}
		command := "node"
		if service.RuntimeType == "python" {
			command = "python"
		} else if service.RuntimeType != "node" {
			return nil, mcpadmin.ErrInvalidInput
		}
		arguments := []string{"exec", "-i", service.ContainerName, command}
		if command == "python" {
			arguments = append(arguments, "-u")
		}
		arguments = append(arguments, "/app/"+service.EntryFile)
		commandProcess := exec.CommandContext(ctx, "docker", arguments...)
		stderr := &limitedStderr{}
		commandProcess.Stderr = stderr
		transport := &mcp.CommandTransport{Command: commandProcess}
		protocolClient, err := newProtocolClient()
		if err != nil {
			return nil, err
		}
		session, err := protocolClient.Connect(ctx, transport, nil)
		if err != nil {
			return nil, fmt.Errorf("无法连接托管的 Stdio MCP 服务: %w (%s)", err, stderr.String())
		}
		return session, nil
	}
	endpointURL := service.UpstreamURL
	for _, item := range credentials.AllParameters() {
		if item.Location == "path" {
			endpointURL = strings.ReplaceAll(endpointURL, "{"+item.Name+"}", url.PathEscape(item.Value))
		}
	}
	if strings.Contains(endpointURL, "{") || strings.Contains(endpointURL, "}") {
		return nil, mcpadmin.ErrInvalidInput
	}
	endpoint, err := url.Parse(endpointURL)
	if err != nil || endpoint.Hostname() == "" {
		return nil, mcpadmin.ErrInvalidInput
	}
	client := &http.Client{
		Transport:     &safeTransport{base: endpoint, credentials: credentials, resolver: c.resolver, allowPrivate: c.allowPrivate},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	var transport mcp.Transport
	switch service.Transport {
	case "streamable_http":
		transport = &mcp.StreamableClientTransport{
			Endpoint: endpointURL, HTTPClient: client, MaxRetries: -1, DisableStandaloneSSE: true,
		}
	case "sse":
		transport = &mcp.SSEClientTransport{Endpoint: endpointURL, HTTPClient: client}
	default:
		return nil, mcpadmin.ErrInvalidInput
	}
	protocolClient, err := newProtocolClient()
	if err != nil {
		return nil, err
	}
	session, err := protocolClient.Connect(ctx, transport, nil)
	if err != nil {
		return nil, errors.New("无法连接上游 MCP 服务")
	}
	return session, nil
}

type limitedStderr struct {
	mu   sync.Mutex
	data []byte
}

func (s *limitedStderr) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = append(s.data, p...)
	if len(s.data) > 4096 {
		s.data = append([]byte(nil), s.data[len(s.data)-4096:]...)
	}
	return len(p), nil
}
func (s *limitedStderr) String() string { s.mu.Lock(); defer s.mu.Unlock(); return string(s.data) }

type safeTransport struct {
	base         *url.URL
	credentials  mcpadmin.UpstreamCredentials
	resolver     *net.Resolver
	allowPrivate map[string]bool
}

func (t *safeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// SSE 服务会给出消息提交地址；凭据只允许发送到登记的同一源。
	if !strings.EqualFold(req.URL.Scheme, t.base.Scheme) || !strings.EqualFold(req.URL.Host, t.base.Host) {
		return nil, errors.New("上游 MCP 请求地址超出登记服务")
	}
	hostname := strings.ToLower(t.base.Hostname())
	if t.base.Scheme == "http" && !t.allowPrivate[hostname] {
		return nil, errors.New("HTTP MCP 地址必须加入私有主机白名单")
	}
	addresses, err := t.resolver.LookupIPAddr(req.Context(), hostname)
	if err != nil || len(addresses) == 0 {
		return nil, errors.New("无法解析 MCP 服务地址")
	}
	allowed := make([]net.IP, 0, len(addresses))
	for _, address := range addresses {
		if !forbiddenIP(address.IP) && (t.allowPrivate[hostname] || !unsafeIP(address.IP)) {
			allowed = append(allowed, address.IP)
		}
	}
	if len(allowed) == 0 {
		return nil, errors.New("MCP 服务地址指向受保护网络")
	}
	cloned := req.Clone(req.Context())
	cloned.Header.Del("Authorization")
	for _, item := range t.credentials.AllParameters() {
		switch item.Location {
		case "header":
			cloned.Header.Set(item.Name, item.Value)
		case "query":
			query := cloned.URL.Query()
			query.Set(item.Name, item.Value)
			cloned.URL.RawQuery = query.Encode()
		}
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			_, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, errors.New("MCP 网络地址格式不正确")
			}
			var lastErr error
			for _, ip := range allowed {
				connection, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
				if err == nil {
					return connection, nil
				}
				lastErr = err
			}
			return nil, fmt.Errorf("无法连接 MCP 服务: %w", lastErr)
		},
		TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 15 * time.Second,
	}
	defer transport.CloseIdleConnections()
	return transport.RoundTrip(cloned)
}

func unsafeIP(ip net.IP) bool {
	if forbiddenIP(ip) || ip.IsLoopback() || ip.IsPrivate() {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		first, _ := strconv.Atoi(strings.Split(v4.String(), ".")[0])
		return first == 0 || first >= 224
	}
	return false
}

func forbiddenIP(ip net.IP) bool {
	return ip == nil || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsMulticast()
}
