package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"harness-admin/internal/httpx"
	"harness-admin/internal/modelgateway"
)

const maxGatewayRequestBody = 4 * 1024 * 1024
const maxGatewayResponseBody = 32 * 1024 * 1024

type ModelGatewayService interface {
	ListAPIKeys(context.Context, string) ([]modelgateway.APIKey, error)
	CreateAPIKey(context.Context, modelgateway.CreateAPIKeyInput) (modelgateway.CreatedAPIKey, error)
	RevokeAPIKey(context.Context, modelgateway.RevokeAPIKeyInput) (modelgateway.APIKey, error)
	Prepare(context.Context, string, []byte) (modelgateway.PreparedRequest, error)
	RecordCall(context.Context, modelgateway.RecordCallInput) error
	ListCalls(context.Context, string, int) ([]modelgateway.Call, error)
}

type ModelGatewayForwarder interface {
	Forward(context.Context, modelgateway.Route, []byte, bool) (*http.Response, error)
}

type createGatewayAPIKeyRequest struct {
	Name      string     `json:"name"`
	ExpiresAt *time.Time `json:"expiresAt"`
}

type gatewayAPIKeyResponse struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	KeyPrefix  string     `json:"keyPrefix"`
	Status     string     `json:"status"`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	RevokedAt  *time.Time `json:"revokedAt,omitempty"`
	Secret     string     `json:"secret,omitempty"`
}

type gatewayCallResponse struct {
	ID               string    `json:"id"`
	APIKeyID         string    `json:"apiKeyId"`
	APIKeyName       string    `json:"apiKeyName"`
	RequestID        string    `json:"requestId"`
	ModelCode        string    `json:"modelCode"`
	UpstreamModelID  string    `json:"upstreamModelId"`
	Streaming        bool      `json:"streaming"`
	Result           string    `json:"result"`
	HTTPStatus       int       `json:"httpStatus"`
	LatencyMS        int64     `json:"latencyMs"`
	PromptTokens     int       `json:"promptTokens"`
	CompletionTokens int       `json:"completionTokens"`
	TotalTokens      int       `json:"totalTokens"`
	ErrorCode        string    `json:"errorCode"`
	SourceIP         string    `json:"sourceIp"`
	CreatedAt        time.Time `json:"createdAt"`
}

func registerModelGatewayRoutes(router *gin.Engine, deps Dependencies) {
	admin := router.Group("/api/v1/model-gateway", requireSession(deps))
	admin.GET("/api-keys", requirePermission(deps, "model.gateway.read"), listGatewayAPIKeysHandler(deps))
	admin.POST("/api-keys", requirePermission(deps, "model.gateway.write"), requireCSRF(deps), createGatewayAPIKeyHandler(deps))
	admin.PATCH("/api-keys/:id/revoke", requirePermission(deps, "model.gateway.write"), requireCSRF(deps), revokeGatewayAPIKeyHandler(deps))
	admin.GET("/calls", requirePermission(deps, "model.gateway.read"), listGatewayCallsHandler(deps))

	router.POST("/v1/chat/completions", chatCompletionsGatewayHandler(deps))
}

func listGatewayAPIKeysHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		items, err := deps.ModelGateway.ListAPIKeys(ctx.Request.Context(), currentSession(ctx).User.OrganizationID)
		if err != nil {
			handleModelGatewayAdminError(ctx, deps.Logger, err)
			return
		}
		response := make([]gatewayAPIKeyResponse, 0, len(items))
		for _, item := range items {
			response = append(response, presentGatewayAPIKey(item, ""))
		}
		httpx.OK(ctx, http.StatusOK, response)
	}
}

func createGatewayAPIKeyHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var request createGatewayAPIKeyRequest
		if !bindModelJSON(ctx, &request) {
			return
		}
		session := currentSession(ctx)
		created, err := deps.ModelGateway.CreateAPIKey(ctx.Request.Context(), modelgateway.CreateAPIKeyInput{
			OrganizationID: session.User.OrganizationID, ActorID: session.User.ID, Name: request.Name,
			ExpiresAt: request.ExpiresAt, RequestID: contextString(ctx, "request_id"), SourceIP: ctx.ClientIP(),
		})
		if err != nil {
			handleModelGatewayAdminError(ctx, deps.Logger, err)
			return
		}
		httpx.OK(ctx, http.StatusCreated, presentGatewayAPIKey(created.APIKey, created.Secret))
	}
}

func revokeGatewayAPIKeyHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if !validUUIDParam(ctx, "调用密钥") {
			return
		}
		session := currentSession(ctx)
		item, err := deps.ModelGateway.RevokeAPIKey(ctx.Request.Context(), modelgateway.RevokeAPIKeyInput{
			OrganizationID: session.User.OrganizationID, ActorID: session.User.ID, APIKeyID: ctx.Param("id"),
			RequestID: contextString(ctx, "request_id"), SourceIP: ctx.ClientIP(),
		})
		if err != nil {
			handleModelGatewayAdminError(ctx, deps.Logger, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, presentGatewayAPIKey(item, ""))
	}
}

func listGatewayCallsHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		limit := 100
		if raw := ctx.Query("limit"); raw != "" {
			value, err := strconv.Atoi(raw)
			if err != nil {
				httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", "limit 必须是整数")
				return
			}
			limit = value
		}
		items, err := deps.ModelGateway.ListCalls(ctx.Request.Context(), currentSession(ctx).User.OrganizationID, limit)
		if err != nil {
			handleModelGatewayAdminError(ctx, deps.Logger, err)
			return
		}
		response := make([]gatewayCallResponse, 0, len(items))
		for _, item := range items {
			response = append(response, gatewayCallResponse{
				ID: item.ID, APIKeyID: item.APIKeyID, APIKeyName: item.APIKeyName, RequestID: item.RequestID,
				ModelCode: item.ModelCode, UpstreamModelID: item.UpstreamModelID, Streaming: item.Streaming,
				Result: item.Result, HTTPStatus: item.HTTPStatus, LatencyMS: item.LatencyMS,
				PromptTokens: item.PromptTokens, CompletionTokens: item.CompletionTokens, TotalTokens: item.TotalTokens,
				ErrorCode: item.ErrorCode, SourceIP: item.SourceIP, CreatedAt: item.CreatedAt,
			})
		}
		httpx.OK(ctx, http.StatusOK, response)
	}
}

func chatCompletionsGatewayHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		startedAt := time.Now()
		body, err := io.ReadAll(io.LimitReader(ctx.Request.Body, maxGatewayRequestBody+1))
		if err != nil || len(body) > maxGatewayRequestBody {
			writeGatewayError(ctx, http.StatusBadRequest, "invalid_request", "请求体无效或超过 4 MiB")
			return
		}
		credential := bearerCredential(ctx.GetHeader("Authorization"))
		prepared, err := deps.ModelGateway.Prepare(ctx.Request.Context(), credential, body)
		if err != nil {
			handleGatewayRequestError(ctx, deps.Logger, err)
			return
		}
		upstream, err := deps.GatewayForwarder.Forward(ctx.Request.Context(), prepared.Route, prepared.Body, prepared.Streaming)
		if err != nil {
			status := http.StatusBadGateway
			writeGatewayError(ctx, status, "provider_unreachable", "无法连接模型供应商")
			recordGatewayCall(deps, ctx, prepared, status, time.Since(startedAt), tokenUsage{}, "provider_unreachable")
			return
		}
		defer upstream.Body.Close()
		copyGatewayHeaders(ctx.Writer.Header(), upstream.Header)
		ctx.Header("X-Request-ID", contextString(ctx, "request_id"))

		if prepared.Streaming {
			ctx.Status(upstream.StatusCode)
			copyErr := streamGatewayResponse(ctx, upstream.Body)
			errorCode := upstreamErrorCode(upstream.StatusCode, nil)
			if copyErr != nil {
				errorCode = "stream_interrupted"
			}
			recordGatewayCall(deps, ctx, prepared, upstream.StatusCode, time.Since(startedAt), tokenUsage{}, errorCode)
			return
		}

		responseBody, readErr := io.ReadAll(io.LimitReader(upstream.Body, maxGatewayResponseBody+1))
		if readErr != nil || len(responseBody) > maxGatewayResponseBody {
			writeGatewayError(ctx, http.StatusBadGateway, "invalid_provider_response", "模型供应商响应无效或过大")
			recordGatewayCall(deps, ctx, prepared, http.StatusBadGateway, time.Since(startedAt), tokenUsage{}, "invalid_provider_response")
			return
		}
		ctx.Data(upstream.StatusCode, upstream.Header.Get("Content-Type"), responseBody)
		recordGatewayCall(deps, ctx, prepared, upstream.StatusCode, time.Since(startedAt), parseTokenUsage(responseBody), upstreamErrorCode(upstream.StatusCode, responseBody))
	}
}

func streamGatewayResponse(ctx *gin.Context, source io.Reader) error {
	buffer := make([]byte, 16*1024)
	for {
		read, readErr := source.Read(buffer)
		if read > 0 {
			if _, writeErr := ctx.Writer.Write(buffer[:read]); writeErr != nil {
				return writeErr
			}
			ctx.Writer.Flush()
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

type tokenUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func parseTokenUsage(body []byte) tokenUsage {
	var envelope struct {
		Usage tokenUsage `json:"usage"`
	}
	_ = json.Unmarshal(body, &envelope)
	return envelope.Usage
}

func upstreamErrorCode(status int, body []byte) string {
	if status >= 200 && status < 400 {
		return ""
	}
	var envelope struct {
		Error struct {
			Code string `json:"code"`
			Type string `json:"type"`
		} `json:"error"`
	}
	if len(body) > 0 && json.Unmarshal(body, &envelope) == nil {
		if envelope.Error.Code != "" {
			return truncateGatewayCode(envelope.Error.Code)
		}
		if envelope.Error.Type != "" {
			return truncateGatewayCode(envelope.Error.Type)
		}
	}
	return "provider_http_" + strconv.Itoa(status)
}

func recordGatewayCall(deps Dependencies, ctx *gin.Context, prepared modelgateway.PreparedRequest, status int, latency time.Duration, usage tokenUsage, errorCode string) {
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx.Request.Context()), 3*time.Second)
	defer cancel()
	result := "success"
	if status < 200 || status >= 400 || errorCode != "" {
		result = "failed"
	}
	err := deps.ModelGateway.RecordCall(recordCtx, modelgateway.RecordCallInput{
		OrganizationID: prepared.Caller.OrganizationID, APIKeyID: prepared.Caller.APIKeyID,
		ModelID: prepared.Route.ModelID, ProviderID: prepared.Route.ProviderID,
		RequestID: contextString(ctx, "request_id"), ModelCode: prepared.Route.ModelCode,
		UpstreamModelID: prepared.Route.UpstreamModelID, Streaming: prepared.Streaming,
		Result: result, HTTPStatus: status, LatencyMS: latency.Milliseconds(),
		PromptTokens: usage.PromptTokens, CompletionTokens: usage.CompletionTokens,
		TotalTokens: usage.TotalTokens, ErrorCode: errorCode, SourceIP: ctx.ClientIP(),
	})
	if err != nil {
		deps.Logger.Error("记录模型网关调用失败", "error", err, "request_id", contextString(ctx, "request_id"))
	}
}

func bearerCredential(header string) string {
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
}

func copyGatewayHeaders(target http.Header, source http.Header) {
	for _, key := range []string{"Content-Type", "Cache-Control", "X-Request-ID", "OpenAI-Organization", "OpenAI-Processing-Ms"} {
		if value := source.Get(key); value != "" {
			target.Set(key, value)
		}
	}
}

func presentGatewayAPIKey(item modelgateway.APIKey, secret string) gatewayAPIKeyResponse {
	return gatewayAPIKeyResponse{
		ID: item.ID, Name: item.Name, KeyPrefix: item.KeyPrefix, Status: item.Status,
		ExpiresAt: item.ExpiresAt, LastUsedAt: item.LastUsedAt, CreatedAt: item.CreatedAt,
		RevokedAt: item.RevokedAt, Secret: secret,
	}
}

func handleModelGatewayAdminError(ctx *gin.Context, logger *slog.Logger, err error) {
	switch {
	case errors.Is(err, modelgateway.ErrInvalidInput):
		httpx.Fail(ctx, http.StatusBadRequest, "invalid_gateway_configuration", "模型网关参数不正确")
	case errors.Is(err, modelgateway.ErrAPIKeyNotFound):
		httpx.Fail(ctx, http.StatusNotFound, "gateway_api_key_not_found", "模型网关调用密钥不存在")
	default:
		logger.Error("模型网关管理接口处理失败", "error", err, "request_id", contextString(ctx, "request_id"))
		httpx.Fail(ctx, http.StatusInternalServerError, "internal_error", "服务暂时不可用")
	}
}

func handleGatewayRequestError(ctx *gin.Context, logger *slog.Logger, err error) {
	switch {
	case errors.Is(err, modelgateway.ErrInvalidCredential), errors.Is(err, modelgateway.ErrAPIKeyRevoked), errors.Is(err, modelgateway.ErrAPIKeyExpired):
		writeGatewayError(ctx, http.StatusUnauthorized, "invalid_api_key", err.Error())
	case errors.Is(err, modelgateway.ErrInvalidInput):
		writeGatewayError(ctx, http.StatusBadRequest, "invalid_request", "请求参数格式不正确，必须提供模型编码")
	case errors.Is(err, modelgateway.ErrModelNotFound):
		writeGatewayError(ctx, http.StatusNotFound, "model_not_found", "请求的模型编码不存在")
	case errors.Is(err, modelgateway.ErrModelDisabled), errors.Is(err, modelgateway.ErrProviderDisabled):
		writeGatewayError(ctx, http.StatusConflict, "model_unavailable", err.Error())
	default:
		logger.Error("模型网关请求处理失败", "error", err, "request_id", contextString(ctx, "request_id"))
		writeGatewayError(ctx, http.StatusInternalServerError, "internal_error", "模型网关暂时不可用")
	}
}

func writeGatewayError(ctx *gin.Context, status int, code, message string) {
	errorType := "invalid_request_error"
	if status == http.StatusUnauthorized {
		errorType = "authentication_error"
	} else if status >= 500 {
		errorType = "api_error"
	}
	ctx.AbortWithStatusJSON(status, gin.H{"error": gin.H{
		"message": message, "type": errorType, "param": nil, "code": code,
	}})
}

func truncateGatewayCode(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 80 {
		return value[:80]
	}
	return value
}
