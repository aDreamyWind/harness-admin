package modelprovider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"harness-admin/internal/modeladmin"
)

const maxDiscoveredModels = 500

func (p *Prober) Discover(ctx context.Context, credential modeladmin.ProviderCredential) ([]modeladmin.DiscoveredModel, error) {
	return p.discover(ctx, credential, false)
}

func (p *Prober) discover(ctx context.Context, credential modeladmin.ProviderCredential, probeOnly bool) ([]modeladmin.DiscoveredModel, error) {
	base, err := url.Parse(credential.Provider.BaseURL)
	if err != nil || base.Host == "" || (base.Scheme != "https" && base.Scheme != "http") {
		return nil, errors.New("供应商地址无效")
	}
	if base.Scheme == "http" {
		if _, ok := p.allowPrivateHosts[strings.ToLower(base.Hostname())]; !ok {
			return nil, errors.New("HTTP 供应商地址必须加入私有主机白名单")
		}
	}
	endpoint := *base
	endpoint.RawQuery = ""
	endpoint.Fragment = ""
	kind := credential.Provider.ProviderKind
	switch kind {
	case "aliyun_bailian":
		endpoint.Path = "/api/v1/models"
	case "ollama":
		endpoint.Path = "/api/tags"
	case "gemini":
		endpoint.Path = strings.TrimRight(base.Path, "/") + "/models"
	default:
		endpoint.Path = strings.TrimRight(base.Path, "/") + "/models"
	}
	if credential.Provider.ModelListURL != "" {
		custom, err := url.Parse(credential.Provider.ModelListURL)
		if err != nil || custom.Host == "" || (custom.Scheme != "https" && custom.Scheme != "http") || custom.User != nil || custom.RawQuery != "" || custom.Fragment != "" {
			return nil, errors.New("模型目录地址无效")
		}
		endpoint = *custom
	}
	if endpoint.Scheme == "http" {
		if _, ok := p.allowPrivateHosts[strings.ToLower(endpoint.Hostname())]; !ok {
			return nil, errors.New("HTTP 模型目录地址必须加入私有主机白名单")
		}
	}
	client, err := p.clientFor(ctx, endpoint.Hostname(), time.Duration(credential.Provider.RequestTimeoutMS)*time.Millisecond)
	if err != nil {
		return nil, err
	}
	items := make([]modeladmin.DiscoveredModel, 0)
	seen := map[string]struct{}{}
	pageToken := ""
	fallbackFromBailian := false
	for page := 0; page < 10; page++ {
		query := endpoint.Query()
		switch kind {
		case "aliyun_bailian":
			query.Set("page_no", fmt.Sprint(page+1))
			query.Set("page_size", "100")
		case "gemini":
			query.Set("pageSize", "100")
			if pageToken != "" {
				query.Set("pageToken", pageToken)
			}
		case "claude":
			query.Set("limit", "100")
			if pageToken != "" {
				query.Set("after_id", pageToken)
			}
		case "siliconflow":
			if page < 4 {
				query.Set("type", []string{"text", "image", "audio", "video"}[page])
			} else {
				query.Del("type")
				query.Set("sub_type", []string{"chat", "embedding", "reranker"}[page-4])
			}
		}
		endpoint.RawQuery = query.Encode()
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
		if err != nil {
			return nil, errors.New("无法创建供应商请求")
		}
		request.Header.Set("Accept", "application/json")
		switch kind {
		case "claude":
			request.Header.Set("x-api-key", credential.APIKey)
			request.Header.Set("anthropic-version", "2023-06-01")
		case "gemini":
			request.Header.Set("x-goog-api-key", credential.APIKey)
		default:
			request.Header.Set("Authorization", "Bearer "+credential.APIKey)
		}
		response, err := client.Do(request)
		if err != nil {
			return nil, errors.New("无法连接模型供应商")
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 2*1024*1024+1))
		response.Body.Close()
		if readErr != nil || len(body) > 2*1024*1024 {
			return nil, errors.New("供应商响应过大或读取失败")
		}
		if response.StatusCode == http.StatusNotFound && kind == "aliyun_bailian" && page == 0 && credential.Provider.ModelListURL == "" {
			// 旧版 DashScope 地址可能没有完整目录接口，退回兼容模型列表。
			fallbackFromBailian = true
			kind = "openai_compatible"
			endpoint.Path = strings.TrimRight(base.Path, "/") + "/models"
			endpoint.RawQuery = ""
			page--
			continue
		}
		if response.StatusCode == http.StatusNotFound && fallbackFromBailian {
			return nil, modeladmin.ErrModelListURLRequired
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return nil, fmt.Errorf("供应商返回 HTTP %d", response.StatusCode)
		}
		var envelope struct {
			Data   []json.RawMessage `json:"data"`
			Models []json.RawMessage `json:"models"`
			Output struct {
				Models []json.RawMessage `json:"models"`
			} `json:"output"`
			NextPageToken string `json:"nextPageToken"`
			HasMore       bool   `json:"has_more"`
			LastID        string `json:"last_id"`
		}
		if err := json.Unmarshal(body, &envelope); err != nil {
			return nil, errors.New("模型列表格式不正确")
		}
		raw := envelope.Data
		if kind == "aliyun_bailian" {
			raw = envelope.Output.Models
		}
		if kind == "ollama" || kind == "gemini" {
			raw = envelope.Models
		}
		if raw == nil {
			return nil, errors.New("供应商返回的模型列表格式不正确")
		}
		for _, entry := range raw {
			model := parseDiscoveredModel(entry, kind)
			if kind == "siliconflow" {
				switch page {
				case 1:
					model.ModelType = "image"
				case 2:
					model.ModelType = "audio"
				case 3:
					model.ModelType = "video"
				case 4:
					model.ModelType = "chat"
				case 5:
					model.ModelType = "embedding"
				case 6:
					model.ModelType = "rerank"
				}
			}
			if model.UpstreamModelID == "" || utf8.RuneCountInString(model.UpstreamModelID) > 200 {
				continue
			}
			if _, ok := seen[model.UpstreamModelID]; ok {
				if kind == "siliconflow" && page >= 4 {
					for i := range items {
						if items[i].UpstreamModelID == model.UpstreamModelID {
							items[i].ModelType = model.ModelType
							break
						}
					}
				}
				continue
			}
			seen[model.UpstreamModelID] = struct{}{}
			items = append(items, model)
			if len(items) >= maxDiscoveredModels {
				return items, nil
			}
		}
		if probeOnly {
			return items, nil
		}
		switch kind {
		case "aliyun_bailian":
			if len(raw) < 100 {
				return items, nil
			}
		case "gemini":
			if envelope.NextPageToken == "" {
				return items, nil
			}
			pageToken = envelope.NextPageToken
		case "claude":
			if !envelope.HasMore || envelope.LastID == "" {
				return items, nil
			}
			pageToken = envelope.LastID
		case "siliconflow":
			if page == 6 {
				return items, nil
			}
		default:
			return items, nil
		}
	}
	return items, nil
}

func parseDiscoveredModel(raw json.RawMessage, kind string) modeladmin.DiscoveredModel {
	var value struct {
		ID                string   `json:"id"`
		Model             string   `json:"model"`
		Name              string   `json:"name"`
		DisplayName       string   `json:"displayName"`
		ContextWindow     int      `json:"context_window"`
		MaxOutputTokens   int      `json:"max_output_tokens"`
		InputTokenLimit   int      `json:"inputTokenLimit"`
		OutputTokenLimit  int      `json:"outputTokenLimit"`
		Capabilities      []string `json:"capabilities"`
		Features          []string `json:"features"`
		InferenceMetadata struct {
			ResponseModality []string `json:"response_modality"`
			RequestModality  []string `json:"request_modality"`
		} `json:"inference_metadata"`
		ModelInfo struct {
			ContextWindow   int `json:"context_window"`
			MaxOutputTokens int `json:"max_output_tokens"`
		} `json:"model_info"`
	}
	if json.Unmarshal(raw, &value) != nil {
		return modeladmin.DiscoveredModel{}
	}
	id := value.ID
	if kind == "aliyun_bailian" {
		id = value.Model
	}
	if kind == "gemini" {
		id = strings.TrimPrefix(value.Name, "models/")
	}
	if kind == "ollama" {
		id = value.Name
	}
	name := value.Name
	if kind == "gemini" && value.DisplayName != "" {
		name = value.DisplayName
	}
	if kind == "ollama" || name == "" {
		name = id
	}
	if utf8.RuneCountInString(name) > 120 {
		name = string([]rune(name)[:120])
	}
	modelType := "unknown"
	capabilities := []string{}
	switch kind {
	case "deepseek", "claude":
		modelType, capabilities = "chat", []string{"text"}
	case "aliyun_bailian":
		for _, modality := range value.InferenceMetadata.ResponseModality {
			switch strings.ToLower(modality) {
			case "text":
				modelType = "chat"
			case "image":
				modelType = "image"
			case "video":
				modelType = "video"
			case "audio":
				modelType = "audio"
			}
		}
		for _, capability := range value.Capabilities {
			switch strings.ToUpper(capability) {
			case "TG":
				modelType = "chat"
			case "IG":
				modelType = "image"
			case "VG":
				modelType = "video"
			case "TTS", "ASR":
				modelType = "audio"
			case "REALTIME-TEXT-TO-SPEECH", "REALTIME-ASR", "REALTIME-AUDIO-TRANSLATE", "REALTIME-CHATTING":
				modelType = "audio"
			case "TR":
				modelType = "embedding"
			case "ME":
				modelType = "embedding"
			case "VU":
				if modelType == "unknown" {
					modelType = "chat"
				}
				capabilities = append(capabilities, "vision")
			}
		}
		for _, feature := range value.Features {
			switch feature {
			case "function-calling":
				capabilities = append(capabilities, "tools")
			case "structured-outputs":
				capabilities = append(capabilities, "json")
			}
		}
		if modelType == "chat" {
			capabilities = append(capabilities, "text")
		}
		for _, modality := range value.InferenceMetadata.RequestModality {
			if strings.EqualFold(modality, "Image") && modelType == "chat" {
				capabilities = append(capabilities, "vision")
			}
		}
	case "siliconflow":
		// 该接口返回的类型不一定出现在条目中，后续由调用方按查询类型补充。
	}
	contextWindow := value.ContextWindow
	if value.ModelInfo.ContextWindow > 0 {
		contextWindow = value.ModelInfo.ContextWindow
	}
	if value.InputTokenLimit > 0 {
		contextWindow = value.InputTokenLimit
	}
	maxOutput := value.MaxOutputTokens
	if value.ModelInfo.MaxOutputTokens > 0 {
		maxOutput = value.ModelInfo.MaxOutputTokens
	}
	if value.OutputTokenLimit > 0 {
		maxOutput = value.OutputTokenLimit
	}
	if contextWindow > 0 && maxOutput > contextWindow {
		maxOutput = 0
	}
	return modeladmin.DiscoveredModel{UpstreamModelID: id, Name: name, ModelType: modelType, Capabilities: capabilities, ContextWindowTokens: contextWindow, MaxOutputTokens: maxOutput}
}
