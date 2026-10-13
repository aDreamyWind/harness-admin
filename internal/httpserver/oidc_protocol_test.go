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
	tokenInput     oidcmodule.TokenInput
	accessToken    string
	requiredScope  string
}

func (*oidcProviderStub) Issuer() string { return "https://id.example.com" }
func (*oidcProviderStub) JWKS() map[string]any {
	return map[string]any{"keys": []map[string]any{{"kid": "key-1"}}}
}
func (s *oidcProviderStub) Authorize(_ context.Context, input oidcmodule.AuthorizeInput) (oidcmodule.AuthorizationResult, error) {
	s.authorizeInput = input
	return oidcmodule.AuthorizationResult{RedirectURI: "https://deepseek.example.com/callback?code=test"}, nil
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

func newOIDCProtocolTestRouter(provider OIDCProviderService) http.Handler {
	gin.SetMode(gin.TestMode)
	return New(Dependencies{
		AppName: "Harness Admin", Environment: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth: &accountAuthStub{session: accountTestSession()}, OIDCProvider: provider, SessionCookie: "test_session",
	})
}
