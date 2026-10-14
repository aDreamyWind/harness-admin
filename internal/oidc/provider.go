package oidc

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	authorizationCodeTTL = 5 * time.Minute
	accessTokenTTL       = time.Hour
	refreshTokenTTL      = 30 * 24 * time.Hour
)

type ProviderService struct {
	repository ProviderRepository
	signer     TokenSigner
	issuer     string
	now        func() time.Time
	newCode    func() (string, error)
	newRefresh func() (string, error)
}

func NewProviderService(repository ProviderRepository, signer TokenSigner, issuer string) (*ProviderService, error) {
	issuer = strings.TrimRight(strings.TrimSpace(issuer), "/")
	parsed, err := url.Parse(issuer)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return nil, errors.New("OIDC_ISSUER 必须是完整的 HTTP 或 HTTPS 地址")
	}
	return &ProviderService{
		repository: repository, signer: signer, issuer: issuer, now: time.Now,
		newCode:    func() (string, error) { return randomValue("hac_", 32) },
		newRefresh: func() (string, error) { return randomValue("har_", 48) },
	}, nil
}

func (s *ProviderService) Issuer() string       { return s.issuer }
func (s *ProviderService) JWKS() map[string]any { return s.signer.JWKS() }

func (s *ProviderService) Authorize(ctx context.Context, input AuthorizeInput) (AuthorizationResult, error) {
	if input.Subject.ID == "" || input.Subject.OrganizationID == "" || input.ClientID == "" {
		return AuthorizationResult{}, ErrInvalidInput
	}
	client, err := s.repository.FindProtocolClient(ctx, input.ClientID)
	if err != nil || client.Status != "active" || client.OrganizationID != input.Subject.OrganizationID {
		return AuthorizationResult{}, ErrInvalidClient
	}
	if !redirectURIAllowed(client.Client, input.RedirectURI) {
		return AuthorizationResult{}, ErrInvalidInput
	}
	scopes := normalizeValues(strings.Fields(input.Scope), true)
	if len(scopes) == 0 || !slices.Contains(scopes, "openid") || !scopesAllowed(scopes, client.AllowedScopes) ||
		input.ChallengeMethod != "S256" || !validCodeChallenge(input.CodeChallenge) || len(input.Nonce) > 255 {
		return AuthorizationResult{}, ErrInvalidInput
	}
	code, err := s.newCode()
	if err != nil {
		return AuthorizationResult{}, fmt.Errorf("生成授权码失败: %w", err)
	}
	digest := sha256.Sum256([]byte(code))
	now := s.now().UTC()
	if err := s.repository.SaveAuthorizationGrant(ctx, AuthorizationGrant{
		ID: uuid.NewString(), OrganizationID: client.OrganizationID, ClientRecordID: client.ID,
		UserID: input.Subject.ID, CodeHash: digest[:], RedirectURI: input.RedirectURI,
		Scopes: scopes, Nonce: input.Nonce, CodeChallenge: input.CodeChallenge,
		ExpiresAt: now.Add(authorizationCodeTTL),
	}); err != nil {
		return AuthorizationResult{}, fmt.Errorf("保存授权码失败: %w", err)
	}
	redirect, _ := url.Parse(input.RedirectURI)
	query := redirect.Query()
	query.Set("code", code)
	if input.State != "" {
		query.Set("state", input.State)
	}
	redirect.RawQuery = query.Encode()
	return AuthorizationResult{RedirectURI: redirect.String()}, nil
}

func (s *ProviderService) ExchangeToken(ctx context.Context, input TokenInput) (TokenResult, error) {
	client, err := s.authenticateClient(ctx, input.ClientID, input.ClientSecret)
	if err != nil {
		return TokenResult{}, err
	}
	switch input.GrantType {
	case "authorization_code":
		return s.exchangeAuthorizationCode(ctx, client, input)
	case "refresh_token":
		return s.exchangeRefreshToken(ctx, client, input.RefreshToken)
	default:
		return TokenResult{}, ErrInvalidInput
	}
}

func (s *ProviderService) UserInfo(ctx context.Context, accessToken string) (UserInfo, error) {
	subject, claims, err := s.authenticateAccessToken(ctx, accessToken)
	if err != nil {
		return UserInfo{}, err
	}
	scopes := strings.Fields(claimString(claims, "scope"))
	result := UserInfo{Subject: subject.ID, OrganizationID: subject.OrganizationID, OrganizationCode: subject.OrganizationCode}
	if slices.Contains(scopes, "profile") {
		result.Name = subject.DisplayName
		result.PreferredUsername = subject.Username
	}
	if slices.Contains(scopes, "email") {
		result.Email = subject.Email
	}
	return result, nil
}

func (s *ProviderService) AuthenticateAccessToken(ctx context.Context, accessToken, requiredScope string) (Subject, error) {
	subject, claims, err := s.authenticateAccessToken(ctx, accessToken)
	if err != nil {
		return Subject{}, err
	}
	if requiredScope != "" && !slices.Contains(strings.Fields(claimString(claims, "scope")), requiredScope) {
		return Subject{}, ErrInvalidToken
	}
	return subject, nil
}

func (s *ProviderService) authenticateAccessToken(ctx context.Context, accessToken string) (Subject, map[string]any, error) {
	claims, err := s.signer.Verify(accessToken)
	if err != nil || claimString(claims, "iss") != s.issuer || claimString(claims, "token_use") != "access" || claimExpiry(claims) <= s.now().UTC().Unix() {
		return Subject{}, nil, ErrInvalidToken
	}
	userID, organizationID := claimString(claims, "sub"), claimString(claims, "org_id")
	clientID := claimString(claims, "aud")
	if userID == "" || organizationID == "" || clientID == "" {
		return Subject{}, nil, ErrInvalidToken
	}
	client, err := s.repository.FindProtocolClient(ctx, clientID)
	if err != nil || client.Status != "active" || client.OrganizationID != organizationID {
		return Subject{}, nil, ErrInvalidToken
	}
	subject, err := s.repository.FindSubject(ctx, organizationID, userID)
	if err != nil {
		return Subject{}, nil, ErrInvalidToken
	}
	return subject, claims, nil
}

func (s *ProviderService) authenticateClient(ctx context.Context, clientID, secret string) (ProtocolClient, error) {
	if clientID == "" {
		return ProtocolClient{}, ErrInvalidClient
	}
	client, err := s.repository.FindProtocolClient(ctx, clientID)
	if err != nil || client.Status != "active" {
		return ProtocolClient{}, ErrInvalidClient
	}
	if client.TokenEndpointAuthMethod == TokenEndpointAuthNone {
		if secret != "" {
			return ProtocolClient{}, ErrInvalidClient
		}
		return client, nil
	}
	if client.TokenEndpointAuthMethod != TokenEndpointAuthClientSecretBasic || secret == "" {
		return ProtocolClient{}, ErrInvalidClient
	}
	digest := sha256.Sum256([]byte(secret))
	if len(client.ClientSecretHash) != len(digest) || subtle.ConstantTimeCompare(client.ClientSecretHash, digest[:]) != 1 {
		return ProtocolClient{}, ErrInvalidClient
	}
	return client, nil
}

func redirectURIAllowed(client Client, candidate string) bool {
	if !validRedirectURI(candidate) {
		return false
	}
	if slices.Contains(client.RedirectURIs, candidate) {
		return true
	}
	if client.ApplicationType != ApplicationTypeDesktop {
		return false
	}
	candidateURL, err := url.Parse(candidate)
	if err != nil || candidateURL.Scheme != "http" || !isLoopbackHost(candidateURL.Hostname()) {
		return false
	}
	for _, registered := range client.RedirectURIs {
		registeredURL, parseErr := url.Parse(registered)
		if parseErr == nil && registeredURL.Scheme == candidateURL.Scheme &&
			strings.EqualFold(registeredURL.Hostname(), candidateURL.Hostname()) &&
			registeredURL.Path == candidateURL.Path && registeredURL.RawQuery == candidateURL.RawQuery &&
			isLoopbackHost(registeredURL.Hostname()) {
			return true
		}
	}
	return false
}

func isLoopbackHost(hostname string) bool {
	ip := net.ParseIP(hostname)
	return ip != nil && ip.IsLoopback()
}

func (s *ProviderService) exchangeAuthorizationCode(ctx context.Context, client ProtocolClient, input TokenInput) (TokenResult, error) {
	if input.Code == "" || input.RedirectURI == "" || !validCodeVerifier(input.CodeVerifier) {
		return TokenResult{}, ErrInvalidGrant
	}
	codeDigest := sha256.Sum256([]byte(input.Code))
	verifierDigest := sha256.Sum256([]byte(input.CodeVerifier))
	challenge := base64.RawURLEncoding.EncodeToString(verifierDigest[:])
	grant, err := s.repository.ExchangeAuthorizationGrant(ctx, codeDigest[:], client.ID, input.RedirectURI, challenge)
	if err != nil {
		return TokenResult{}, ErrInvalidGrant
	}
	return s.issueTokens(ctx, client, grant.Subject, grant.Scopes, grant.Nonce, true)
}

func (s *ProviderService) exchangeRefreshToken(ctx context.Context, client ProtocolClient, token string) (TokenResult, error) {
	if token == "" {
		return TokenResult{}, ErrInvalidGrant
	}
	plain, err := s.newRefresh()
	if err != nil {
		return TokenResult{}, fmt.Errorf("生成刷新令牌失败: %w", err)
	}
	oldDigest := sha256.Sum256([]byte(token))
	newDigest := sha256.Sum256([]byte(plain))
	now := s.now().UTC()
	grant, err := s.repository.RotateRefreshGrant(ctx, oldDigest[:], client.ID, RefreshGrant{
		ID: uuid.NewString(), TokenHash: newDigest[:], ExpiresAt: now.Add(refreshTokenTTL),
	})
	if err != nil {
		return TokenResult{}, ErrInvalidGrant
	}
	result, err := s.issueSignedTokens(client, grant.Subject, grant.Scopes, "")
	if err != nil {
		return TokenResult{}, err
	}
	result.RefreshToken = plain
	return result, nil
}

func (s *ProviderService) issueTokens(ctx context.Context, client ProtocolClient, subject Subject, scopes []string, nonce string, allowRefresh bool) (TokenResult, error) {
	result, err := s.issueSignedTokens(client, subject, scopes, nonce)
	if err != nil {
		return TokenResult{}, err
	}
	if !allowRefresh || !slices.Contains(scopes, "offline_access") {
		return result, nil
	}
	plain, err := s.newRefresh()
	if err != nil {
		return TokenResult{}, fmt.Errorf("生成刷新令牌失败: %w", err)
	}
	digest := sha256.Sum256([]byte(plain))
	now := s.now().UTC()
	if err := s.repository.SaveRefreshGrant(ctx, RefreshGrant{
		ID: uuid.NewString(), OrganizationID: client.OrganizationID, ClientRecordID: client.ID,
		UserID: subject.ID, TokenHash: digest[:], Scopes: scopes, ExpiresAt: now.Add(refreshTokenTTL),
	}); err != nil {
		return TokenResult{}, fmt.Errorf("保存刷新令牌失败: %w", err)
	}
	result.RefreshToken = plain
	return result, nil
}

func (s *ProviderService) issueSignedTokens(client ProtocolClient, subject Subject, scopes []string, nonce string) (TokenResult, error) {
	now := s.now().UTC()
	expiresAt := now.Add(accessTokenTTL)
	base := map[string]any{
		"iss": s.issuer, "sub": subject.ID, "aud": client.ClientID,
		"iat": now.Unix(), "exp": expiresAt.Unix(), "org_id": subject.OrganizationID,
		"org_code": subject.OrganizationCode,
	}
	accessClaims := cloneClaims(base)
	accessClaims["token_use"] = "access"
	accessClaims["scope"] = strings.Join(scopes, " ")
	accessToken, err := s.signer.Sign(accessClaims)
	if err != nil {
		return TokenResult{}, fmt.Errorf("签发访问令牌失败: %w", err)
	}
	idClaims := cloneClaims(base)
	idClaims["token_use"] = "id"
	idClaims["name"] = subject.DisplayName
	idClaims["preferred_username"] = subject.Username
	if slices.Contains(scopes, "email") {
		idClaims["email"] = subject.Email
	}
	if nonce != "" {
		idClaims["nonce"] = nonce
	}
	atHash := sha256.Sum256([]byte(accessToken))
	idClaims["at_hash"] = base64.RawURLEncoding.EncodeToString(atHash[:len(atHash)/2])
	idToken, err := s.signer.Sign(idClaims)
	if err != nil {
		return TokenResult{}, fmt.Errorf("签发身份令牌失败: %w", err)
	}
	return TokenResult{
		AccessToken: accessToken, TokenType: "Bearer", ExpiresIn: int64(accessTokenTTL.Seconds()),
		IDToken: idToken, Scope: strings.Join(scopes, " "),
	}, nil
}

func scopesAllowed(requested, allowed []string) bool {
	for _, scope := range requested {
		if !slices.Contains(allowed, scope) {
			return false
		}
	}
	return true
}

func validCodeChallenge(value string) bool {
	if len(value) < 43 || len(value) > 128 {
		return false
	}
	_, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil
}

func validCodeVerifier(value string) bool {
	if len(value) < 43 || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~", character) {
			return false
		}
	}
	return true
}

func cloneClaims(input map[string]any) map[string]any {
	result := make(map[string]any, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func claimString(claims map[string]any, key string) string {
	value, _ := claims[key].(string)
	return value
}

func claimExpiry(claims map[string]any) int64 {
	value, _ := claims["exp"].(float64)
	return int64(value)
}
