package oidc

import (
	"context"
	"crypto/sha256"
	"testing"
)

type repositoryStub struct {
	created CreateClientInput
	updated UpdateClientInput
}

func (*repositoryStub) ListClients(context.Context, string) ([]Client, error) { return nil, nil }
func (r *repositoryStub) CreateClient(_ context.Context, input CreateClientInput) (Client, error) {
	r.created = input
	return Client{ID: "client-record-1", OrganizationID: input.OrganizationID, Name: input.Name,
		ClientID: input.ClientID, ClientSecretPrefix: input.ClientSecretPrefix,
		ApplicationType: input.ApplicationType, TokenEndpointAuthMethod: input.TokenEndpointAuthMethod,
		RedirectURIs: input.RedirectURIs, AllowedScopes: input.AllowedScopes, Status: "active"}, nil
}
func (r *repositoryStub) UpdateClient(_ context.Context, input UpdateClientInput) (Client, error) {
	r.updated = input
	return Client{ID: input.ClientRecordID, OrganizationID: input.OrganizationID, Name: input.Name,
		RedirectURIs: input.RedirectURIs, AllowedScopes: input.AllowedScopes}, nil
}
func (*repositoryStub) ChangeClientStatus(context.Context, ChangeClientStatusInput) (Client, error) {
	return Client{}, nil
}
func (*repositoryStub) RotateClientSecret(context.Context, RotateClientSecretInput) (Client, error) {
	return Client{}, nil
}
func (*repositoryStub) RevealClientSecret(context.Context, RevealClientSecretInput) (string, error) {
	return "harness_client_secret_revealed", nil
}

func TestCreateClientStoresSecretHashAndEncryptedSecretInput(t *testing.T) {
	repository := &repositoryStub{}
	service := NewService(repository)
	service.newClientID = func() (string, error) { return "harness_client_test", nil }
	service.newSecret = func() (string, error) { return "harness_client_secret_abcdefghijklmnopqrstuvwxyz", nil }

	created, err := service.CreateClient(context.Background(), CreateClientInput{
		OrganizationID: "org-1", ActorID: "user-1", Name: " DeepSeek Harness ",
		RedirectURIs:  []string{"https://harness.example.com/callback", "https://harness.example.com/callback"},
		AllowedScopes: []string{"MODEL.INVOKE", "openid", "profile"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ClientSecret != "harness_client_secret_abcdefghijklmnopqrstuvwxyz" {
		t.Fatalf("客户端密钥 = %q", created.ClientSecret)
	}
	expected := sha256.Sum256([]byte(created.ClientSecret))
	if string(repository.created.ClientSecretHash) != string(expected[:]) {
		t.Fatal("仓储输入未保存客户端密钥 SHA-256 哈希")
	}
	if repository.created.ClientSecret != created.ClientSecret {
		t.Fatal("仓储输入未收到用于加密保存的完整客户端密钥")
	}
	if repository.created.Name != "DeepSeek Harness" || len(repository.created.RedirectURIs) != 1 {
		t.Fatalf("客户端设置未规范化: %#v", repository.created)
	}
	wantScopes := []string{"model.invoke", "openid", "profile"}
	for index := range wantScopes {
		if repository.created.AllowedScopes[index] != wantScopes[index] {
			t.Fatalf("授权范围 = %#v", repository.created.AllowedScopes)
		}
	}
}

func TestCreateClientRejectsUnsafeRedirectURI(t *testing.T) {
	service := NewService(&repositoryStub{})
	for _, redirectURI := range []string{
		"http://harness.example.com/callback",
		"https://user@example.com/callback",
		"https://harness.example.com/callback#fragment",
	} {
		_, err := service.CreateClient(context.Background(), CreateClientInput{
			OrganizationID: "org-1", ActorID: "user-1", Name: "DeepSeek Harness",
			RedirectURIs: []string{redirectURI}, AllowedScopes: []string{"openid"},
		})
		if err != ErrInvalidInput {
			t.Fatalf("回调地址 %q 错误 = %v", redirectURI, err)
		}
	}
}

func TestCreateClientAllowsLoopbackHTTPRedirect(t *testing.T) {
	service := NewService(&repositoryStub{})
	service.newClientID = func() (string, error) { return "harness_client_test", nil }
	service.newSecret = func() (string, error) { return "harness_client_secret_test", nil }
	_, err := service.CreateClient(context.Background(), CreateClientInput{
		OrganizationID: "org-1", ActorID: "user-1", Name: "本地开发应用",
		RedirectURIs: []string{"http://127.0.0.1:9000/callback"}, AllowedScopes: []string{"openid"},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCreateDesktopClientDoesNotGenerateSecret(t *testing.T) {
	repository := &repositoryStub{}
	service := NewService(repository)
	service.newClientID = func() (string, error) { return "harness_client_desktop", nil }
	service.newSecret = func() (string, error) {
		t.Fatal("桌面应用不应生成客户端密钥")
		return "", nil
	}
	created, err := service.CreateClient(context.Background(), CreateClientInput{
		OrganizationID: "org-1", ActorID: "user-1", Name: "DeepSeek Harness",
		ApplicationType: ApplicationTypeDesktop,
		RedirectURIs:    []string{"http://127.0.0.1/oauth/callback"},
		AllowedScopes:   []string{"openid", "model.invoke"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ClientSecret != "" || len(repository.created.ClientSecretHash) != 0 || repository.created.ClientSecretPrefix != "" {
		t.Fatalf("桌面应用不应包含密钥: %#v", repository.created)
	}
	if repository.created.TokenEndpointAuthMethod != TokenEndpointAuthNone {
		t.Fatalf("令牌端点认证方式 = %q", repository.created.TokenEndpointAuthMethod)
	}
}
