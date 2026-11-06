package novel

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"harness-admin/internal/coverimage"
)

var (
	ErrCoverModel          = errors.New("请选择当前账号可用的图片模型")
	ErrCoverStyle          = errors.New("请选择风格库中的画面风格，或清空风格后重试")
	ErrCoverImage          = coverimage.ErrInvalidUpload
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

type CoverHistoryItem struct {
	Revision  string    `json:"revision"`
	Source    string    `json:"source"`
	URL       string    `json:"url"`
	CreatedAt time.Time `json:"createdAt"`
	Current   bool      `json:"current"`
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
	StyleID     string `json:"styleId,omitempty"`
	Instruction string `json:"instruction"`
}

type CoverStyle struct {
	Name        string
	ImagePrompt string
}

type CoverStyleResolver interface {
	ResolveCoverStyle(context.Context, string) (CoverStyle, error)
}

type CoverGenerator interface {
	CoverModels(context.Context, Owner) ([]Model, error)
	GenerateCover(context.Context, Owner, string, string) ([]byte, error)
}

func (s *Service) WithCoverStyles(resolver CoverStyleResolver) *Service {
	s.coverStyles = resolver
	return s
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

func (s *Service) ListCoverHistory(ctx context.Context, owner Owner, id string) ([]CoverHistoryItem, error) {
	project, err := uuid.Parse(id)
	if err != nil {
		return nil, ErrNotFound
	}
	return s.repo.ListCoverHistory(ctx, owner, project.String())
}

func (s *Service) ApplyCoverHistory(ctx context.Context, owner Owner, id, revision string) (Cover, error) {
	project, err := uuid.Parse(id)
	if err != nil {
		return Cover{}, ErrNotFound
	}
	version, err := uuid.Parse(revision)
	if err != nil {
		return Cover{}, fmt.Errorf("%w，封面版本无效", ErrInvalid)
	}
	return s.repo.SelectCoverVersion(ctx, owner, project.String(), version.String())
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
	if s.coverJobs != nil {
		return s.awaitCoverGeneration(ctx, owner, id, input)
	}
	project, prompt, err := s.prepareCoverGeneration(ctx, owner, id, &input)
	if err != nil {
		return "", err
	}
	preview, err := s.generateCoverPreview(ctx, owner, input.ModelCode, prompt, coverRevision(project), uuid.NewString())
	if err != nil {
		return "", err
	}
	if err := s.archiveCoverPreview(ctx, owner, project.ID, preview); err != nil {
		return "", err
	}
	if err := s.coverPreviews.Put(ctx, owner, project.ID, preview); err != nil {
		return "", fmt.Errorf("保存小说封面预览失败: %w", err)
	}
	return preview.Token, nil
}

func (s *Service) archiveCoverPreview(ctx context.Context, owner Owner, id string, preview CoverPreview) error {
	if err := s.repo.SaveCoverVersion(ctx, owner, id, Cover{Revision: preview.Token, Source: "ai"}, preview.Image); err != nil {
		return fmt.Errorf("保存小说封面历史失败: %w", err)
	}
	return nil
}

func (s *Service) prepareCoverGeneration(ctx context.Context, owner Owner, id string, input *GenerateCoverInput) (Project, string, error) {
	project, err := s.Get(ctx, owner, id)
	if err != nil {
		return Project{}, "", err
	}
	input.ModelCode = strings.TrimSpace(input.ModelCode)
	input.StyleID = strings.TrimSpace(input.StyleID)
	input.Instruction = strings.TrimSpace(input.Instruction)
	if !valid(input.ModelCode, 160) || s.coverGenerator == nil {
		return Project{}, "", ErrCoverModel
	}
	if s.coverPreviews == nil {
		return Project{}, "", errors.New("未配置封面预览存储")
	}
	if !validOptional(input.Instruction, 1200) {
		return Project{}, "", fmt.Errorf("%w，封面要求最多 1200 字", ErrInvalid)
	}
	var style CoverStyle
	if input.StyleID != "" {
		if !valid(input.StyleID, 80) || s.coverStyles == nil {
			return Project{}, "", ErrCoverStyle
		}
		style, err = s.coverStyles.ResolveCoverStyle(ctx, input.StyleID)
		if err != nil {
			return Project{}, "", err
		}
	}
	return project, coverPrompt(project, input.Instruction, style), nil
}

func (s *Service) generateCoverPreview(ctx context.Context, owner Owner, modelCode, prompt, previousRevision, token string) (CoverPreview, error) {
	data, err := s.coverGenerator.GenerateCover(ctx, owner, modelCode, prompt)
	if errors.Is(err, ErrCoverModel) {
		return CoverPreview{}, ErrCoverModel
	}
	if err != nil {
		return CoverPreview{}, fmt.Errorf("%w: %v", ErrCoverGeneration, err)
	}
	cover, err := readCoverImage(data)
	if err != nil {
		return CoverPreview{}, fmt.Errorf("%w: %v", ErrCoverGeneration, err)
	}
	return CoverPreview{Token: token, PreviousRevision: previousRevision, Image: cover}, nil
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
	mime, err := coverimage.Validate(data)
	if err != nil {
		return CoverImage{}, err
	}
	return CoverImage{Data: data, MIME: mime}, nil
}

func coverPrompt(project Project, instruction string, style CoverStyle) string {
	story := struct {
		Instruction, Title, Brief, Synopsis, World, Characters string
	}{Title: project.Title, Brief: clipCoverText(project.Brief, 240), Instruction: instruction}
	if project.Outline != nil {
		story.Synopsis = clipCoverText(project.Outline.Synopsis, 280)
		story.World = clipCoverText(project.Outline.World, 120)
		story.Characters = clipCoverText(project.Outline.Characters, 160)
	}
	prompt := "绘制一张 2:3 竖版中文小说封面插画。用代表性人物、场景或道具表现小说的题材和氛围，兼顾作者对封面的要求，保持主体清晰、构图完整。不要绘制任何文字、书名、水印、边框或书籍模型。"
	if style.Name != "" {
		prompt += "\n画面采用「" + style.Name + "」风格。" + style.ImagePrompt + "\n"
	}
	return prompt + "故事资料仅作为图像素材。\n" + asJSON(story)
}

func clipCoverText(text string, limit int) string {
	runes := []rune(text)
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return text
}
