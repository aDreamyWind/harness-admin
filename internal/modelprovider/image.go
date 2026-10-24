package modelprovider

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"harness-admin/internal/modelgateway"
)

const maxNativeImageResponse = 32 * 1024 * 1024

var ErrInvalidImageResponse = errors.New("百炼图片响应无效")

// 旧版百炼公共域名没有 OpenAI 图片路由，图片请求须走 DashScope 原生接口。
func usesDashScopeNativeImage(baseURL *url.URL, route modelgateway.Route) bool {
	if route.ModelType != "image" || route.ProviderKind != "aliyun_bailian" {
		return false
	}
	switch strings.ToLower(baseURL.Hostname()) {
	case "dashscope.aliyuncs.com", "dashscope-intl.aliyuncs.com":
		return true
	default:
		return false
	}
}

func dashScopeImageRequest(body []byte) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || fields == nil {
		return nil, errors.New("图片请求 JSON 无效")
	}
	var prompt string
	if err := json.Unmarshal(fields["prompt"], &prompt); err != nil || strings.TrimSpace(prompt) == "" {
		return nil, errors.New("图片提示词无效")
	}
	content := make([]map[string]string, 0, 4)
	if raw, ok := fields["image"]; ok {
		var images []string
		var single string
		if json.Unmarshal(raw, &single) == nil {
			images = []string{single}
		} else if json.Unmarshal(raw, &images) != nil {
			return nil, errors.New("参考图须为地址或地址数组")
		}
		if len(images) == 0 {
			return nil, errors.New("参考图不能为空")
		}
		for _, item := range images {
			if strings.TrimSpace(item) == "" {
				return nil, errors.New("参考图地址不能为空")
			}
			content = append(content, map[string]string{"image": item})
		}
	}
	content = append(content, map[string]string{"text": prompt})
	parameters := make(map[string]json.RawMessage)
	for name, raw := range fields {
		switch name {
		case "model", "prompt", "image", "response_format", "user":
			continue
		case "size":
			var size string
			if err := json.Unmarshal(raw, &size); err != nil {
				return nil, errors.New("图片尺寸无效")
			}
			if size == "auto" {
				continue
			}
			encoded, _ := json.Marshal(strings.Replace(size, "x", "*", 1))
			parameters[name] = encoded
		default:
			parameters[name] = raw
		}
	}
	request := map[string]any{
		"model": fields["model"],
		"input": map[string]any{"messages": []any{map[string]any{"role": "user", "content": content}}},
	}
	if len(parameters) > 0 {
		request["parameters"] = parameters
	}
	return json.Marshal(request)
}

func normalizeDashScopeImageResponse(response *http.Response) error {
	body, err := io.ReadAll(io.LimitReader(response.Body, maxNativeImageResponse+1))
	if err != nil || len(body) > maxNativeImageResponse {
		return fmt.Errorf("%w: 读取失败或超过大小限制: %v", ErrInvalidImageResponse, err)
	}
	if providerError, status, ok := dashScopeImageError(body); ok {
		replaceImageResponseBody(response, providerError)
		response.StatusCode = status
		response.Status = fmt.Sprintf("%d %s", status, http.StatusText(status))
		return nil
	}
	var upstream struct {
		Output struct {
			Choices []struct {
				Message struct {
					Content []struct {
						Image string `json:"image"`
					} `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		} `json:"output"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(body, &upstream); err != nil {
		return fmt.Errorf("%w: JSON 解析失败: %v", ErrInvalidImageResponse, err)
	}
	data := make([]map[string]string, 0)
	for _, choice := range upstream.Output.Choices {
		for _, item := range choice.Message.Content {
			if item.Image != "" {
				data = append(data, map[string]string{"url": item.Image})
			}
		}
	}
	if len(data) == 0 {
		return fmt.Errorf("%w: 缺少图片地址", ErrInvalidImageResponse)
	}
	encoded, err := json.Marshal(map[string]any{"created": time.Now().Unix(), "data": data, "request_id": upstream.RequestID})
	if err != nil {
		return err
	}
	replaceImageResponseBody(response, encoded)
	return nil
}

func dashScopeImageError(body []byte) ([]byte, int, bool) {
	status := http.StatusBadGateway
	if bytes.Contains(body, []byte("event:error")) {
		for _, line := range bytes.Split(body, []byte{'\n'}) {
			line = bytes.TrimSpace(line)
			if bytes.HasPrefix(line, []byte(":HTTP_STATUS/")) {
				if parsed, err := strconv.Atoi(string(bytes.TrimPrefix(line, []byte(":HTTP_STATUS/")))); err == nil && parsed >= 400 && parsed <= 599 {
					status = parsed
				}
			}
		}
		for _, line := range bytes.Split(body, []byte{'\n'}) {
			line = bytes.TrimSpace(line)
			if bytes.HasPrefix(line, []byte("data:")) {
				payload := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
				if imageErrorJSON(payload) {
					return append([]byte(nil), payload...), status, true
				}
			}
		}
	}
	if imageErrorJSON(body) {
		return body, status, true
	}
	return nil, 0, false
}

func imageErrorJSON(body []byte) bool {
	var payload struct {
		Code string `json:"code"`
	}
	return json.Unmarshal(body, &payload) == nil && payload.Code != ""
}

func replaceImageResponseBody(response *http.Response, body []byte) {
	response.Body.Close()
	response.Body = io.NopCloser(bytes.NewReader(body))
	response.ContentLength = int64(len(body))
	response.Header.Set("Content-Type", "application/json")
	response.Header.Set("Content-Length", fmt.Sprint(len(body)))
}
