package modelprovider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"harness-admin/internal/modelgateway"
)

func TestImageGenerationAllowsLongResponse(t *testing.T) {
	client := &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{ResponseHeaderTimeout: 15 * time.Second}}
	allowImageGenerationTime(client)
	if client.Timeout != imageGenerationTimeout || client.Transport.(*http.Transport).ResponseHeaderTimeout != imageGenerationTimeout {
		t.Fatalf("图片生成超时未延长到 %s", imageGenerationTimeout)
	}
}

func TestGatewayForwarderClassifiesInvalidProviderRoute(t *testing.T) {
	_, err := NewGatewayForwarder(nil).Forward(context.Background(), modelgateway.Route{
		ProviderBaseURL: "not-a-url", ModelType: "image",
	}, []byte(`{"model":"image-model","prompt":"测试"}`), false)
	if !errors.Is(err, ErrInvalidProviderRoute) {
		t.Fatalf("无效供应商地址应与网络故障区分: %v", err)
	}
}

func TestGatewayForwarderRoutesImageAndVideoProtocols(t *testing.T) {
	baseURL, err := url.Parse("https://workspace.cn-beijing.maas.aliyuncs.com/compatible-mode/v1")
	if err != nil {
		t.Fatal(err)
	}
	route := modelgateway.Route{ProviderAPIKey: "provider-secret", ModelType: "image", ProviderKind: "aliyun_bailian"}
	endpoint, err := endpointForRoute(baseURL, route, false)
	if err != nil || endpoint != baseURL.String()+"/images/generations" {
		t.Fatalf("图片地址错误: %s %v", endpoint, err)
	}
	route.ModelType = "video"
	endpoint, err = endpointForRoute(baseURL, route, false)
	if err != nil || endpoint != "https://workspace.cn-beijing.maas.aliyuncs.com/api/v1/services/aigc/video-generation/video-synthesis" {
		t.Fatalf("视频地址错误: %s %v", endpoint, err)
	}
	request, err := newGatewayRequest(context.Background(), endpoint, route, []byte(`{"model":"happyhorse-1.1-t2v"}`), false)
	if err != nil || request.Header.Get("X-DashScope-Async") != "enable" ||
		request.Header.Get("Authorization") != "Bearer provider-secret" || request.Header.Get("Accept") != "application/json" {
		t.Fatalf("视频请求头错误: %#v %v", request, err)
	}
	if taskURL := videoTaskEndpoint(baseURL, "task/1"); !strings.HasSuffix(taskURL, "/api/v1/tasks/task%2F1") {
		t.Fatalf("任务查询地址错误: %s", taskURL)
	}
	route.ProviderKind = "openai_compatible"
	if _, err := endpointForRoute(baseURL, route, false); err == nil {
		t.Fatal("非百炼连接不应转发到百炼视频接口")
	}
}

func TestGatewayRequestAcceptMatchesStreamingMode(t *testing.T) {
	route := modelgateway.Route{ProviderAPIKey: "provider-secret", ModelType: "image"}
	request, err := newGatewayRequest(context.Background(), "https://dashscope.aliyuncs.com/api/v1/services/aigc/multimodal-generation/generation", route, []byte(`{}`), false)
	if err != nil || request.Header.Get("Accept") != "application/json" {
		t.Fatalf("非流式图片请求应接收 JSON: %v %v", request, err)
	}
	route.ModelType = "chat"
	request, err = newGatewayRequest(context.Background(), "https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions", route, []byte(`{}`), true)
	if err != nil || request.Header.Get("Accept") != "text/event-stream" {
		t.Fatalf("流式对话请求应接收 SSE: %v %v", request, err)
	}
}

func TestDashScopeNativeImageAdapter(t *testing.T) {
	baseURL, err := url.Parse("https://dashscope.aliyuncs.com/compatible-mode/v1")
	if err != nil {
		t.Fatal(err)
	}
	route := modelgateway.Route{ModelType: "image", ProviderKind: "aliyun_bailian"}
	endpoint, err := endpointForRoute(baseURL, route, false)
	if err != nil || endpoint != "https://dashscope.aliyuncs.com/api/v1/services/aigc/multimodal-generation/generation" {
		t.Fatalf("旧域名图片地址错误: %s %v", endpoint, err)
	}
	adapted, err := dashScopeImageRequest([]byte(`{"model":"qwen-image-3.0","prompt":"红纸鹤","image":["https://example.com/a.png"],"size":"1024x1024","n":1,"prompt_extend":true,"response_format":"url"}`))
	if err != nil {
		t.Fatal(err)
	}
	var request struct {
		Model string `json:"model"`
		Input struct {
			Messages []struct {
				Content []map[string]string `json:"content"`
			} `json:"messages"`
		} `json:"input"`
		Parameters struct {
			Size         string `json:"size"`
			N            int    `json:"n"`
			PromptExtend bool   `json:"prompt_extend"`
		} `json:"parameters"`
	}
	if err := json.Unmarshal(adapted, &request); err != nil {
		t.Fatal(err)
	}
	if request.Model != "qwen-image-3.0" || request.Parameters.Size != "1024*1024" || request.Parameters.N != 1 ||
		!request.Parameters.PromptExtend || len(request.Input.Messages) != 1 || len(request.Input.Messages[0].Content) != 2 ||
		request.Input.Messages[0].Content[0]["image"] != "https://example.com/a.png" || request.Input.Messages[0].Content[1]["text"] != "红纸鹤" {
		t.Fatalf("图片请求转换错误: %s", adapted)
	}
	response := &http.Response{Body: io.NopCloser(strings.NewReader(`{"output":{"choices":[{"message":{"content":[{"image":"https://example.com/result.png"}]}}]},"request_id":"upstream-1"}`)), Header: make(http.Header)}
	if err := normalizeDashScopeImageResponse(response); err != nil {
		t.Fatal(err)
	}
	result, err := io.ReadAll(response.Body)
	if err != nil || !strings.Contains(string(result), `"url":"https://example.com/result.png"`) || !strings.Contains(string(result), `"request_id":"upstream-1"`) {
		t.Fatalf("图片响应转换错误: %s %v", result, err)
	}
	if _, err := dashScopeImageRequest([]byte(`{"model":"qwen-image-3.0","prompt":"红纸鹤","image":[]}`)); err == nil {
		t.Fatal("空参考图不应发送给上游")
	}
}

func TestDashScopeNativeImagePreservesEmbeddedProviderError(t *testing.T) {
	for _, test := range []struct {
		name, body, code string
		status           int
	}{
		{name: "JSON 错误", body: `{"code":"Model.AccessDenied","message":"model is not available"}`, code: "Model.AccessDenied", status: http.StatusBadGateway},
		{name: "SSE 错误", body: ":HTTP_STATUS/403\nevent:error\ndata:{\"code\":\"AccessDenied\",\"message\":\"denied\"}\n", code: "AccessDenied", status: http.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(test.body)), Header: make(http.Header)}
			if err := normalizeDashScopeImageResponse(response); err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(response.Body)
			if err != nil || response.StatusCode != test.status || !strings.Contains(string(body), test.code) || response.Header.Get("Content-Type") != "application/json" {
				t.Fatalf("错误未保留: status=%d body=%s err=%v", response.StatusCode, body, err)
			}
		})
	}
}
