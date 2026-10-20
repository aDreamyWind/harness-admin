package httpserver

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"harness-admin/internal/httpx"
	"harness-admin/internal/modeladmin"
)

type ModelAdminService interface {
	ListProviders(context.Context, string) ([]modeladmin.Provider, error)
	CreateProvider(context.Context, modeladmin.CreateProviderInput) (modeladmin.Provider, error)
	UpdateProvider(context.Context, modeladmin.UpdateProviderInput) (modeladmin.Provider, error)
	ChangeProviderStatus(context.Context, modeladmin.ChangeProviderStatusInput) (modeladmin.Provider, error)
	ProbeProvider(context.Context, modeladmin.ProbeInput) (modeladmin.ProbeResult, error)
	RevealProviderCredential(context.Context, modeladmin.RevealProviderCredentialInput) (string, error)
	ListModels(context.Context, string) ([]modeladmin.CatalogModel, error)
	CreateModel(context.Context, modeladmin.CreateModelInput) (modeladmin.CatalogModel, error)
	UpdateModel(context.Context, modeladmin.UpdateModelInput) (modeladmin.CatalogModel, error)
	ChangeModelStatus(context.Context, modeladmin.ChangeModelStatusInput) (modeladmin.CatalogModel, error)
}

type providerWriteRequest struct {
	Code               string  `json:"code"`
	Name               string  `json:"name"`
	ProviderKind       string  `json:"providerKind"`
	BaseURL            string  `json:"baseUrl"`
	APIKey             *string `json:"apiKey"`
	RequestTimeoutMS   int     `json:"requestTimeoutMs"`
	StreamUsageEnabled *bool   `json:"streamUsageEnabled"`
}

type statusWriteRequest struct {
	Status string `json:"status"`
}

type modelWriteRequest struct {
	Code                 string   `json:"code"`
	ProviderConnectionID string   `json:"providerConnectionId"`
	UpstreamModelID      string   `json:"upstreamModelId"`
	Name                 string   `json:"name"`
	ModelType            string   `json:"modelType"`
	Capabilities         []string `json:"capabilities"`
	ContextWindowTokens  int      `json:"contextWindowTokens"`
	MaxOutputTokens      int      `json:"maxOutputTokens"`
}

type providerResponse struct {
	ID                   string     `json:"id"`
	Code                 string     `json:"code"`
	Name                 string     `json:"name"`
	ProviderKind         string     `json:"providerKind"`
	BaseURL              string     `json:"baseUrl"`
	Status               string     `json:"status"`
	RequestTimeoutMS     int        `json:"requestTimeoutMs"`
	StreamUsageEnabled   bool       `json:"streamUsageEnabled"`
	CredentialConfigured bool       `json:"credentialConfigured"`
	LastProbeStatus      string     `json:"lastProbeStatus"`
	LastProbeAt          *time.Time `json:"lastProbeAt,omitempty"`
	CreatedAt            time.Time  `json:"createdAt"`
	UpdatedAt            time.Time  `json:"updatedAt"`
}

type modelResponse struct {
	ID                   string    `json:"id"`
	ProviderConnectionID string    `json:"providerConnectionId"`
	ProviderName         string    `json:"providerName"`
	Code                 string    `json:"code"`
	UpstreamModelID      string    `json:"upstreamModelId"`
	Name                 string    `json:"name"`
	ModelType            string    `json:"modelType"`
	Capabilities         []string  `json:"capabilities"`
	ContextWindowTokens  int       `json:"contextWindowTokens"`
	MaxOutputTokens      int       `json:"maxOutputTokens"`
	Status               string    `json:"status"`
	CreatedAt            time.Time `json:"createdAt"`
	UpdatedAt            time.Time `json:"updatedAt"`
}

func registerModelRoutes(router *gin.Engine, deps Dependencies) {
	providers := router.Group("/api/v1/model-providers", requireSession(deps))
	providers.GET("", requirePermission(deps, "model.provider.read"), listProvidersHandler(deps))
	providers.POST("", requirePermission(deps, "model.provider.write"), requireCSRF(deps), createProviderHandler(deps))
	providers.PUT("/:id", requirePermission(deps, "model.provider.write"), requireCSRF(deps), updateProviderHandler(deps))
	providers.PATCH("/:id/status", requirePermission(deps, "model.provider.write"), requireCSRF(deps), changeProviderStatusHandler(deps))
	providers.POST("/:id/probe", requirePermission(deps, "model.provider.write"), requireCSRF(deps), probeProviderHandler(deps))
	providers.GET("/:id/credential", requirePermission(deps, "model.provider.write"), revealProviderCredentialHandler(deps))

	models := router.Group("/api/v1/models", requireSession(deps))
	models.GET("", requirePermission(deps, "model.catalog.read"), listModelsHandler(deps))
	models.POST("", requirePermission(deps, "model.catalog.write"), requireCSRF(deps), createModelHandler(deps))
	models.PUT("/:id", requirePermission(deps, "model.catalog.write"), requireCSRF(deps), updateModelHandler(deps))
	models.PATCH("/:id/status", requirePermission(deps, "model.catalog.write"), requireCSRF(deps), changeModelStatusHandler(deps))
}

func listProvidersHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		items, err := deps.ModelAdmin.ListProviders(ctx.Request.Context(), currentSession(ctx).User.OrganizationID)
		if err != nil {
			handleModelAdminError(ctx, deps.Logger, err)
			return
		}
		response := make([]providerResponse, 0, len(items))
		for _, item := range items {
			response = append(response, presentProvider(item))
		}
		httpx.OK(ctx, http.StatusOK, response)
	}
}

func createProviderHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var request providerWriteRequest
		if !bindModelJSON(ctx, &request) {
			return
		}
		if request.APIKey == nil {
			httpx.Fail(ctx, http.StatusBadRequest, "invalid_provider", "创建供应商连接时必须填写 API Key")
			return
		}
		session := currentSession(ctx)
		provider, err := deps.ModelAdmin.CreateProvider(ctx.Request.Context(), modeladmin.CreateProviderInput{
			OrganizationID: session.User.OrganizationID, ActorID: session.User.ID, Code: request.Code,
			Name: request.Name, ProviderKind: request.ProviderKind, BaseURL: request.BaseURL, APIKey: *request.APIKey,
			RequestTimeoutMS: request.RequestTimeoutMS, StreamUsageEnabled: request.StreamUsageEnabled != nil && *request.StreamUsageEnabled,
			RequestID: contextString(ctx, "request_id"), SourceIP: ctx.ClientIP(),
		})
		if err != nil {
			handleModelAdminError(ctx, deps.Logger, err)
			return
		}
		httpx.OK(ctx, http.StatusCreated, presentProvider(provider))
	}
}

func updateProviderHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if !validUUIDParam(ctx, "供应商连接") {
			return
		}
		var request providerWriteRequest
		if !bindModelJSON(ctx, &request) {
			return
		}
		session := currentSession(ctx)
		provider, err := deps.ModelAdmin.UpdateProvider(ctx.Request.Context(), modeladmin.UpdateProviderInput{
			OrganizationID: session.User.OrganizationID, ActorID: session.User.ID, ProviderID: ctx.Param("id"),
			Name: request.Name, ProviderKind: request.ProviderKind, BaseURL: request.BaseURL, APIKey: request.APIKey, RequestTimeoutMS: request.RequestTimeoutMS,
			StreamUsageEnabled: request.StreamUsageEnabled,
			RequestID:          contextString(ctx, "request_id"), SourceIP: ctx.ClientIP(),
		})
		if err != nil {
			handleModelAdminError(ctx, deps.Logger, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, presentProvider(provider))
	}
}

func changeProviderStatusHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if !validUUIDParam(ctx, "供应商连接") {
			return
		}
		var request statusWriteRequest
		if !bindModelJSON(ctx, &request) {
			return
		}
		session := currentSession(ctx)
		provider, err := deps.ModelAdmin.ChangeProviderStatus(ctx.Request.Context(), modeladmin.ChangeProviderStatusInput{
			OrganizationID: session.User.OrganizationID, ActorID: session.User.ID, ProviderID: ctx.Param("id"), Status: request.Status,
			RequestID: contextString(ctx, "request_id"), SourceIP: ctx.ClientIP(),
		})
		if err != nil {
			handleModelAdminError(ctx, deps.Logger, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, presentProvider(provider))
	}
}

func probeProviderHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if !validUUIDParam(ctx, "供应商连接") {
			return
		}
		session := currentSession(ctx)
		result, err := deps.ModelAdmin.ProbeProvider(ctx.Request.Context(), modeladmin.ProbeInput{
			OrganizationID: session.User.OrganizationID, ActorID: session.User.ID, ProviderID: ctx.Param("id"),
			RequestID: contextString(ctx, "request_id"), SourceIP: ctx.ClientIP(),
		})
		if err != nil {
			handleModelAdminError(ctx, deps.Logger, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, gin.H{"status": result.Status, "latencyMs": result.LatencyMS, "checkedAt": result.CheckedAt})
	}
}

func revealProviderCredentialHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if !validUUIDParam(ctx, "供应商连接") {
			return
		}
		session := currentSession(ctx)
		value, err := deps.ModelAdmin.RevealProviderCredential(ctx.Request.Context(), modeladmin.RevealProviderCredentialInput{
			OrganizationID: session.User.OrganizationID, ActorID: session.User.ID, ProviderID: ctx.Param("id"),
			RequestID: contextString(ctx, "request_id"), SourceIP: ctx.ClientIP(),
		})
		if err != nil {
			handleModelAdminError(ctx, deps.Logger, err)
			return
		}
		ctx.Header("Cache-Control", "no-store")
		ctx.Header("Pragma", "no-cache")
		httpx.OK(ctx, http.StatusOK, gin.H{"secret": value})
	}
}

func listModelsHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		items, err := deps.ModelAdmin.ListModels(ctx.Request.Context(), currentSession(ctx).User.OrganizationID)
		if err != nil {
			handleModelAdminError(ctx, deps.Logger, err)
			return
		}
		response := make([]modelResponse, 0, len(items))
		for _, item := range items {
			response = append(response, presentModel(item))
		}
		httpx.OK(ctx, http.StatusOK, response)
	}
}

func createModelHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var request modelWriteRequest
		if !bindModelJSON(ctx, &request) {
			return
		}
		if _, err := uuid.Parse(request.ProviderConnectionID); err != nil {
			httpx.Fail(ctx, http.StatusBadRequest, "invalid_model", "供应商连接标识格式不正确")
			return
		}
		session := currentSession(ctx)
		model, err := deps.ModelAdmin.CreateModel(ctx.Request.Context(), modeladmin.CreateModelInput{
			OrganizationID: session.User.OrganizationID, ActorID: session.User.ID, ProviderConnectionID: request.ProviderConnectionID,
			Code: request.Code, UpstreamModelID: request.UpstreamModelID, Name: request.Name, ModelType: request.ModelType,
			Capabilities: request.Capabilities, ContextWindowTokens: request.ContextWindowTokens, MaxOutputTokens: request.MaxOutputTokens,
			RequestID: contextString(ctx, "request_id"), SourceIP: ctx.ClientIP(),
		})
		if err != nil {
			handleModelAdminError(ctx, deps.Logger, err)
			return
		}
		httpx.OK(ctx, http.StatusCreated, presentModel(model))
	}
}

func updateModelHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if !validUUIDParam(ctx, "模型") {
			return
		}
		var request modelWriteRequest
		if !bindModelJSON(ctx, &request) {
			return
		}
		if _, err := uuid.Parse(request.ProviderConnectionID); err != nil {
			httpx.Fail(ctx, http.StatusBadRequest, "invalid_model", "供应商连接标识格式不正确")
			return
		}
		session := currentSession(ctx)
		model, err := deps.ModelAdmin.UpdateModel(ctx.Request.Context(), modeladmin.UpdateModelInput{
			OrganizationID: session.User.OrganizationID, ActorID: session.User.ID, ModelID: ctx.Param("id"),
			ProviderConnectionID: request.ProviderConnectionID, UpstreamModelID: request.UpstreamModelID, Name: request.Name,
			ModelType: request.ModelType, Capabilities: request.Capabilities, ContextWindowTokens: request.ContextWindowTokens,
			MaxOutputTokens: request.MaxOutputTokens, RequestID: contextString(ctx, "request_id"), SourceIP: ctx.ClientIP(),
		})
		if err != nil {
			handleModelAdminError(ctx, deps.Logger, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, presentModel(model))
	}
}

func changeModelStatusHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if !validUUIDParam(ctx, "模型") {
			return
		}
		var request statusWriteRequest
		if !bindModelJSON(ctx, &request) {
			return
		}
		session := currentSession(ctx)
		model, err := deps.ModelAdmin.ChangeModelStatus(ctx.Request.Context(), modeladmin.ChangeModelStatusInput{
			OrganizationID: session.User.OrganizationID, ActorID: session.User.ID, ModelID: ctx.Param("id"), Status: request.Status,
			RequestID: contextString(ctx, "request_id"), SourceIP: ctx.ClientIP(),
		})
		if err != nil {
			handleModelAdminError(ctx, deps.Logger, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, presentModel(model))
	}
}

func bindModelJSON(ctx *gin.Context, value any) bool {
	ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, 64*1024)
	if err := ctx.ShouldBindJSON(value); err != nil {
		httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", "请求参数格式不正确")
		return false
	}
	return true
}

func validUUIDParam(ctx *gin.Context, resource string) bool {
	if _, err := uuid.Parse(ctx.Param("id")); err != nil {
		httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", resource+"标识格式不正确")
		return false
	}
	return true
}

func presentProvider(item modeladmin.Provider) providerResponse {
	return providerResponse{ID: item.ID, Code: item.Code, Name: item.Name, ProviderKind: item.ProviderKind, BaseURL: item.BaseURL,
		Status: item.Status, RequestTimeoutMS: item.RequestTimeoutMS, StreamUsageEnabled: item.StreamUsageEnabled, CredentialConfigured: item.CredentialConfigured,
		LastProbeStatus: item.LastProbeStatus, LastProbeAt: item.LastProbeAt, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt}
}

func presentModel(item modeladmin.CatalogModel) modelResponse {
	return modelResponse{ID: item.ID, ProviderConnectionID: item.ProviderConnectionID, ProviderName: item.ProviderName,
		Code: item.Code, UpstreamModelID: item.UpstreamModelID, Name: item.Name, ModelType: item.ModelType,
		Capabilities: item.Capabilities, ContextWindowTokens: item.ContextWindowTokens, MaxOutputTokens: item.MaxOutputTokens,
		Status: item.Status, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt}
}

func handleModelAdminError(ctx *gin.Context, logger *slog.Logger, err error) {
	switch {
	case errors.Is(err, modeladmin.ErrInvalidInput):
		httpx.Fail(ctx, http.StatusBadRequest, "invalid_model_configuration", "模型接入参数不正确")
	case errors.Is(err, modeladmin.ErrProviderNotFound):
		httpx.Fail(ctx, http.StatusNotFound, "model_provider_not_found", "模型供应商连接不存在")
	case errors.Is(err, modeladmin.ErrProviderConflict):
		httpx.Fail(ctx, http.StatusConflict, "model_provider_conflict", "模型供应商连接编码已存在")
	case errors.Is(err, modeladmin.ErrModelNotFound):
		httpx.Fail(ctx, http.StatusNotFound, "model_not_found", "模型不存在")
	case errors.Is(err, modeladmin.ErrModelConflict):
		httpx.Fail(ctx, http.StatusConflict, "model_conflict", "模型编码或上游模型标识已存在")
	case errors.Is(err, modeladmin.ErrProviderDisabled):
		httpx.Fail(ctx, http.StatusConflict, "model_provider_disabled", "模型供应商连接已停用")
	case errors.Is(err, modeladmin.ErrProbeFailed):
		httpx.Fail(ctx, http.StatusBadGateway, "model_provider_unreachable", "无法连接模型供应商，请检查地址、网络和凭据")
	default:
		logger.Error("模型接入接口处理失败", "error", err, "request_id", contextString(ctx, "request_id"))
		httpx.Fail(ctx, http.StatusInternalServerError, "internal_error", "服务暂时不可用")
	}
}
