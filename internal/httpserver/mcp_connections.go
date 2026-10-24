package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"harness-admin/internal/httpx"
	"harness-admin/internal/mcpadmin"
	"harness-admin/internal/mcpclient"
	"harness-admin/internal/mcpconnection"
)

type MCPConnectionService interface {
	List(context.Context, string) ([]mcpconnection.Connection, error)
	Create(context.Context, mcpconnection.CreateInput) (mcpconnection.CreatedConnection, error)
	ReplaceGrants(context.Context, mcpconnection.GrantsInput) (mcpconnection.Connection, error)
	ChangeStatus(context.Context, mcpconnection.Actor, string, string) (mcpconnection.Connection, error)
	RevealSecret(context.Context, mcpconnection.Actor, string) (string, error)
	RevealUpstreamAuthorization(context.Context, mcpconnection.Actor, string) (string, error)
	RevealUpstreamCredentials(context.Context, mcpconnection.Actor, string) (mcpadmin.UpstreamCredentials, error)
	RotateSecret(context.Context, mcpconnection.Actor, string) (string, error)
	UpdateUpstreamCredentials(context.Context, mcpconnection.Actor, string, mcpadmin.UpstreamCredentials) (mcpconnection.Connection, error)
	Lookup(context.Context, string) (mcpconnection.GatewayConnection, error)
	CheckHealth(context.Context, string, string, mcpconnection.HealthProber) (mcpconnection.Health, error)
	AuthorizeDeveloper(context.Context, mcpconnection.GatewayConnection, string) (mcpconnection.Caller, error)
	AuthorizeUser(context.Context, mcpconnection.GatewayConnection, string, string) (mcpconnection.Caller, error)
	ListForUser(context.Context, string, string) ([]mcpconnection.Connection, error)
	RecordCall(context.Context, mcpconnection.CallRecord) error
	ListUsage(context.Context, string) ([]mcpconnection.Usage, error)
}

type MCPToolCaller interface {
	CallTool(context.Context, mcpadmin.ServiceRecord, mcpadmin.UpstreamCredentials, string, json.RawMessage) (json.RawMessage, error)
	ListTools(context.Context, mcpadmin.ServiceRecord, mcpadmin.UpstreamCredentials) ([]mcpadmin.Tool, error)
}

type MCPContentCaller interface {
	ListResources(context.Context, mcpadmin.ServiceRecord, mcpadmin.UpstreamCredentials) ([]*mcp.Resource, []*mcp.ResourceTemplate, error)
	ReadResource(context.Context, mcpadmin.ServiceRecord, mcpadmin.UpstreamCredentials, string) (*mcp.ReadResourceResult, error)
	ListPrompts(context.Context, mcpadmin.ServiceRecord, mcpadmin.UpstreamCredentials) ([]*mcp.Prompt, error)
	GetPrompt(context.Context, mcpadmin.ServiceRecord, mcpadmin.UpstreamCredentials, string, map[string]string) (*mcp.GetPromptResult, error)
}

type MCPSkillCaller interface {
	SupportsSkills(context.Context, mcpadmin.ServiceRecord, mcpadmin.UpstreamCredentials) (bool, error)
	ListSkills(context.Context, mcpadmin.ServiceRecord, mcpadmin.UpstreamCredentials, string) (*mcpclient.ListSkillsResult, error)
	GetSkill(context.Context, mcpadmin.ServiceRecord, mcpadmin.UpstreamCredentials, string) (*mcpclient.GetSkillResult, error)
}

type mcpConnectionRequest struct {
	ServiceID             string                    `json:"serviceId"`
	Code                  string                    `json:"code"`
	Name                  string                    `json:"name"`
	Purpose               string                    `json:"purpose"`
	AuthMode              string                    `json:"authMode"`
	ApplicationName       string                    `json:"applicationName"`
	ExpiresAt             *time.Time                `json:"expiresAt"`
	UpstreamAuthorization string                    `json:"upstreamAuthorization"`
	UpstreamHeaderName    string                    `json:"upstreamHeaderName"`
	UpstreamHeaderValue   string                    `json:"upstreamHeaderValue"`
	UpstreamQueryName     string                    `json:"upstreamQueryName"`
	UpstreamQueryValue    string                    `json:"upstreamQueryValue"`
	UpstreamParameters    []mcpadmin.ParameterValue `json:"upstreamParameters"`
	RequestsPerMinute     int                       `json:"requestsPerMinute"`
	DepartmentIDs         []string                  `json:"departmentIds"`
	UserIDs               []string                  `json:"userIds"`
}

type mcpGrantsRequest struct {
	DepartmentIDs []string `json:"departmentIds"`
	UserIDs       []string `json:"userIds"`
}

type mcpConnectionCredentialsRequest struct {
	Authorization string                    `json:"authorization"`
	HeaderName    string                    `json:"headerName"`
	HeaderValue   string                    `json:"headerValue"`
	QueryName     string                    `json:"queryName"`
	QueryValue    string                    `json:"queryValue"`
	Parameters    []mcpadmin.ParameterValue `json:"parameters"`
}

func (r mcpConnectionCredentialsRequest) credentials() mcpadmin.UpstreamCredentials {
	return mcpadmin.UpstreamCredentials{Authorization: r.Authorization,
		HeaderName: r.HeaderName, HeaderValue: r.HeaderValue, QueryName: r.QueryName, QueryValue: r.QueryValue,
		Parameters: r.Parameters}
}

func registerMCPConnectionRoutes(router *gin.Engine, deps Dependencies) {
	admin := router.Group("/api/v1/mcp-connections", requireSession(deps))
	admin.GET("", requirePermission(deps, "mcp.connection.read"), func(ctx *gin.Context) {
		items, err := deps.MCPConnections.List(ctx.Request.Context(), currentSession(ctx).User.OrganizationID)
		if err != nil {
			handleMCPConnectionError(ctx, deps, err)
			return
		}
		for i := range items {
			items[i].GatewayURL = mcpGatewayURL(deps, items[i].Code)
		}
		httpx.OK(ctx, http.StatusOK, items)
	})
	admin.POST("", requirePermission(deps, "mcp.connection.write"), requireCSRF(deps), func(ctx *gin.Context) {
		var request mcpConnectionRequest
		if !bindMCPJSON(ctx, &request) {
			return
		}
		if _, err := uuid.Parse(request.ServiceID); err != nil {
			httpx.Fail(ctx, http.StatusBadRequest, "invalid_mcp_connection", "MCP 服务标识格式不正确")
			return
		}
		actor := mcpActor(ctx)
		item, err := deps.MCPConnections.Create(ctx.Request.Context(), mcpconnection.CreateInput{
			Actor:     mcpconnection.Actor{OrganizationID: actor.OrganizationID, UserID: actor.UserID, RequestID: actor.RequestID, SourceIP: actor.SourceIP},
			ServiceID: request.ServiceID, Code: request.Code, Name: request.Name, Purpose: request.Purpose,
			AuthMode: request.AuthMode, ApplicationName: request.ApplicationName, ExpiresAt: request.ExpiresAt,
			UpstreamAuthorization: request.UpstreamAuthorization,
			UpstreamHeaderName:    request.UpstreamHeaderName, UpstreamHeaderValue: request.UpstreamHeaderValue,
			UpstreamQueryName: request.UpstreamQueryName, UpstreamQueryValue: request.UpstreamQueryValue,
			UpstreamParameters: request.UpstreamParameters,
			RequestsPerMinute:  request.RequestsPerMinute,
			DepartmentIDs:      request.DepartmentIDs, UserIDs: request.UserIDs,
		})
		if err != nil {
			handleMCPConnectionError(ctx, deps, err)
			return
		}
		ctx.Header("Cache-Control", "no-store")
		item.GatewayURL = mcpGatewayURL(deps, item.Code)
		httpx.OK(ctx, http.StatusCreated, item)
	})
	admin.GET("/:id/health", requirePermission(deps, "mcp.connection.read"), func(ctx *gin.Context) {
		if !validMCPID(ctx) {
			return
		}
		result, err := deps.MCPConnections.CheckHealth(ctx.Request.Context(), currentSession(ctx).User.OrganizationID,
			ctx.Param("id"), deps.MCPToolCaller)
		if err != nil {
			handleMCPConnectionError(ctx, deps, err)
			return
		}
		ctx.Header("Cache-Control", "no-store")
		httpx.OK(ctx, http.StatusOK, result)
	})
	admin.PATCH("/:id/status", requirePermission(deps, "mcp.connection.write"), requireCSRF(deps), func(ctx *gin.Context) {
		if !validMCPID(ctx) {
			return
		}
		var request mcpStatusRequest
		if !bindMCPJSON(ctx, &request) {
			return
		}
		actor := mcpActor(ctx)
		item, err := deps.MCPConnections.ChangeStatus(ctx.Request.Context(),
			mcpconnection.Actor{OrganizationID: actor.OrganizationID, UserID: actor.UserID, RequestID: actor.RequestID, SourceIP: actor.SourceIP},
			ctx.Param("id"), request.Status)
		if err != nil {
			handleMCPConnectionError(ctx, deps, err)
			return
		}
		item.GatewayURL = mcpGatewayURL(deps, item.Code)
		httpx.OK(ctx, http.StatusOK, item)
	})
	admin.PUT("/:id/grants", requirePermission(deps, "mcp.connection.write"), requireCSRF(deps), func(ctx *gin.Context) {
		if !validMCPID(ctx) {
			return
		}
		var request mcpGrantsRequest
		if !bindMCPJSON(ctx, &request) {
			return
		}
		actor := mcpActor(ctx)
		item, err := deps.MCPConnections.ReplaceGrants(ctx.Request.Context(), mcpconnection.GrantsInput{
			Actor:        mcpconnection.Actor{OrganizationID: actor.OrganizationID, UserID: actor.UserID, RequestID: actor.RequestID, SourceIP: actor.SourceIP},
			ConnectionID: ctx.Param("id"), DepartmentIDs: request.DepartmentIDs, UserIDs: request.UserIDs,
		})
		if err != nil {
			handleMCPConnectionError(ctx, deps, err)
			return
		}
		item.GatewayURL = mcpGatewayURL(deps, item.Code)
		httpx.OK(ctx, http.StatusOK, item)
	})
	admin.GET("/:id/secret", requirePermission(deps, "mcp.connection.write"), func(ctx *gin.Context) {
		if !validMCPID(ctx) {
			return
		}
		actor := mcpActor(ctx)
		value, err := deps.MCPConnections.RevealSecret(ctx.Request.Context(),
			mcpconnection.Actor{OrganizationID: actor.OrganizationID, UserID: actor.UserID, RequestID: actor.RequestID, SourceIP: actor.SourceIP},
			ctx.Param("id"))
		if err != nil {
			handleMCPConnectionError(ctx, deps, err)
			return
		}
		ctx.Header("Cache-Control", "no-store")
		httpx.OK(ctx, http.StatusOK, gin.H{"secret": value})
	})
	admin.POST("/:id/rotate-secret", requirePermission(deps, "mcp.connection.write"), requireCSRF(deps), func(ctx *gin.Context) {
		if !validMCPID(ctx) {
			return
		}
		actor := mcpActor(ctx)
		value, err := deps.MCPConnections.RotateSecret(ctx.Request.Context(),
			mcpconnection.Actor{OrganizationID: actor.OrganizationID, UserID: actor.UserID, RequestID: actor.RequestID, SourceIP: actor.SourceIP}, ctx.Param("id"))
		if err != nil {
			handleMCPConnectionError(ctx, deps, err)
			return
		}
		ctx.Header("Cache-Control", "no-store")
		httpx.OK(ctx, http.StatusOK, gin.H{"secret": value})
	})
	admin.GET("/:id/upstream-authorization", requirePermission(deps, "mcp.connection.write"), func(ctx *gin.Context) {
		if !validMCPID(ctx) {
			return
		}
		actor := mcpActor(ctx)
		value, err := deps.MCPConnections.RevealUpstreamAuthorization(ctx.Request.Context(),
			mcpconnection.Actor{OrganizationID: actor.OrganizationID, UserID: actor.UserID, RequestID: actor.RequestID, SourceIP: actor.SourceIP},
			ctx.Param("id"))
		if err != nil {
			handleMCPConnectionError(ctx, deps, err)
			return
		}
		ctx.Header("Cache-Control", "no-store")
		httpx.OK(ctx, http.StatusOK, gin.H{"authorization": value})
	})
	admin.GET("/:id/upstream-credentials", requirePermission(deps, "mcp.connection.write"), func(ctx *gin.Context) {
		if !validMCPID(ctx) {
			return
		}
		actor := mcpActor(ctx)
		value, err := deps.MCPConnections.RevealUpstreamCredentials(ctx.Request.Context(),
			mcpconnection.Actor{OrganizationID: actor.OrganizationID, UserID: actor.UserID, RequestID: actor.RequestID, SourceIP: actor.SourceIP}, ctx.Param("id"))
		if err != nil {
			handleMCPConnectionError(ctx, deps, err)
			return
		}
		ctx.Header("Cache-Control", "no-store")
		httpx.OK(ctx, http.StatusOK, value)
	})
	admin.PUT("/:id/upstream-credentials", requirePermission(deps, "mcp.connection.write"), requireCSRF(deps), func(ctx *gin.Context) {
		if !validMCPID(ctx) {
			return
		}
		var request mcpConnectionCredentialsRequest
		if !bindMCPJSON(ctx, &request) {
			return
		}
		actor := mcpActor(ctx)
		item, err := deps.MCPConnections.UpdateUpstreamCredentials(ctx.Request.Context(),
			mcpconnection.Actor{OrganizationID: actor.OrganizationID, UserID: actor.UserID, RequestID: actor.RequestID, SourceIP: actor.SourceIP},
			ctx.Param("id"), request.credentials())
		if err != nil {
			handleMCPConnectionError(ctx, deps, err)
			return
		}
		item.GatewayURL = mcpGatewayURL(deps, item.Code)
		httpx.OK(ctx, http.StatusOK, item)
	})
	admin.GET("/usage", requirePermission(deps, "mcp.connection.read"), func(ctx *gin.Context) {
		items, err := deps.MCPConnections.ListUsage(ctx.Request.Context(), currentSession(ctx).User.OrganizationID)
		if err != nil {
			handleMCPConnectionError(ctx, deps, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, items)
	})

	router.GET("/api/v1/mcp/connections", mcpUserDirectoryHandler(deps))
	gateway := mcpGatewayHandler(deps)
	router.POST("/mcp/:code", gateway)
	router.GET("/mcp/:code", gateway)
	router.DELETE("/mcp/:code", gateway)
	legacy := newMCPLegacyGateway(deps)
	router.GET("/mcp/:code/sse", legacy.stream)
	router.POST("/mcp/:code/messages", legacy.message)
}

func mcpUserDirectoryHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		subject, err := deps.OIDCProvider.AuthenticateAccessToken(ctx.Request.Context(),
			bearerCredential(ctx.GetHeader("Authorization")), "mcp.invoke")
		if err != nil {
			ctx.Header("WWW-Authenticate", "Bearer")
			httpx.Fail(ctx, http.StatusUnauthorized, "invalid_token", "请使用具有 MCP 调用权限的员工访问令牌")
			return
		}
		items, err := deps.MCPConnections.ListForUser(ctx.Request.Context(), subject.OrganizationID, subject.ID)
		if err != nil {
			handleMCPConnectionError(ctx, deps, err)
			return
		}
		data := make([]gin.H, 0, len(items))
		for _, item := range items {
			data = append(data, gin.H{
				"code": item.Code, "name": item.Name, "serviceName": item.ServiceName,
				"url":       mcpGatewayURL(deps, item.Code),
				"transport": "streamable_http",
			})
		}
		ctx.Header("Cache-Control", "no-store")
		httpx.OK(ctx, http.StatusOK, data)
	}
}

func mcpGatewayURL(deps Dependencies, code string) string {
	return strings.TrimRight(deps.OIDCProvider.Issuer(), "/") + "/mcp/" + code
}

func mcpGatewayHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		route, caller, ok := authorizedMCPGatewayRoute(ctx, deps)
		if !ok {
			return
		}
		if len(route.Tools) == 0 && !supportsMCPContent(deps) {
			httpx.Fail(ctx, http.StatusConflict, "mcp_tools_unavailable", "此连接的服务尚未发现工具，请管理员先刷新工具列表")
			return
		}
		ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, 4*1024*1024)
		includeContent := false
		requestedMethod := ""
		if ctx.Request.Method == http.MethodPost {
			body, readErr := io.ReadAll(ctx.Request.Body)
			if readErr != nil {
				httpx.Fail(ctx, http.StatusRequestEntityTooLarge, "mcp_request_too_large", "MCP 请求内容过大")
				return
			}
			var envelope struct {
				Method string `json:"method"`
			}
			_ = json.Unmarshal(body, &envelope)
			requestedMethod = envelope.Method
			includeContent = mcpMethodNeedsCatalog(envelope.Method)
			ctx.Request.Body = io.NopCloser(bytes.NewReader(body))
		}
		server, err := buildMCPGatewayServer(ctx.Request.Context(), deps, route, caller, includeContent, requestedMethod)
		if err != nil {
			if errors.Is(err, mcpadmin.ErrUpstream) {
				httpx.Fail(ctx, http.StatusBadGateway, "mcp_upstream_error", "读取上游 MCP 目录失败")
				return
			}
			httpx.Fail(ctx, http.StatusInternalServerError, "invalid_mcp_tool", "MCP 工具配置无效")
			return
		}
		mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server },
			&mcp.StreamableHTTPOptions{Stateless: true}).ServeHTTP(ctx.Writer, ctx.Request)
	}
}

func authorizedMCPGatewayRoute(ctx *gin.Context, deps Dependencies) (mcpconnection.GatewayConnection, mcpconnection.Caller, bool) {
	if origin := ctx.GetHeader("Origin"); origin != "" {
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Host == "" || parsed.User != nil || !strings.EqualFold(parsed.Host, ctx.Request.Host) {
			httpx.Fail(ctx, http.StatusForbidden, "mcp_origin_forbidden", "MCP 请求来源不受信任")
			return mcpconnection.GatewayConnection{}, mcpconnection.Caller{}, false
		}
	}
	route, err := deps.MCPConnections.Lookup(ctx.Request.Context(), ctx.Param("code"))
	if err != nil {
		handleMCPConnectionError(ctx, deps, err)
		return mcpconnection.GatewayConnection{}, mcpconnection.Caller{}, false
	}
	credential := bearerCredential(ctx.GetHeader("Authorization"))
	var caller mcpconnection.Caller
	if route.Purpose == "harness" {
		subject, tokenErr := deps.OIDCProvider.AuthenticateAccessToken(ctx.Request.Context(), credential, "mcp.invoke")
		if tokenErr != nil {
			ctx.Header("WWW-Authenticate", "Bearer")
			httpx.Fail(ctx, http.StatusUnauthorized, "invalid_token", "请使用具有 MCP 调用权限的员工访问令牌")
			return mcpconnection.GatewayConnection{}, mcpconnection.Caller{}, false
		}
		caller, err = deps.MCPConnections.AuthorizeUser(ctx.Request.Context(), route, subject.OrganizationID, subject.ID)
	} else {
		caller, err = deps.MCPConnections.AuthorizeDeveloper(ctx.Request.Context(), route, credential)
	}
	if err != nil {
		handleMCPConnectionError(ctx, deps, err)
		return mcpconnection.GatewayConnection{}, mcpconnection.Caller{}, false
	}
	return route, caller, true
}

func mcpMethodNeedsCatalog(method string) bool {
	return method == "initialize" || method == "server/discover" || strings.HasPrefix(method, "resources/") || strings.HasPrefix(method, "prompts/") || strings.HasPrefix(method, "skills/")
}

func buildMCPGatewayServer(ctx context.Context, deps Dependencies, route mcpconnection.GatewayConnection, caller mcpconnection.Caller, includeContent bool, requestedMethod string) (*mcp.Server, error) {
	service, credentials := mcpGatewayUpstream(route)
	capabilities := &mcp.ServerCapabilities{Tools: &mcp.ToolCapabilities{}, Resources: &mcp.ResourceCapabilities{}, Prompts: &mcp.PromptCapabilities{}}
	var skillCaller MCPSkillCaller
	if includeContent {
		if candidate, ok := deps.MCPToolCaller.(MCPSkillCaller); ok {
			probeCtx, stop := context.WithTimeout(ctx, 10*time.Second)
			supported, err := candidate.SupportsSkills(probeCtx, service, credentials)
			stop()
			if err != nil {
				deps.Logger.Warn("检查上游 MCP Skills 能力失败", "error", err, "connection_id", route.ID)
				if strings.HasPrefix(requestedMethod, "skills/") {
					return nil, mcpadmin.ErrUpstream
				}
			} else if supported {
				capabilities.AddExtension(mcpclient.SkillsExtension, nil)
				skillCaller = candidate
			}
		}
	}
	server := mcp.NewServer(&mcp.Implementation{Name: route.Name, Version: "1.0.0"},
		&mcp.ServerOptions{Capabilities: capabilities})
	if skillCaller != nil {
		if err := registerMCPGatewaySkills(server, deps, route, skillCaller); err != nil {
			return nil, err
		}
	}
	for _, tool := range route.Tools {
		var schema map[string]any
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil || schema["type"] != "object" {
			deps.Logger.Error("MCP 工具快照结构无效", "service_id", route.ServiceID, "tool", tool.Name)
			return nil, errors.New("MCP 工具配置无效")
		}
		name := tool.Name
		server.AddTool(&mcp.Tool{Name: name, Description: tool.Description, InputSchema: schema},
			func(callCtx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				started := time.Now()
				upstreamCtx, stopUpstream := context.WithTimeout(callCtx, 60*time.Second)
				defer stopUpstream()
				currentService, currentCredentials, callErr := mcpGatewayCurrentUpstream(upstreamCtx, deps, route)
				var raw json.RawMessage
				if callErr == nil {
					raw, callErr = deps.MCPToolCaller.CallTool(upstreamCtx, currentService, currentCredentials, name, request.Params.Arguments)
				}
				record := mcpconnection.CallRecord{
					ConnectionID: route.ID, OrganizationID: route.OrganizationID, ServiceID: route.ServiceID,
					ToolName: name, UserID: caller.UserID, DepartmentID: caller.DepartmentID,
					Result: "success", LatencyMS: time.Since(started).Milliseconds(),
				}
				var result mcp.CallToolResult
				if callErr != nil {
					record.Result = "failed"
				} else if err := json.Unmarshal(raw, &result); err != nil {
					record.Result = "failed"
					callErr = err
				} else if result.IsError {
					record.Result = "failed"
				}
				recordCtx, cancel := context.WithTimeout(context.WithoutCancel(callCtx), 3*time.Second)
				defer cancel()
				if err := deps.MCPConnections.RecordCall(recordCtx, record); err != nil {
					deps.Logger.Error("记录 MCP 工具调用失败", "error", err, "connection_id", route.ID)
					return nil, errors.New("记录 MCP 工具调用失败")
				}
				if callErr != nil {
					deps.Logger.Warn("上游 MCP 工具调用失败", "error", callErr, "connection_id", route.ID, "tool", name)
					return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "上游 MCP 工具调用失败"}}}, nil
				}
				return &result, nil
			})
	}
	if content, ok := deps.MCPToolCaller.(MCPContentCaller); ok && includeContent {
		catalogCtx, stopCatalog := context.WithTimeout(ctx, 15*time.Second)
		resources, templates, resourceErr := content.ListResources(catalogCtx, service, credentials)
		prompts, promptErr := content.ListPrompts(catalogCtx, service, credentials)
		stopCatalog()
		if resourceErr != nil {
			deps.Logger.Warn("读取上游 MCP 资源目录失败", "error", resourceErr, "connection_id", route.ID)
		}
		if promptErr != nil {
			deps.Logger.Warn("读取上游 MCP 提示词目录失败", "error", promptErr, "connection_id", route.ID)
		}
		if (resourceErr != nil && strings.HasPrefix(requestedMethod, "resources/")) ||
			(promptErr != nil && strings.HasPrefix(requestedMethod, "prompts/")) ||
			(len(route.Tools) == 0 && resourceErr != nil && promptErr != nil && skillCaller == nil) {
			return nil, mcpadmin.ErrUpstream
		}
		if resourceErr == nil {
			for _, resource := range resources {
				server.AddResource(resource, func(callCtx context.Context, request *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
					upstreamCtx, stop := context.WithTimeout(callCtx, 30*time.Second)
					defer stop()
					currentService, currentCredentials, err := mcpGatewayCurrentUpstream(upstreamCtx, deps, route)
					if err != nil {
						return nil, err
					}
					return content.ReadResource(upstreamCtx, currentService, currentCredentials, request.Params.URI)
				})
			}
			for _, template := range templates {
				server.AddResourceTemplate(template, func(callCtx context.Context, request *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
					upstreamCtx, stop := context.WithTimeout(callCtx, 30*time.Second)
					defer stop()
					currentService, currentCredentials, err := mcpGatewayCurrentUpstream(upstreamCtx, deps, route)
					if err != nil {
						return nil, err
					}
					return content.ReadResource(upstreamCtx, currentService, currentCredentials, request.Params.URI)
				})
			}
		}
		if promptErr == nil {
			for _, prompt := range prompts {
				server.AddPrompt(prompt, func(callCtx context.Context, request *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
					upstreamCtx, stop := context.WithTimeout(callCtx, 30*time.Second)
					defer stop()
					currentService, currentCredentials, err := mcpGatewayCurrentUpstream(upstreamCtx, deps, route)
					if err != nil {
						return nil, err
					}
					return content.GetPrompt(upstreamCtx, currentService, currentCredentials, request.Params.Name, request.Params.Arguments)
				})
			}
		}
	}
	return server, nil
}

func mcpGatewayUpstream(route mcpconnection.GatewayConnection) (mcpadmin.ServiceRecord, mcpadmin.UpstreamCredentials) {
	service := mcpadmin.ServiceRecord{ID: route.ServiceID, UpstreamURL: route.UpstreamURL, Transport: route.Transport,
		SourceType: route.SourceType, RuntimeType: route.RuntimeType, EntryFile: route.EntryFile,
		ContainerName: route.ContainerName, DeploymentStatus: route.DeploymentStatus}
	credentials := mcpadmin.UpstreamCredentials{
		Authorization: route.UpstreamAuthorization,
		HeaderName:    route.UpstreamHeaderName, HeaderValue: route.UpstreamHeaderValue,
		QueryName: route.UpstreamQueryName, QueryValue: route.UpstreamQueryValue,
		Parameters: route.UpstreamParameters,
	}
	return service, mcpadmin.MergeCredentials(mcpadmin.UpstreamCredentials{Parameters: route.ServiceParameters}, credentials)
}

func mcpGatewayCurrentUpstream(ctx context.Context, deps Dependencies, original mcpconnection.GatewayConnection) (mcpadmin.ServiceRecord, mcpadmin.UpstreamCredentials, error) {
	current, err := deps.MCPConnections.Lookup(ctx, original.Code)
	if err != nil || current.ID != original.ID {
		return mcpadmin.ServiceRecord{}, mcpadmin.UpstreamCredentials{}, errors.New("MCP 连接已失效")
	}
	service, credentials := mcpGatewayUpstream(current)
	return service, credentials, nil
}

func supportsMCPContent(deps Dependencies) bool {
	_, ok := deps.MCPToolCaller.(MCPContentCaller)
	return ok
}

func handleMCPConnectionError(ctx *gin.Context, deps Dependencies, err error) {
	switch {
	case errors.Is(err, mcpconnection.ErrInvalidInput):
		httpx.Fail(ctx, http.StatusBadRequest, "invalid_mcp_connection", "MCP 连接参数不正确")
	case errors.Is(err, mcpconnection.ErrNotFound), errors.Is(err, mcpadmin.ErrNotFound):
		httpx.Fail(ctx, http.StatusNotFound, "mcp_connection_not_found", "MCP 连接或服务不存在")
	case errors.Is(err, mcpconnection.ErrConflict):
		httpx.Fail(ctx, http.StatusConflict, "mcp_connection_conflict", "MCP 连接编码已存在")
	case errors.Is(err, mcpconnection.ErrInactive), errors.Is(err, mcpadmin.ErrDisabled):
		httpx.Fail(ctx, http.StatusConflict, "mcp_connection_inactive", "MCP 连接或所属服务不可用")
	case errors.Is(err, mcpconnection.ErrUnauthorized):
		ctx.Header("WWW-Authenticate", "Bearer")
		httpx.Fail(ctx, http.StatusUnauthorized, "invalid_mcp_credential", "MCP 连接凭据无效")
	case errors.Is(err, mcpconnection.ErrForbidden):
		httpx.Fail(ctx, http.StatusForbidden, "mcp_forbidden", "当前员工无权使用此 MCP 连接")
	case errors.Is(err, mcpconnection.ErrRateLimited):
		httpx.Fail(ctx, http.StatusTooManyRequests, "mcp_rate_limited", "MCP 连接请求过于频繁")
	case errors.Is(err, mcpconnection.ErrSecretUnavailable):
		httpx.Fail(ctx, http.StatusConflict, "mcp_secret_unavailable", "此连接没有可查看的入口密钥")
	default:
		deps.Logger.Error("MCP 连接操作失败", "error", err, "request_id", contextString(ctx, "request_id"))
		httpx.Fail(ctx, http.StatusInternalServerError, "internal_error", "服务暂时不可用")
	}
}
