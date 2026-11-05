package novel

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"strings"
	"testing"
)

type testCoverGenerator struct {
	data   []byte
	owner  Owner
	model  string
	prompt string
	during func()
}

type testCoverPreviews struct {
	owner   Owner
	id      string
	preview CoverPreview
}

func (s *testCoverPreviews) Put(_ context.Context, owner Owner, id string, preview CoverPreview) error {
	s.owner, s.id, s.preview = owner, id, preview
	return nil
}

func (s *testCoverPreviews) Get(_ context.Context, owner Owner, id string) (CoverPreview, error) {
	if s.owner != owner || s.id != id {
		return CoverPreview{}, ErrCoverPreviewExpired
	}
	return s.preview, nil
}

func (*testCoverGenerator) CoverModels(context.Context, Owner) ([]Model, error) {
	return []Model{{Code: "art", Name: "测试图片模型"}}, nil
}

func (g *testCoverGenerator) GenerateCover(_ context.Context, owner Owner, model, prompt string) ([]byte, error) {
	g.owner, g.model, g.prompt = owner, model, prompt
	if g.during != nil {
		g.during()
	}
	return g.data, nil
}

type testCoverStyleResolver func(context.Context, string) (CoverStyle, error)

func (resolve testCoverStyleResolver) ResolveCoverStyle(ctx context.Context, id string) (CoverStyle, error) {
	return resolve(ctx, id)
}

func TestCoverUsesOptionalCatalogStyle(t *testing.T) {
	ctx := context.Background()
	owner := Owner{OrganizationID: "org", UserID: "author"}
	var pngBytes bytes.Buffer
	if err := png.Encode(&pngBytes, image.NewNRGBA(image.Rect(0, 0, 8, 12))); err != nil {
		t.Fatal(err)
	}
	generator := &testCoverGenerator{data: pngBytes.Bytes()}
	style := CoverStyle{Name: "东方水墨画风", ImagePrompt: "宣纸质地，墨色渗化"}
	var resolved []string
	resolver := testCoverStyleResolver(func(_ context.Context, id string) (CoverStyle, error) {
		resolved = append(resolved, id)
		if id == "ink-wash" {
			return style, nil
		}
		return CoverStyle{}, ErrCoverStyle
	})
	s := NewService(&memoryRepository{}, nil, generator, &testCoverPreviews{}).WithCoverStyles(resolver)
	p, err := s.Create(ctx, owner, CreateInput{Title: "潮汐档案", Brief: "守塔人听到呼救", ChapterCount: 1})
	if err != nil {
		t.Fatal(err)
	}
	input := GenerateCoverInput{ModelCode: "art", Instruction: "突出雨夜里的灯塔"}
	if _, err = s.GenerateCover(ctx, owner, p.ID, input); err != nil || len(resolved) != 0 || strings.Contains(generator.prompt, style.ImagePrompt) {
		t.Fatalf("留空风格应直接生成: %q %v", generator.prompt, err)
	}
	input.StyleID = "ink-wash"
	if _, err = s.GenerateCover(ctx, owner, p.ID, input); err != nil || len(resolved) != 1 || resolved[0] != input.StyleID ||
		!strings.Contains(generator.prompt, style.Name) || !strings.Contains(generator.prompt, style.ImagePrompt) || !strings.Contains(generator.prompt, input.Instruction) {
		t.Fatalf("封面未同时使用目录模板和画面要求: %q %v", generator.prompt, err)
	}
	generator.prompt = ""
	input.StyleID = "missing-style"
	if _, err = s.GenerateCover(ctx, owner, p.ID, input); !errors.Is(err, ErrCoverStyle) || generator.prompt != "" {
		t.Fatalf("不可用风格未拒绝或仍调用模型: %v", err)
	}
}

func TestCoverUploadGenerationAndReplacement(t *testing.T) {
	ctx := context.Background()
	owner := Owner{OrganizationID: "org", UserID: "author"}
	repo := &memoryRepository{}
	var pngBytes bytes.Buffer
	if err := png.Encode(&pngBytes, image.NewNRGBA(image.Rect(0, 0, 8, 12))); err != nil {
		t.Fatal(err)
	}
	generator := &testCoverGenerator{data: pngBytes.Bytes()}
	service := NewService(repo, nil, generator, &testCoverPreviews{})
	p, err := service.Create(ctx, owner, CreateInput{Title: "潮汐档案", Brief: "守塔人听到呼救", ChapterCount: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.CoverContent(ctx, owner, p.ID, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("无封面的作品应返回不存在: %v", err)
	}
	if _, err = service.UploadCover(ctx, owner, p.ID, pngBytes.Bytes()[:24]); !errors.Is(err, ErrCoverImage) {
		t.Fatalf("截断图片未拒绝: %v", err)
	}
	other := Owner{OrganizationID: owner.OrganizationID, UserID: "another"}
	if _, err = service.GenerateCover(ctx, other, p.ID, GenerateCoverInput{ModelCode: "art"}); !errors.Is(err, ErrNotFound) || generator.prompt != "" {
		t.Fatalf("其他作者不应消耗模型资源: %v", err)
	}
	upload, err := service.UploadCover(ctx, owner, p.ID, pngBytes.Bytes())
	if err != nil || upload.Source != "upload" || upload.Revision == "" || repo.project.Version != p.Version {
		t.Fatalf("上传未成功或改变了作品版本: %+v %v", upload, err)
	}
	if _, err := service.CoverContent(ctx, other, p.ID, upload.Revision); !errors.Is(err, ErrNotFound) {
		t.Fatalf("其他作者不应读取封面: %v", err)
	}
	p, err = service.Apply(ctx, owner, p.ID, Action{Kind: "save_outline", Version: p.Version, Outline: &Outline{
		Synopsis: "调查远处的声源", World: "孤岛灯塔", Characters: "巡灯员小林",
		Beats: []Beat{{Title: "雾笛", Goal: "发现异常信号"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	token, err := service.GenerateCover(ctx, owner, p.ID, GenerateCoverInput{ModelCode: " art ", Instruction: "冷色调"})
	if err != nil || token == "" || repo.project.Cover.Revision != upload.Revision || repo.project.Version != p.Version {
		t.Fatalf("生成预览失败或改变了作品: %q %v", token, err)
	}
	if generator.owner != owner || generator.model != "art" {
		t.Fatalf("未使用作者身份及所选模型: %+v %q", generator.owner, generator.model)
	}
	for _, text := range []string{"潮汐档案", "守塔人听到呼救", "调查远处的声源", "孤岛灯塔", "巡灯员小林", "冷色调"} {
		if !strings.Contains(generator.prompt, text) {
			t.Fatalf("封面提示缺少 %q: %s", text, generator.prompt)
		}
	}
	img, err := service.CoverPreviewContent(ctx, owner, p.ID, token)
	if err != nil || img.MIME != "image/png" || !bytes.Equal(img.Data, pngBytes.Bytes()) {
		t.Fatalf("未读取预览图片: %s %v", img.MIME, err)
	}
	if _, err := service.CoverPreviewContent(ctx, other, p.ID, token); !errors.Is(err, ErrNotFound) {
		t.Fatalf("其他作者不应读取封面预览: %v", err)
	}
	if _, err := service.ApplyCoverPreview(ctx, other, p.ID, token); !errors.Is(err, ErrNotFound) {
		t.Fatalf("其他作者不应应用封面预览: %v", err)
	}
	generator.data = []byte("不是图片")
	if _, err := service.GenerateCover(ctx, owner, p.ID, GenerateCoverInput{ModelCode: "art"}); !errors.Is(err, ErrCoverGeneration) ||
		repo.project.Cover.Revision != upload.Revision {
		t.Fatalf("无效模型结果覆盖已有封面: %v", err)
	}
	if _, err := service.CoverPreviewContent(ctx, owner, p.ID, token); err != nil {
		t.Fatalf("生成失败后上一张预览应可用: %v", err)
	}
	generator.data = pngBytes.Bytes()
	newToken, err := service.GenerateCover(ctx, owner, p.ID, GenerateCoverInput{ModelCode: "art", Instruction: "冷色调，减少云雾"})
	if err != nil || newToken == token || !strings.Contains(generator.prompt, "减少云雾") {
		t.Fatalf("重新生成未应用修改意见或未返回新预览: %q %v", newToken, err)
	}
	if _, err := service.ApplyCoverPreview(ctx, owner, p.ID, token); !errors.Is(err, ErrCoverPreviewExpired) {
		t.Fatalf("已替换的预览不应再次应用: %v", err)
	}
	cover, err := service.ApplyCoverPreview(ctx, owner, p.ID, newToken)
	if err != nil || cover.Source != "ai" || cover.Revision != newToken || repo.project.Version != p.Version {
		t.Fatalf("应用未更新封面或改变了作品版本: %+v %v", cover, err)
	}
	if repeated, err := service.ApplyCoverPreview(ctx, owner, p.ID, newToken); err != nil || repeated != cover {
		t.Fatalf("重试确认应返回相同结果: %+v %v", repeated, err)
	}
	img, err = service.CoverContent(ctx, owner, p.ID, cover.Revision)
	if err != nil || img.MIME != "image/png" || !bytes.Equal(img.Data, pngBytes.Bytes()) {
		t.Fatalf("未读取当前图片: %s %v", img.MIME, err)
	}
	if _, err := service.CoverContent(ctx, owner, p.ID, upload.Revision); !errors.Is(err, ErrNotFound) {
		t.Fatalf("已替换的地址仍可读: %v", err)
	}
	generator.during = func() {
		if _, err := service.UploadCover(ctx, owner, p.ID, pngBytes.Bytes()); err != nil {
			t.Fatal(err)
		}
	}
	staleToken, err := service.GenerateCover(ctx, owner, p.ID, GenerateCoverInput{ModelCode: "art"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyCoverPreview(ctx, owner, p.ID, staleToken); !errors.Is(err, ErrCoverConflict) ||
		repo.project.Cover.Source != "upload" {
		t.Fatalf("过期的封面版本覆盖了新上传封面: %v", err)
	}
}
