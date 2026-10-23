package postgres

import (
	"testing"

	"harness-admin/internal/modeladmin"
)

func TestImportedModelStatus(t *testing.T) {
	tests := []struct {
		name         string
		input        modeladmin.ImportModelsInput
		modelType    string
		providerKind string
		want         string
	}{
		{"默认停用", modeladmin.ImportModelsInput{}, "image", "aliyun_bailian", "disabled"},
		{"全量启用图片", modeladmin.ImportModelsInput{ActivateImportedModels: true}, "image", "aliyun_bailian", "active"},
		{"全量启用视频", modeladmin.ImportModelsInput{ActivateImportedModels: true}, "video", "aliyun_bailian", "active"},
		{"全量启用未接入网关的协议", modeladmin.ImportModelsInput{ActivateImportedModels: true}, "chat", "claude", "active"},
		{"旧参数启用对话", modeladmin.ImportModelsInput{ActivateSupportedChat: true}, "chat", "aliyun_bailian", "active"},
		{"旧参数不启用图片", modeladmin.ImportModelsInput{ActivateSupportedChat: true}, "image", "aliyun_bailian", "disabled"},
		{"旧参数不启用未接入网关的协议", modeladmin.ImportModelsInput{ActivateSupportedChat: true}, "chat", "gemini", "disabled"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := importedModelStatus(test.input, test.modelType, test.providerKind); got != test.want {
				t.Fatalf("状态 = %q，预期 %q", got, test.want)
			}
		})
	}
}
