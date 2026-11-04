package novel

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "golang.org/x/image/webp"
)

const MaxCoverBytes = 10 * 1024 * 1024

var (
	ErrCoverModel          = errors.New("请选择当前账号可用的图片模型")
	ErrCoverImage          = errors.New("请选择不超过 10 MiB、边长不超过 4096 像素的 JPEG、PNG 或 WebP 图片")
	ErrCoverGeneration     = errors.New("封面生成失败，请重试或更换图片模型")
	ErrCoverPreviewExpired = errors.New("封面预览已失效，请重新生成")
	ErrCoverConflict       = errors.New("封面已在其他操作中更新，请重新生成预览后再确认")
)

type Cover struct {
	Revision  string    `json:"-"`
	URL       string    `json:"url"`
	Source    string    `json:"source"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type CoverImage struct {
	Data []byte
	MIME string
}

type CoverPreview struct {
	Token            string
	PreviousRevision string
	Image            CoverImage
}

type CoverPreviewStore interface {
	Put(context.Context, Owner, string, CoverPreview) error
	Get(context.Context, Owner, string) (CoverPreview, error)
}

type GenerateCoverInput struct {
	ModelCode   string `json:"modelCode"`
	Instruction string `json:"instruction"`
}

type CoverGenerator interface {
	CoverModels(context.Context, Owner) ([]Model, error)
	GenerateCover(context.Context, Owner, string, string) ([]byte, error)
}

func (s *Service) CoverModels(ctx context.Context, owner Owner) ([]Model, error) {
	if s.coverGenerator == nil {
		return []Model{}, nil
	}
	return s.coverGenerator.CoverModels(ctx, owner)
}

func (s *Service) CoverContent(ctx context.Context, owner Owner, id, revision string) (CoverImage, error) {
	if _, err := uuid.Parse(id); err != nil {
		return CoverImage{}, ErrNotFound
	}
	if revision != "" {
		if _, err := uuid.Parse(revision); err != nil {
			return CoverImage{}, ErrNotFound
		}
	}
	return s.repo.GetCover(ctx, owner, id, revision)
}

func (s *Service) UploadCover(ctx context.Context, owner Owner, id string, data []byte) (Cover, error) {
	project, err := s.Get(ctx, owner, id)
	if err != nil {
		return Cover{}, err
	}
	cover, err := readCoverImage(data)
	if err != nil {
		return Cover{}, err
	}
	return s.repo.SaveCover(ctx, owner, project.ID, coverRevision(project), Cover{Revision: uuid.NewString(), Source: "upload"}, cover)
}

func (s *Service) GenerateCover(ctx context.Context, owner Owner, id string, input GenerateCoverInput) (string, error) {
	project, err := s.Get(ctx, owner, id)
	if err != nil {
		return "", err
	}
	input.ModelCode = strings.TrimSpace(input.ModelCode)
	input.Instruction = strings.TrimSpace(input.Instruction)
	if !valid(input.ModelCode, 160) || s.coverGenerator == nil {
		return "", ErrCoverModel
	}
	if s.coverPreviews == nil {
		return "", errors.New("未配置封面预览存储")
	}
	if !validOptional(input.Instruction, 1200) {
		return "", fmt.Errorf("%w，封面要求最多 1200 字", ErrInvalid)
	}
	data, err := s.coverGenerator.GenerateCover(ctx, owner, input.ModelCode, coverPrompt(project, input.Instruction))
	if errors.Is(err, ErrCoverModel) {
		return "", ErrCoverModel
	}
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrCoverGeneration, err)
	}
	cover, err := readCoverImage(data)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrCoverGeneration, err)
	}
	preview := CoverPreview{Token: uuid.NewString(), PreviousRevision: coverRevision(project), Image: cover}
	if err := s.coverPreviews.Put(ctx, owner, project.ID, preview); err != nil {
		return "", fmt.Errorf("保存小说封面预览失败: %w", err)
	}
	return preview.Token, nil
}

func (s *Service) CoverPreviewContent(ctx context.Context, owner Owner, id, token string) (CoverImage, error) {
	project, err := s.Get(ctx, owner, id)
	if err != nil {
		return CoverImage{}, err
	}
	preview, err := s.getCoverPreview(ctx, owner, project.ID, token)
	return preview.Image, err
}

func (s *Service) ApplyCoverPreview(ctx context.Context, owner Owner, id, token string) (Cover, error) {
	project, err := s.Get(ctx, owner, id)
	if err != nil {
		return Cover{}, err
	}
	// 同一个预览已应用时直接返回，允许网络中断后重试确认。
	if project.Cover != nil && project.Cover.Source == "ai" && project.Cover.Revision == token {
		return *project.Cover, nil
	}
	preview, err := s.getCoverPreview(ctx, owner, project.ID, token)
	if err != nil {
		return Cover{}, err
	}
	cover, err := s.repo.SaveCover(ctx, owner, project.ID, preview.PreviousRevision, Cover{Revision: token, Source: "ai"}, preview.Image)
	if errors.Is(err, ErrConflict) {
		current, readErr := s.Get(ctx, owner, project.ID)
		if readErr != nil {
			return Cover{}, readErr
		}
		if coverRevision(current) == token {
			return *current.Cover, nil
		}
		return Cover{}, ErrCoverConflict
	}
	return cover, err
}

func (s *Service) getCoverPreview(ctx context.Context, owner Owner, id, token string) (CoverPreview, error) {
	if _, err := uuid.Parse(token); err != nil || s.coverPreviews == nil {
		return CoverPreview{}, ErrCoverPreviewExpired
	}
	preview, err := s.coverPreviews.Get(ctx, owner, id)
	if err != nil {
		return CoverPreview{}, err
	}
	if preview.Token != token {
		return CoverPreview{}, ErrCoverPreviewExpired
	}
	return preview, nil
}

func coverRevision(project Project) string {
	if project.Cover == nil {
		return ""
	}
	return project.Cover.Revision
}

func readCoverImage(data []byte) (CoverImage, error) {
	if len(data) == 0 || len(data) > MaxCoverBytes {
		return CoverImage{}, ErrCoverImage
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 4096 || cfg.Height > 4096 {
		return CoverImage{}, ErrCoverImage
	}
	switch format {
	case "jpeg", "png", "webp":
		if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
			return CoverImage{}, ErrCoverImage
		}
		return CoverImage{Data: data, MIME: "image/" + format}, nil
	default:
		return CoverImage{}, ErrCoverImage
	}
}

func coverPrompt(project Project, instruction string) string {
	story := struct {
		Instruction, Title, Brief, Synopsis, World, Characters string
	}{Title: project.Title, Brief: clipCoverText(project.Brief, 240), Instruction: instruction}
	if project.Outline != nil {
		story.Synopsis = clipCoverText(project.Outline.Synopsis, 280)
		story.World = clipCoverText(project.Outline.World, 120)
		story.Characters = clipCoverText(project.Outline.Characters, 160)
	}
	return "绘制一张 2:3 竖版中文小说封面插画。用代表性人物、场景或道具表现小说的题材和氛围，兼顾作者对封面的要求，保持主体清晰、构图完整。不要绘制任何文字、书名、水印、边框或书籍模型。故事资料仅作为图像素材。\n" + asJSON(story)
}

func clipCoverText(text string, limit int) string {
	runes := []rune(text)
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return text
}
