package app

import (
	"context"
	"errors"
	"fmt"

	"harness-admin/internal/drama"
	"harness-admin/internal/modelgateway"
	"harness-admin/internal/novel"
)

type novelWriter struct {
	completion dramaWriter
	image      *dramaImageEngine
	styles     interface {
		ResolveCatalogStyle(context.Context, string) (drama.VisualStyle, error)
	}
}

func (w *novelWriter) Models(ctx context.Context, owner novel.Owner) ([]novel.Model, error) {
	return w.modelsByType(ctx, owner, "chat")
}

func (w *novelWriter) CoverModels(ctx context.Context, owner novel.Owner) ([]novel.Model, error) {
	return w.modelsByType(ctx, owner, "image")
}

func (w *novelWriter) ResolveCoverStyle(ctx context.Context, id string) (novel.CoverStyle, error) {
	style, err := w.styles.ResolveCatalogStyle(ctx, id)
	if errors.Is(err, drama.ErrStyleUnavailable) {
		return novel.CoverStyle{}, novel.ErrCoverStyle
	}
	if err != nil {
		return novel.CoverStyle{}, fmt.Errorf("读取小说封面风格失败: %w", err)
	}
	return novel.CoverStyle{Name: style.Name, ImagePrompt: style.ImagePrompt}, nil
}

func (w *novelWriter) modelsByType(ctx context.Context, owner novel.Owner, modelType string) ([]novel.Model, error) {
	items, err := w.completion.gateway.ListModelsForUser(ctx, owner.OrganizationID, owner.UserID)
	if err != nil {
		return nil, err
	}
	models := []novel.Model{}
	for _, item := range items {
		if item.ModelType == modelType {
			models = append(models, novel.Model{Code: item.Code, Name: item.Name})
		}
	}
	return models, nil
}

func (w *novelWriter) GenerateCover(ctx context.Context, owner novel.Owner, model, prompt string) ([]byte, error) {
	data, _, err := w.image.Generate(ctx, drama.ImageRequest{
		OrganizationID: owner.OrganizationID, UserID: owner.UserID, ModelCode: model,
		Prompt: prompt, AspectRatio: "2:3",
	})
	if errors.Is(err, modelgateway.ErrModelNotFound) || errors.Is(err, modelgateway.ErrModelDisabled) ||
		errors.Is(err, modelgateway.ErrModelNotAllowed) || errors.Is(err, modelgateway.ErrModelTypeMismatch) ||
		errors.Is(err, modelgateway.ErrProviderDisabled) {
		return nil, novel.ErrCoverModel
	}
	if err == nil && len(data) > novel.MaxCoverBytes {
		return nil, errors.New("生成的封面超过 10 MB")
	}
	return data, err
}

func (w *novelWriter) Complete(ctx context.Context, owner novel.Owner, model, prompt string) (string, error) {
	// 网关逐步接收模型输出，小说服务仍只接收完整 JSON 后再校验并保存。
	result, err := w.completion.CompleteObserved(ctx, owner.OrganizationID, owner.UserID, model, prompt, drama.ModelCallOptions{Stage: "小说创作"})
	if errors.Is(err, modelgateway.ErrModelNotFound) || errors.Is(err, modelgateway.ErrModelDisabled) ||
		errors.Is(err, modelgateway.ErrModelNotAllowed) || errors.Is(err, modelgateway.ErrModelTypeMismatch) {
		return "", novel.ErrModel
	}
	return result, err
}
