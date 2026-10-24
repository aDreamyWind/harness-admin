package httpserver

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"harness-admin/internal/httpx"
	"harness-admin/internal/mcpadmin"
)

// mcpLegacyGateway serves the 2024-11-05 HTTP+SSE transport. Each open stream
// owns its MCP session; message POSTs are authorized again before entering it.
type mcpLegacyGateway struct {
	deps     Dependencies
	mu       sync.Mutex
	sessions map[string]*mcpLegacySession
}

type mcpLegacySession struct {
	transport *mcp.SSEServerTransport
	routeID   string
	userID    string
	closed    chan struct{}
	inFlight  chan struct{}
}

func newMCPLegacyGateway(deps Dependencies) *mcpLegacyGateway {
	return &mcpLegacyGateway{deps: deps, sessions: make(map[string]*mcpLegacySession)}
}

func (g *mcpLegacyGateway) stream(ctx *gin.Context) {
	route, caller, ok := authorizedMCPGatewayRoute(ctx, g.deps)
	if !ok {
		return
	}
	if len(route.Tools) == 0 && !supportsMCPContent(g.deps) {
		httpx.Fail(ctx, http.StatusConflict, "mcp_tools_unavailable", "此连接的服务尚未发现工具，请管理员先刷新工具列表")
		return
	}
	server, err := buildMCPGatewayServer(ctx.Request.Context(), g.deps, route, caller, true, "initialize")
	if err != nil {
		if errors.Is(err, mcpadmin.ErrUpstream) {
			httpx.Fail(ctx, http.StatusBadGateway, "mcp_upstream_error", "读取上游 MCP 目录失败")
			return
		}
		httpx.Fail(ctx, http.StatusInternalServerError, "invalid_mcp_tool", "MCP 工具配置无效")
		return
	}
	secret := make([]byte, 24)
	if _, err := rand.Read(secret); err != nil {
		httpx.Fail(ctx, http.StatusInternalServerError, "internal_error", "服务暂时不可用")
		return
	}
	sessionID := base64.RawURLEncoding.EncodeToString(secret)
	endpoint := "/mcp/" + route.Code + "/messages?sessionid=" + sessionID
	session := &mcpLegacySession{
		transport: &mcp.SSEServerTransport{Endpoint: endpoint, Response: ctx.Writer},
		routeID:   route.ID, userID: caller.UserID,
		closed: make(chan struct{}), inFlight: make(chan struct{}, 8),
	}
	g.mu.Lock()
	if len(g.sessions) >= 256 {
		g.mu.Unlock()
		httpx.Fail(ctx, http.StatusTooManyRequests, "mcp_sse_sessions_full", "SSE 会话数量已达上限")
		return
	}
	g.sessions[sessionID] = session
	g.mu.Unlock()
	defer func() {
		g.mu.Lock()
		delete(g.sessions, sessionID)
		close(session.closed)
		g.mu.Unlock()
	}()

	ctx.Header("Content-Type", "text/event-stream")
	ctx.Header("Cache-Control", "no-cache, no-store")
	ctx.Header("X-Accel-Buffering", "no")
	ctx.Header("Connection", "keep-alive")
	streamCtx, cancel := context.WithTimeout(ctx.Request.Context(), 30*time.Minute)
	defer cancel()
	serverSession, err := server.Connect(streamCtx, session.transport, nil)
	if err != nil {
		g.deps.Logger.Warn("建立 MCP SSE 会话失败", "error", err, "connection_id", route.ID)
		return
	}
	defer serverSession.Close()
	<-streamCtx.Done()
}

func (g *mcpLegacyGateway) message(ctx *gin.Context) {
	route, caller, ok := authorizedMCPGatewayRoute(ctx, g.deps)
	if !ok {
		return
	}
	sessionID := ctx.Query("sessionid")
	if sessionID == "" {
		httpx.Fail(ctx, http.StatusBadRequest, "mcp_sse_session_required", "缺少 SSE 会话标识")
		return
	}
	g.mu.Lock()
	session := g.sessions[sessionID]
	g.mu.Unlock()
	if session == nil || session.routeID != route.ID || session.userID != caller.UserID {
		httpx.Fail(ctx, http.StatusNotFound, "mcp_sse_session_not_found", "SSE 会话不存在或已结束")
		return
	}
	select {
	case <-session.closed:
		httpx.Fail(ctx, http.StatusNotFound, "mcp_sse_session_not_found", "SSE 会话不存在或已结束")
		return
	case session.inFlight <- struct{}{}:
		defer func() { <-session.inFlight }()
	default:
		httpx.Fail(ctx, http.StatusTooManyRequests, "mcp_sse_session_busy", "SSE 会话请求过多")
		return
	}
	ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, 4*1024*1024)
	session.transport.ServeHTTP(ctx.Writer, ctx.Request)
}
