package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"harness-admin/internal/drama"
	"harness-admin/internal/modelgateway"
)

type dramaImageGateway interface {
	PrepareForUserOperation(context.Context, string, string, []byte, string) (modelgateway.PreparedRequest, error)
	modelgateway.CallRecorder
}
type dramaImageForwarder interface {
	dramaForwarder
	DownloadImage(context.Context, string) ([]byte, error)
}
type dramaImageEngine struct {
	gateway   dramaImageGateway
	forwarder dramaImageForwarder
	source    string
}

func (w *dramaImageEngine) Generate(ctx context.Context, in drama.ImageRequest) (data []byte, mime string, callErr error) {
	// 保持目标比例；供应商不支持尺寸时明确返回错误，不静默生成错误比例。
	size := map[string]string{"9:16": "864x1536", "16:9": "1536x864", "1:1": "1024x1024", "2:3": "1024x1536"}[in.AspectRatio]
	payload := map[string]any{"model": in.ModelCode, "prompt": in.Prompt, "n": 1, "size": size}
	if in.ReferenceDataURI != "" {
		payload["image"] = in.ReferenceDataURI
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, "", err
	}
	prepared, err := w.gateway.PrepareForUserOperation(ctx, in.OrganizationID, in.UserID, body, "image")
	if err != nil {
		return nil, "", &drama.ImageFailure{Message: "图片模型调用未通过，请检查模型可用性、权限或调用限制。", Cause: err}
	}
	route := prepared.Route
	route.ProviderTimeoutMS = 10 * 60 * 1000
	source := w.source
	if source == "" {
		source = "ai_drama"
	}
	status := http.StatusBadGateway
	logErr := modelgateway.ExecuteRecordedCall(ctx, w.gateway, prepared, modelgateway.CallLogOptions{
		Source: source, Operation: "image",
	}, func() modelgateway.CallOutcome {
		response, err := w.forwarder.Forward(ctx, route, prepared.Body, false)
		callErr = err
		var raw []byte
		truncated := false
		if response != nil {
			defer response.Body.Close()
			status = response.StatusCode
			raw, err = io.ReadAll(io.LimitReader(response.Body, 32*1024*1024+1))
			if err != nil {
				callErr = err
			}
			if len(raw) > 32*1024*1024 {
				raw = raw[:32*1024*1024]
				truncated = true
				callErr = errors.New("图片响应超过 32 MB")
			}
		}
		if callErr == nil && (status < 200 || status >= 300) {
			var failure struct {
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			_ = json.Unmarshal(raw, &failure)
			if failure.Error.Message != "" {
				callErr = fmt.Errorf("图片模型返回状态 %d: %s", status, failure.Error.Message)
			} else {
				callErr = fmt.Errorf("图片模型返回状态 %d，请查看调用记录", status)
			}
		}
		if callErr == nil {
			var result struct {
				Data []struct {
					URL    string `json:"url"`
					Base64 string `json:"b64_json"`
				} `json:"data"`
			}
			if err = json.Unmarshal(raw, &result); err != nil || len(result.Data) == 0 {
				callErr = errors.New("图片模型未返回可读取的图片，请查看调用记录")
			} else if result.Data[0].Base64 != "" {
				data, callErr = base64.StdEncoding.DecodeString(result.Data[0].Base64)
			} else if result.Data[0].URL != "" {
				data, callErr = w.forwarder.DownloadImage(ctx, result.Data[0].URL)
			} else {
				callErr = errors.New("图片模型返回了空图片")
			}
			if callErr == nil {
				mime, callErr = dramaImageMIME(data)
			}
		}
		result, errorMessage := "success", ""
		if callErr != nil {
			result, errorMessage = "failed", callErr.Error()
		}
		return modelgateway.CallOutcome{
			Result: result, HTTPStatus: status, ErrorMessage: errorMessage,
			ResponseBody: raw, ResponseTruncated: truncated,
		}
	})
	if logErr != nil {
		return nil, "", &drama.ImageFailure{Message: "记录图片调用日志失败，请联系管理员检查服务。", Cause: fmt.Errorf("记录图片模型调用失败: %w", logErr)}
	}
	if callErr != nil {
		failure := &drama.ImageFailure{Message: imageFailureMessage(status, callErr)}
		if source == "ai_drama" {
			failure.Cause = callErr
		}
		return data, mime, failure
	}
	return data, mime, nil
}

func imageFailureMessage(status int, err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "图片模型响应超时，请重试。"
	case status == 400:
		return "图片模型拒绝了请求（HTTP 400），请确认模型支持所选比例和参考图。"
	case status == 401 || status == 403:
		return fmt.Sprintf("图片模型鉴权失败（HTTP %d），请检查供应商配置。", status)
	case status == 429:
		return "图片模型调用过于频繁，请稍后重试。"
	case status >= 500:
		return "图片供应商暂时不可用，请稍后重试。"
	case status >= 200 && status < 300:
		return "图片模型返回的图片无法读取，请查看调用记录。"
	default:
		return "图片生成失败，请查看调用记录。"
	}
}

func dramaImageMIME(data []byte) (string, error) {
	if len(data) == 0 || len(data) > 20*1024*1024 {
		return "", errors.New("图片为空或超过 20 MB")
	}
	mime := http.DetectContentType(data)
	switch mime {
	case "image/png", "image/jpeg", "image/webp", "image/gif":
		return mime, nil
	}
	return "", errors.New("模型返回的内容不是支持的图片格式")
}

type dramaMediaFiles struct{ directory string }

func (f *dramaMediaFiles) Put(ctx context.Context, id string, data []byte, mime string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if _, err := uuid.Parse(id); err != nil {
		return "", err
	}
	actual, err := dramaImageMIME(data)
	if err != nil {
		return "", err
	}
	if mime != actual {
		return "", errors.New("图片内容与媒体类型不一致")
	}
	extension := map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/webp": ".webp", "image/gif": ".gif"}[mime]
	key := id + extension
	if err = os.MkdirAll(f.directory, 0700); err != nil {
		return "", fmt.Errorf("创建漫剧媒体目录失败: %w", err)
	}
	temp, err := os.CreateTemp(f.directory, ".image-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(temp.Name())
	_, writeErr := temp.Write(data)
	closeErr := temp.Close()
	if writeErr != nil {
		return "", writeErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	if err = os.Rename(temp.Name(), filepath.Join(f.directory, key)); err != nil {
		return "", err
	}
	return key, nil
}
func (f *dramaMediaFiles) Get(ctx context.Context, key string) ([]byte, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if key == "" || filepath.Base(key) != key || strings.ContainsAny(key, "/\\") {
		return nil, "", drama.ErrNotFound
	}
	ext := filepath.Ext(key)
	if _, err := uuid.Parse(strings.TrimSuffix(key, ext)); err != nil {
		return nil, "", drama.ErrNotFound
	}
	if ext != ".png" && ext != ".jpg" && ext != ".webp" && ext != ".gif" {
		return nil, "", drama.ErrNotFound
	}
	file, err := os.Open(filepath.Join(f.directory, key))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", drama.ErrNotFound
		}
		return nil, "", err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 20*1024*1024+1))
	if err != nil {
		return nil, "", err
	}
	mime, err := dramaImageMIME(data)
	return data, mime, err
}
