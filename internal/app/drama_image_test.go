package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"harness-admin/internal/drama"
	"harness-admin/internal/modelgateway"
	"io"
	"net/http"
	"strings"
	"testing"
)

const visualPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a7x8AAAAASUVORK5CYII="

type visualGatewayStub struct {
	record     modelgateway.RecordCallInput
	recordErr  error
	prepareErr error
}

func (g *visualGatewayStub) PrepareForUserOperation(_ context.Context, org, user string, body []byte, op string) (modelgateway.PreparedRequest, error) {
	if op != "image" {
		panic("unexpected operation")
	}
	if g.prepareErr != nil {
		return modelgateway.PreparedRequest{}, g.prepareErr
	}
	return modelgateway.PreparedRequest{Caller: modelgateway.Caller{OrganizationID: org, UserID: user}, Route: modelgateway.Route{ModelID: "model", ProviderID: "provider", ModelType: "image", ModelCode: "painter"}, Body: body}, nil
}
func (g *visualGatewayStub) RecordCall(_ context.Context, in modelgateway.RecordCallInput) error {
	g.record = in
	return g.recordErr
}

type visualForwarderStub struct {
	status      int
	body        string
	err         error
	downloadErr error
	timeout     int
	payload     []byte
}

func (f *visualForwarderStub) Forward(_ context.Context, route modelgateway.Route, body []byte, _ bool) (*http.Response, error) {
	f.timeout = route.ProviderTimeoutMS
	f.payload = body
	if f.err != nil {
		return nil, f.err
	}
	status := f.status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(f.body))}, nil
}
func (f *visualForwarderStub) DownloadImage(context.Context, string) ([]byte, error) {
	data, _ := base64.StdEncoding.DecodeString(visualPNG)
	return data, f.downloadErr
}
func TestDramaImageModelLogsBothFormatsAndFailures(t *testing.T) {
	for _, tc := range []struct {
		name, body       string
		err, downloadErr error
		wantError        bool
	}{{"base64", `{"data":[{"b64_json":"` + visualPNG + `"}]}`, nil, nil, false}, {"URL", `{"data":[{"url":"https://images.example/test.png"}]}`, nil, nil, false}, {"empty", `{"data":[]}`, nil, nil, true}, {"nonimage", `{"data":[{"b64_json":"aGVsbG8="}]}`, nil, nil, true}, {"timeout", "", context.DeadlineExceeded, nil, true}, {"download", `{"data":[{"url":"https://images.example/test.png"}]}`, nil, errors.New("download failed"), true}} {
		t.Run(tc.name, func(t *testing.T) {
			gateway := &visualGatewayStub{}
			forwarder := &visualForwarderStub{body: tc.body, err: tc.err, downloadErr: tc.downloadErr}
			engine := &dramaImageEngine{gateway: gateway, forwarder: forwarder}
			data, mime, err := engine.Generate(context.Background(), drama.ImageRequest{OrganizationID: "org", UserID: "user", ModelCode: "painter", Prompt: "人物全身", AspectRatio: "9:16", ReferenceDataURI: "data:image/png;base64," + visualPNG})
			if (err != nil) != tc.wantError {
				t.Fatalf("err=%v", err)
			}
			if !tc.wantError && (len(data) == 0 || mime != "image/png") {
				t.Fatal("图片未正常解析")
			}
			if gateway.record.Operation != "image" || gateway.record.Source != "ai_drama" || gateway.record.UserID != "user" || gateway.record.RequestID == "" || forwarder.timeout != 600000 {
				t.Fatal("未记录调用或超时不正确")
			}
			if tc.wantError && gateway.record.Result != "failed" {
				t.Fatal("失败未记录")
			}
			if string(gateway.record.ResponseBody) != tc.body {
				t.Fatal("原始响应未保存")
			}
			var body map[string]any
			if err = json.Unmarshal(gateway.record.RequestBody, &body); err != nil || body["size"] != "864x1536" || body["image"] != "data:image/png;base64,"+visualPNG {
				t.Fatal("参考图或比例丢失")
			}
		})
	}
}
func TestDramaMediaFilesRoundTripAndTraversal(t *testing.T) {
	files := &dramaMediaFiles{directory: t.TempDir()}
	data, _ := base64.StdEncoding.DecodeString(visualPNG)
	id := "bdc624f0-aedb-4580-86a0-c024f45eb27c"
	key, err := files.Put(context.Background(), id, data, "image/png")
	if err != nil {
		t.Fatal(err)
	}
	actual, mime, err := files.Get(context.Background(), key)
	if err != nil || string(actual) != string(data) || mime != "image/png" {
		t.Fatal("文件读写失败")
	}
	for _, key := range []string{"../test.png", "/tmp/test.png", id + ".svg", ""} {
		if _, _, err := files.Get(context.Background(), key); err == nil {
			t.Fatal("无效文件键未拒绝")
		}
	}
	if _, err := files.Put(context.Background(), id, []byte("not an image"), "image/png"); err == nil {
		t.Fatal("非图片内容未拒绝")
	}
}

func TestDramaImageFailureKeepsSupplierEchoInPrivateLog(t *testing.T) {
	const secret = "PRIVATE_STYLE_TEMPLATE"
	gateway := &visualGatewayStub{}
	forwarder := &visualForwarderStub{status: http.StatusBadRequest, body: `{"error":{"message":"request echoed PRIVATE_STYLE_TEMPLATE"}}`}
	engine := &dramaImageEngine{gateway: gateway, forwarder: forwarder}
	_, _, err := engine.Generate(context.Background(), drama.ImageRequest{OrganizationID: "org", UserID: "user", ModelCode: "painter", Prompt: secret, AspectRatio: "9:16"})
	var public *drama.ImageFailure
	if !errors.As(err, &public) || strings.Contains(public.Message, secret) || !strings.Contains(public.Message, "HTTP 400") {
		t.Fatalf("公开失败信息不正确: %v", err)
	}
	if !strings.Contains(string(gateway.record.ResponseBody), secret) || !strings.Contains(gateway.record.ErrorMessage, secret) || !strings.Contains(string(gateway.record.RequestBody), secret) {
		t.Fatal("排查日志未保留原始请求和响应")
	}
}
