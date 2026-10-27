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

type dramaForwarderStub struct{}

func (*dramaForwarderStub) Forward(context.Context, modelgateway.Route, []byte, bool) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"{\"synopsis\":\"故事\"}"}}],"usage":{"prompt_tokens":12,"completion_tokens":8,"total_tokens":20}}`))}, nil
}

func TestDramaWriterRecordsGatewayCall(t *testing.T) {
	gateway := &dramaGatewayStub{}
	writer := &dramaWriter{gateway: gateway, forwarder: &dramaForwarderStub{}}
	result, err := writer.Complete(context.Background(), "org", "user", "writer", "请总结故事")
	if err != nil {
		t.Fatal(err)
	}
	if result != `{"synopsis":"故事"}` {
		t.Fatalf("模型内容错误: %s", result)
	}
	if gateway.record.RequestID == "" || gateway.record.TotalTokens != 20 || gateway.record.Result != "success" || gateway.record.UserID != "user" {
		t.Fatalf("模型调用未正确计入网关: %+v", gateway.record)
	}
}
