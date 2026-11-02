package app

import (
	"context"
	"encoding/json"
	"harness-admin/internal/drama"
	"io"
	"net/http"
	"strings"
	"testing"

	"harness-admin/internal/modelgateway"
)

type dramaGatewayStub struct{ record modelgateway.RecordCallInput }

func (*dramaGatewayStub) PrepareForUser(_ context.Context, org, user string, body []byte) (modelgateway.PreparedRequest, error) {
	return modelgateway.PreparedRequest{
		Caller: modelgateway.Caller{OrganizationID: org, UserID: user, DepartmentID: "department", AuthType: "user"},
		Route:  modelgateway.Route{ModelID: "model", ProviderID: "provider", ModelCode: "writer", ModelType: "chat"},
		Body:   body,
	}, nil
}
func (*dramaGatewayStub) ListModelsForUser(context.Context, string, string) ([]modelgateway.AvailableModel, error) {
	return nil, nil
}
func (g *dramaGatewayStub) RecordCall(_ context.Context, input modelgateway.RecordCallInput) error {
	g.record = input
	return nil
}

type dramaForwarderStub struct {
	timeoutMS int
	err       error
}

func (f *dramaForwarderStub) Forward(_ context.Context, route modelgateway.Route, _ []byte, _ bool) (*http.Response, error) {
	f.timeoutMS = route.ProviderTimeoutMS
	if f.err != nil {
		return nil, f.err
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"{\"synopsis\":\"故事\"}"}}],"usage":{"prompt_tokens":12,"completion_tokens":8,"total_tokens":20}}`))}, nil
}

func TestDramaWriterUsesTenMinuteTimeout(t *testing.T) {
	if dramaRequestTimeoutMS != 600000 {
		t.Fatalf("漫剧单次模型调用应等待 10 分钟，实际 %d 毫秒", dramaRequestTimeoutMS)
	}
}

func TestDramaWriterRecordsGatewayCall(t *testing.T) {
	gateway := &dramaGatewayStub{}
	forwarder := &dramaForwarderStub{}
	writer := &dramaWriter{gateway: gateway, forwarder: forwarder}
	result, err := writer.Complete(context.Background(), "org", "user", "writer", "请总结故事")
	if err != nil {
		t.Fatal(err)
	}
	if result != `{"synopsis":"故事"}` {
		t.Fatalf("模型内容错误: %s", result)
	}
	if forwarder.timeoutMS != dramaRequestTimeoutMS {
		t.Fatalf("漫剧解析请求超时未延长: %d", forwarder.timeoutMS)
	}
	if gateway.record.RequestID == "" || gateway.record.TotalTokens != 20 || gateway.record.Result != "success" || gateway.record.UserID != "user" {
		t.Fatalf("模型调用未正确计入网关: %+v", gateway.record)
	}
	if gateway.record.Source != "ai_drama" || gateway.record.Operation != "chat" ||
		!strings.Contains(string(gateway.record.RequestBody), "请总结故事") ||
		!strings.Contains(string(gateway.record.ResponseBody), `"prompt_tokens":12`) {
		t.Fatal("漫剧调用没有保存发往供应商的请求和原始响应")
	}
}

func TestDramaWriterReportsTimeoutClearly(t *testing.T) {
	gateway := &dramaGatewayStub{}
	forwarder := &dramaForwarderStub{err: context.DeadlineExceeded}
	writer := &dramaWriter{gateway: gateway, forwarder: forwarder}
	_, err := writer.Complete(context.Background(), "org", "user", "writer", "请总结故事")
	if err == nil || !strings.Contains(err.Error(), "文本模型响应超时") {
		t.Fatalf("超时应提示可继续解析: %v", err)
	}
	if gateway.record.Result != "failed" || gateway.record.ErrorMessage != context.DeadlineExceeded.Error() ||
		!strings.Contains(string(gateway.record.RequestBody), "请总结故事") {
		t.Fatal("超时调用应记录完整请求和错误")
	}
}

type dramaStreamForwarderStub struct {
	payload   string
	streaming bool
}

func (f *dramaStreamForwarderStub) Forward(_ context.Context, _ modelgateway.Route, _ []byte, stream bool) (*http.Response, error) {
	f.streaming = stream
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(f.payload))}, nil
}
func TestDramaWriterStreamsContentAndRecordsRawSSE(t *testing.T) {
	gateway := &dramaGatewayStub{}
	forwarder := &dramaStreamForwarderStub{payload: "data: {\"choices\":[{\"delta\":{\"content\":\"{\\\"ok\\\":\"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"true}\"},\"finish_reason\":\"stop\"}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":12,\"completion_tokens\":8,\"total_tokens\":20}}\n\ndata: [DONE]\n\n"}
	writer := &dramaWriter{gateway: gateway, forwarder: forwarder}
	live := ""
	result, err := writer.CompleteStream(context.Background(), "org", "user", "writer", "生成", func(text string) error { live = text; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if result != `{"ok":true}` || live != result || !forwarder.streaming {
		t.Fatalf("流式结果不完整：%q / %q", result, live)
	}
	if !gateway.record.Streaming || gateway.record.TotalTokens != 20 || string(gateway.record.ResponseBody) != forwarder.payload {
		t.Fatal("流式请求、原始响应或用量未完整记录")
	}
}
func TestDramaWriterRejectsReasoningOnlyAndInterruptedStream(t *testing.T) {
	for _, payload := range []string{"data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"推理\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n", "data: {\"choices\":[{\"delta\":{\"content\":\"半截\"}}]}\n\n"} {
		gateway := &dramaGatewayStub{}
		writer := &dramaWriter{gateway: gateway, forwarder: &dramaStreamForwarderStub{payload: payload}}
		if _, err := writer.CompleteStream(context.Background(), "org", "user", "writer", "生成", nil); err == nil {
			t.Fatal("仅推理或断流不能视为完成")
		}
		if gateway.record.Result != "failed" || len(gateway.record.ResponseBody) == 0 {
			t.Fatal("失败流也必须记录原始响应")
		}
	}
}

func TestDramaReasoningPolicyUsesVerifiedVendorAndModel(t *testing.T) {
	for _, tc := range []struct {
		host, model, stage string
		want               any
	}{
		{"https://dashscope.aliyuncs.com/compatible-mode/v1", "deepseek-v4-flash-0731", "name", false},
		{"https://dashscope.aliyuncs.com/compatible-mode/v1", "deepseek-v4-flash-0731", "director", true},
		{"https://dashscope.aliyuncs.com/compatible-mode/v1", "unknown-model", "name", nil},
		{"https://other.example.com/v1", "deepseek-v4-flash-0731", "name", nil},
	} {
		p := modelgateway.PreparedRequest{Route: modelgateway.Route{ProviderBaseURL: tc.host, UpstreamModelID: tc.model}, Body: []byte(`{"model":"upstream","stream":true}`)}
		if _, err := applyDramaReasoningPolicy(&p, tc.stage); err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		if err := json.Unmarshal(p.Body, &body); err != nil {
			t.Fatal(err)
		}
		if body["enable_thinking"] != tc.want {
			t.Fatal("供应商能力匹配错误", string(p.Body))
		}
		if tc.stage == "director" && body["reasoning_effort"] != "high" {
			t.Fatal("导演应保留标准思考强度")
		}
	}
}
func TestDramaWriterRecordsStreamingMilestonesWithoutExposingThinking(t *testing.T) {
	gateway := &dramaGatewayStub{}
	writer := &dramaWriter{gateway: gateway, forwarder: &dramaStreamForwarderStub{payload: "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"隐藏思考文本\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"{\\\"ok\\\":true}\"},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: {\"choices\":[],\"usage\":{\"completion_tokens\":12,\"completion_tokens_details\":{\"reasoning_tokens\":9}}}\n\ndata: [DONE]\n\n"}}
	phases := []string{}
	text, err := writer.CompleteObserved(context.Background(), "org", "user", "model", "样本", drama.ModelCallOptions{Stage: "review", OnProgress: func(p drama.ModelProgress) error {
		phases = append(phases, p.Phase)
		if strings.Contains(p.Description(), "隐藏思考") {
			t.Fatal("不可展示思考正文")
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	timing := gateway.record.Timing
	if text != `{"ok":true}` || timing == nil || timing.HeadersAt == nil || timing.FirstReasoningAt == nil || timing.FirstContentAt == nil || (timing.ReasoningTokens == nil || *timing.ReasoningTokens != 9) || timing.FirstContentAt.Before(*timing.FirstReasoningAt) || timing.FinishedAt.Before(*timing.FirstContentAt) {
		t.Fatal("流式里程碑未正确记录", text, timing)
	}
	if strings.Join(phases, ",") != "sent,headers,thinking,content,finished,recorded" {
		t.Fatal(phases)
	}
}
