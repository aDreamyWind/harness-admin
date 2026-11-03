package app

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"harness-admin/internal/drama"
	"harness-admin/internal/modelgateway"
)

type dramaVideoGatewayStub struct {
	records []modelgateway.RecordCallInput
}

func (s *dramaVideoGatewayStub) PrepareForUserOperation(_ context.Context, org, user string, body []byte, operation string) (modelgateway.PreparedRequest, error) {
	return modelgateway.PreparedRequest{Route: modelgateway.Route{ProviderKind: "aliyun_bailian", UpstreamModelID: "wan3.0-video"}, Caller: modelgateway.Caller{OrganizationID: org, UserID: user}, Body: body}, nil
}
func (s *dramaVideoGatewayStub) RecordCall(_ context.Context, in modelgateway.RecordCallInput) error {
	s.records = append(s.records, in)
	return nil
}

type dramaVideoForwarderStub struct {
	payload string
	task    string
}

func (s *dramaVideoForwarderStub) Forward(_ context.Context, _ modelgateway.Route, body []byte, _ bool) (*http.Response, error) {
	s.payload = string(body)
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"output":{"task_id":"fake-task","task_status":"PENDING"}}`))}, nil
}
func (s *dramaVideoForwarderStub) QueryVideoTask(_ context.Context, _ modelgateway.Route, id string) (*http.Response, error) {
	s.task = id
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"output":{"task_status":"SUCCEEDED","video_url":"https://example.com/test.mp4"}}`))}, nil
}
func (s *dramaVideoForwarderStub) DownloadVideo(context.Context, string) ([]byte, error) {
	return []byte("虚构视频"), nil
}

func TestDramaVideoEngineSubmitsReferencesAndLogsTaskQuery(t *testing.T) {
	gateway, forwarder := &dramaVideoGatewayStub{}, &dramaVideoForwarderStub{}
	engine := &dramaVideoEngine{gateway: gateway, forwarder: forwarder}
	result, err := engine.GenerateSegmentVideo(context.Background(), drama.SegmentVideoRequest{
		OrganizationID: "org", UserID: "user", ModelCode: "video", Prompt: "人物看向图1", AspectRatio: "9:16",
		Duration: 4, ReferenceImages: []string{"data:image/png;base64,abc"}, ImageIDs: []string{"image1"},
	})
	if err != nil || string(result) != "虚构视频" || forwarder.task != "fake-task" {
		t.Fatal(string(result), err, forwarder.task)
	}
	for _, fragment := range []string{`"type":"reference_image"`, `"url":"data:image/png;base64,abc"`, `"duration":4`, `"ratio":"9:16"`} {
		if !strings.Contains(forwarder.payload, fragment) {
			t.Fatalf("请求缺少 %s：%s", fragment, forwarder.payload)
		}
	}
	if len(gateway.records) != 2 || gateway.records[1].Operation != "video_task_query" || !strings.Contains(string(gateway.records[0].RequestBody), "image-job:image1") || strings.Contains(string(gateway.records[0].RequestBody), "base64") {
		t.Fatalf("调用记录不正确：%+v", gateway.records)
	}
}
