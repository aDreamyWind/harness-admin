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
	"harness-admin/internal/platformadmin"
)

const platformSessionContextKey = "platform_session"
const platformCSRFCookieName = "harness_platform_csrf"

type PlatformAdminService interface {
	Login(context.Context, platformadmin.LoginInput) (platformadmin.LoginResult, error)
	Authenticate(context.Context, string) (platformadmin.Session, error)
	ValidateCSRF(platformadmin.Session, string) error
	Logout(context.Context, platformadmin.Session, string, string) error
	ListOrganizations(context.Context, platformadmin.Session) ([]platformadmin.Organization, error)
	CreateOrganization(context.Context, platformadmin.CreateOrganizationInput) (platformadmin.Organization, error)
	UpdateOrganizationStatus(context.Context, platformadmin.Session, string, string, string, string) (platformadmin.Organization, error)
}

type platformLoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type createPlatformOrganizationRequest struct {
	Code             string `json:"code"`
	Name             string `json:"name"`
	AdminUsername    string `json:"adminUsername"`
	AdminDisplayName string `json:"adminDisplayName"`
	AdminEmail       string `json:"adminEmail"`
	AdminPassword    string `json:"adminPassword"`
}

type updatePlatformOrganizationStatusRequest struct {
	Status string `json:"status"`
}

type platformAdminResponse struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
}

type platformLoginResponse struct {
	Admin     platformAdminResponse `json:"admin"`
	CSRFToken string                `json:"csrfToken"`
	ExpiresAt time.Time             `json:"expiresAt"`
}

type platformOrganizationResponse struct {
	ID            string    `json:"id"`
	Code          string    `json:"code"`
	Name          string    `json:"name"`
	Status        string    `json:"status"`
	AdminUsername string    `json:"adminUsername"`
	UserCount     int64     `json:"userCount"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

func registerPlatformRoutes(router *gin.Engine, deps Dependencies) {
	group := router.Group("/api/v1/platform")
	group.POST("/auth/login", platformLoginHandler(deps))
	group.GET("/auth/me", requirePlatformSession(deps), platformMeHandler())
	group.POST("/auth/logout", requirePlatformSession(deps), requirePlatformCSRF(deps), platformLogoutHandler(deps))
	group.GET("/organizations", requirePlatformSession(deps), listPlatformOrganizationsHandler(deps))
	group.POST("/organizations", requirePlatformSession(deps), requirePlatformCSRF(deps), createPlatformOrganizationHandler(deps))
	group.PUT("/organizations/:id/status", requirePlatformSession(deps), requirePlatformCSRF(deps), updatePlatformOrganizationStatusHandler(deps))
}

func platformLoginHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, 16*1024)
		var request platformLoginRequest
		if err := ctx.ShouldBindJSON(&request); err != nil {
			httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", "登录参数格式不正确")
			return
		}
		result, err := deps.PlatformAdmin.Login(ctx.Request.Context(), platformadmin.LoginInput{
			Username: request.Username, Password: request.Password, UserAgent: ctx.Request.UserAgent(),
			SourceIP: ctx.ClientIP(), RequestID: contextString(ctx, "request_id"),
		})
		if err != nil {
			handlePlatformError(ctx, deps.Logger, err)
			return
		}
		setPlatformSessionCookie(ctx, deps, result.Token, result.ExpiresAt)
		setPlatformCSRFCookie(ctx, deps, result.CSRFToken, result.ExpiresAt)
		httpx.OK(ctx, http.StatusOK, platformLoginResponse{
			Admin:     platformAdminResponse{ID: result.Admin.ID, Username: result.Admin.Username, DisplayName: result.Admin.DisplayName},
			CSRFToken: result.CSRFToken, ExpiresAt: result.ExpiresAt,
		})
	}
}

func platformMeHandler() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		admin := currentPlatformSession(ctx).Admin
		httpx.OK(ctx, http.StatusOK, platformAdminResponse{ID: admin.ID, Username: admin.Username, DisplayName: admin.DisplayName})
	}
}

func platformLogoutHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if err := deps.PlatformAdmin.Logout(ctx.Request.Context(), currentPlatformSession(ctx), contextString(ctx, "request_id"), ctx.ClientIP()); err != nil {
			handlePlatformError(ctx, deps.Logger, err)
			return
		}
		clearPlatformCookies(ctx, deps)
		httpx.OK(ctx, http.StatusOK, gin.H{"loggedOut": true})
	}
}

func listPlatformOrganizationsHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		items, err := deps.PlatformAdmin.ListOrganizations(ctx.Request.Context(), currentPlatformSession(ctx))
		if err != nil {
			handlePlatformError(ctx, deps.Logger, err)
			return
		}
		response := make([]platformOrganizationResponse, 0, len(items))
		for _, item := range items {
			response = append(response, presentPlatformOrganization(item))
		}
		httpx.OK(ctx, http.StatusOK, response)
	}
}

func createPlatformOrganizationHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, 32*1024)
		var request createPlatformOrganizationRequest
		if err := ctx.ShouldBindJSON(&request); err != nil {
			httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", "企业参数格式不正确")
			return
		}
		organization, err := deps.PlatformAdmin.CreateOrganization(ctx.Request.Context(), platformadmin.CreateOrganizationInput{
			Session: currentPlatformSession(ctx), Code: request.Code, Name: request.Name,
			AdminUsername: request.AdminUsername, AdminDisplayName: request.AdminDisplayName,
			AdminEmail: request.AdminEmail, AdminPassword: request.AdminPassword,
			RequestID: contextString(ctx, "request_id"), SourceIP: ctx.ClientIP(),
		})
		if err != nil {
			handlePlatformError(ctx, deps.Logger, err)
			return
		}
		httpx.OK(ctx, http.StatusCreated, presentPlatformOrganization(organization))
	}
}

func updatePlatformOrganizationStatusHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if _, err := uuid.Parse(ctx.Param("id")); err != nil {
			httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", "企业标识格式不正确")
			return
		}
		var request updatePlatformOrganizationStatusRequest
		if err := ctx.ShouldBindJSON(&request); err != nil {
			httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", "企业状态参数格式不正确")
			return
		}
		organization, err := deps.PlatformAdmin.UpdateOrganizationStatus(ctx.Request.Context(), currentPlatformSession(ctx),
			ctx.Param("id"), request.Status, contextString(ctx, "request_id"), ctx.ClientIP())
		if err != nil {
			handlePlatformError(ctx, deps.Logger, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, presentPlatformOrganization(organization))
	}
}

func requirePlatformSession(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		token, err := ctx.Cookie(deps.PlatformSessionCookie)
		if err != nil {
			clearPlatformCookies(ctx, deps)
			httpx.Fail(ctx, http.StatusUnauthorized, "unauthorized", "请先登录平台管理")
			return
		}
		session, err := deps.PlatformAdmin.Authenticate(ctx.Request.Context(), token)
		if err != nil {
			clearPlatformCookies(ctx, deps)
			handlePlatformError(ctx, deps.Logger, err)
			return
		}
		ctx.Set(platformSessionContextKey, session)
		ctx.Next()
	}
}

func requirePlatformCSRF(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if err := deps.PlatformAdmin.ValidateCSRF(currentPlatformSession(ctx), ctx.GetHeader("X-CSRF-Token")); err != nil {
			handlePlatformError(ctx, deps.Logger, err)
			return
		}
		ctx.Next()
	}
}

func currentPlatformSession(ctx *gin.Context) platformadmin.Session {
	value, _ := ctx.Get(platformSessionContextKey)
	session, _ := value.(platformadmin.Session)
	return session
}

func handlePlatformError(ctx *gin.Context, logger *slog.Logger, err error) {
	switch {
	case errors.Is(err, platformadmin.ErrInvalidCredentials):
		httpx.Fail(ctx, http.StatusUnauthorized, "invalid_credentials", "用户名或密码错误")
	case errors.Is(err, platformadmin.ErrAccountDisabled):
		httpx.Fail(ctx, http.StatusForbidden, "account_unavailable", err.Error())
	case errors.Is(err, platformadmin.ErrTooManyAttempts):
		ctx.Header("Retry-After", "900")
		httpx.Fail(ctx, http.StatusTooManyRequests, "too_many_attempts", "登录失败次数过多，请稍后再试")
	case errors.Is(err, platformadmin.ErrUnauthorized):
		httpx.Fail(ctx, http.StatusUnauthorized, "unauthorized", "平台登录状态已失效，请重新登录")
	case errors.Is(err, platformadmin.ErrInvalidCSRF):
		httpx.Fail(ctx, http.StatusForbidden, "invalid_csrf", "请求校验失败，请刷新页面后重试")
	case errors.Is(err, platformadmin.ErrInvalidInput):
		httpx.Fail(ctx, http.StatusBadRequest, "invalid_organization", err.Error())
	case errors.Is(err, platformadmin.ErrOrganizationConflict):
		httpx.Fail(ctx, http.StatusConflict, "organization_conflict", err.Error())
	case errors.Is(err, platformadmin.ErrOrganizationNotFound):
		httpx.Fail(ctx, http.StatusNotFound, "organization_not_found", err.Error())
	default:
		logger.Error("平台管理接口处理失败", "error", err, "request_id", contextString(ctx, "request_id"))
		httpx.Fail(ctx, http.StatusInternalServerError, "internal_error", "服务暂时不可用")
	}
}

func presentPlatformOrganization(item platformadmin.Organization) platformOrganizationResponse {
	return platformOrganizationResponse{ID: item.ID, Code: item.Code, Name: item.Name, Status: item.Status,
		AdminUsername: item.AdminUsername, UserCount: item.UserCount, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt}
}

func setPlatformSessionCookie(ctx *gin.Context, deps Dependencies, token string, expiresAt time.Time) {
	http.SetCookie(ctx.Writer, &http.Cookie{Name: deps.PlatformSessionCookie, Value: token, Path: "/api", Expires: expiresAt,
		MaxAge: int(time.Until(expiresAt).Seconds()), HttpOnly: true, Secure: deps.Environment == "production", SameSite: http.SameSiteLaxMode})
}

func setPlatformCSRFCookie(ctx *gin.Context, deps Dependencies, token string, expiresAt time.Time) {
	http.SetCookie(ctx.Writer, &http.Cookie{Name: platformCSRFCookieName, Value: token, Path: "/", Expires: expiresAt,
		MaxAge: int(time.Until(expiresAt).Seconds()), Secure: deps.Environment == "production", SameSite: http.SameSiteLaxMode})
}

func clearPlatformCookies(ctx *gin.Context, deps Dependencies) {
	http.SetCookie(ctx.Writer, &http.Cookie{Name: deps.PlatformSessionCookie, Path: "/api", MaxAge: -1, HttpOnly: true,
		Secure: deps.Environment == "production", SameSite: http.SameSiteLaxMode})
	http.SetCookie(ctx.Writer, &http.Cookie{Name: platformCSRFCookieName, Path: "/", MaxAge: -1,
		Secure: deps.Environment == "production", SameSite: http.SameSiteLaxMode})
}
