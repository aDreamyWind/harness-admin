package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"harness-admin/internal/httpx"
	"harness-admin/internal/mcpadmin"
)

type MCPAdminService interface {
	List(context.Context, string) ([]mcpadmin.ServiceRecord, error)
	Create(context.Context, mcpadmin.CreateInput) (mcpadmin.ServiceRecord, error)
	Update(context.Context, mcpadmin.UpdateInput) (mcpadmin.ServiceRecord, error)
	RevealCredentials(context.Context, mcpadmin.Actor, string) (mcpadmin.UpstreamCredentials, error)
	UpdateCredentials(context.Context, mcpadmin.UpdateCredentialsInput) (mcpadmin.ServiceRecord, error)
	ChangeStatus(context.Context, mcpadmin.ChangeStatusInput) (mcpadmin.ServiceRecord, error)
	Discover(context.Context, mcpadmin.DiscoverInput) ([]mcpadmin.Tool, error)
	ListTools(context.Context, string, string) ([]mcpadmin.Tool, error)
	Test(context.Context, mcpadmin.TestInput) (json.RawMessage, error)
}

type mcpServiceRequest struct {
	Code             string                         `json:"code"`
	Name             string                         `json:"name"`
	Description      string                         `json:"description"`
	Transport        string                         `json:"transport"`
	UpstreamURL      string                         `json:"upstreamUrl"`
	Parameters       []mcpadmin.ParameterDefinition `json:"parameters"`
	CredentialValues []mcpadmin.ParameterValue      `json:"credentialValues"`
}

type mcpStatusRequest struct {
	Status string `json:"status"`
}

type mcpUpstreamRequest struct {
	ConnectionID  string                          `json:"connectionId"`
	Authorization string                          `json:"authorization"`
	HeaderName    string                          `json:"headerName"`
	HeaderValue   string                          `json:"headerValue"`
	QueryName     string                          `json:"queryName"`
	QueryValue    string                          `json:"queryValue"`
	Parameters    []mcpadmin.ParameterValue       `json:"parameters"`
	Definitions   *[]mcpadmin.ParameterDefinition `json:"definitions"`
	Name          string                          `json:"name"`
	Arguments     json.RawMessage                 `json:"arguments"`
}

func (r mcpUpstreamRequest) credentials() mcpadmin.UpstreamCredentials {
	return mcpadmin.UpstreamCredentials{
		Authorization: r.Authorization, HeaderName: r.HeaderName, HeaderValue: r.HeaderValue,
		QueryName: r.QueryName, QueryValue: r.QueryValue,
		Parameters: r.Parameters,
	}
}

func registerMCPRoutes(router *gin.Engine, deps Dependencies) {
	group := router.Group("/api/v1/mcp-services", requireSession(deps))
	group.GET("", requirePermission(deps, "mcp.service.read"), func(ctx *gin.Context) {
		items, err := deps.MCPAdmin.List(ctx.Request.Context(), currentSession(ctx).User.OrganizationID)
		if err != nil {
			handleMCPError(ctx, deps, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, items)
	})
	group.POST("", requirePermission(deps, "mcp.service.write"), requireCSRF(deps), func(ctx *gin.Context) {
		var request mcpServiceRequest
		if !bindMCPJSON(ctx, &request) {
			return
		}
		actor := mcpActor(ctx)
		item, err := deps.MCPAdmin.Create(ctx.Request.Context(), mcpadmin.CreateInput{
			Actor: actor, Code: request.Code, Name: request.Name, Description: request.Description,
			Transport: request.Transport, UpstreamURL: request.UpstreamURL, Parameters: request.Parameters,
			Credentials: mcpadmin.UpstreamCredentials{Parameters: request.CredentialValues},
		})
		if err != nil {
			handleMCPError(ctx, deps, err)
			return
		}
		httpx.OK(ctx, http.StatusCreated, item)
	})
	group.PUT("/:id", requirePermission(deps, "mcp.service.write"), requireCSRF(deps), func(ctx *gin.Context) {
		if !validMCPID(ctx) {
			return
		}
		var request mcpServiceRequest
		if !bindMCPJSON(ctx, &request) {
			return
		}
		item, err := deps.MCPAdmin.Update(ctx.Request.Context(), mcpadmin.UpdateInput{
			Actor: mcpActor(ctx), ServiceID: ctx.Param("id"), Name: request.Name,
			Description: request.Description, Transport: request.Transport,
			UpstreamURL: request.UpstreamURL, Parameters: request.Parameters,
		})
		if err != nil {
			handleMCPError(ctx, deps, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, item)
	})
	group.GET("/:id/credentials", requirePermission(deps, "mcp.service.write"), func(ctx *gin.Context) {
		if !validMCPID(ctx) {
			return
		}
		value, err := deps.MCPAdmin.RevealCredentials(ctx.Request.Context(), mcpActor(ctx), ctx.Param("id"))
		if err != nil {
			handleMCPError(ctx, deps, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, value)
	})
	group.PUT("/:id/credentials", requirePermission(deps, "mcp.service.write"), requireCSRF(deps), func(ctx *gin.Context) {
		if !validMCPID(ctx) {
			return
		}
		var request mcpUpstreamRequest
		if !bindMCPJSON(ctx, &request) {
			return
		}
		item, err := deps.MCPAdmin.UpdateCredentials(ctx.Request.Context(), mcpadmin.UpdateCredentialsInput{
			Actor: mcpActor(ctx), ServiceID: ctx.Param("id"), Credentials: request.credentials(), Definitions: request.Definitions,
		})
		if err != nil {
			handleMCPError(ctx, deps, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, item)
	})
	group.PATCH("/:id/status", requirePermission(deps, "mcp.service.write"), requireCSRF(deps), func(ctx *gin.Context) {
		if !validMCPID(ctx) {
			return
		}
		var request mcpStatusRequest
		if !bindMCPJSON(ctx, &request) {
			return
		}
		item, err := deps.MCPAdmin.ChangeStatus(ctx.Request.Context(), mcpadmin.ChangeStatusInput{
			Actor: mcpActor(ctx), ServiceID: ctx.Param("id"), Status: request.Status,
		})
		if err != nil {
			handleMCPError(ctx, deps, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, item)
	})
	group.GET("/:id/tools", requirePermission(deps, "mcp.service.read"), func(ctx *gin.Context) {
		if !validMCPID(ctx) {
			return
		}
		items, err := deps.MCPAdmin.ListTools(ctx.Request.Context(), currentSession(ctx).User.OrganizationID, ctx.Param("id"))
		if err != nil {
			handleMCPError(ctx, deps, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, items)
	})
	group.POST("/:id/discover", requirePermission(deps, "mcp.service.write"), requireCSRF(deps), func(ctx *gin.Context) {
		if !validMCPID(ctx) {
			return
		}
		var request mcpUpstreamRequest
		if !bindMCPJSON(ctx, &request) {
			return
		}
		items, err := deps.MCPAdmin.Discover(ctx.Request.Context(), mcpadmin.DiscoverInput{
			Actor: mcpActor(ctx), ServiceID: ctx.Param("id"), ConnectionID: request.ConnectionID, Credentials: request.credentials(),
		})
		if err != nil {
			handleMCPError(ctx, deps, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, items)
	})
	group.POST("/:id/test", requirePermission(deps, "mcp.service.write"), requireCSRF(deps), func(ctx *gin.Context) {
		if !validMCPID(ctx) {
			return
		}
		var request mcpUpstreamRequest
		if !bindMCPJSON(ctx, &request) {
			return
		}
		result, err := deps.MCPAdmin.Test(ctx.Request.Context(), mcpadmin.TestInput{
			Actor: mcpActor(ctx), ServiceID: ctx.Param("id"), ConnectionID: request.ConnectionID,
			Name: request.Name, Arguments: request.Arguments, Credentials: request.credentials(),
		})
		if err != nil {
			handleMCPError(ctx, deps, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, result)
	})
}

func mcpActor(ctx *gin.Context) mcpadmin.Actor {
	session := currentSession(ctx)
	return mcpadmin.Actor{
		OrganizationID: session.User.OrganizationID, UserID: session.User.ID,
		RequestID: contextString(ctx, "request_id"), SourceIP: ctx.ClientIP(),
	}
}

func validMCPID(ctx *gin.Context) bool {
	if _, err := uuid.Parse(ctx.Param("id")); err != nil {
		httpx.Fail(ctx, http.StatusBadRequest, "invalid_mcp_service", "MCP 服务标识格式不正确")
		return false
	}
	return true
}

func bindMCPJSON(ctx *gin.Context, target any) bool {
	ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, 512*1024)
	if err := ctx.ShouldBindJSON(target); err != nil {
		httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", "MCP 请求参数格式不正确")
		return false
	}
	return true
}

func handleMCPError(ctx *gin.Context, deps Dependencies, err error) {
	switch {
	case errors.Is(err, mcpadmin.ErrInvalidInput):
		httpx.Fail(ctx, http.StatusBadRequest, "invalid_mcp_service", "MCP 服务参数不正确")
	case errors.Is(err, mcpadmin.ErrNotFound):
		httpx.Fail(ctx, http.StatusNotFound, "mcp_service_not_found", "MCP 服务不存在")
	case errors.Is(err, mcpadmin.ErrConflict):
		httpx.Fail(ctx, http.StatusConflict, "mcp_service_conflict", "MCP 服务编码已存在")
	case errors.Is(err, mcpadmin.ErrDisabled):
		httpx.Fail(ctx, http.StatusConflict, "mcp_service_disabled", "MCP 服务已停用")
	case errors.Is(err, mcpadmin.ErrUpstream):
		deps.Logger.Warn("上游 MCP 服务操作失败", "error", err, "request_id", contextString(ctx, "request_id"))
		httpx.Fail(ctx, http.StatusBadGateway, "mcp_upstream_error", "无法连接或调用上游 MCP 服务，请检查地址、鉴权和服务状态")
	default:
		deps.Logger.Error("MCP 服务操作失败", "error", err, "request_id", contextString(ctx, "request_id"))
		httpx.Fail(ctx, http.StatusInternalServerError, "internal_error", "服务暂时不可用")
	}
}
