package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strings"

	"harness-admin/internal/coverimage"
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
	if err == nil && len(data) > coverimage.MaxBytes {
		return nil, errors.New("生成的封面超过 10 MiB")
	}
	return data, err
}

func (w *novelWriter) Complete(ctx context.Context, owner novel.Owner, model, prompt string) (string, error) {
	return w.complete(ctx, owner, model, prompt, nil)
}

func (w *novelWriter) CompleteStoryMemory(ctx context.Context, owner novel.Owner, model, prompt, jobID string) (string, error) {
	writer := w.completion
	writer.operation = "story_memory"
	ctx = modelgateway.WithCallRequestID(ctx, jobID)
	return writer.complete(ctx, owner.OrganizationID, owner.UserID, model, prompt, true, drama.ModelCallOptions{Stage: "故事档案"}, nil)
}

func (w *novelWriter) CompleteIdeaRefinement(ctx context.Context, owner novel.Owner, model, prompt string) (string, error) {
	result, err := w.complete(ctx, owner, model, prompt, applyNovelIdeaTemperature)
	if errors.Is(err, errModelContentInspection) {
		return "", novel.ErrContentInspection
	}
	return result, err
}

func (w *novelWriter) complete(ctx context.Context, owner novel.Owner, model, prompt string, configure func(*modelgateway.PreparedRequest) error) (string, error) {
	// 网关逐步接收模型输出，小说服务仍只接收完整 JSON 后再校验并保存。
	result, err := w.completion.complete(ctx, owner.OrganizationID, owner.UserID, model, prompt, true, drama.ModelCallOptions{Stage: "小说创作"}, configure)
	if errors.Is(err, modelgateway.ErrModelNotFound) || errors.Is(err, modelgateway.ErrModelDisabled) ||
		errors.Is(err, modelgateway.ErrModelNotAllowed) || errors.Is(err, modelgateway.ErrModelTypeMismatch) {
		return "", novel.ErrModel
	}
	return result, err
}

func applyNovelIdeaTemperature(prepared *modelgateway.PreparedRequest) error {
	if !strings.EqualFold(prepared.Route.UpstreamModelID, "deepseek-v4-flash-0731") {
		return nil
	}
	u, err := url.Parse(prepared.Route.ProviderBaseURL)
	if err != nil {
		return err
	}
	if !isBailianHost(u.Hostname()) {
		return nil
	}
	var body map[string]any
	if err := json.Unmarshal(prepared.Body, &body); err != nil {
		return err
	}
	// 百炼兼容接口的温度范围不含 2，取 float32 中最接近 2 的较小值，避免舍入到 2。
	body["temperature"] = float64(math.Nextafter32(2, 0))
	prepared.Body, err = json.Marshal(body)
	return err
}
