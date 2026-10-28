package app

import (
	"context"
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
