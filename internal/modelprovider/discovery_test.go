package modelprovider

import (
	"encoding/json"
	"testing"
)

func TestParseBailianModalities(t *testing.T) {
	tests := []struct {
		raw            string
		kind           string
		wantType       string
		wantCapability string
	}{
		{`{"model":"qwen-image","name":"通义图片","inference_metadata":{"response_modality":["Image"],"request_modality":["Text"]}}`, "aliyun_bailian", "image", ""},
		{`{"model":"wan-video","inference_metadata":{"response_modality":["Video"]}}`, "aliyun_bailian", "video", ""},
		{`{"model":"qwen-vl","inference_metadata":{"response_modality":["Text"],"request_modality":["Image"]},"features":["function-calling"]}`, "aliyun_bailian", "chat", "vision"},
		{`{"name":"models/gemini-2.5-pro","displayName":"Gemini"}`, "gemini", "unknown", ""},
	}
	for _, test := range tests {
		model := parseDiscoveredModel(json.RawMessage(test.raw), test.kind)
		if model.UpstreamModelID == "" {
			t.Fatalf("缺少模型标识：%s", test.raw)
		}
		if model.ModelType != test.wantType {
			t.Fatalf("类型 = %q，期望 %q：%s", model.ModelType, test.wantType, test.raw)
		}
		if test.wantCapability != "" {
			found := false
			for _, value := range model.Capabilities {
				if value == test.wantCapability {
					found = true
				}
			}
			if !found {
				t.Fatalf("缺少能力 %q：%#v", test.wantCapability, model)
			}
		}
	}
}
