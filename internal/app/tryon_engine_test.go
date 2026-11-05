package app

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"harness-admin/internal/modelgateway"
	"harness-admin/internal/tryon"
)

type tryOnGatewayFixture struct{ record modelgateway.RecordCallInput }

func (*tryOnGatewayFixture) ListModelsForUser(context.Context, string, string) ([]modelgateway.AvailableModel, error) {
	return []modelgateway.AvailableModel{
		{Code: "vision", ModelType: "chat", UpstreamModelID: "qwen3-vl-plus"},
		{Code: "editor", ModelType: "image", ProviderKind: "aliyun_bailian", UpstreamModelID: "qwen-image-3.0-pro"},
		{Code: "edit_only", ModelType: "image", ProviderKind: "aliyun_bailian", UpstreamModelID: "qwen-image-edit-plus"},
		{Code: "text2img", ModelType: "image", ProviderKind: "aliyun_bailian", UpstreamModelID: "wan2.2-t2i-plus"},
	}, nil
}

func (*tryOnGatewayFixture) PrepareForUserOperation(_ context.Context, org, user string, body []byte, operation string) (modelgateway.PreparedRequest, error) {
	return modelgateway.PreparedRequest{Caller: modelgateway.Caller{OrganizationID: org, UserID: user},
		Route: modelgateway.Route{ModelID: "model", ProviderID: "provider", ModelType: operation}, Body: body}, nil
}

func (g *tryOnGatewayFixture) RecordCall(_ context.Context, in modelgateway.RecordCallInput) error {
	g.record = in
	return nil
}

func TestTryOnEngineReferencesAndRecordedOperations(t *testing.T) {
	ctx := context.Background()
	owner := tryon.Owner{OrganizationID: "org", UserID: "user"}
	gateway := &tryOnGatewayFixture{}
	forwarder := &visualForwarderStub{body: `{"choices":[{"message":{"content":"{\"items\":[{\"name\":\"长裤\",\"category\":\"裤子\",\"box\":[0.1,0.4,0.8,1]}]}"}}],"usage":{"prompt_tokens":23,"completion_tokens":9,"total_tokens":32}}`}
	e := &tryOnEngine{gateway: gateway, forwarder: forwarder, image: &dramaImageEngine{gateway: gateway, forwarder: forwarder, source: "ai_tryon"}}
	models, err := e.Models(ctx, owner)
	if err != nil || len(models.Image) != 2 || len(models.Person) != 1 || models.Person[0].Code != "editor" {
		t.Fatalf("试穿模型筛选失败: %#v %v", models, err)
	}
	input := tryon.Image{Data: []byte("fixture"), MIME: "image/png"}
	items, err := e.Detect(ctx, owner, "vision", input)
	record := gateway.record
	if err != nil || len(items) != 1 || items[0].Category != "裤子" || record.Source != "ai_tryon" ||
		record.Operation != "garment_detect" || record.TotalTokens != 32 || !strings.Contains(string(record.RequestBody), input.DataURI()) ||
		string(record.ResponseBody) != forwarder.body {
		t.Fatalf("衣物识别及日志不完整: %#v %v", items, err)
	}
	forwarder.body = `{"data":[{"b64_json":"` + visualPNG + `"}]}`
	person := tryon.Image{Data: []byte("person"), MIME: "image/png"}
	garment := tryon.Image{Data: []byte("garment"), MIME: "image/jpeg"}
	pose := tryon.Image{Data: []byte("pose"), MIME: "image/webp"}
	result, err := e.Generate(ctx, owner, "editor", "tryon_generate", "图 1 人物，图 2 衣物，图 3 姿势", []tryon.Image{person, garment, pose})
	if err != nil || result.MIME != "image/png" || gateway.record.Source != "ai_tryon" || gateway.record.Operation != "tryon_generate" {
		t.Fatalf("试穿图片或调用记录错误: %v", err)
	}
	var req struct {
		Images []string `json:"image"`
	}
	if err := json.Unmarshal(forwarder.payload, &req); err != nil || !slices.Equal(req.Images, []string{person.DataURI(), garment.DataURI(), pose.DataURI()}) {
		t.Fatalf("上游参考图丢失或顺序不正确: %#v %v", req, err)
	}
	for _, operation := range []string{"garment_reconstruct", "pose_standardize", "person_reconstruct"} {
		_, err := e.Generate(ctx, owner, "edit_only", operation, "根据参考图生成独立素材", []tryon.Image{person})
		if err != nil || gateway.record.Source != "ai_tryon" || gateway.record.Operation != operation ||
			len(gateway.record.RequestBody) == 0 || len(gateway.record.ResponseBody) == 0 {
			t.Fatalf("素材预览 %s 未使用图生图模型和独立的调用记录: %v", operation, err)
		}
	}
}
