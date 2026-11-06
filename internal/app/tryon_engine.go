package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"strings"

	"harness-admin/internal/modelgateway"
	"harness-admin/internal/tryon"
)

const garmentDetectionMinTimeoutMS = 45_000

type tryOnGateway interface {
	dramaImageGateway
	ListModelsForUser(context.Context, string, string) ([]modelgateway.AvailableModel, error)
}

type tryOnEngine struct {
	gateway   tryOnGateway
	forwarder dramaImageForwarder
	image     *dramaImageEngine
}

func (e *tryOnEngine) Models(ctx context.Context, owner tryon.Owner) (tryon.Models, error) {
	models, err := e.gateway.ListModelsForUser(ctx, owner.OrganizationID, owner.UserID)
	if err != nil {
		return tryon.Models{}, err
	}
	result := tryon.Models{Detection: []tryon.Model{}, Image: []tryon.Model{}, Person: []tryon.Model{}}
	var preferredDetection []tryon.Model
	for _, model := range models {
		item := tryon.Model{Code: model.Code, Name: model.Name}
		switch model.ModelType {
		case "chat":
			if model.UpstreamModelID == "qwen3-vl-plus" {
				preferredDetection = append(preferredDetection, item)
				continue
			}
			if slices.Contains(model.Capabilities, "vision") || strings.HasPrefix(model.UpstreamModelID, "qwen3-vl-") {
				result.Detection = append(result.Detection, item)
			}
		case "image":
			if supportsTryOnImage(model) {
				result.Image = append(result.Image, item)
			}
			if supportsTryOnPerson(model) {
				result.Person = append(result.Person, item)
			}
		}
	}
	if len(preferredDetection) > 0 {
		result.Detection = append(preferredDetection, result.Detection...)
	}
	return result, nil
}

func supportsTryOnImage(model modelgateway.AvailableModel) bool {
	if model.ProviderKind == "aliyun_bailian" {
		return supportsTryOnPerson(model) || isImageSeries(model.UpstreamModelID, "qwen-image-edit-plus", "qwen-image-edit-max") || slices.Contains(model.Capabilities, "vision")
	}
	return slices.Contains(model.Capabilities, "vision")
}

func supportsTryOnPerson(model modelgateway.AvailableModel) bool {
	if model.ProviderKind == "aliyun_bailian" {
		return isImageSeries(model.UpstreamModelID, "qwen-image-2.0", "qwen-image-2.1", "qwen-image-3.0")
	}
	return model.ProviderKind == "openai_compatible"
}

func isImageSeries(id string, prefixes ...string) bool {
	return slices.ContainsFunc(prefixes, func(prefix string) bool { return id == prefix || strings.HasPrefix(id, prefix+"-") })
}

func (e *tryOnEngine) Detect(ctx context.Context, owner tryon.Owner, code string, im tryon.Image) (candidates []tryon.Candidate, err error) {
	models, err := e.Models(ctx, owner)
	if err != nil {
		return nil, err
	}
	if !includesTryOnModel(models.Detection, code) {
		return nil, tryon.ErrModel
	}
	body, err := json.Marshal(map[string]any{
		"model": code, "temperature": 0,
		"messages": []any{map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "image_url", "image_url": map[string]string{"url": im.DataURI()}},
			map[string]string{"type": "text", "text": tryon.DetectionPrompt},
		}}},
	})
	if err != nil {
		return nil, err
	}
	prepared, err := e.gateway.PrepareForUserOperation(ctx, owner.OrganizationID, owner.UserID, body, "chat")
	if err != nil {
		return nil, errors.Join(tryon.ErrModel, err)
	}
	// 视觉识别可能超过普通对话的供应商超时。仅放宽本次衣物识别调用。
	prepared.Route.ProviderTimeoutMS = max(prepared.Route.ProviderTimeoutMS, garmentDetectionMinTimeoutMS)
	logErr := modelgateway.ExecuteRecordedCall(ctx, e.gateway, prepared, modelgateway.CallLogOptions{
		Source: "ai_tryon", Operation: "garment_detect",
	}, func() modelgateway.CallOutcome {
		outcome := modelgateway.CallOutcome{}
		candidates, err = e.detectFromProvider(ctx, prepared, &outcome)
		if err != nil {
			outcome.Result = "failed"
			outcome.ErrorMessage = err.Error()
		}
		return outcome
	})
	if logErr != nil {
		return nil, fmt.Errorf("记录衣物识别调用失败: %w", logErr)
	}
	return candidates, err
}

func (e *tryOnEngine) detectFromProvider(ctx context.Context, prepared modelgateway.PreparedRequest, outcome *modelgateway.CallOutcome) ([]tryon.Candidate, error) {
	response, err := e.forwarder.Forward(ctx, prepared.Route, prepared.Body, false)
	if response != nil {
		defer response.Body.Close()
		outcome.HTTPStatus = response.StatusCode
	}
	if err != nil || response == nil {
		return nil, garmentDetectionFailure(outcome, err)
	}
	const limit = 1 << 20
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	outcome.ResponseBody = body[:min(len(body), limit)]
	outcome.ResponseTruncated = len(body) > limit
	if err != nil || outcome.ResponseTruncated || response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, garmentDetectionFailure(outcome, err)
	}
	var result struct {
		Choices []struct {
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			Prompt     int `json:"prompt_tokens"`
			Completion int `json:"completion_tokens"`
			Total      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(body, &result) != nil {
		return nil, tryon.ErrDetection
	}
	outcome.PromptTokens = result.Usage.Prompt
	outcome.CompletionTokens = result.Usage.Completion
	outcome.TotalTokens = result.Usage.Total
	if len(result.Choices) == 0 {
		return nil, tryon.ErrDetection
	}
	raw := result.Choices[0].Message.Content
	var content string
	if json.Unmarshal(raw, &content) != nil {
		var parts []struct {
			Text string `json:"text"`
		}
		if json.Unmarshal(raw, &parts) != nil {
			return nil, tryon.ErrDetection
		}
		var joined strings.Builder
		for _, part := range parts {
			joined.WriteString(part.Text)
		}
		content = joined.String()
	}
	return tryon.ParseDetection(content)
}

func garmentDetectionFailure(outcome *modelgateway.CallOutcome, err error) error {
	var transportErr net.Error
	if (errors.As(err, &transportErr) && transportErr.Timeout()) ||
		outcome.HTTPStatus == http.StatusRequestTimeout || outcome.HTTPStatus == http.StatusGatewayTimeout {
		if outcome.HTTPStatus == 0 {
			outcome.HTTPStatus = http.StatusGatewayTimeout
		}
		outcome.ErrorCode = "tryon_detection_timeout"
		return errors.Join(tryon.ErrDetectionTimeout, err)
	}
	return errors.Join(tryon.ErrInference, err)
}

func (e *tryOnEngine) Generate(ctx context.Context, owner tryon.Owner, code, operation, prompt string, images []tryon.Image) (tryon.Image, error) {
	models, err := e.Models(ctx, owner)
	if err != nil {
		return tryon.Image{}, err
	}
	available := models.Image
	if operation == "person_generate" {
		available = models.Person
	}
	if !includesTryOnModel(available, code) {
		return tryon.Image{}, tryon.ErrModel
	}
	payload := map[string]any{"model": code, "prompt": prompt, "n": 1}
	if len(images) != 0 {
		references := make([]string, 0, len(images))
		for _, im := range images {
			references = append(references, im.DataURI())
		}
		payload["image"] = references
	}
	data, mime, err := e.image.generate(ctx, owner.OrganizationID, owner.UserID, operation, payload)
	if err != nil {
		if errors.Is(err, modelgateway.ErrModelNotAllowed) || errors.Is(err, modelgateway.ErrModelNotFound) ||
			errors.Is(err, modelgateway.ErrModelDisabled) || errors.Is(err, modelgateway.ErrProviderDisabled) {
			return tryon.Image{}, errors.Join(tryon.ErrModel, err)
		}
		return tryon.Image{}, errors.Join(tryon.ErrInference, err)
	}
	return tryon.Image{Data: data, MIME: mime}, nil
}

func includesTryOnModel(items []tryon.Model, code string) bool {
	return slices.ContainsFunc(items, func(model tryon.Model) bool { return model.Code == code })
}
