package httpserver

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"harness-admin/internal/httpx"
)

type HealthChecker interface {
	Ping(context.Context) error
}

type Dependencies struct {
	AppName     string
	Environment string
	Logger      *slog.Logger
	Database    HealthChecker
	Cache       HealthChecker
}

type bootstrapResponse struct {
	Name        string          `json:"name"`
	Environment string          `json:"environment"`
	Phase       string          `json:"phase"`
	Services    map[string]bool `json:"services"`
	Modules     []moduleStatus  `json:"modules"`
}

type moduleStatus struct {
	Key    string `json:"key"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

func New(deps Dependencies) http.Handler {
	if deps.Environment == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()
	router.Use(requestID(), accessLog(deps.Logger), gin.Recovery())

	router.GET("/healthz", func(ctx *gin.Context) {
		httpx.OK(ctx, http.StatusOK, gin.H{"status": "ok"})
	})
	router.GET("/readyz", readinessHandler(deps))
	router.GET("/api/v1/system/bootstrap", bootstrapHandler(deps))
	router.NoRoute(func(ctx *gin.Context) {
		httpx.Fail(ctx, http.StatusNotFound, "route_not_found", "请求的接口不存在")
	})

	return router
}

func readinessHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		checks := map[string]string{"database": "up", "redis": "up"}
		ready := true

		checkCtx, cancel := context.WithTimeout(ctx.Request.Context(), 2*time.Second)
		defer cancel()

		if deps.Database == nil || deps.Database.Ping(checkCtx) != nil {
			checks["database"] = "down"
			ready = false
		}
		if deps.Cache == nil || deps.Cache.Ping(checkCtx) != nil {
			checks["redis"] = "down"
			ready = false
		}

		if !ready {
			httpx.Fail(ctx, http.StatusServiceUnavailable, "service_not_ready", "基础设施尚未就绪")
			return
		}
		httpx.OK(ctx, http.StatusOK, gin.H{"status": "ready", "checks": checks})
	}
}

func bootstrapHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		httpx.OK(ctx, http.StatusOK, bootstrapResponse{
			Name:        deps.AppName,
			Environment: deps.Environment,
			Phase:       "foundation",
			Services: map[string]bool{
				"api":      true,
				"database": deps.Database != nil,
				"redis":    deps.Cache != nil,
			},
			Modules: []moduleStatus{
				{Key: "identity", Name: "身份与会话", Status: "building"},
				{Key: "access", Name: "组织与权限", Status: "planned"},
				{Key: "gateway", Name: "模型网关", Status: "planned"},
				{Key: "audit", Name: "审计与追踪", Status: "building"},
			},
		})
	}
}

func requestID() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		id := ctx.GetHeader("X-Request-ID")
		if id == "" {
			id = uuid.NewString()
		}
		ctx.Set("request_id", id)
		ctx.Header("X-Request-ID", id)
		ctx.Next()
	}
}

func accessLog(logger *slog.Logger) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		startedAt := time.Now()
		ctx.Next()
		requestID, _ := ctx.Get("request_id")
		logger.Info("HTTP 请求",
			"method", ctx.Request.Method,
			"path", ctx.Request.URL.Path,
			"status", ctx.Writer.Status(),
			"duration_ms", time.Since(startedAt).Milliseconds(),
			"request_id", requestID,
		)
	}
}
