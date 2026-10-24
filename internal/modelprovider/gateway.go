package modelprovider

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"harness-admin/internal/modelgateway"
)

type GatewayForwarder struct {
	prober *Prober
}

const imageGenerationTimeout = 10 * time.Minute

var ErrInvalidProviderRoute = errors.New("模型供应商路由配置无效")
var ErrInvalidImageRequest = errors.New("图片请求参数无效")

func NewGatewayForwarder(allowPrivateHosts []string) *GatewayForwarder {
	return &GatewayForwarder{prober: NewProber(allowPrivateHosts)}
}

func (f *GatewayForwarder) Forward(ctx context.Context, route modelgateway.Route, body []byte, streaming bool) (*http.Response, error) {
	baseURL, err := url.Parse(route.ProviderBaseURL)
	if err != nil || (baseURL.Scheme != "https" && baseURL.Scheme != "http") || baseURL.Hostname() == "" {
		return nil, fmt.Errorf("%w: 模型供应商地址无效", ErrInvalidProviderRoute)
	}
	if baseURL.Scheme == "http" {
		if _, allowed := f.prober.allowPrivateHosts[strings.ToLower(baseURL.Hostname())]; !allowed {
			return nil, fmt.Errorf("%w: HTTP 模型供应商地址必须加入私有主机白名单", ErrInvalidProviderRoute)
		}
	}
	client, err := f.prober.clientFor(ctx, baseURL.Hostname(), time.Duration(route.ProviderTimeoutMS)*time.Millisecond)
	if err != nil {
		return nil, err
	}
	if streaming {
		client.Timeout = 0
	} else if route.ModelType == "image" {
		allowImageGenerationTime(client)
	}
	endpoint, err := endpointForRoute(baseURL, route, streaming)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidProviderRoute, err)
	}
	if usesDashScopeNativeImage(baseURL, route) {
		body, err = dashScopeImageRequest(body)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidImageRequest, err)
		}
	}
	request, err := newGatewayRequest(ctx, endpoint, route, body, streaming)
	if err != nil {
		return nil, fmt.Errorf("%w: 无法创建模型供应商请求: %v", ErrInvalidProviderRoute, err)
	}
	response, err := client.Do(request)
	if err != nil || !usesDashScopeNativeImage(baseURL, route) || response.StatusCode < 200 || response.StatusCode >= 300 {
		return response, err
	}
	if err := normalizeDashScopeImageResponse(response); err != nil {
		response.Body.Close()
		return nil, err
	}
	return response, nil
}

func allowImageGenerationTime(client *http.Client) {
	if client.Timeout >= imageGenerationTimeout {
		return
	}
	client.Timeout = imageGenerationTimeout
	if transport, ok := client.Transport.(*http.Transport); ok {
		transport.ResponseHeaderTimeout = imageGenerationTimeout
	}
}

func endpointForRoute(baseURL *url.URL, route modelgateway.Route, streaming bool) (string, error) {
	switch route.ModelType {
	case "", "chat":
		return strings.TrimRight(baseURL.String(), "/") + "/chat/completions", nil
	case "image":
		if usesDashScopeNativeImage(baseURL, route) {
			return baseURL.Scheme + "://" + baseURL.Host + "/api/v1/services/aigc/multimodal-generation/generation", nil
		}
		return strings.TrimRight(baseURL.String(), "/") + "/images/generations", nil
	case "video":
		if route.ProviderKind != "aliyun_bailian" || streaming {
			return "", errors.New("供应商不支持该视频调用协议")
		}
		return baseURL.Scheme + "://" + baseURL.Host + "/api/v1/services/aigc/video-generation/video-synthesis", nil
	default:
		return "", errors.New("供应商不支持该模型类型")
	}
}

func newGatewayRequest(ctx context.Context, endpoint string, route modelgateway.Route, body []byte, streaming bool) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if streaming {
		request.Header.Set("Accept", "text/event-stream")
	} else {
		request.Header.Set("Accept", "application/json")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+route.ProviderAPIKey)
	if route.ModelType == "video" {
		request.Header.Set("X-DashScope-Async", "enable")
	}
	return request, nil
}

func (f *GatewayForwarder) QueryVideoTask(ctx context.Context, route modelgateway.Route, upstreamTaskID string) (*http.Response, error) {
	if route.ProviderKind != "aliyun_bailian" || route.ModelType != "video" || upstreamTaskID == "" {
		return nil, errors.New("供应商不支持该视频任务查询协议")
	}
	baseURL, err := url.Parse(route.ProviderBaseURL)
	if err != nil || (baseURL.Scheme != "https" && baseURL.Scheme != "http") || baseURL.Hostname() == "" {
		return nil, errors.New("模型供应商地址无效")
	}
	if baseURL.Scheme == "http" {
		if _, allowed := f.prober.allowPrivateHosts[strings.ToLower(baseURL.Hostname())]; !allowed {
			return nil, errors.New("HTTP 模型供应商地址必须加入私有主机白名单")
		}
	}
	client, err := f.prober.clientFor(ctx, baseURL.Hostname(), time.Duration(route.ProviderTimeoutMS)*time.Millisecond)
	if err != nil {
		return nil, err
	}
	endpoint := videoTaskEndpoint(baseURL, upstreamTaskID)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, errors.New("无法创建视频任务查询请求")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+route.ProviderAPIKey)
	return client.Do(request)
}

func videoTaskEndpoint(baseURL *url.URL, upstreamTaskID string) string {
	return baseURL.Scheme + "://" + baseURL.Host + "/api/v1/tasks/" + url.PathEscape(upstreamTaskID)
}
