package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"

	"harness-admin/internal/drama"
	"harness-admin/internal/modelgateway"
)

type dramaModelGateway interface {
	PrepareForUser(context.Context, string, string, []byte) (modelgateway.PreparedRequest, error)
	ListModelsForUser(context.Context, string, string) ([]modelgateway.AvailableModel, error)
	RecordCall(context.Context, modelgateway.RecordCallInput) error
}

type dramaForwarder interface {
	Forward(context.Context, modelgateway.Route, []byte, bool) (*http.Response, error)
}

const dramaRequestTimeoutMS = 120000

type dramaWriter struct {
	gateway   dramaModelGateway
	forwarder dramaForwarder
}

func (w *dramaWriter) AvailableModels(ctx context.Context, organizationID, userID string) ([]drama.Model, error) {
	items, err := w.gateway.ListModelsForUser(ctx, organizationID, userID)
	if err != nil {
		return nil, err
	}
	result := make([]drama.Model, 0, len(items))
	for _, item := range items {
		if item.ModelType == "chat" || item.ModelType == "image" || item.ModelType == "video" {
			result = append(result, drama.Model{Code: item.Code, Name: item.Name, ModelType: item.ModelType})
		}
	}
	return result, nil
}

func (w *dramaWriter) Complete(ctx context.Context, organizationID, userID, modelCode, prompt string) (string, error) {
	body, err := json.Marshal(map[string]any{
		"model": modelCode, "stream": false,
		"messages": []map[string]string{
			{"role": "system", "content": "你是专业漫剧创作团队。只返回合法 JSON，不输出 Markdown。"},
			{"role": "user", "content": prompt},
		},
	})
	if err != nil {
		return "", err
	}
	prepared, err := w.gateway.PrepareForUser(ctx, organizationID, userID, body)
	if err != nil {
		return "", fmt.Errorf("模型调用准备失败: %w", err)
	}
	// 大段剧本解析通常超过供应商连接用于普通对话的短超时。
	route := prepared.Route
	if route.ProviderTimeoutMS < dramaRequestTimeoutMS {
		route.ProviderTimeoutMS = dramaRequestTimeoutMS
	}
	started := time.Now()
	response, callErr := w.forwarder.Forward(ctx, route, prepared.Body, false)
	status := 0
	if response != nil {
		status = response.StatusCode
	}
	if callErr != nil {
		status = http.StatusBadGateway
	}
	if callErr == nil && response == nil {
		callErr = errors.New("模型供应商未返回响应")
		status = http.StatusBadGateway
	}
	var content []byte
	if callErr == nil {
		defer response.Body.Close()
		content, callErr = io.ReadAll(io.LimitReader(response.Body, 4*1024*1024+1))
		if len(content) > 4*1024*1024 {
			callErr = errors.New("模型响应过大")
		}
	}
	result := "success"
	if callErr != nil || status < 200 || status >= 300 {
		result = "failed"
	}
	var completion struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if result == "success" && json.Unmarshal(content, &completion) != nil {
		callErr = errors.New("模型响应不是有效 JSON")
		result = "failed"
	}
	if result == "success" && (len(completion.Choices) == 0 || completion.Choices[0].Message.Content == "") {
		callErr = errors.New("模型返回的内容无法读取")
		result = "failed"
	}
	recordCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	recordErr := w.gateway.RecordCall(recordCtx, modelgateway.RecordCallInput{
		OrganizationID: organizationID, UserID: userID, DepartmentID: prepared.Caller.DepartmentID,
		AuthType: "user", ModelID: prepared.Route.ModelID, ProviderID: prepared.Route.ProviderID, RequestID: uuid.NewString(),
		ModelCode: prepared.Route.ModelCode, ModelType: prepared.Route.ModelType,
		UpstreamModelID: prepared.Route.UpstreamModelID, Result: result, HTTPStatus: status,
		LatencyMS: time.Since(started).Milliseconds(), PromptTokens: completion.Usage.PromptTokens,
		CompletionTokens: completion.Usage.CompletionTokens, TotalTokens: completion.Usage.TotalTokens,
	})
	if recordErr != nil {
		return "", fmt.Errorf("记录模型调用失败: %w", recordErr)
	}
	if callErr != nil {
		if errors.Is(callErr, context.DeadlineExceeded) {
			return "", errors.New("文本模型响应超时，请在项目页点击继续解析")
		}
		return "", fmt.Errorf("模型调用失败: %w", callErr)
	}
	if status < 200 || status >= 300 {
		return "", fmt.Errorf("模型供应商返回状态 %d", status)
	}
	return completion.Choices[0].Message.Content, nil
}
