package drama

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type styleRepoStub struct{}

func (styleRepoStub) ListStyles(context.Context) ([]StyleOption, error) {
	return []StyleOption{{ID: "dark-concept", Name: "黑暗原画概念风"}}, nil
}
func (styleRepoStub) ResolveStyle(_ context.Context, key string) (VisualStyle, error) {
	if key != "dark-concept" && key != "黑暗原画概念风" {
		return VisualStyle{}, ErrStyleUnavailable
	}
	return VisualStyle{StyleOption: StyleOption{ID: "dark-concept", Name: "黑暗原画概念风"}, ImagePrompt: "私有生图模板", VideoPrompt: "私有视频模板"}, nil
}
func (s styleRepoStub) ResolveCatalogStyle(ctx context.Context, id string) (VisualStyle, error) {
	if id != "dark-concept" {
		return VisualStyle{}, ErrStyleUnavailable
	}
	return s.ResolveStyle(ctx, id)
}
func TestStylePrivateTemplatesNeverSerializeAndIDIsValidated(t *testing.T) {
	service := NewService(&fakeRepository{settings: Settings{TextModelCode: "writer"}}, &fakeWriter{}).WithStyles(styleRepoStub{})
	input := CreateInput{OrganizationID: "org", OwnerID: "user", Title: "风格测试", SourceText: "甲走入屋内。", SourceMode: "upload", StyleID: "dark-concept", Style: "伪造风格名", AspectRatio: "16:9"}
	project, err := service.Create(context.Background(), input)
	if err != nil || project.StyleID != "dark-concept" || project.Style != "黑暗原画概念风" {
		t.Fatal("应使用服务端风格ID及名称", err)
	}
	input.StyleID = "not-found"
	if _, err = service.Create(context.Background(), input); !errors.Is(err, ErrStyleUnavailable) {
		t.Fatal("不存在的风格应拒绝")
	}
	private, _ := styleRepoStub{}.ResolveStyle(context.Background(), "dark-concept")
	encoded, _ := json.Marshal(private)
	if strings.Contains(string(encoded), "私有") || strings.Contains(string(encoded), "Prompt") {
		t.Fatal("模板被序列化")
	}
	prompt := BuildAssetImagePrompt(AssetForm{Kind: "character", AssetName: "甲", Name: "变异形象", Gender: "male"}, "左臂变为藤蔓", private, true)
	if !strings.Contains(prompt, "私有生图模板") || strings.Contains(prompt, "私有视频模板") || !strings.Contains(prompt, "参考原始形象") {
		t.Fatal("提示词组装不正确")
	}
	raw, _ := json.Marshal(ImageJob{ID: "task", Prompt: prompt})
	if strings.Contains(string(raw), "私有") || strings.Contains(string(raw), "prompt") {
		t.Fatal("任务响应不应返回最终提示词")
	}
}
