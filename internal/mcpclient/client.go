package mcpclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

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

func (c *Client) connect(ctx context.Context, service mcpadmin.ServiceRecord, credentials mcpadmin.UpstreamCredentials) (*mcp.ClientSession, error) {
	if !mcpadmin.ValidUpstreamCredentials(credentials) {
		return nil, mcpadmin.ErrInvalidInput
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
	session, err := mcp.NewClient(&mcp.Implementation{Name: "harness-admin", Version: "0.1.0"}, nil).Connect(ctx, transport, nil)
	if err != nil {
		return nil, errors.New("无法连接上游 MCP 服务")
	}
	return session, nil
}

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
