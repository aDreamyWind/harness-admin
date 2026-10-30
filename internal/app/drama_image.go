package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"harness-admin/internal/drama"
	"harness-admin/internal/modelgateway"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type dramaImageGateway interface {
	PrepareForUserOperation(context.Context, string, string, []byte, string) (modelgateway.PreparedRequest, error)
	RecordCall(context.Context, modelgateway.RecordCallInput) error
}
type dramaImageForwarder interface {
	dramaForwarder
	DownloadImage(context.Context, string) ([]byte, error)
}
type dramaImageEngine struct {
	gateway   dramaImageGateway
	forwarder dramaImageForwarder
}

func (w *dramaImageEngine) Generate(ctx context.Context, in drama.ImageRequest) (data []byte, mime string, callErr error) {
	// 保持目标比例；供应商不支持尺寸时明确返回错误，不静默生成错误比例。
	size := map[string]string{"9:16": "864x1536", "16:9": "1536x864", "1:1": "1024x1024"}[in.AspectRatio]
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
		return nil, "", err
	}
	started := time.Now()
	route := prepared.Route
	route.ProviderTimeoutMS = 10 * 60 * 1000
	response, err := w.forwarder.Forward(ctx, route, prepared.Body, false)
	callErr = err
	status := 502
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
	recordCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err = w.gateway.RecordCall(recordCtx, modelgateway.RecordCallInput{OrganizationID: in.OrganizationID, UserID: in.UserID, DepartmentID: prepared.Caller.DepartmentID, AuthType: "user", ModelID: route.ModelID, ProviderID: route.ProviderID, RequestID: uuid.NewString(), ModelCode: route.ModelCode, ModelType: "image", UpstreamModelID: route.UpstreamModelID, Result: result, HTTPStatus: status, LatencyMS: time.Since(started).Milliseconds(), Source: "ai_drama", Operation: "image", RequestBody: prepared.Body, ResponseBody: raw, ResponseTruncated: truncated, ErrorMessage: errorMessage}); err != nil {
		return nil, "", fmt.Errorf("记录图片模型调用失败: %w", err)
	}
	return data, mime, callErr
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
