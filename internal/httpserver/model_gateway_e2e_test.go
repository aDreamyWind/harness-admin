package httpserver

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"harness-admin/internal/modelgateway"
	"harness-admin/internal/modelprovider"
)

const gatewayE2ESecret = "harness_sk_e2e_test_abcdefghijklmnopqrstuvwxyz"

// 此夹具保留真实的网关业务服务、HTTP 路由和供应商转发器，仅替换持久层与上游服务。
type gatewayE2ERepository struct {
	modelgateway.Repository
	mu    sync.Mutex
	route modelgateway.Route
	calls []modelgateway.RecordCallInput
}

func (r *gatewayE2ERepository) FindAPIKeyByHash(_ context.Context, hash []byte) (modelgateway.APIKey, error) {
	expected := sha256.Sum256([]byte(gatewayE2ESecret))
	if string(hash) != string(expected[:]) {
		return modelgateway.APIKey{}, modelgateway.ErrAPIKeyNotFound
	}
	return modelgateway.APIKey{ID: "key-1", OrganizationID: "org-1", Status: "active"}, nil
}

func (r *gatewayE2ERepository) FindRoute(_ context.Context, organizationID, code string) (modelgateway.Route, error) {
	if organizationID != "org-1" || code != "main" {
		return modelgateway.Route{}, modelgateway.ErrModelNotFound
	}
	return r.route, nil
}

func (*gatewayE2ERepository) TouchAPIKey(context.Context, string) error { return nil }

func (r *gatewayE2ERepository) RecordCall(_ context.Context, input modelgateway.RecordCallInput) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, input)
	return nil
}

func (r *gatewayE2ERepository) recordedCalls() []modelgateway.RecordCallInput {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]modelgateway.RecordCallInput(nil), r.calls...)
}

func newGatewayE2EServer(t *testing.T, upstreamURL string) (*httptest.Server, *gatewayE2ERepository) {
	t.Helper()
	repository := &gatewayE2ERepository{route: modelgateway.Route{
		ModelID: "model-1", ModelCode: "main", ModelStatus: "active", UpstreamModelID: "mock-model-v1",
		ProviderID: "provider-1", ProviderStatus: "active", ProviderBaseURL: upstreamURL + "/v1",
		ProviderAPIKey: "mock-provider-secret", ProviderTimeoutMS: 5000, ProviderStreamUsageEnabled: true,
	}}
	server := httptest.NewServer(New(Dependencies{
		AppName: "Harness Admin", Environment: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth: &accountAuthStub{session: accountTestSession()}, Access: accountAccessStub{},
		ModelGateway:     modelgateway.NewService(repository, nil, nil),
		GatewayForwarder: modelprovider.NewGatewayForwarder([]string{"127.0.0.1"}),
	}))
	t.Cleanup(server.Close)
	return server, repository
}

func gatewayE2ERequest(t *testing.T, target, body string) *http.Request {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, target+"/v1/chat/completions", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+gatewayE2ESecret)
	request.Header.Set("Content-Type", "application/json")
	return request
}

func TestGatewayE2EStreamsToolCallBeforeCompletionAndRecordsUsage(t *testing.T) {
	release := make(chan struct{})
	var releaseOnce sync.Once
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/v1/chat/completions" ||
			request.Header.Get("Authorization") != "Bearer mock-provider-secret" {
			t.Errorf("上游请求路径或鉴权错误: %s %s", request.Method, request.URL.Path)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		var body struct {
			Model         string `json:"model"`
			Stream        bool   `json:"stream"`
			StreamOptions struct {
				IncludeUsage bool `json:"include_usage"`
			} `json:"stream_options"`
			Messages []json.RawMessage `json:"messages"`
			Tools    []json.RawMessage `json:"tools"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil || body.Model != "mock-model-v1" || !body.Stream ||
			!body.StreamOptions.IncludeUsage || len(body.Messages) != 1 || len(body.Tools) != 1 ||
			!strings.Contains(string(body.Messages[0]), `"image_url"`) {
			t.Errorf("上游请求未保留流式、工具、多模态内容或未替换模型: %#v, %v", body, err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-1\",\"type\":\"function\",\"function\":{\"name\":\"search\",\"arguments\":\"{\\\"q\\\":\"}}]}}]}\n\n")
		writer.(http.Flusher).Flush()
		select {
		case <-release:
		case <-request.Context().Done():
			return
		}
		_, _ = io.WriteString(writer, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"\\\"ok\\\"}\"}}]}}]}\n\n")
		_, _ = io.WriteString(writer, "data: {\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":5,\"total_tokens\":8}}\n\n")
		_, _ = io.WriteString(writer, "data: [DONE]\n\n")
	}))
	defer upstream.Close()
	defer releaseOnce.Do(func() { close(release) })
	server, repository := newGatewayE2EServer(t, upstream.URL)
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Do(gatewayE2ERequest(t, server.URL, `{"model":"main","messages":[{"role":"user","content":[{"type":"text","text":"搜索"},{"type":"image_url","image_url":{"url":"https://example.com/image.png"}}]}],"tools":[{"type":"function","function":{"name":"search","parameters":{"type":"object"}}}],"stream":true}`))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("流式响应错误: %d %s", response.StatusCode, response.Header.Get("Content-Type"))
	}
	reader := bufio.NewReader(response.Body)
	firstChunk, err := reader.ReadString('\n')
	if err != nil || !strings.Contains(firstChunk, `"tool_calls"`) {
		t.Fatalf("第一块工具调用未实时送达: %q, %v", firstChunk, err)
	}
	if calls := repository.recordedCalls(); len(calls) != 0 {
		t.Fatalf("上游尚未结束，不应提前完成记账: %#v", calls)
	}
	releaseOnce.Do(func() { close(release) })
	rest, err := io.ReadAll(reader)
	if err != nil || !strings.Contains(string(rest), "data: [DONE]") || !strings.Contains(string(rest), `"total_tokens":8`) {
		t.Fatalf("后续 SSE 或用量块丢失: %s, %v", rest, err)
	}
	var toolArguments strings.Builder
	for _, line := range strings.Split(firstChunk+string(rest), "\n") {
		if !strings.HasPrefix(line, "data: ") || line == "data: [DONE]" {
			continue
		}
		var event struct {
			Choices []struct {
				Delta struct {
					ToolCalls []struct {
						Function struct {
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
			t.Fatalf("SSE 数据块不是合法 JSON: %s: %v", line, err)
		}
		for _, choice := range event.Choices {
			for _, call := range choice.Delta.ToolCalls {
				toolArguments.WriteString(call.Function.Arguments)
			}
		}
	}
	if toolArguments.String() != `{"q":"ok"}` {
		t.Fatalf("分块工具参数未完整透传: %q", toolArguments.String())
	}
	calls := repository.recordedCalls()
	if len(calls) != 1 || !calls[0].Streaming || !calls[0].UsageReported || calls[0].TotalTokens != 8 ||
		calls[0].PromptTokens != 3 || calls[0].CompletionTokens != 5 || calls[0].Result != "success" {
		t.Fatalf("流式工具调用用量记账错误: %#v", calls)
	}
}

func TestGatewayE2EPreservesNonStreamingToolCallsAndUnknownUsage(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil || body.Model != "mock-model-v1" {
			t.Errorf("上游模型重写失败: %#v, %v", body, err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"call-2","type":"function","function":{"name":"search","arguments":"{}"}}]}}]}`)
	}))
	defer upstream.Close()
	server, repository := newGatewayE2EServer(t, upstream.URL)
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Do(gatewayE2ERequest(t, server.URL, `{"model":"main","messages":[],"stream":false}`))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != http.StatusOK || !strings.Contains(string(body), `"tool_calls"`) {
		t.Fatalf("非流式工具调用响应丢失: status=%d body=%s err=%v", response.StatusCode, body, err)
	}
	calls := repository.recordedCalls()
	if len(calls) != 1 || calls[0].UsageReported || calls[0].TotalTokens != 0 || calls[0].Result != "success" {
		t.Fatalf("上游未报告用量时不能误当零 Token 用量: %#v", calls)
	}
}
