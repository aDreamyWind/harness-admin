package httpserver

import (
	"bufio"
	"bytes"
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
	"github.com/google/uuid"

	"harness-admin/internal/httpx"
	"harness-admin/internal/modelgateway"
	"harness-admin/internal/modelprovider"
	oidcmodule "harness-admin/internal/oidc"
)

const maxGatewayRequestBody = 4 * 1024 * 1024
const maxMediaGatewayRequestBody = 20 * 1024 * 1024
const maxGatewayResponseBody = 32 * 1024 * 1024

type ModelGatewayService interface {
	ListAPIKeys(context.Context, string) ([]modelgateway.APIKey, error)
	CreateAPIKey(context.Context, modelgateway.CreateAPIKeyInput) (modelgateway.CreatedAPIKey, error)
	UpdateAPIKey(context.Context, modelgateway.UpdateAPIKeyInput) (modelgateway.APIKey, error)
	RevokeAPIKey(context.Context, modelgateway.RevokeAPIKeyInput) (modelgateway.APIKey, error)
	RevealAPIKeySecret(context.Context, modelgateway.RevealAPIKeySecretInput) (string, error)
	Prepare(context.Context, string, []byte) (modelgateway.PreparedRequest, error)
	PrepareForUser(context.Context, string, string, []byte) (modelgateway.PreparedRequest, error)
	PrepareOperation(context.Context, string, []byte, string) (modelgateway.PreparedRequest, error)
	PrepareForUserOperation(context.Context, string, string, []byte, string) (modelgateway.PreparedRequest, error)
	SaveVideoTask(context.Context, modelgateway.SaveVideoTaskInput) (modelgateway.VideoTask, error)
	FindVideoTaskForAPIKey(context.Context, string, string) (modelgateway.VideoTask, error)
	FindVideoTaskForUser(context.Context, string, string, string) (modelgateway.VideoTask, error)
	RecordCall(context.Context, modelgateway.RecordCallInput) error
	ListCalls(context.Context, modelgateway.CallQuery) (modelgateway.CallPage, error)
	ListUsage(context.Context, modelgateway.UsageQuery) (modelgateway.UsagePage, error)
	ListAlerts(context.Context, modelgateway.AlertQuery) (modelgateway.AlertPage, error)
	AcknowledgeAlert(context.Context, modelgateway.AcknowledgeAlertInput) (modelgateway.Alert, error)
	GetAlertSettings(context.Context, string) (modelgateway.AlertSettings, error)
	UpdateAlertSettings(context.Context, modelgateway.UpdateAlertSettingsInput) (modelgateway.AlertSettings, error)
	ListModelsForAPIKey(context.Context, string) ([]modelgateway.AvailableModel, error)
	ListModelsForUser(context.Context, string, string) ([]modelgateway.AvailableModel, error)
}

type ModelGatewayForwarder interface {
	Forward(context.Context, modelgateway.Route, []byte, bool) (*http.Response, error)
	QueryVideoTask(context.Context, modelgateway.Route, string) (*http.Response, error)
}

type createGatewayAPIKeyRequest struct {
	Name              string     `json:"name"`
	ExpiresAt         *time.Time `json:"expiresAt"`
	RequestsPerMinute *int       `json:"requestsPerMinute"`
	MonthlyTokenLimit *int64     `json:"monthlyTokenLimit"`
	AllowedModelCodes []string   `json:"allowedModelCodes"`
}

type updateGatewayAlertSettingsRequest struct {
	AutoRefreshEnabled *bool `json:"autoRefreshEnabled"`
}

type gatewayAPIKeyResponse struct {
	ID                string                      `json:"id"`
	Name              string                      `json:"name"`
	KeyPrefix         string                      `json:"keyPrefix"`
	Status            string                      `json:"status"`
	ExpiresAt         *time.Time                  `json:"expiresAt,omitempty"`
	LastUsedAt        *time.Time                  `json:"lastUsedAt,omitempty"`
	RequestsPerMinute int                         `json:"requestsPerMinute"`
	MonthlyTokenLimit int64                       `json:"monthlyTokenLimit"`
	MonthlyTokenUsed  int64                       `json:"monthlyTokenUsed"`
	AllowedModelCodes []string                    `json:"allowedModelCodes"`
	AllowedModels     []modelgateway.AllowedModel `json:"allowedModels"`
	CreatedAt         time.Time                   `json:"createdAt"`
	RevokedAt         *time.Time                  `json:"revokedAt,omitempty"`
	Secret            string                      `json:"secret,omitempty"`
}

type gatewayCallPageResponse struct {
	Items    []gatewayCallResponse `json:"items"`
	Total    int64                 `json:"total"`
	Page     int                   `json:"page"`
	PageSize int                   `json:"pageSize"`
}

type gatewayCallResponse struct {
	ID               string    `json:"id"`
	APIKeyID         string    `json:"apiKeyId"`
	APIKeyName       string    `json:"apiKeyName"`
	AuthType         string    `json:"authType"`
	UserID           string    `json:"userId,omitempty"`
	UserName         string    `json:"userName,omitempty"`
	DepartmentID     string    `json:"departmentId,omitempty"`
	RequestID        string    `json:"requestId"`
	ModelCode        string    `json:"modelCode"`
	ModelName        string    `json:"modelName"`
	ModelType        string    `json:"modelType"`
	UpstreamModelID  string    `json:"upstreamModelId"`
	ProviderName     string    `json:"providerName"`
	Streaming        bool      `json:"streaming"`
	Result           string    `json:"result"`
	HTTPStatus       int       `json:"httpStatus"`
	LatencyMS        int64     `json:"latencyMs"`
	PromptTokens     int       `json:"promptTokens"`
	CompletionTokens int       `json:"completionTokens"`
	TotalTokens      int       `json:"totalTokens"`
	UsageReported    bool      `json:"usageReported"`
	QuotaExceeded    bool      `json:"quotaExceeded"`
	ErrorCode        string    `json:"errorCode"`
	SourceIP         string    `json:"sourceIp"`
	CreatedAt        time.Time `json:"createdAt"`
}

func registerModelGatewayRoutes(router *gin.Engine, deps Dependencies) {
	admin := router.Group("/api/v1/model-gateway", requireSession(deps))
	admin.GET("/api-keys", requirePermission(deps, "model.gateway.read"), listGatewayAPIKeysHandler(deps))
	admin.POST("/api-keys", requirePermission(deps, "model.gateway.write"), requireCSRF(deps), createGatewayAPIKeyHandler(deps))
	admin.PUT("/api-keys/:id", requirePermission(deps, "model.gateway.write"), requireCSRF(deps), updateGatewayAPIKeyHandler(deps))
	admin.PATCH("/api-keys/:id/revoke", requirePermission(deps, "model.gateway.write"), requireCSRF(deps), revokeGatewayAPIKeyHandler(deps))
	admin.GET("/api-keys/:id/secret", requirePermission(deps, "model.gateway.write"), revealGatewayAPIKeySecretHandler(deps))
	admin.GET("/calls", requirePermission(deps, "model.gateway.read"), listGatewayCallsHandler(deps))
	admin.GET("/usage", requirePermission(deps, "model.gateway.read"), listGatewayUsageHandler(deps))
	admin.GET("/alerts", requirePermission(deps, "model.gateway.read"), listGatewayAlertsHandler(deps))
	admin.GET("/alerts/settings", requirePermission(deps, "model.gateway.read"), getGatewayAlertSettingsHandler(deps))
	admin.PUT("/alerts/settings", requirePermission(deps, "model.gateway.write"), requireCSRF(deps), updateGatewayAlertSettingsHandler(deps))
	admin.POST("/alerts/:id/acknowledge", requirePermission(deps, "model.gateway.write"), requireCSRF(deps), acknowledgeGatewayAlertHandler(deps))

	router.POST("/v1/chat/completions", chatCompletionsGatewayHandler(deps))
	router.POST("/v1/images/generations", mediaGenerationGatewayHandler(deps, "image"))
	router.POST("/v1/videos/generations", mediaGenerationGatewayHandler(deps, "video"))
	router.GET("/v1/videos/tasks/:id", videoTaskGatewayHandler(deps))
	router.GET("/v1/models", listGatewayModelsHandler(deps))
}

func listGatewayModelsHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		credential := bearerCredential(ctx.GetHeader("Authorization"))
		var items []modelgateway.AvailableModel
		var err error
		if strings.HasPrefix(credential, "harness_sk_") {
			items, err = deps.ModelGateway.ListModelsForAPIKey(ctx.Request.Context(), credential)
		} else if deps.OIDCProvider != nil {
			var subject oidcmodule.Subject
			subject, err = deps.OIDCProvider.AuthenticateAccessToken(ctx.Request.Context(), credential, "model.invoke")
			if err == nil {
				items, err = deps.ModelGateway.ListModelsForUser(ctx.Request.Context(), subject.OrganizationID, subject.ID)
			} else {
				err = modelgateway.ErrInvalidCredential
			}
		} else {
			err = modelgateway.ErrInvalidCredential
		}
		if err != nil {
			handleGatewayRequestError(ctx, deps.Logger, err)
			return
		}
		data := make([]gin.H, 0, len(items))
		for _, item := range items {
			data = append(data, gin.H{
				"id": item.Code, "object": "model", "created": item.CreatedAt.Unix(), "owned_by": "harness-admin",
				"name": item.Name, "model_type": item.ModelType, "capabilities": item.Capabilities,
			})
		}
		ctx.Header("Cache-Control", "no-store")
		ctx.JSON(http.StatusOK, gin.H{"object": "list", "data": data})
	}
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
		requestsPerMinute := 60
		if request.RequestsPerMinute != nil {
			requestsPerMinute = *request.RequestsPerMinute
		}
		monthlyTokenLimit := int64(0)
		if request.MonthlyTokenLimit != nil {
			monthlyTokenLimit = *request.MonthlyTokenLimit
		}
		created, err := deps.ModelGateway.CreateAPIKey(ctx.Request.Context(), modelgateway.CreateAPIKeyInput{
			OrganizationID: session.User.OrganizationID, ActorID: session.User.ID, Name: request.Name,
			ExpiresAt: request.ExpiresAt, RequestsPerMinute: requestsPerMinute,
			MonthlyTokenLimit: monthlyTokenLimit, AllowedModelCodes: request.AllowedModelCodes,
			RequestID: contextString(ctx, "request_id"), SourceIP: ctx.ClientIP(),
		})
		if err != nil {
			handleModelGatewayAdminError(ctx, deps.Logger, err)
			return
		}
		httpx.OK(ctx, http.StatusCreated, presentGatewayAPIKey(created.APIKey, created.Secret))
	}
}

func updateGatewayAPIKeyHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if !validUUIDParam(ctx, "调用密钥") {
			return
		}
		var request createGatewayAPIKeyRequest
		if !bindModelJSON(ctx, &request) {
			return
		}
		if request.RequestsPerMinute == nil || request.MonthlyTokenLimit == nil {
			httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", "修改调用密钥时必须提供限流和额度设置")
			return
		}
		session := currentSession(ctx)
		item, err := deps.ModelGateway.UpdateAPIKey(ctx.Request.Context(), modelgateway.UpdateAPIKeyInput{
			OrganizationID: session.User.OrganizationID, ActorID: session.User.ID, APIKeyID: ctx.Param("id"),
			Name: request.Name, ExpiresAt: request.ExpiresAt, RequestsPerMinute: *request.RequestsPerMinute,
			MonthlyTokenLimit: *request.MonthlyTokenLimit, AllowedModelCodes: request.AllowedModelCodes,
			RequestID: contextString(ctx, "request_id"), SourceIP: ctx.ClientIP(),
		})
		if err != nil {
			handleModelGatewayAdminError(ctx, deps.Logger, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, presentGatewayAPIKey(item, ""))
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

func revealGatewayAPIKeySecretHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if !validUUIDParam(ctx, "调用密钥") {
			return
		}
		session := currentSession(ctx)
		value, err := deps.ModelGateway.RevealAPIKeySecret(ctx.Request.Context(), modelgateway.RevealAPIKeySecretInput{
			OrganizationID: session.User.OrganizationID, ActorID: session.User.ID, APIKeyID: ctx.Param("id"),
			RequestID: contextString(ctx, "request_id"), SourceIP: ctx.ClientIP(),
		})
		if err != nil {
			handleModelGatewayAdminError(ctx, deps.Logger, err)
			return
		}
		ctx.Header("Cache-Control", "no-store")
		ctx.Header("Pragma", "no-cache")
		httpx.OK(ctx, http.StatusOK, gin.H{"secret": value})
	}
}

func listGatewayCallsHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		page, ok := positiveQueryInt(ctx, "page", 1)
		if !ok {
			return
		}
		pageSize, ok := positiveQueryInt(ctx, "pageSize", 20)
		if !ok {
			return
		}
		startedAt, ok := optionalQueryTime(ctx, "startedAt")
		if !ok {
			return
		}
		endedAt, ok := optionalQueryTime(ctx, "endedAt")
		if !ok {
			return
		}
		apiKeyID := strings.TrimSpace(ctx.Query("apiKeyId"))
		if apiKeyID != "" {
			if _, err := uuid.Parse(apiKeyID); err != nil {
				httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", "调用密钥标识格式不正确")
				return
			}
		}
		result, err := deps.ModelGateway.ListCalls(ctx.Request.Context(), modelgateway.CallQuery{
			OrganizationID: currentSession(ctx).User.OrganizationID, APIKeyID: apiKeyID,
			ModelCode: ctx.Query("modelCode"), Result: ctx.Query("result"), StartedAt: startedAt, EndedAt: endedAt,
			Page: page, PageSize: pageSize,
		})
		if err != nil {
			handleModelGatewayAdminError(ctx, deps.Logger, err)
			return
		}
		response := make([]gatewayCallResponse, 0, len(result.Items))
		for _, item := range result.Items {
			response = append(response, gatewayCallResponse{
				ID: item.ID, APIKeyID: item.APIKeyID, APIKeyName: item.APIKeyName,
				AuthType: item.AuthType, UserID: item.UserID, UserName: item.UserName, DepartmentID: item.DepartmentID, RequestID: item.RequestID,
				ModelCode: item.ModelCode, ModelName: item.ModelName, ModelType: item.ModelType,
				UpstreamModelID: item.UpstreamModelID, ProviderName: item.ProviderName, Streaming: item.Streaming,
				Result: item.Result, HTTPStatus: item.HTTPStatus, LatencyMS: item.LatencyMS,
				PromptTokens: item.PromptTokens, CompletionTokens: item.CompletionTokens, TotalTokens: item.TotalTokens,
				UsageReported: item.UsageReported, QuotaExceeded: item.QuotaExceeded,
				ErrorCode: item.ErrorCode, SourceIP: item.SourceIP, CreatedAt: item.CreatedAt,
			})
		}
		httpx.OK(ctx, http.StatusOK, gatewayCallPageResponse{Items: response, Total: result.Total, Page: result.Page, PageSize: result.PageSize})
	}
}

func listGatewayUsageHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		page, ok := positiveQueryInt(ctx, "page", 1)
		if !ok {
			return
		}
		pageSize, ok := positiveQueryInt(ctx, "pageSize", 20)
		if !ok {
			return
		}
		result, err := deps.ModelGateway.ListUsage(ctx.Request.Context(), modelgateway.UsageQuery{
			OrganizationID: currentSession(ctx).User.OrganizationID,
			Month:          ctx.Query("month"), GroupBy: ctx.Query("groupBy"), Page: page, PageSize: pageSize,
		})
		if err != nil {
			handleModelGatewayAdminError(ctx, deps.Logger, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, result)
	}
}

func listGatewayAlertsHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		page, ok := positiveQueryInt(ctx, "page", 1)
		if !ok {
			return
		}
		pageSize, ok := positiveQueryInt(ctx, "pageSize", 20)
		if !ok {
			return
		}
		result, err := deps.ModelGateway.ListAlerts(ctx.Request.Context(), modelgateway.AlertQuery{
			OrganizationID: currentSession(ctx).User.OrganizationID,
			Month:          ctx.Query("month"), Status: ctx.Query("status"), Page: page, PageSize: pageSize,
		})
		if err != nil {
			handleModelGatewayAdminError(ctx, deps.Logger, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, result)
	}
}

func acknowledgeGatewayAlertHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if _, err := uuid.Parse(ctx.Param("id")); err != nil {
			httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", "预警标识格式不正确")
			return
		}
		session := currentSession(ctx)
		item, err := deps.ModelGateway.AcknowledgeAlert(ctx.Request.Context(), modelgateway.AcknowledgeAlertInput{
			OrganizationID: session.User.OrganizationID, ActorID: session.User.ID, AlertID: ctx.Param("id"),
			RequestID: contextString(ctx, "request_id"), SourceIP: ctx.ClientIP(),
		})
		if err != nil {
			handleModelGatewayAdminError(ctx, deps.Logger, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, item)
	}
}

func getGatewayAlertSettingsHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		settings, err := deps.ModelGateway.GetAlertSettings(ctx.Request.Context(), currentSession(ctx).User.OrganizationID)
		if err != nil {
			handleModelGatewayAdminError(ctx, deps.Logger, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, settings)
	}
}

func updateGatewayAlertSettingsHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var request updateGatewayAlertSettingsRequest
		if !bindModelJSON(ctx, &request) {
			return
		}
		if request.AutoRefreshEnabled == nil {
			httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", "必须提供后台自动刷新开关")
			return
		}
		session := currentSession(ctx)
		settings, err := deps.ModelGateway.UpdateAlertSettings(ctx.Request.Context(), modelgateway.UpdateAlertSettingsInput{
			OrganizationID: session.User.OrganizationID, ActorID: session.User.ID,
			AutoRefreshEnabled: *request.AutoRefreshEnabled,
			RequestID:          contextString(ctx, "request_id"), SourceIP: ctx.ClientIP(),
		})
		if err != nil {
			handleModelGatewayAdminError(ctx, deps.Logger, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, settings)
	}
}

func chatCompletionsGatewayHandler(deps Dependencies) gin.HandlerFunc {
	return mediaGenerationGatewayHandler(deps, "chat")
}

func mediaGenerationGatewayHandler(deps Dependencies, operation string) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		startedAt := time.Now()
		limit := maxGatewayRequestBody
		if operation != "chat" {
			limit = maxMediaGatewayRequestBody
		}
		body, err := io.ReadAll(io.LimitReader(ctx.Request.Body, int64(limit+1)))
		if err != nil || len(body) > limit {
			writeGatewayError(ctx, http.StatusBadRequest, "invalid_request", "请求体无效或超过大小限制")
			return
		}
		credential := bearerCredential(ctx.GetHeader("Authorization"))
		var prepared modelgateway.PreparedRequest
		if strings.HasPrefix(credential, "harness_sk_") {
			if operation == "chat" {
				prepared, err = deps.ModelGateway.Prepare(ctx.Request.Context(), credential, body)
			} else {
				prepared, err = deps.ModelGateway.PrepareOperation(ctx.Request.Context(), credential, body, operation)
			}
		} else if deps.OIDCProvider != nil {
			var subject oidcmodule.Subject
			subject, err = deps.OIDCProvider.AuthenticateAccessToken(ctx.Request.Context(), credential, "model.invoke")
			if err == nil {
				if operation == "chat" {
					prepared, err = deps.ModelGateway.PrepareForUser(ctx.Request.Context(), subject.OrganizationID, subject.ID, body)
				} else {
					prepared, err = deps.ModelGateway.PrepareForUserOperation(ctx.Request.Context(), subject.OrganizationID, subject.ID, body, operation)
				}
			} else {
				err = modelgateway.ErrInvalidCredential
			}
		} else {
			err = modelgateway.ErrInvalidCredential
		}
		if err != nil {
			handleGatewayRequestError(ctx, deps.Logger, err)
			return
		}
		upstream, err := deps.GatewayForwarder.Forward(ctx.Request.Context(), prepared.Route, prepared.Body, prepared.Streaming)
		if err != nil {
			status := http.StatusBadGateway
			code, message := "provider_unreachable", "无法连接模型供应商"
			switch {
			case errors.Is(err, modelprovider.ErrInvalidImageRequest):
				status, code, message = http.StatusBadRequest, "invalid_request", "图片请求参数无效"
			case errors.Is(err, modelprovider.ErrInvalidProviderRoute):
				code, message = "invalid_provider_route", "模型供应商路由配置无效"
			case errors.Is(err, modelprovider.ErrInvalidImageResponse):
				code, message = "invalid_provider_response", "模型供应商图片响应无效"
			}
			deps.Logger.Error("模型供应商调用失败", "error", err, "request_id", contextString(ctx, "request_id"),
				"provider_id", prepared.Route.ProviderID, "model_id", prepared.Route.ModelID)
			writeGatewayError(ctx, status, code, message)
			recordGatewayCall(deps, ctx, prepared, status, time.Since(startedAt), tokenUsage{}, code)
			return
		}
		defer upstream.Body.Close()
		copyGatewayHeaders(ctx.Writer.Header(), upstream.Header)
		ctx.Header("X-Request-ID", contextString(ctx, "request_id"))

		if prepared.Streaming {
			ctx.Status(upstream.StatusCode)
			usage, copyErr := streamGatewayResponse(ctx, upstream.Body)
			errorCode := upstreamErrorCode(upstream.StatusCode, nil)
			if copyErr != nil {
				errorCode = "stream_interrupted"
			}
			recordGatewayCall(deps, ctx, prepared, upstream.StatusCode, time.Since(startedAt), usage, errorCode)
			return
		}

		responseBody, readErr := io.ReadAll(io.LimitReader(upstream.Body, maxGatewayResponseBody+1))
		if readErr != nil || len(responseBody) > maxGatewayResponseBody {
			writeGatewayError(ctx, http.StatusBadGateway, "invalid_provider_response", "模型供应商响应无效或过大")
			recordGatewayCall(deps, ctx, prepared, http.StatusBadGateway, time.Since(startedAt), tokenUsage{}, "invalid_provider_response")
			return
		}
		contentType := upstream.Header.Get("Content-Type")
		if operation != "chat" && upstream.StatusCode >= 400 {
			if providerError, ok := dashScopeEventError(responseBody); ok {
				responseBody = providerError
				contentType = "application/json"
			}
		}
		if operation == "video" && upstream.StatusCode >= 200 && upstream.StatusCode < 300 {
			upstreamTaskID := videoTaskID(responseBody)
			if upstreamTaskID == "" {
				writeGatewayError(ctx, http.StatusBadGateway, "invalid_provider_response", "视频供应商未返回任务标识")
				recordGatewayCall(deps, ctx, prepared, http.StatusBadGateway, time.Since(startedAt), tokenUsage{}, "invalid_provider_response")
				return
			}
			task, saveErr := deps.ModelGateway.SaveVideoTask(ctx.Request.Context(), modelgateway.SaveVideoTaskInput{
				Caller: prepared.Caller, Route: prepared.Route, UpstreamTaskID: upstreamTaskID,
				RequestID: contextString(ctx, "request_id"), SourceIP: ctx.ClientIP(),
			})
			if saveErr != nil {
				deps.Logger.Error("保存视频任务失败", "error", saveErr, "request_id", contextString(ctx, "request_id"))
				writeGatewayError(ctx, http.StatusBadGateway, "video_task_unavailable", "视频任务已提交，但无法保存查询标识")
				recordGatewayCall(deps, ctx, prepared, http.StatusBadGateway, time.Since(startedAt), tokenUsage{}, "video_task_unavailable")
				return
			}
			responseBody, err = replaceVideoTaskID(responseBody, task.ID)
			if err != nil {
				writeGatewayError(ctx, http.StatusBadGateway, "invalid_provider_response", "视频供应商响应无效")
				return
			}
		}
		ctx.Header("Content-Type", contentType)
		ctx.Data(upstream.StatusCode, contentType, responseBody)
		recordGatewayCall(deps, ctx, prepared, upstream.StatusCode, time.Since(startedAt), parseTokenUsage(responseBody), upstreamErrorCode(upstream.StatusCode, responseBody))
	}
}

func videoTaskGatewayHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		credential := bearerCredential(ctx.GetHeader("Authorization"))
		var task modelgateway.VideoTask
		var err error
		if strings.HasPrefix(credential, "harness_sk_") {
			task, err = deps.ModelGateway.FindVideoTaskForAPIKey(ctx.Request.Context(), credential, ctx.Param("id"))
		} else if deps.OIDCProvider != nil {
			var subject oidcmodule.Subject
			subject, err = deps.OIDCProvider.AuthenticateAccessToken(ctx.Request.Context(), credential, "model.invoke")
			if err == nil {
				task, err = deps.ModelGateway.FindVideoTaskForUser(ctx.Request.Context(), subject.OrganizationID, subject.ID, ctx.Param("id"))
			} else {
				err = modelgateway.ErrInvalidCredential
			}
		} else {
			err = modelgateway.ErrInvalidCredential
		}
		if err != nil {
			handleGatewayRequestError(ctx, deps.Logger, err)
			return
		}
		upstream, err := deps.GatewayForwarder.QueryVideoTask(ctx.Request.Context(), task.Route, task.UpstreamTaskID)
		if err != nil {
			writeGatewayError(ctx, http.StatusBadGateway, "provider_unreachable", "无法查询视频供应商任务")
			return
		}
		defer upstream.Body.Close()
		responseBody, readErr := io.ReadAll(io.LimitReader(upstream.Body, maxGatewayResponseBody+1))
		if readErr != nil || len(responseBody) > maxGatewayResponseBody {
			writeGatewayError(ctx, http.StatusBadGateway, "invalid_provider_response", "视频供应商响应无效或过大")
			return
		}
		contentType := upstream.Header.Get("Content-Type")
		if upstream.StatusCode >= 400 {
			if providerError, ok := dashScopeEventError(responseBody); ok {
				responseBody = providerError
				contentType = "application/json"
			}
		}
		if upstream.StatusCode >= 200 && upstream.StatusCode < 300 {
			responseBody, err = replaceVideoTaskID(responseBody, task.ID)
			if err != nil {
				writeGatewayError(ctx, http.StatusBadGateway, "invalid_provider_response", "视频供应商响应无效")
				return
			}
		}
		copyGatewayHeaders(ctx.Writer.Header(), upstream.Header)
		ctx.Header("X-Request-ID", contextString(ctx, "request_id"))
		ctx.Header("Content-Type", contentType)
		ctx.Data(upstream.StatusCode, contentType, responseBody)
	}
}

func videoTaskID(body []byte) string {
	var response struct {
		Output struct {
			TaskID string `json:"task_id"`
		} `json:"output"`
	}
	if json.Unmarshal(body, &response) != nil {
		return ""
	}
	return strings.TrimSpace(response.Output.TaskID)
}

func replaceVideoTaskID(body []byte, taskID string) ([]byte, error) {
	var response map[string]json.RawMessage
	if err := json.Unmarshal(body, &response); err != nil || response == nil {
		return nil, errors.New("视频任务响应不是对象")
	}
	var output map[string]json.RawMessage
	if err := json.Unmarshal(response["output"], &output); err != nil || output == nil {
		return nil, errors.New("视频任务输出不是对象")
	}
	encodedID, _ := json.Marshal(taskID)
	output["task_id"] = encodedID
	encodedOutput, err := json.Marshal(output)
	if err != nil {
		return nil, err
	}
	response["output"] = encodedOutput
	return json.Marshal(response)
}

func streamGatewayResponse(ctx *gin.Context, source io.Reader) (tokenUsage, error) {
	var usage tokenUsage
	scanner := bufio.NewScanner(source)
	scanner.Buffer(make([]byte, 16*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if _, err := io.WriteString(ctx.Writer, line+"\n"); err != nil {
			return usage, err
		}
		ctx.Writer.Flush()
		if parsed, ok := parseSSETokenUsage(line); ok {
			usage = parsed
		}
	}
	return usage, scanner.Err()
}

func parseSSETokenUsage(line string) (tokenUsage, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "data:") {
		return tokenUsage{}, false
	}
	payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	if payload == "" || payload == "[DONE]" {
		return tokenUsage{}, false
	}
	usage := parseTokenUsage([]byte(payload))
	return usage, usage.Reported
}

type tokenUsage struct {
	PromptTokens     int  `json:"prompt_tokens"`
	CompletionTokens int  `json:"completion_tokens"`
	TotalTokens      int  `json:"total_tokens"`
	Reported         bool `json:"-"`
}

func parseTokenUsage(body []byte) tokenUsage {
	var envelope struct {
		Usage json.RawMessage `json:"usage"`
	}
	if json.Unmarshal(body, &envelope) != nil || len(envelope.Usage) == 0 || string(envelope.Usage) == "null" {
		return tokenUsage{}
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(envelope.Usage, &fields) != nil || len(fields) == 0 {
		return tokenUsage{}
	}
	reported := false
	for _, key := range []string{"total_tokens", "prompt_tokens", "completion_tokens"} {
		if raw, exists := fields[key]; exists && string(raw) != "null" {
			var count int
			if json.Unmarshal(raw, &count) != nil || count < 0 {
				return tokenUsage{}
			}
			reported = true
		}
	}
	if !reported {
		return tokenUsage{}
	}
	var usage tokenUsage
	if json.Unmarshal(envelope.Usage, &usage) != nil || usage.PromptTokens < 0 || usage.CompletionTokens < 0 || usage.TotalTokens < 0 {
		return tokenUsage{}
	}
	if _, hasTotal := fields["total_tokens"]; !hasTotal {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
	usage.Reported = true
	return usage
}

func upstreamErrorCode(status int, body []byte) string {
	if status >= 200 && status < 400 {
		return ""
	}
	var envelope struct {
		Code  string `json:"code"`
		Error struct {
			Code string `json:"code"`
			Type string `json:"type"`
		} `json:"error"`
	}
	if len(body) > 0 && json.Unmarshal(body, &envelope) == nil {
		if envelope.Code != "" {
			return truncateGatewayCode(envelope.Code)
		}
		if envelope.Error.Code != "" {
			return truncateGatewayCode(envelope.Error.Code)
		}
		if envelope.Error.Type != "" {
			return truncateGatewayCode(envelope.Error.Type)
		}
	}
	return "provider_http_" + strconv.Itoa(status)
}

func dashScopeEventError(body []byte) ([]byte, bool) {
	if !bytes.Contains(body, []byte("event:error")) {
		return nil, false
	}
	for _, line := range bytes.Split(body, []byte{'\n'}) {
		line = bytes.TrimSpace(line)
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		payload := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		var providerError struct {
			Code string `json:"code"`
		}
		if json.Unmarshal(payload, &providerError) == nil && providerError.Code != "" {
			return append([]byte(nil), payload...), true
		}
	}
	return nil, false
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
		UserID: prepared.Caller.UserID, AuthType: prepared.Caller.AuthType,
		DepartmentID: prepared.Caller.DepartmentID,
		ModelID:      prepared.Route.ModelID, ProviderID: prepared.Route.ProviderID,
		RequestID: contextString(ctx, "request_id"), ModelCode: prepared.Route.ModelCode, ModelType: prepared.Route.ModelType,
		UpstreamModelID: prepared.Route.UpstreamModelID, Streaming: prepared.Streaming,
		Result: result, HTTPStatus: status, LatencyMS: latency.Milliseconds(),
		PromptTokens: usage.PromptTokens, CompletionTokens: usage.CompletionTokens,
		TotalTokens: usage.TotalTokens, UsageReported: usage.Reported,
		ErrorCode: errorCode, SourceIP: ctx.ClientIP(),
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
		ExpiresAt: item.ExpiresAt, LastUsedAt: item.LastUsedAt,
		RequestsPerMinute: item.RequestsPerMinute, MonthlyTokenLimit: item.MonthlyTokenLimit,
		MonthlyTokenUsed: item.MonthlyTokenUsed, AllowedModelCodes: item.AllowedModelCodes,
		AllowedModels: item.AllowedModels,
		CreatedAt:     item.CreatedAt,
		RevokedAt:     item.RevokedAt, Secret: secret,
	}
}

func positiveQueryInt(ctx *gin.Context, name string, fallback int) (int, bool) {
	raw := strings.TrimSpace(ctx.Query(name))
	if raw == "" {
		return fallback, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", name+" 必须是正整数")
		return 0, false
	}
	return value, true
}

func optionalQueryTime(ctx *gin.Context, name string) (*time.Time, bool) {
	raw := strings.TrimSpace(ctx.Query(name))
	if raw == "" {
		return nil, true
	}
	value, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", name+" 必须是 RFC 3339 时间")
		return nil, false
	}
	return &value, true
}

func handleModelGatewayAdminError(ctx *gin.Context, logger *slog.Logger, err error) {
	switch {
	case errors.Is(err, modelgateway.ErrInvalidInput):
		httpx.Fail(ctx, http.StatusBadRequest, "invalid_gateway_configuration", "模型网关参数不正确")
	case errors.Is(err, modelgateway.ErrAPIKeyNotFound):
		httpx.Fail(ctx, http.StatusNotFound, "gateway_api_key_not_found", "模型网关调用密钥不存在")
	case errors.Is(err, modelgateway.ErrAlertNotFound):
		httpx.Fail(ctx, http.StatusNotFound, "gateway_alert_not_found", "预警不存在或已解除")
	case errors.Is(err, modelgateway.ErrAPIKeySecretUnavailable):
		httpx.Fail(ctx, http.StatusConflict, "gateway_api_key_secret_unavailable", "该密钥创建于支持再次查看之前，请重新创建调用密钥")
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
	case errors.Is(err, modelgateway.ErrModelNotAllowed):
		writeGatewayError(ctx, http.StatusForbidden, "model_not_allowed", err.Error())
	case errors.Is(err, modelgateway.ErrModelTypeMismatch):
		writeGatewayError(ctx, http.StatusBadRequest, "model_type_mismatch", err.Error())
	case errors.Is(err, modelgateway.ErrVideoTaskNotFound):
		writeGatewayError(ctx, http.StatusNotFound, "video_task_not_found", err.Error())
	case errors.Is(err, modelgateway.ErrRateLimited):
		ctx.Header("Retry-After", "60")
		writeGatewayError(ctx, http.StatusTooManyRequests, "rate_limit_exceeded", err.Error())
	case errors.Is(err, modelgateway.ErrMonthlyQuota):
		writeGatewayError(ctx, http.StatusTooManyRequests, "insufficient_quota", err.Error())
	default:
		logger.Error("模型网关请求处理失败", "error", err, "request_id", contextString(ctx, "request_id"))
		writeGatewayError(ctx, http.StatusInternalServerError, "internal_error", "模型网关暂时不可用")
	}
}

func writeGatewayError(ctx *gin.Context, status int, code, message string) {
	errorType := "invalid_request_error"
	if status == http.StatusUnauthorized {
		errorType = "authentication_error"
	} else if status == http.StatusTooManyRequests {
		errorType = "rate_limit_error"
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
