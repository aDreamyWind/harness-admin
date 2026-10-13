package oidc

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"
)

func TestProviderAuthorizationCodeFlow(t *testing.T) {
	repository := newProviderRepositoryStub()
	signer, err := NewRS256Signer("")
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewProviderService(repository, signer, "https://id.example.com")
	if err != nil {
		t.Fatal(err)
	}
	fixedNow := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return fixedNow }
	service.newCode = func() (string, error) { return "authorization-code", nil }
	refreshValues := []string{"refresh-token-1", "refresh-token-2"}
	service.newRefresh = func() (string, error) {
		if len(refreshValues) == 0 {
			return "refresh-token-3", nil
		}
		value := refreshValues[0]
		refreshValues = refreshValues[1:]
		return value, nil
	}
	verifier := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~"
	verifierDigest := sha256.Sum256([]byte(verifier))
	challenge := encodeSegment(verifierDigest[:])

	authorized, err := service.Authorize(context.Background(), AuthorizeInput{
		Subject: repository.subject, ClientID: repository.client.ClientID,
		RedirectURI: repository.client.RedirectURIs[0], Scope: "openid profile offline_access",
		State: "state-1", Nonce: "nonce-1", CodeChallenge: challenge, ChallengeMethod: "S256",
	})
	if err != nil {
		t.Fatalf("Authorize() 返回错误: %v", err)
	}
	if authorized.RedirectURI != "https://deepseek.example.com/callback?code=authorization-code&state=state-1" {
		t.Fatalf("回调地址不正确: %s", authorized.RedirectURI)
	}

	tokens, err := service.ExchangeToken(context.Background(), TokenInput{
		GrantType: "authorization_code", ClientID: repository.client.ClientID, ClientSecret: "client-secret",
		Code: "authorization-code", RedirectURI: repository.client.RedirectURIs[0], CodeVerifier: verifier,
	})
	if err != nil {
		t.Fatalf("ExchangeToken() 返回错误: %v", err)
	}
	if tokens.AccessToken == "" || tokens.IDToken == "" || tokens.RefreshToken != "refresh-token-1" || tokens.ExpiresIn != 3600 {
		t.Fatalf("令牌响应不完整: %#v", tokens)
	}
	info, err := service.UserInfo(context.Background(), tokens.AccessToken)
	if err != nil {
		t.Fatalf("UserInfo() 返回错误: %v", err)
	}
	if info.Subject != repository.subject.ID || info.Name != repository.subject.DisplayName || info.Email != "" {
		t.Fatalf("UserInfo 范围过滤错误: %#v", info)
	}
	refreshed, err := service.ExchangeToken(context.Background(), TokenInput{
		GrantType: "refresh_token", ClientID: repository.client.ClientID,
		ClientSecret: "client-secret", RefreshToken: "refresh-token-1",
	})
	if err != nil {
		t.Fatalf("刷新令牌返回错误: %v", err)
	}
	if refreshed.RefreshToken != "refresh-token-2" || refreshed.AccessToken == "" {
		t.Fatalf("刷新令牌未正确轮换: %#v", refreshed)
	}
	if _, err := service.ExchangeToken(context.Background(), TokenInput{
		GrantType: "refresh_token", ClientID: repository.client.ClientID,
		ClientSecret: "client-secret", RefreshToken: "refresh-token-1",
	}); !errors.Is(err, ErrInvalidGrant) {
		t.Fatalf("旧刷新令牌应立即失效，实际为 %v", err)
	}
}

func TestProviderRejectsWrongClientSecretAndPKCE(t *testing.T) {
	repository := newProviderRepositoryStub()
	signer, _ := NewRS256Signer("")
	service, _ := NewProviderService(repository, signer, "https://id.example.com")
	if _, err := service.ExchangeToken(context.Background(), TokenInput{
		GrantType: "authorization_code", ClientID: repository.client.ClientID, ClientSecret: "wrong",
	}); !errors.Is(err, ErrInvalidClient) {
		t.Fatalf("错误密钥应返回 ErrInvalidClient，实际为 %v", err)
	}
	if _, err := service.Authorize(context.Background(), AuthorizeInput{
		Subject: repository.subject, ClientID: repository.client.ClientID,
		RedirectURI: "https://attacker.example.com/callback", Scope: "openid",
		CodeChallenge: "abcdefghijklmnopqrstuvwxyzABCDEFGH0123456789", ChallengeMethod: "S256",
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("未登记回调地址应被拒绝，实际为 %v", err)
	}
}

type providerRepositoryStub struct {
	client  ProtocolClient
	subject Subject
	grant   AuthorizationGrant
	refresh RefreshGrant
}

func newProviderRepositoryStub() *providerRepositoryStub {
	digest := sha256.Sum256([]byte("client-secret"))
	return &providerRepositoryStub{
		client: ProtocolClient{Client: Client{
			ID: "client-record-1", OrganizationID: "org-1", ClientID: "client-1",
			RedirectURIs:  []string{"https://deepseek.example.com/callback"},
			AllowedScopes: []string{"openid", "profile", "email", "model.invoke", "offline_access"}, Status: "active",
		}, ClientSecretHash: digest[:]},
		subject: Subject{ID: "user-1", OrganizationID: "org-1", Organization: "测试企业", OrganizationCode: "test", Username: "admin", DisplayName: "管理员", Email: "admin@example.com"},
	}
}

func (r *providerRepositoryStub) FindProtocolClient(_ context.Context, clientID string) (ProtocolClient, error) {
	if clientID != r.client.ClientID {
		return ProtocolClient{}, ErrClientNotFound
	}
	return r.client, nil
}

func (r *providerRepositoryStub) SaveAuthorizationGrant(_ context.Context, grant AuthorizationGrant) error {
	r.grant = grant
	return nil
}

func (r *providerRepositoryStub) ExchangeAuthorizationGrant(_ context.Context, hash []byte, clientID, redirectURI, challenge string) (AuthorizationGrant, error) {
	if r.grant.ID == "" || string(hash) != string(r.grant.CodeHash) || clientID != r.grant.ClientRecordID || redirectURI != r.grant.RedirectURI || challenge != r.grant.CodeChallenge {
		return AuthorizationGrant{}, ErrInvalidGrant
	}
	grant := r.grant
	grant.Subject = r.subject
	r.grant = AuthorizationGrant{}
	return grant, nil
}

func (r *providerRepositoryStub) SaveRefreshGrant(_ context.Context, grant RefreshGrant) error {
	r.refresh = grant
	return nil
}

func (r *providerRepositoryStub) RotateRefreshGrant(_ context.Context, hash []byte, clientID string, replacement RefreshGrant) (RefreshGrant, error) {
	if r.refresh.ID == "" || string(hash) != string(r.refresh.TokenHash) || clientID != r.refresh.ClientRecordID {
		return RefreshGrant{}, ErrInvalidGrant
	}
	replacement.OrganizationID = r.refresh.OrganizationID
	replacement.ClientRecordID = r.refresh.ClientRecordID
	replacement.UserID = r.refresh.UserID
	replacement.Scopes = r.refresh.Scopes
	replacement.Subject = r.subject
	r.refresh = replacement
	return replacement, nil
}

func (r *providerRepositoryStub) FindSubject(_ context.Context, organizationID, userID string) (Subject, error) {
	if organizationID != r.subject.OrganizationID || userID != r.subject.ID {
		return Subject{}, errors.New("not found")
	}
	return r.subject, nil
}
