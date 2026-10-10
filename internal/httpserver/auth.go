package httpserver

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"harness-admin/internal/httpx"
	"harness-admin/internal/identity"
)

const sessionContextKey = "identity_session"
const csrfCookieName = "harness_admin_csrf"

type AuthService interface {
	Login(context.Context, identity.LoginInput) (identity.LoginResult, error)
	Authenticate(context.Context, string) (identity.Session, error)
	ValidateCSRF(identity.Session, string) error
	Logout(context.Context, identity.Session, string, string) error
	ChangePassword(context.Context, identity.ChangePasswordInput) (int64, error)
	ListSessions(context.Context, identity.Session) ([]identity.UserSession, error)
	RevokeManagedSession(context.Context, identity.Session, string, string, string) error
	RevokeOtherSessions(context.Context, identity.Session, string, string) (int64, error)
}

type loginRequest struct {
	OrganizationCode string `json:"organizationCode"`
	Username         string `json:"username"`
	Password         string `json:"password"`
}

type userResponse struct {
	ID               string   `json:"id"`
	OrganizationID   string   `json:"organizationId"`
	Organization     string   `json:"organization"`
	OrganizationCode string   `json:"organizationCode"`
	Username         string   `json:"username"`
	Email            string   `json:"email,omitempty"`
	DisplayName      string   `json:"displayName"`
	PermissionCodes  []string `json:"permissionCodes"`
}

type loginResponse struct {
	User      userResponse `json:"user"`
	CSRFToken string       `json:"csrfToken"`
	ExpiresAt time.Time    `json:"expiresAt"`
}

type changePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

type userSessionResponse struct {
	ID         string    `json:"id"`
	UserAgent  string    `json:"userAgent"`
	SourceIP   string    `json:"sourceIp,omitempty"`
	ExpiresAt  time.Time `json:"expiresAt"`
	LastSeenAt time.Time `json:"lastSeenAt"`
	CreatedAt  time.Time `json:"createdAt"`
	Current    bool      `json:"current"`
}

func registerAuthRoutes(router *gin.Engine, deps Dependencies) {
	group := router.Group("/api/v1/auth")
	group.POST("/login", loginHandler(deps))
	group.GET("/me", requireSession(deps), meHandler())
	group.POST("/logout", requireSession(deps), requireCSRF(deps), logoutHandler(deps))
	group.PUT("/password", requireSession(deps), requireCSRF(deps), changePasswordHandler(deps))
	group.GET("/sessions", requireSession(deps), listSessionsHandler(deps))
	group.DELETE("/sessions/:id", requireSession(deps), requireCSRF(deps), revokeSessionHandler(deps))
	group.POST("/sessions/revoke-others", requireSession(deps), requireCSRF(deps), revokeOtherSessionsHandler(deps))
}

func loginHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, 64*1024)
		var request loginRequest
		if err := ctx.ShouldBindJSON(&request); err != nil {
			httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", "登录参数格式不正确")
			return
		}
		if len(request.Password) > 1024 {
			httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", "登录参数格式不正确")
			return
		}

		result, err := deps.Auth.Login(ctx.Request.Context(), identity.LoginInput{
			OrganizationCode: request.OrganizationCode,
			Username:         request.Username,
			Password:         request.Password,
			UserAgent:        ctx.Request.UserAgent(),
			SourceIP:         ctx.ClientIP(),
			RequestID:        contextString(ctx, "request_id"),
		})
		if err != nil {
			handleAuthError(ctx, deps.Logger, err)
			return
		}

		setSessionCookie(ctx, deps, result.Token, result.ExpiresAt)
		setCSRFCookie(ctx, deps, result.CSRFToken, result.ExpiresAt)
		httpx.OK(ctx, http.StatusOK, loginResponse{
			User:      presentUser(result.User),
			CSRFToken: result.CSRFToken,
			ExpiresAt: result.ExpiresAt,
		})
	}
}

func meHandler() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		session := currentSession(ctx)
		httpx.OK(ctx, http.StatusOK, presentUser(session.User))
	}
}

func logoutHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		session := currentSession(ctx)
		if err := deps.Auth.Logout(
			ctx.Request.Context(),
			session,
			contextString(ctx, "request_id"),
			ctx.ClientIP(),
		); err != nil {
			handleAuthError(ctx, deps.Logger, err)
			return
		}
		clearSessionCookie(ctx, deps)
		clearCSRFCookie(ctx, deps)
		httpx.OK(ctx, http.StatusOK, gin.H{"loggedOut": true})
	}
}

func changePasswordHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, 8*1024)
		var request changePasswordRequest
		if err := ctx.ShouldBindJSON(&request); err != nil || len(request.CurrentPassword) > 1024 || len(request.NewPassword) > 1024 {
			httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", "密码参数格式不正确")
			return
		}
		revoked, err := deps.Auth.ChangePassword(ctx.Request.Context(), identity.ChangePasswordInput{
			Session:         currentSession(ctx),
			CurrentPassword: request.CurrentPassword,
			NewPassword:     request.NewPassword,
			RequestID:       contextString(ctx, "request_id"),
			SourceIP:        ctx.ClientIP(),
		})
		if err != nil {
			handleAccountSecurityError(ctx, deps.Logger, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, gin.H{"changed": true, "revokedSessions": revoked})
	}
}

func listSessionsHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		items, err := deps.Auth.ListSessions(ctx.Request.Context(), currentSession(ctx))
		if err != nil {
			handleAccountSecurityError(ctx, deps.Logger, err)
			return
		}
		response := make([]userSessionResponse, 0, len(items))
		for _, item := range items {
			response = append(response, userSessionResponse{
				ID: item.ID, UserAgent: item.UserAgent, SourceIP: item.SourceIP,
				ExpiresAt: item.ExpiresAt, LastSeenAt: item.LastSeenAt,
				CreatedAt: item.CreatedAt, Current: item.Current,
			})
		}
		httpx.OK(ctx, http.StatusOK, response)
	}
}

func revokeSessionHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if _, err := uuid.Parse(ctx.Param("id")); err != nil {
			httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", "会话标识格式不正确")
			return
		}
		if err := deps.Auth.RevokeManagedSession(ctx.Request.Context(), currentSession(ctx), ctx.Param("id"),
			contextString(ctx, "request_id"), ctx.ClientIP()); err != nil {
			handleAccountSecurityError(ctx, deps.Logger, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, gin.H{"revoked": true})
	}
}

func revokeOtherSessionsHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		revoked, err := deps.Auth.RevokeOtherSessions(ctx.Request.Context(), currentSession(ctx),
			contextString(ctx, "request_id"), ctx.ClientIP())
		if err != nil {
			handleAccountSecurityError(ctx, deps.Logger, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, gin.H{"revokedSessions": revoked})
	}
}

func handleAccountSecurityError(ctx *gin.Context, logger *slog.Logger, err error) {
	switch {
	case errors.Is(err, identity.ErrInvalidPassword):
		httpx.Fail(ctx, http.StatusBadRequest, "invalid_password", err.Error())
	case errors.Is(err, identity.ErrCurrentPassword):
		httpx.Fail(ctx, http.StatusBadRequest, "current_password_incorrect", err.Error())
	case errors.Is(err, identity.ErrPasswordUnchanged):
		httpx.Fail(ctx, http.StatusConflict, "password_unchanged", err.Error())
	case errors.Is(err, identity.ErrCannotRevokeCurrent):
		httpx.Fail(ctx, http.StatusConflict, "cannot_revoke_current_session", err.Error())
	case errors.Is(err, identity.ErrSessionNotFound):
		httpx.Fail(ctx, http.StatusNotFound, "session_not_found", "会话不存在或已经失效")
	default:
		logger.Error("账号安全接口处理失败", "error", err, "request_id", contextString(ctx, "request_id"))
		httpx.Fail(ctx, http.StatusInternalServerError, "internal_error", "服务暂时不可用")
	}
}

func requireSession(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		token, err := ctx.Cookie(deps.SessionCookie)
		if err != nil {
			clearSessionCookie(ctx, deps)
			clearCSRFCookie(ctx, deps)
			httpx.Fail(ctx, http.StatusUnauthorized, "unauthorized", "请先登录")
			return
		}
		session, err := deps.Auth.Authenticate(ctx.Request.Context(), token)
		if err != nil {
			clearSessionCookie(ctx, deps)
			clearCSRFCookie(ctx, deps)
			handleAuthError(ctx, deps.Logger, err)
			return
		}
		ctx.Set(sessionContextKey, session)
		ctx.Next()
	}
}

func requireCSRF(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if err := deps.Auth.ValidateCSRF(currentSession(ctx), ctx.GetHeader("X-CSRF-Token")); err != nil {
			handleAuthError(ctx, deps.Logger, err)
			return
		}
		ctx.Next()
	}
}

func handleAuthError(ctx *gin.Context, logger *slog.Logger, err error) {
	switch {
	case errors.Is(err, identity.ErrInvalidCredentials):
		httpx.Fail(ctx, http.StatusUnauthorized, "invalid_credentials", "企业代码、用户名或密码错误")
	case errors.Is(err, identity.ErrAccountDisabled):
		httpx.Fail(ctx, http.StatusForbidden, "account_unavailable", "账号当前不可用")
	case errors.Is(err, identity.ErrAccountLocked):
		httpx.Fail(ctx, http.StatusLocked, "account_locked", err.Error())
	case errors.Is(err, identity.ErrTooManyAttempts):
		ctx.Header("Retry-After", "900")
		httpx.Fail(ctx, http.StatusTooManyRequests, "too_many_attempts", "登录失败次数过多，请稍后再试")
	case errors.Is(err, identity.ErrUnauthorized), errors.Is(err, identity.ErrSessionNotFound):
		httpx.Fail(ctx, http.StatusUnauthorized, "unauthorized", "登录状态已失效，请重新登录")
	case errors.Is(err, identity.ErrInvalidCSRF):
		httpx.Fail(ctx, http.StatusForbidden, "invalid_csrf", "请求校验失败，请刷新页面后重试")
	default:
		logger.Error("身份接口处理失败", "error", err, "request_id", contextString(ctx, "request_id"))
		httpx.Fail(ctx, http.StatusInternalServerError, "internal_error", "服务暂时不可用")
	}
}

func presentUser(user identity.User) userResponse {
	permissionCodes := user.PermissionCodes
	if permissionCodes == nil {
		permissionCodes = []string{}
	}
	return userResponse{
		ID:               user.ID,
		OrganizationID:   user.OrganizationID,
		Organization:     user.Organization,
		OrganizationCode: user.OrganizationCode,
		Username:         user.Username,
		Email:            user.Email,
		DisplayName:      user.DisplayName,
		PermissionCodes:  permissionCodes,
	}
}

func currentSession(ctx *gin.Context) identity.Session {
	value, exists := ctx.Get(sessionContextKey)
	if !exists {
		return identity.Session{}
	}
	session, _ := value.(identity.Session)
	return session
}

func setSessionCookie(ctx *gin.Context, deps Dependencies, token string, expiresAt time.Time) {
	http.SetCookie(ctx.Writer, &http.Cookie{
		Name:     deps.SessionCookie,
		Value:    token,
		Path:     "/api",
		Expires:  expiresAt,
		MaxAge:   int(time.Until(expiresAt).Seconds()),
		HttpOnly: true,
		Secure:   deps.Environment == "production",
		SameSite: http.SameSiteLaxMode,
	})
}

func clearSessionCookie(ctx *gin.Context, deps Dependencies) {
	http.SetCookie(ctx.Writer, &http.Cookie{
		Name:     deps.SessionCookie,
		Value:    "",
		Path:     "/api",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   deps.Environment == "production",
		SameSite: http.SameSiteLaxMode,
	})
}

func setCSRFCookie(ctx *gin.Context, deps Dependencies, token string, expiresAt time.Time) {
	http.SetCookie(ctx.Writer, &http.Cookie{
		Name:     csrfCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		MaxAge:   int(time.Until(expiresAt).Seconds()),
		HttpOnly: false,
		Secure:   deps.Environment == "production",
		SameSite: http.SameSiteLaxMode,
	})
}

func clearCSRFCookie(ctx *gin.Context, deps Dependencies) {
	http.SetCookie(ctx.Writer, &http.Cookie{
		Name:     csrfCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: false,
		Secure:   deps.Environment == "production",
		SameSite: http.SameSiteLaxMode,
	})
}

func contextString(ctx *gin.Context, key string) string {
	value, _ := ctx.Get(key)
	result, _ := value.(string)
	return strings.TrimSpace(result)
}
