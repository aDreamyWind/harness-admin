package drama

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type fakeRepository struct {
	project  Project
	settings Settings
	stages   []string
	failed   string
}

func (r *fakeRepository) GetSettings(_ context.Context, _, _ string) (Settings, error) {
	return r.settings, nil
}
func (r *fakeRepository) SaveSettings(_ context.Context, input Settings) (Settings, error) {
	r.settings = input
	return input, nil
}

func (r *fakeRepository) Create(_ context.Context, item Project) (Project, error) {
	r.project = item
	return item, nil
}
func (r *fakeRepository) List(_ context.Context, _, _ string) ([]Project, error) {
	return []Project{r.project}, nil
}
func (r *fakeRepository) Get(_ context.Context, _, _, _ string) (Project, error) {
	return r.project, nil
}
func (r *fakeRepository) GetSource(_ context.Context, _, _, _ string) (string, error) {
	return r.project.SourceText, nil
}
func (r *fakeRepository) UpdateOverview(_ context.Context, _, _, _ string, value json.RawMessage) (Project, error) {
	r.project.Overview = value
	return r.project, nil
}
func (r *fakeRepository) FailInterrupted(context.Context) error { return nil }
func (r *fakeRepository) SaveGeneratedScript(_ context.Context, _, _, _, script string) error {
	r.project.SourceText = script
	return nil
}
func (r *fakeRepository) SaveAnalysis(_ context.Context, _, _, _, status, phase string, data json.RawMessage) error {
	r.stages = append(r.stages, phase)
	r.project.Status, r.project.Phase = status, phase
	if len(data) > 0 {
		switch phase {
		case "story":
			r.project.Overview = data
		case "assets":
			var result struct{ Characters, Scenes, Props json.RawMessage }
			if err := json.Unmarshal(data, &result); err != nil {
				return err
			}
			r.project.Characters, r.project.Scenes, r.project.Props = result.Characters, result.Scenes, result.Props
		case "episodes":
			var result struct{ Episodes json.RawMessage }
			if err := json.Unmarshal(data, &result); err != nil {
				return err
			}
			r.project.Episodes = result.Episodes
		}
	}
	return nil
}
func (r *fakeRepository) FailAnalysis(_ context.Context, _, _, _, message string) error {
	r.failed = message
	r.project.Status = "failed"
	return nil
}

type fakeWriter struct {
	prompts []string
	replies []string
}

func (w *fakeWriter) AvailableModels(context.Context, string, string) ([]Model, error) {
	return []Model{
		{Code: "writer", Name: "编剧模型", ModelType: "chat"},
		{Code: "painter", Name: "图片模型", ModelType: "image"},
		{Code: "director", Name: "视频模型", ModelType: "video"},
	}, nil
}
func (w *fakeWriter) Complete(_ context.Context, _, _, _ string, prompt string) (string, error) {
	w.prompts = append(w.prompts, prompt)
	if len(w.replies) == 0 {
		return "", errors.New("无响应")
	}
	result := w.replies[0]
	w.replies = w.replies[1:]
	return result, nil
}

func TestCreateAndAnalyzePreservesFullSource(t *testing.T) {
	source := strings.Repeat("山", ParseRuneLimit+456)
	repo := &fakeRepository{settings: Settings{TextModelCode: "writer"}}
	writer := &fakeWriter{replies: []string{
		`{"videoStyle":"国风","aspectRatio":"9:16","protagonist":"阿青","genre":"奇幻","synopsis":"寻找故乡","background":"古代","setting":"画中世界"}`,
		`{"characters":[{"name":"阿青","description":"画师"}],"scenes":[],"props":[]}`,
		`{"episodes":[{"title":"第一集","synopsis":"启程"}]}`,
	}}
	service := NewService(repo, writer)
	project, err := service.Create(context.Background(), CreateInput{OrganizationID: "org", OwnerID: "user", Title: "画中人", SourceText: source, Style: "国风", AspectRatio: "9:16"})
	if err != nil {
		t.Fatal(err)
	}
	if project.SourceLength != ParseRuneLimit+456 || repo.project.SourceText != source {
		t.Fatal("完整原文未保存")
	}
	if err := service.Analyze(project); err != nil {
		t.Fatal(err)
	}
	if repo.project.Status != "ready" || len(writer.prompts) != 3 {
		t.Fatalf("解析阶段错误: %+v, 调用次数 %d", repo.project, len(writer.prompts))
	}
	if strings.Contains(writer.prompts[0], strings.Repeat("山", ParseRuneLimit+1)) {
		t.Fatal("解析传入了超出三万字的原文")
	}
	if string(repo.project.Characters) == "[]" || string(repo.project.Episodes) == "[]" {
		t.Fatal("资产或分集未保存")
	}
}

func TestGenerateScriptBeforeAnalysis(t *testing.T) {
	repo := &fakeRepository{settings: Settings{TextModelCode: "writer"}}
	writer := &fakeWriter{replies: []string{
		`{"script":"第一场：夜，画室。"}`,
		`{"videoStyle":"国风","aspectRatio":"16:9","protagonist":"画师","genre":"悬疑","synopsis":"谜题","background":"现代","setting":"画室"}`,
		`{"characters":[],"scenes":[],"props":[]}`,
		`{"episodes":[]}`,
	}}
	service := NewService(repo, writer)
	project, err := service.Create(context.Background(), CreateInput{OrganizationID: "org", OwnerID: "user", Title: "画室", SourceText: "一个画师的故事", SourceMode: "generate", Style: "二维动漫", AspectRatio: "16:9"})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Analyze(project); err != nil {
		t.Fatal(err)
	}
	if repo.project.SourceText != "第一场：夜，画室。" || !strings.Contains(writer.prompts[1], repo.project.SourceText) {
		t.Fatal("没有先生成剧本再解析")
	}
}

func TestCreateRequiresConfiguredAvailableModel(t *testing.T) {
	repo := &fakeRepository{}
	service := NewService(repo, &fakeWriter{})
	_, err := service.Create(context.Background(), CreateInput{OrganizationID: "org", OwnerID: "user", Title: "测试", SourceText: "内容", Style: "国风", AspectRatio: "9:16"})
	if !errors.Is(err, ErrModelNotConfigured) {
		t.Fatalf("应该要求先配置模型，得到 %v", err)
	}
	repo.settings.TextModelCode = "disabled"
	_, err = service.Create(context.Background(), CreateInput{OrganizationID: "org", OwnerID: "user", Title: "测试", SourceText: "内容", Style: "国风", AspectRatio: "9:16"})
	if !errors.Is(err, ErrModelNotConfigured) {
		t.Fatalf("应该拒绝未授权模型，得到 %v", err)
	}
}

func TestSaveSettingsValidatesModelTypes(t *testing.T) {
	repo := &fakeRepository{}
	service := NewService(repo, &fakeWriter{})
	_, err := service.SaveSettings(context.Background(), Settings{OrganizationID: "org", UserID: "user", ImageModelCode: "writer"})
	if !errors.Is(err, ErrModelUnavailable) {
		t.Fatalf("文本模型不应保存为图片模型: %v", err)
	}
	saved, err := service.SaveSettings(context.Background(), Settings{OrganizationID: "org", UserID: "user", TextModelCode: "writer", ImageModelCode: "painter", VideoModelCode: "director"})
	if err != nil {
		t.Fatal(err)
	}
	if saved.TextModelCode != "writer" || repo.settings.ImageModelCode != "painter" || repo.settings.VideoModelCode != "director" {
		t.Fatal("当前用户的默认模型未保存")
	}
}
