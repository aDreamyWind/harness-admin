package modelprovider

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"harness-admin/internal/modelgateway"
)

type GatewayForwarder struct {
	prober *Prober
}

func NewGatewayForwarder(allowPrivateHosts []string) *GatewayForwarder {
	return &GatewayForwarder{prober: NewProber(allowPrivateHosts)}
}

func (f *GatewayForwarder) Forward(ctx context.Context, route modelgateway.Route, body []byte, streaming bool) (*http.Response, error) {
	baseURL, err := url.Parse(route.ProviderBaseURL)
	if err != nil || (baseURL.Scheme != "https" && baseURL.Scheme != "http") {
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
	if streaming {
		client.Timeout = 0
	}
	endpoint := strings.TrimRight(baseURL.String(), "/") + "/chat/completions"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("无法创建模型供应商请求")
	}
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+route.ProviderAPIKey)
	return client.Do(request)
}
