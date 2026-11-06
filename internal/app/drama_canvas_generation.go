package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"harness-admin/internal/drama"
	"harness-admin/internal/modelgateway"
)

type canvasGenerationModelCatalog interface {
	ListModelsForUser(context.Context, string, string) ([]modelgateway.AvailableModel, error)
}

type canvasReferenceDownloader interface {
	DownloadImage(context.Context, string) ([]byte, error)
}

type dramaCanvasGenerationEngine struct {
	catalog           canvasGenerationModelCatalog
	image             *dramaImageEngine
	video             *dramaVideoEngine
	referenceDownload canvasReferenceDownloader
}

const (
	canvasReferenceSizeLimit  = 10 << 20
	canvasReferenceTotalLimit = 40 << 20
)

func (e *dramaCanvasGenerationEngine) CanvasGenerationModels(ctx context.Context, organizationID, userID string) ([]drama.CanvasGenerationModel, error) {
	available, err := e.catalog.ListModelsForUser(ctx, organizationID, userID)
	if err != nil {
		return nil, err
	}
	models := make([]drama.CanvasGenerationModel, 0, len(available))
	for _, model := range available {
		if item, ok := canvasGenerationModel(model); ok {
			models = append(models, item)
		}
	}
	return models, nil
}

func canvasGenerationModel(model modelgateway.AvailableModel) (drama.CanvasGenerationModel, bool) {
	item := drama.CanvasGenerationModel{Code: model.Code, Name: model.Name, Kind: model.ModelType}
	switch model.ModelType {
	case "image":
		item.Resolutions = []string{drama.CanvasResolution1K}
		switch {
		case model.ProviderKind == "openai_compatible":
			if supportsTryOnImage(model) {
				item.MaxReferences = 3
			}
		case model.ProviderKind != "aliyun_bailian":
			return item, false
		case isImageSeries(model.UpstreamModelID, "qwen-image-2.1"):
			item.MaxReferences = 10
			item.Resolutions = append(item.Resolutions, drama.CanvasResolution2K)
		case isImageSeries(model.UpstreamModelID, "qwen-image-2.0", "qwen-image-3.0"):
			item.MaxReferences = 3
			item.Resolutions = append(item.Resolutions, drama.CanvasResolution2K)
		case isImageSeries(model.UpstreamModelID, "qwen-image-edit-plus", "qwen-image-edit-max"):
			item.MinReferences, item.MaxReferences = 1, 3
		default:
			return item, false
		}
	case "video":
		if model.ProviderKind != "aliyun_bailian" {
			return item, false
		}
		item.MinDuration = 2
		switch {
		case isWan27TextVideo(model.UpstreamModelID):
			item.MaxDuration = 15
		case model.UpstreamModelID == "wan2.7-r2v", model.UpstreamModelID == "wan2.7-r2v-2026-06-12":
			item.MinReferences, item.MaxReferences, item.MaxDuration = 1, 5, 15
		case model.UpstreamModelID == "wan3.0-video", model.UpstreamModelID == "wan3.0-video-prime":
			item.MaxReferences, item.MaxDuration = 10, 30
		default:
			return item, false
		}
	default:
		return item, false
	}
	return item, true
}

func (e *dramaCanvasGenerationEngine) GenerateCanvasMedia(ctx context.Context, in drama.CanvasGenerationRequest) ([]byte, string, error) {
	models, err := e.CanvasGenerationModels(ctx, in.OrganizationID, in.UserID)
	if err != nil {
		return nil, "", &drama.CanvasGenerationFailure{Message: "无法读取可用的生成模型，请稍后重试。", Cause: err}
	}
	var selected drama.CanvasGenerationModel
	for _, item := range models {
		if item.Code == in.ModelCode && item.Kind == in.Kind {
			selected = item
			break
		}
	}
	if selected.Code == "" {
		return nil, "", &drama.CanvasGenerationFailure{Message: "该模型无法用于此节点，请重新选择模型。", Cause: drama.ErrModelUnavailable}
	}
	if in.Resolution != "" && !slices.Contains(selected.Resolutions, in.Resolution) {
		return nil, "", &drama.CanvasGenerationFailure{Message: "该模型不支持所选分辨率，请调整尺寸或更换模型。", Cause: drama.ErrInvalidInput}
	}
	if count := len(in.ReferenceImages); count < selected.MinReferences || count > selected.MaxReferences {
		message := fmt.Sprintf("当前模型支持 %d–%d 张参考图。", selected.MinReferences, selected.MaxReferences)
		if selected.MaxReferences == 0 {
			message = "当前模型只支持文字生成，请移除参考图连线或换一个模型。"
		}
		return nil, "", &drama.CanvasGenerationFailure{Message: message, Cause: drama.ErrInvalidInput}
	}
	if in.Kind == "video" && (in.Duration < selected.MinDuration || in.Duration > selected.MaxDuration) {
		message := fmt.Sprintf("当前视频模型支持 %d–%d 秒。", selected.MinDuration, selected.MaxDuration)
		return nil, "", &drama.CanvasGenerationFailure{Message: message, Cause: drama.ErrInvalidInput}
	}
	references := make([]string, len(in.ReferenceImages))
	loggedReferences := make([]string, len(references))
	totalBytes := 0
	for i, source := range in.ReferenceImages {
		reference, size, failure := e.prepareReference(ctx, source, in.Kind)
		if failure != nil {
			failure.Message = fmt.Sprintf("第 %d 张参考图无法使用。%s", i+1, failure.Message)
			return nil, "", failure
		}
		totalBytes += size
		if totalBytes > canvasReferenceTotalLimit {
			return nil, "", &drama.CanvasGenerationFailure{Message: "参考图合计不能超过 40 MiB。", Cause: drama.ErrInvalidInput}
		}
		references[i] = reference
		loggedReferences[i] = fmt.Sprintf("canvas-reference:%d", i+1)
		if i < len(in.ReferenceIDs) && in.ReferenceIDs[i] != "" {
			loggedReferences[i] = "canvas-node:" + in.ReferenceIDs[i]
		}
	}
	ctx = modelgateway.WithCallRequestID(ctx, in.JobID)
	if in.Kind == "video" {
		if strings.TrimSpace(in.Prompt) == "" {
			in.Prompt = "根据参考图生成一段动作自然、画面连贯的视频。"
		}
		data, err := e.video.generateVideo(ctx, drama.SegmentVideoRequest{
			OrganizationID: in.OrganizationID, UserID: in.UserID, ModelCode: in.ModelCode,
			Prompt: in.Prompt, AspectRatio: in.AspectRatio, Duration: in.Duration, ReferenceImages: references,
		}, loggedReferences, true)
		if err != nil {
			message := "视频生成失败，请查看模型网关调用记录。"
			var failure *drama.SegmentVideoFailure
			if errors.As(err, &failure) {
				message = failure.Message
			}
			return nil, "", &drama.CanvasGenerationFailure{Message: message, Cause: err}
		}
		return data, "video/mp4", nil
	}
	if strings.TrimSpace(in.Prompt) == "" {
		in.Prompt = "根据参考图生成一张画面完整、风格统一的图片。"
	}
	size := dramaImageSize(in.AspectRatio)
	if in.Resolution == drama.CanvasResolution2K {
		// 百炼 Qwen Image 支持 512×512 至 2048×2048 的像素面积。
		// 横竖画幅保留精确的 16:9 比例，且都落在供应商的 2K 档位。
		size = map[string]string{"16:9": "2560x1440", "9:16": "1440x2560", "1:1": "2048x2048"}[in.AspectRatio]
		if size == "" {
			return nil, "", &drama.CanvasGenerationFailure{Message: "所选画面比例不支持 2K。", Cause: drama.ErrInvalidInput}
		}
	}
	payload := map[string]any{"model": in.ModelCode, "prompt": in.Prompt, "n": 1, "size": size}
	var loggedBody []byte
	if len(references) > 0 {
		payload["image"] = loggedReferences
		loggedBody, err = json.Marshal(payload)
		if err != nil {
			return nil, "", err
		}
		payload["image"] = references
	}
	data, mime, err := e.image.generateRecorded(ctx, in.OrganizationID, in.UserID, "image", payload, loggedBody)
	var failure *drama.ImageFailure
	if errors.As(err, &failure) {
		err = &drama.CanvasGenerationFailure{Message: failure.Message, Cause: err}
	}
	return data, mime, err
}

func (e *dramaCanvasGenerationEngine) prepareReference(ctx context.Context, source, kind string) (string, int, *drama.CanvasGenerationFailure) {
	header, encoded, hasSeparator := strings.Cut(source, ",")
	inline := hasSeparator && strings.HasPrefix(header, "data:image/")
	if !inline {
		data, err := e.referenceDownload.DownloadImage(ctx, source)
		if err != nil {
			// 传输错误的 URL 字段可能含有用户提供的签名参数。
			var fetchErr *url.Error
			if errors.As(err, &fetchErr) {
				err = fetchErr.Err
			}
			return "", 0, &drama.CanvasGenerationFailure{Message: "无法下载图片，请使用可直接访问的 HTTPS 图片地址或上传图片。", Cause: err}
		}
		mime, err := dramaImageMIME(data)
		if err != nil {
			return "", 0, &drama.CanvasGenerationFailure{Message: "链接内容无法作为参考图，请检查图片格式与大小。", Cause: err}
		}
		if len(data) > canvasReferenceSizeLimit {
			return "", 0, &drama.CanvasGenerationFailure{Message: "图片不能超过 10 MiB。", Cause: drama.ErrInvalidInput}
		}
		header, encoded = "data:"+mime+";base64", base64.StdEncoding.EncodeToString(data)
	}
	padding := len(encoded) - len(strings.TrimRight(encoded, "="))
	size := base64.StdEncoding.DecodedLen(len(encoded)) - padding
	if size <= 0 || size > canvasReferenceSizeLimit {
		return "", 0, &drama.CanvasGenerationFailure{Message: "图片为空或超过 10 MiB。", Cause: drama.ErrInvalidInput}
	}
	switch header {
	case "data:image/png;base64", "data:image/jpeg;base64", "data:image/webp;base64":
	case "data:image/gif;base64":
		if kind == "video" {
			return "", 0, &drama.CanvasGenerationFailure{Message: "视频生成需要 PNG、JPEG 或 WebP 格式参考图。", Cause: drama.ErrInvalidInput}
		}
	default:
		return "", 0, &drama.CanvasGenerationFailure{Message: "参考图格式不受支持。", Cause: drama.ErrInvalidInput}
	}
	if inline {
		return source, size, nil
	}
	return header + "," + encoded, size, nil
}
