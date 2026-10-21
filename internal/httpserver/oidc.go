package httpserver

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"harness-admin/internal/httpx"
	"harness-admin/internal/identity"
	oidcmodule "harness-admin/internal/oidc"
)

type OIDCAdminService interface {
	ListClients(context.Context, string) ([]oidcmodule.Client, error)
	CreateClient(context.Context, oidcmodule.CreateClientInput) (oidcmodule.CreatedClient, error)
	UpdateClient(context.Context, oidcmodule.UpdateClientInput) (oidcmodule.Client, error)
	ChangeClientStatus(context.Context, oidcmodule.ChangeClientStatusInput) (oidcmodule.Client, error)
	RotateClientSecret(context.Context, oidcmodule.RotateClientSecretInput) (oidcmodule.CreatedClient, error)
	RevealClientSecret(context.Context, oidcmodule.RevealClientSecretInput) (string, error)
}

type OIDCProviderService interface {
	Issuer() string
	JWKS() map[string]any
	Authorize(context.Context, oidcmodule.AuthorizeInput) (oidcmodule.AuthorizationResult, error)
	DescribeAuthorization(context.Context, oidcmodule.AuthorizeInput) (oidcmodule.AuthorizationRequest, error)
	ApproveAuthorization(context.Context, oidcmodule.ConsentDecisionInput) (oidcmodule.AuthorizationResult, error)
	DenyAuthorization(context.Context, oidcmodule.ConsentDecisionInput) (oidcmodule.AuthorizationResult, error)
	ListUserConsents(context.Context, string, string) ([]oidcmodule.UserConsent, error)
	RevokeUserConsent(context.Context, oidcmodule.RevokeConsentInput) error
	ExchangeToken(context.Context, oidcmodule.TokenInput) (oidcmodule.TokenResult, error)
	UserInfo(context.Context, string) (oidcmodule.UserInfo, error)
	AuthenticateAccessToken(context.Context, string, string) (oidcmodule.Subject, error)
}

type oidcClientWriteRequest struct {
	Name            string   `json:"name"`
	ApplicationType string   `json:"applicationType"`
	RedirectURIs    []string `json:"redirectUris"`
	AllowedScopes   []string `json:"allowedScopes"`
}

type oidcClientStatusRequest struct {
	Status string `json:"status"`
}

type oidcClientResponse struct {
	ID                      string    `json:"id"`
	Name                    string    `json:"name"`
	ClientID                string    `json:"clientId"`
	ClientSecretPrefix      string    `json:"clientSecretPrefix"`
	ClientSecret            string    `json:"clientSecret,omitempty"`
	ApplicationType         string    `json:"applicationType"`
	TokenEndpointAuthMethod string    `json:"tokenEndpointAuthMethod"`
	RedirectURIs            []string  `json:"redirectUris"`
	AllowedScopes           []string  `json:"allowedScopes"`
	Status                  string    `json:"status"`
	CreatedAt               time.Time `json:"createdAt"`
	UpdatedAt               time.Time `json:"updatedAt"`
}

type oidcTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	IDToken      string `json:"id_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope"`
}

type oidcConsentDecisionRequest struct {
	Approved *bool `json:"approved"`
}

type oidcAuthorizationRequestResponse struct {
	ClientName      string   `json:"clientName"`
	ClientID        string   `json:"clientId"`
	ApplicationType string   `json:"applicationType"`
	RedirectURI     string   `json:"redirectUri"`
	RedirectHost    string   `json:"redirectHost"`
	Scopes          []string `json:"scopes"`
}

type oidcConsentResponse struct {
	RedirectURI string `json:"redirectUri"`
}

type authorizedApplicationResponse struct {
	ID              string    `json:"id"`
	ClientID        string    `json:"clientId"`
	ClientName      string    `json:"clientName"`
	ApplicationType string    `json:"applicationType"`
	Scopes          []string  `json:"scopes"`
	GrantedAt       time.Time `json:"grantedAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

func registerOIDCAdminRoutes(router *gin.Engine, deps Dependencies) {
	clients := router.Group("/api/v1/oidc/clients", requireSession(deps))
	clients.GET("", requirePermission(deps, "oidc.client.read"), listOIDCClientsHandler(deps))
	clients.POST("", requirePermission(deps, "oidc.client.write"), requireCSRF(deps), createOIDCClientHandler(deps))
	clients.PUT("/:id", requirePermission(deps, "oidc.client.write"), requireCSRF(deps), updateOIDCClientHandler(deps))
	clients.PATCH("/:id/status", requirePermission(deps, "oidc.client.write"), requireCSRF(deps), changeOIDCClientStatusHandler(deps))
	clients.POST("/:id/rotate-secret", requirePermission(deps, "oidc.client.write"), requireCSRF(deps), rotateOIDCClientSecretHandler(deps))
	clients.GET("/:id/secret", requirePermission(deps, "oidc.client.write"), revealOIDCClientSecretHandler(deps))
}

func registerOIDCProviderRoutes(router *gin.Engine, deps Dependencies) {
	router.GET("/.well-known/openid-configuration", oidcDiscoveryHandler(deps))
	router.GET("/api/v1/oauth2/jwks", oidcJWKSHandler(deps))
	router.GET("/api/v1/oauth2/authorize", oidcAuthorizeHandler(deps))
	router.GET("/api/v1/oauth2/consent", requireSession(deps), oidcConsentDetailHandler(deps))
	router.POST("/api/v1/oauth2/consent", requireSession(deps), requireCSRF(deps), oidcConsentDecisionHandler(deps))
	router.POST("/api/v1/oauth2/token", oidcTokenHandler(deps))
	router.GET("/api/v1/oauth2/userinfo", oidcUserInfoHandler(deps))
	router.GET("/api/v1/account/authorized-applications", requireSession(deps), listAuthorizedApplicationsHandler(deps))
	router.DELETE("/api/v1/account/authorized-applications/:id", requireSession(deps), requireCSRF(deps), revokeAuthorizedApplicationHandler(deps))
}

func oidcDiscoveryHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		issuer := deps.OIDCProvider.Issuer()
		ctx.JSON(http.StatusOK, gin.H{
			"issuer":                                issuer,
			"authorization_endpoint":                issuer + "/api/v1/oauth2/authorize",
			"token_endpoint":                        issuer + "/api/v1/oauth2/token",
			"userinfo_endpoint":                     issuer + "/api/v1/oauth2/userinfo",
			"jwks_uri":                              issuer + "/api/v1/oauth2/jwks",
			"response_types_supported":              []string{"code"},
			"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
			"subject_types_supported":               []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
			"token_endpoint_auth_methods_supported": []string{"client_secret_basic", "client_secret_post", "none"},
			"code_challenge_methods_supported":      []string{"S256"},
			"scopes_supported":                      []string{"openid", "profile", "email", "model.invoke", "mcp.invoke", "offline_access"},
		})
	}
}

func oidcJWKSHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ctx.Header("Cache-Control", "public, max-age=300")
		ctx.JSON(http.StatusOK, deps.OIDCProvider.JWKS())
	}
}

func oidcAuthorizeHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if ctx.Query("response_type") != "code" {
			oauthError(ctx, http.StatusBadRequest, "unsupported_response_type", "仅支持授权码模式")
			return
		}
		token, err := ctx.Cookie(deps.SessionCookie)
		if err != nil {
			redirectToLogin(ctx, deps.ConsoleURL)
			return
		}
		session, err := deps.Auth.Authenticate(ctx.Request.Context(), token)
		if err != nil {
			clearSessionCookie(ctx, deps)
			clearCSRFCookie(ctx, deps)
			redirectToLogin(ctx, deps.ConsoleURL)
			return
		}
		result, err := deps.OIDCProvider.Authorize(ctx.Request.Context(), authorizeInputFromContext(ctx, session))
		if err != nil {
			if !errors.Is(err, oidcmodule.ErrInvalidClient) && !errors.Is(err, oidcmodule.ErrInvalidInput) {
				deps.Logger.Error("OIDC 授权失败", "error", err, "request_id", contextString(ctx, "request_id"))
				oauthError(ctx, http.StatusInternalServerError, "server_error", "授权服务暂时不可用")
				return
			}
			oauthError(ctx, http.StatusBadRequest, "invalid_request", "授权请求不正确")
			return
		}
		if result.ConsentRequired {
			location := strings.TrimRight(deps.ConsoleURL, "/") + "/oauth/consent"
			if ctx.Request.URL.RawQuery != "" {
				location += "?" + ctx.Request.URL.RawQuery
			}
			ctx.Redirect(http.StatusFound, location)
			return
		}
		ctx.Redirect(http.StatusFound, result.RedirectURI)
	}
}

func oidcConsentDetailHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if ctx.Query("response_type") != "code" {
			httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", "仅支持授权码模式")
			return
		}
		request, err := deps.OIDCProvider.DescribeAuthorization(
			ctx.Request.Context(), authorizeInputFromContext(ctx, currentSession(ctx)),
		)
		if err != nil {
			handleConsentError(ctx, deps.Logger, err)
			return
		}
		redirect, _ := url.Parse(request.RedirectURI)
		ctx.Header("Cache-Control", "no-store")
		httpx.OK(ctx, http.StatusOK, oidcAuthorizationRequestResponse{
			ClientName: request.ClientName, ClientID: request.ClientID,
			ApplicationType: request.ApplicationType, RedirectURI: request.RedirectURI,
			RedirectHost: redirect.Host, Scopes: request.Scopes,
		})
	}
}

func oidcConsentDecisionHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if ctx.Query("response_type") != "code" {
			httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", "仅支持授权码模式")
			return
		}
		ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, 4*1024)
		var request oidcConsentDecisionRequest
		if err := ctx.ShouldBindJSON(&request); err != nil || request.Approved == nil {
			httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", "请选择同意或拒绝应用授权")
			return
		}
		input := oidcmodule.ConsentDecisionInput{
			Authorization: authorizeInputFromContext(ctx, currentSession(ctx)),
			RequestID:     contextString(ctx, "request_id"), SourceIP: ctx.ClientIP(),
		}
		var result oidcmodule.AuthorizationResult
		var err error
		if *request.Approved {
			result, err = deps.OIDCProvider.ApproveAuthorization(ctx.Request.Context(), input)
		} else {
			result, err = deps.OIDCProvider.DenyAuthorization(ctx.Request.Context(), input)
		}
		if err != nil {
			handleConsentError(ctx, deps.Logger, err)
			return
		}
		ctx.Header("Cache-Control", "no-store")
		httpx.OK(ctx, http.StatusOK, oidcConsentResponse{RedirectURI: result.RedirectURI})
	}
}

func listAuthorizedApplicationsHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		session := currentSession(ctx)
		items, err := deps.OIDCProvider.ListUserConsents(
			ctx.Request.Context(), session.User.OrganizationID, session.User.ID,
		)
		if err != nil {
			deps.Logger.Error("查询用户应用授权失败", "error", err, "request_id", contextString(ctx, "request_id"))
			httpx.Fail(ctx, http.StatusInternalServerError, "internal_error", "已授权应用暂时无法读取")
			return
		}
		response := make([]authorizedApplicationResponse, 0, len(items))
		for _, item := range items {
			response = append(response, authorizedApplicationResponse{
				ID: item.ID, ClientID: item.ClientID, ClientName: item.ClientName,
				ApplicationType: item.ApplicationType, Scopes: item.Scopes,
				GrantedAt: item.GrantedAt, UpdatedAt: item.UpdatedAt,
			})
		}
		httpx.OK(ctx, http.StatusOK, response)
	}
}

func revokeAuthorizedApplicationHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if _, err := uuid.Parse(ctx.Param("id")); err != nil {
			httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", "应用授权标识格式不正确")
			return
		}
		session := currentSession(ctx)
		err := deps.OIDCProvider.RevokeUserConsent(ctx.Request.Context(), oidcmodule.RevokeConsentInput{
			OrganizationID: session.User.OrganizationID, UserID: session.User.ID,
			ConsentID: ctx.Param("id"), RequestID: contextString(ctx, "request_id"), SourceIP: ctx.ClientIP(),
		})
		if errors.Is(err, oidcmodule.ErrConsentNotFound) {
			httpx.Fail(ctx, http.StatusNotFound, "consent_not_found", "应用授权不存在或已经撤销")
			return
		}
		if err != nil {
			deps.Logger.Error("撤销用户应用授权失败", "error", err, "request_id", contextString(ctx, "request_id"))
			httpx.Fail(ctx, http.StatusInternalServerError, "internal_error", "应用授权暂时无法撤销")
			return
		}
		httpx.OK(ctx, http.StatusOK, gin.H{"revoked": true})
	}
}

func authorizeInputFromContext(ctx *gin.Context, session identity.Session) oidcmodule.AuthorizeInput {
	return oidcmodule.AuthorizeInput{
		Subject: oidcmodule.Subject{
			ID: session.User.ID, OrganizationID: session.User.OrganizationID,
			Organization: session.User.Organization, OrganizationCode: session.User.OrganizationCode,
			Username: session.User.Username, DisplayName: session.User.DisplayName, Email: session.User.Email,
		},
		ClientID: ctx.Query("client_id"), RedirectURI: ctx.Query("redirect_uri"),
		Scope: ctx.Query("scope"), State: ctx.Query("state"), Nonce: ctx.Query("nonce"),
		CodeChallenge: ctx.Query("code_challenge"), ChallengeMethod: ctx.Query("code_challenge_method"),
	}
}

func handleConsentError(ctx *gin.Context, logger *slog.Logger, err error) {
	if errors.Is(err, oidcmodule.ErrInvalidClient) || errors.Is(err, oidcmodule.ErrInvalidInput) {
		httpx.Fail(ctx, http.StatusBadRequest, "invalid_request", "授权请求不正确或应用不可用")
		return
	}
	logger.Error("OIDC 用户授权处理失败", "error", err, "request_id", contextString(ctx, "request_id"))
	httpx.Fail(ctx, http.StatusInternalServerError, "internal_error", "授权服务暂时不可用")
}

func oidcTokenHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, 32*1024)
		if err := ctx.Request.ParseForm(); err != nil {
			oauthError(ctx, http.StatusBadRequest, "invalid_request", "令牌请求格式不正确")
			return
		}
		clientID, clientSecret := ctx.PostForm("client_id"), ctx.PostForm("client_secret")
		if basicID, basicSecret, ok := ctx.Request.BasicAuth(); ok {
			if clientID != "" && clientID != basicID {
				oauthError(ctx, http.StatusUnauthorized, "invalid_client", "客户端认证失败")
				return
			}
			clientID, clientSecret = basicID, basicSecret
		}
		result, err := deps.OIDCProvider.ExchangeToken(ctx.Request.Context(), oidcmodule.TokenInput{
			GrantType: ctx.PostForm("grant_type"), ClientID: clientID, ClientSecret: clientSecret,
			Code: ctx.PostForm("code"), RedirectURI: ctx.PostForm("redirect_uri"),
			CodeVerifier: ctx.PostForm("code_verifier"), RefreshToken: ctx.PostForm("refresh_token"),
		})
		if err != nil {
			switch {
			case errors.Is(err, oidcmodule.ErrInvalidClient):
				ctx.Header("WWW-Authenticate", `Basic realm="OIDC token endpoint"`)
				oauthError(ctx, http.StatusUnauthorized, "invalid_client", "客户端认证失败")
			case errors.Is(err, oidcmodule.ErrInvalidGrant):
				oauthError(ctx, http.StatusBadRequest, "invalid_grant", "授权凭据无效或已过期")
			case errors.Is(err, oidcmodule.ErrInvalidInput):
				oauthError(ctx, http.StatusBadRequest, "unsupported_grant_type", "不支持的授权类型")
			default:
				deps.Logger.Error("OIDC 令牌签发失败", "error", err, "request_id", contextString(ctx, "request_id"))
				oauthError(ctx, http.StatusInternalServerError, "server_error", "令牌服务暂时不可用")
			}
			return
		}
		ctx.Header("Cache-Control", "no-store")
		ctx.Header("Pragma", "no-cache")
		ctx.JSON(http.StatusOK, oidcTokenResponse{
			AccessToken: result.AccessToken, TokenType: result.TokenType, ExpiresIn: result.ExpiresIn,
			IDToken: result.IDToken, RefreshToken: result.RefreshToken, Scope: result.Scope,
		})
	}
}

func oidcUserInfoHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		authorization := strings.Fields(ctx.GetHeader("Authorization"))
		if len(authorization) != 2 || !strings.EqualFold(authorization[0], "Bearer") {
			oauthBearerError(ctx)
			return
		}
		info, err := deps.OIDCProvider.UserInfo(ctx.Request.Context(), authorization[1])
		if err != nil {
			oauthBearerError(ctx)
			return
		}
		ctx.JSON(http.StatusOK, info)
	}
}

func redirectToLogin(ctx *gin.Context, consoleURL string) {
	returnTo := ctx.Request.URL.RequestURI()
	ctx.Redirect(http.StatusFound, strings.TrimRight(consoleURL, "/")+"/?returnTo="+url.QueryEscape(returnTo))
}

func oauthError(ctx *gin.Context, status int, code, description string) {
	ctx.Header("Cache-Control", "no-store")
	ctx.JSON(status, gin.H{"error": code, "error_description": description})
}

func oauthBearerError(ctx *gin.Context) {
	ctx.Header("WWW-Authenticate", `Bearer error="invalid_token"`)
	oauthError(ctx, http.StatusUnauthorized, "invalid_token", "访问令牌无效或已过期")
}

func listOIDCClientsHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		items, err := deps.OIDCAdmin.ListClients(ctx.Request.Context(), currentSession(ctx).User.OrganizationID)
		if err != nil {
			handleOIDCAdminError(ctx, deps.Logger, err)
			return
		}
		response := make([]oidcClientResponse, 0, len(items))
		for _, item := range items {
			response = append(response, presentOIDCClient(item, ""))
		}
		httpx.OK(ctx, http.StatusOK, response)
	}
}

func createOIDCClientHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var request oidcClientWriteRequest
		if !bindModelJSON(ctx, &request) {
			return
		}
		session := currentSession(ctx)
		created, err := deps.OIDCAdmin.CreateClient(ctx.Request.Context(), oidcmodule.CreateClientInput{
			OrganizationID: session.User.OrganizationID, ActorID: session.User.ID,
			Name: request.Name, ApplicationType: request.ApplicationType,
			RedirectURIs: request.RedirectURIs, AllowedScopes: request.AllowedScopes,
			RequestID: contextString(ctx, "request_id"), SourceIP: ctx.ClientIP(),
		})
		if err != nil {
			handleOIDCAdminError(ctx, deps.Logger, err)
			return
		}
		httpx.OK(ctx, http.StatusCreated, presentOIDCClient(created.Client, created.ClientSecret))
	}
}

func updateOIDCClientHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if !validUUIDParam(ctx, "统一登录应用") {
			return
		}
		var request oidcClientWriteRequest
		if !bindModelJSON(ctx, &request) {
			return
		}
		session := currentSession(ctx)
		item, err := deps.OIDCAdmin.UpdateClient(ctx.Request.Context(), oidcmodule.UpdateClientInput{
			OrganizationID: session.User.OrganizationID, ActorID: session.User.ID, ClientRecordID: ctx.Param("id"),
			Name: request.Name, RedirectURIs: request.RedirectURIs, AllowedScopes: request.AllowedScopes,
			RequestID: contextString(ctx, "request_id"), SourceIP: ctx.ClientIP(),
		})
		if err != nil {
			handleOIDCAdminError(ctx, deps.Logger, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, presentOIDCClient(item, ""))
	}
}

func changeOIDCClientStatusHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if !validUUIDParam(ctx, "统一登录应用") {
			return
		}
		var request oidcClientStatusRequest
		if !bindModelJSON(ctx, &request) {
			return
		}
		session := currentSession(ctx)
		item, err := deps.OIDCAdmin.ChangeClientStatus(ctx.Request.Context(), oidcmodule.ChangeClientStatusInput{
			OrganizationID: session.User.OrganizationID, ActorID: session.User.ID, ClientRecordID: ctx.Param("id"),
			Status: request.Status, RequestID: contextString(ctx, "request_id"), SourceIP: ctx.ClientIP(),
		})
		if err != nil {
			handleOIDCAdminError(ctx, deps.Logger, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, presentOIDCClient(item, ""))
	}
}

func rotateOIDCClientSecretHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if !validUUIDParam(ctx, "统一登录应用") {
			return
		}
		session := currentSession(ctx)
		created, err := deps.OIDCAdmin.RotateClientSecret(ctx.Request.Context(), oidcmodule.RotateClientSecretInput{
			OrganizationID: session.User.OrganizationID, ActorID: session.User.ID, ClientRecordID: ctx.Param("id"),
			RequestID: contextString(ctx, "request_id"), SourceIP: ctx.ClientIP(),
		})
		if err != nil {
			handleOIDCAdminError(ctx, deps.Logger, err)
			return
		}
		httpx.OK(ctx, http.StatusOK, presentOIDCClient(created.Client, created.ClientSecret))
	}
}

func revealOIDCClientSecretHandler(deps Dependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if !validUUIDParam(ctx, "统一登录应用") {
			return
		}
		session := currentSession(ctx)
		value, err := deps.OIDCAdmin.RevealClientSecret(ctx.Request.Context(), oidcmodule.RevealClientSecretInput{
			OrganizationID: session.User.OrganizationID, ActorID: session.User.ID, ClientRecordID: ctx.Param("id"),
			RequestID: contextString(ctx, "request_id"), SourceIP: ctx.ClientIP(),
		})
		if err != nil {
			handleOIDCAdminError(ctx, deps.Logger, err)
			return
		}
		ctx.Header("Cache-Control", "no-store")
		ctx.Header("Pragma", "no-cache")
		httpx.OK(ctx, http.StatusOK, gin.H{"secret": value})
	}
}

func presentOIDCClient(item oidcmodule.Client, secret string) oidcClientResponse {
	return oidcClientResponse{
		ID: item.ID, Name: item.Name, ClientID: item.ClientID,
		ClientSecretPrefix: item.ClientSecretPrefix, ClientSecret: secret,
		ApplicationType: item.ApplicationType, TokenEndpointAuthMethod: item.TokenEndpointAuthMethod,
		RedirectURIs: item.RedirectURIs, AllowedScopes: item.AllowedScopes,
		Status: item.Status, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}
}

func handleOIDCAdminError(ctx *gin.Context, logger *slog.Logger, err error) {
	switch {
	case errors.Is(err, oidcmodule.ErrInvalidInput):
		httpx.Fail(ctx, http.StatusBadRequest, "invalid_oidc_client", "应用接入参数不正确")
	case errors.Is(err, oidcmodule.ErrClientNotFound):
		httpx.Fail(ctx, http.StatusNotFound, "oidc_client_not_found", "统一登录应用不存在")
	case errors.Is(err, oidcmodule.ErrClientSecretUnsupported):
		httpx.Fail(ctx, http.StatusConflict, "client_secret_unsupported", "桌面应用不使用客户端密钥")
	case errors.Is(err, oidcmodule.ErrClientSecretUnavailable):
		httpx.Fail(ctx, http.StatusConflict, "client_secret_unavailable", "该应用创建于支持再次查看之前，请先轮换一次客户端密钥")
	default:
		logger.Error("应用接入接口处理失败", "error", err, "request_id", contextString(ctx, "request_id"))
		httpx.Fail(ctx, http.StatusInternalServerError, "internal_error", "服务暂时不可用")
	}
}
