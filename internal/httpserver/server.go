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
	AppName               string
	Environment           string
	Logger                *slog.Logger
	Database              HealthChecker
	Cache                 HealthChecker
	Auth                  AuthService
	Access                AuthorizationService
	UserAdmin             UserAdminService
	RoleAdmin             RoleAdminService
	DepartmentAdmin       DepartmentAdminService
	OrganizationAdmin     OrganizationAdminService
	AuditEvents           AuditEventService
	ModelAdmin            ModelAdminService
	ModelGateway          ModelGatewayService
	ModelPolicy           ModelPolicyService
	MCPAdmin              MCPAdminService
	MCPDeploy             MCPDeploymentService
	MCPConnections        MCPConnectionService
	MCPToolCaller         MCPToolCaller
	Skills                SkillService
	GatewayForwarder      ModelGatewayForwarder
	OIDCAdmin             OIDCAdminService
	OIDCProvider          OIDCProviderService
	ConsoleURL            string
	AuditRecorder         AuditRecorder
	PlatformAdmin         PlatformAdminService
	SessionCookie         string
	PlatformSessionCookie string
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
	_ = router.SetTrustedProxies(nil)
	router.Use(requestID(), accessLog(deps.Logger), gin.Recovery())

	router.GET("/healthz", func(ctx *gin.Context) {
		httpx.OK(ctx, http.StatusOK, gin.H{"status": "ok"})
	})
	router.GET("/readyz", readinessHandler(deps))
	router.GET("/api/v1/system/bootstrap", bootstrapHandler(deps))
	if deps.Auth != nil {
		registerAuthRoutes(router, deps)
	}
	if deps.PlatformAdmin != nil {
		registerPlatformRoutes(router, deps)
	}
	if deps.Auth != nil && deps.Access != nil && deps.UserAdmin != nil {
		registerUserRoutes(router, deps)
	}
	if deps.Auth != nil && deps.Access != nil && deps.RoleAdmin != nil {
		registerRoleRoutes(router, deps)
	}
	if deps.Auth != nil && deps.Access != nil && deps.DepartmentAdmin != nil {
		registerDepartmentRoutes(router, deps)
	}
	if deps.Auth != nil && deps.Access != nil && deps.OrganizationAdmin != nil {
		registerOrganizationRoutes(router, deps)
	}
	if deps.Auth != nil && deps.Access != nil && deps.AuditEvents != nil {
		registerAuditRoutes(router, deps)
	}
	if deps.Auth != nil && deps.Access != nil && deps.ModelAdmin != nil {
		registerModelRoutes(router, deps)
	}
	if deps.Auth != nil && deps.Access != nil && deps.ModelGateway != nil && deps.GatewayForwarder != nil {
		registerModelGatewayRoutes(router, deps)
	}
	if deps.Auth != nil && deps.Access != nil && deps.ModelPolicy != nil {
		registerModelPolicyRoutes(router, deps)
	}
	if deps.Auth != nil && deps.Access != nil && deps.MCPAdmin != nil {
		registerMCPRoutes(router, deps)
	}
	if deps.Auth != nil && deps.Access != nil && deps.MCPDeploy != nil {
		registerMCPDeploymentRoutes(router, deps)
	}
	if deps.Auth != nil && deps.Access != nil && deps.MCPConnections != nil && deps.MCPToolCaller != nil && deps.OIDCProvider != nil {
		registerMCPConnectionRoutes(router, deps)
	}
	if deps.Auth != nil && deps.Access != nil && deps.Skills != nil && deps.OIDCProvider != nil {
		registerSkillRoutes(router, deps)
	}
	if deps.Auth != nil && deps.Access != nil && deps.OIDCAdmin != nil {
		registerOIDCAdminRoutes(router, deps)
	}
	if deps.Auth != nil && deps.OIDCProvider != nil {
		registerOIDCProviderRoutes(router, deps)
	}
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
			Phase:       "model-access",
			Services: map[string]bool{
				"api":      true,
				"database": deps.Database != nil,
				"redis":    deps.Cache != nil,
			},
			Modules: []moduleStatus{
				{Key: "identity", Name: "身份与会话", Status: "ready"},
				{Key: "access", Name: "组织与权限", Status: "ready"},
				{Key: "models", Name: "模型目录", Status: "ready"},
				{Key: "gateway", Name: "模型网关", Status: "building"},
				{Key: "oidc", Name: "统一登录", Status: "ready"},
				{Key: "audit", Name: "审计与追踪", Status: "ready"},
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
