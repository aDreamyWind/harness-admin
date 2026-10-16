package httpserver

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	oidcmodule "harness-admin/internal/oidc"
)

type oidcProviderStub struct {
	authorizeInput oidcmodule.AuthorizeInput
	decisionInput  oidcmodule.ConsentDecisionInput
	tokenInput     oidcmodule.TokenInput
	accessToken    string
	requiredScope  string
	requireConsent bool
	revokedConsent string
}

func (*oidcProviderStub) Issuer() string { return "https://id.example.com" }
func (*oidcProviderStub) JWKS() map[string]any {
	return map[string]any{"keys": []map[string]any{{"kid": "key-1"}}}
}
func (s *oidcProviderStub) Authorize(_ context.Context, input oidcmodule.AuthorizeInput) (oidcmodule.AuthorizationResult, error) {
	s.authorizeInput = input
	if s.requireConsent {
		return oidcmodule.AuthorizationResult{ConsentRequired: true}, nil
	}
	return oidcmodule.AuthorizationResult{RedirectURI: "https://deepseek.example.com/callback?code=test"}, nil
}
func (s *oidcProviderStub) DescribeAuthorization(_ context.Context, input oidcmodule.AuthorizeInput) (oidcmodule.AuthorizationRequest, error) {
	s.authorizeInput = input
	return oidcmodule.AuthorizationRequest{
		ClientName: "DeepSeek Harness", ClientID: input.ClientID, ApplicationType: "web",
		RedirectURI: input.RedirectURI, Scopes: strings.Fields(input.Scope),
	}, nil
}
func (s *oidcProviderStub) ApproveAuthorization(_ context.Context, input oidcmodule.ConsentDecisionInput) (oidcmodule.AuthorizationResult, error) {
	s.decisionInput = input
	return oidcmodule.AuthorizationResult{RedirectURI: "https://deepseek.example.com/callback?code=approved"}, nil
}
func (s *oidcProviderStub) DenyAuthorization(_ context.Context, input oidcmodule.ConsentDecisionInput) (oidcmodule.AuthorizationResult, error) {
	s.decisionInput = input
	return oidcmodule.AuthorizationResult{RedirectURI: "https://deepseek.example.com/callback?error=access_denied"}, nil
}
func (*oidcProviderStub) ListUserConsents(context.Context, string, string) ([]oidcmodule.UserConsent, error) {
	return []oidcmodule.UserConsent{{ID: "917e4f53-3cb0-4df2-8027-c0a49fd58d25", ClientName: "DeepSeek Harness", ClientID: "client-1", ApplicationType: "web", Scopes: []string{"openid"}}}, nil
}
func (s *oidcProviderStub) RevokeUserConsent(_ context.Context, input oidcmodule.RevokeConsentInput) error {
	s.revokedConsent = input.ConsentID
	return nil
}
func (s *oidcProviderStub) ExchangeToken(_ context.Context, input oidcmodule.TokenInput) (oidcmodule.TokenResult, error) {
	s.tokenInput = input
	return oidcmodule.TokenResult{AccessToken: "access", TokenType: "Bearer", ExpiresIn: 3600, IDToken: "id", Scope: "openid"}, nil
}
func (*oidcProviderStub) UserInfo(context.Context, string) (oidcmodule.UserInfo, error) {
	return oidcmodule.UserInfo{Subject: "user-1", OrganizationID: "org-1", OrganizationCode: "test"}, nil
}
func (s *oidcProviderStub) AuthenticateAccessToken(_ context.Context, token, scope string) (oidcmodule.Subject, error) {
	s.accessToken, s.requiredScope = token, scope
	return oidcmodule.Subject{ID: "user-1", OrganizationID: "org-1"}, nil
}

func TestOIDCAuthorizeRedirectsToLoginWithoutSession(t *testing.T) {
	router := newOIDCProtocolTestRouter(&oidcProviderStub{})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/oauth2/authorize?client_id=client-1&response_type=code", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusFound {
		t.Fatalf("状态码 = %d；响应 = %s", response.Code, response.Body.String())
	}
	location, _ := url.Parse(response.Header().Get("Location"))
	if location.Path != "/" || !strings.HasPrefix(location.Query().Get("returnTo"), "/api/v1/oauth2/authorize?") {
		t.Fatalf("登录跳转地址不正确: %s", location.String())
	}
}

func TestOIDCAuthorizeUsesCurrentSession(t *testing.T) {
	provider := &oidcProviderStub{}
	router := newOIDCProtocolTestRouter(provider)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/oauth2/authorize?response_type=code&client_id=client-1&redirect_uri=https%3A%2F%2Fdeepseek.example.com%2Fcallback&scope=openid&code_challenge=abc&code_challenge_method=S256", nil)
	request.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusFound || response.Header().Get("Location") != "https://deepseek.example.com/callback?code=test" {
		t.Fatalf("授权跳转不正确: %d %s", response.Code, response.Header().Get("Location"))
	}
	if provider.authorizeInput.Subject.ID != "user-1" || provider.authorizeInput.Subject.OrganizationID != "org-1" {
		t.Fatalf("授权用户上下文不完整: %#v", provider.authorizeInput.Subject)
	}
}

func TestOIDCAuthorizeRedirectsToConsentWhenUserHasNotApproved(t *testing.T) {
	provider := &oidcProviderStub{requireConsent: true}
	router := newOIDCProtocolTestRouter(provider)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/oauth2/authorize?response_type=code&client_id=client-1&redirect_uri=https%3A%2F%2Fdeepseek.example.com%2Fcallback&scope=openid&code_challenge=abc&code_challenge_method=S256", nil)
	request.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusFound || !strings.HasPrefix(response.Header().Get("Location"), "https://console.example.com/oauth/consent?") {
		t.Fatalf("首次授权应跳转确认页: %d %s", response.Code, response.Header().Get("Location"))
	}
}

func TestOIDCConsentDetailAndApproval(t *testing.T) {
	provider := &oidcProviderStub{}
	router := newOIDCProtocolTestRouter(provider)
	query := "response_type=code&client_id=client-1&redirect_uri=https%3A%2F%2Fdeepseek.example.com%2Fcallback&scope=openid+model.invoke&code_challenge=abc&code_challenge_method=S256"
	detailRequest := httptest.NewRequest(http.MethodGet, "/api/v1/oauth2/consent?"+query, nil)
	detailRequest.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
	detailResponse := httptest.NewRecorder()
	router.ServeHTTP(detailResponse, detailRequest)
	if detailResponse.Code != http.StatusOK || !strings.Contains(detailResponse.Body.String(), `"clientName":"DeepSeek Harness"`) {
		t.Fatalf("授权详情响应不正确: %d %s", detailResponse.Code, detailResponse.Body.String())
	}

	approveRequest := httptest.NewRequest(http.MethodPost, "/api/v1/oauth2/consent?"+query, strings.NewReader(`{"approved":true}`))
	approveRequest.Header.Set("Content-Type", "application/json")
	approveRequest.Header.Set("X-CSRF-Token", "csrf-token")
	approveRequest.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
	approveResponse := httptest.NewRecorder()
	router.ServeHTTP(approveResponse, approveRequest)
	if approveResponse.Code != http.StatusOK || !strings.Contains(approveResponse.Body.String(), "code=approved") {
		t.Fatalf("同意授权响应不正确: %d %s", approveResponse.Code, approveResponse.Body.String())
	}
	if provider.decisionInput.Authorization.Subject.ID != "user-1" {
		t.Fatalf("授权决定缺少当前用户: %#v", provider.decisionInput)
	}
}

func TestAuthorizedApplicationsCanBeListedAndRevoked(t *testing.T) {
	provider := &oidcProviderStub{}
	router := newOIDCProtocolTestRouter(provider)
	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/account/authorized-applications", nil)
	listRequest.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
	listResponse := httptest.NewRecorder()
	router.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), `"clientName":"DeepSeek Harness"`) {
		t.Fatalf("已授权应用列表响应不正确: %d %s", listResponse.Code, listResponse.Body.String())
	}

	consentID := "917e4f53-3cb0-4df2-8027-c0a49fd58d25"
	revokeRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/account/authorized-applications/"+consentID, nil)
	revokeRequest.Header.Set("X-CSRF-Token", "csrf-token")
	revokeRequest.AddCookie(&http.Cookie{Name: "test_session", Value: "token"})
	revokeResponse := httptest.NewRecorder()
	router.ServeHTTP(revokeResponse, revokeRequest)
	if revokeResponse.Code != http.StatusOK || provider.revokedConsent != consentID {
		t.Fatalf("撤销应用授权响应不正确: %d %s", revokeResponse.Code, revokeResponse.Body.String())
	}
}

func TestOIDCTokenSupportsBasicClientAuthentication(t *testing.T) {
	provider := &oidcProviderStub{}
	router := newOIDCProtocolTestRouter(provider)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/oauth2/token", strings.NewReader("grant_type=authorization_code&code=code-1&redirect_uri=https%3A%2F%2Fdeepseek.example.com%2Fcallback&code_verifier=verifier"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.SetBasicAuth("client-1", "secret-1")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"access_token":"access"`) {
		t.Fatalf("令牌响应不正确: %d %s", response.Code, response.Body.String())
	}
	if provider.tokenInput.ClientID != "client-1" || provider.tokenInput.ClientSecret != "secret-1" {
		t.Fatalf("客户端认证参数不完整: %#v", provider.tokenInput)
	}
}

func TestOIDCTokenAcceptsPublicClientIDWithoutSecret(t *testing.T) {
	provider := &oidcProviderStub{}
	router := newOIDCProtocolTestRouter(provider)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/oauth2/token", strings.NewReader("grant_type=authorization_code&client_id=desktop-client&code=code-1&redirect_uri=http%3A%2F%2F127.0.0.1%3A49152%2Foauth%2Fcallback&code_verifier=verifier"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("令牌响应不正确: %d %s", response.Code, response.Body.String())
	}
	if provider.tokenInput.ClientID != "desktop-client" || provider.tokenInput.ClientSecret != "" {
		t.Fatalf("公开客户端参数不正确: %#v", provider.tokenInput)
	}
}

func TestOIDCDiscoveryAdvertisesPublicClientAuthentication(t *testing.T) {
	router := newOIDCProtocolTestRouter(&oidcProviderStub{})
	request := httptest.NewRequest(http.MethodGet, "/.well-known/openid-configuration", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"token_endpoint_auth_methods_supported":["client_secret_basic","client_secret_post","none"]`) {
		t.Fatalf("发现文档未声明公开客户端认证方式: %d %s", response.Code, response.Body.String())
	}
}

func newOIDCProtocolTestRouter(provider OIDCProviderService) http.Handler {
	gin.SetMode(gin.TestMode)
	return New(Dependencies{
		AppName: "Harness Admin", Environment: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth: &accountAuthStub{session: accountTestSession()}, OIDCProvider: provider,
		SessionCookie: "test_session", ConsoleURL: "https://console.example.com",
	})
}
