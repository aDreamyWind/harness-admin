package oidc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	clientIDPrefix     = "harness_client_"
	clientSecretPrefix = "harness_client_secret_"
)

var supportedScopes = map[string]struct{}{
	"openid": {}, "profile": {}, "email": {}, "model.invoke": {}, "mcp.invoke": {}, "offline_access": {},
}

type Service struct {
	repository  Repository
	newClientID func() (string, error)
	newSecret   func() (string, error)
}

func NewService(repository Repository) *Service {
	return &Service{
		repository:  repository,
		newClientID: func() (string, error) { return randomValue(clientIDPrefix, 18) },
		newSecret:   func() (string, error) { return randomValue(clientSecretPrefix, 32) },
	}
}

func (s *Service) ListClients(ctx context.Context, organizationID string) ([]Client, error) {
	if organizationID == "" {
		return nil, ErrInvalidInput
	}
	items, err := s.repository.ListClients(ctx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("查询统一登录应用失败: %w", err)
	}
	return items, nil
}

func (s *Service) CreateClient(ctx context.Context, input CreateClientInput) (CreatedClient, error) {
	normalizeSettings(&input.Name, &input.RedirectURIs, &input.AllowedScopes)
	input.ApplicationType = strings.ToLower(strings.TrimSpace(input.ApplicationType))
	if input.ApplicationType == "" {
		input.ApplicationType = ApplicationTypeWeb
	}
	if !validSettings(input.OrganizationID, input.ActorID, input.Name, input.ApplicationType, input.RedirectURIs, input.AllowedScopes) {
		return CreatedClient{}, ErrInvalidInput
	}
	clientID, err := s.newClientID()
	if err != nil {
		return CreatedClient{}, fmt.Errorf("生成 OAuth 客户端标识失败: %w", err)
	}
	input.ClientID = clientID
	var secret string
	if input.ApplicationType == ApplicationTypeDesktop {
		input.TokenEndpointAuthMethod = TokenEndpointAuthNone
	} else {
		input.TokenEndpointAuthMethod = TokenEndpointAuthClientSecretBasic
		secret, err = s.newSecret()
		if err != nil {
			return CreatedClient{}, fmt.Errorf("生成 OAuth 客户端密钥失败: %w", err)
		}
		digest := sha256.Sum256([]byte(secret))
		input.ClientSecretHash = digest[:]
		input.ClientSecretPrefix = visibleSecretPrefix(secret)
		input.ClientSecret = secret
	}
	created, err := s.repository.CreateClient(ctx, input)
	if err != nil {
		return CreatedClient{}, fmt.Errorf("创建统一登录应用失败: %w", err)
	}
	return CreatedClient{Client: created, ClientSecret: secret}, nil
}

func (s *Service) UpdateClient(ctx context.Context, input UpdateClientInput) (Client, error) {
	normalizeSettings(&input.Name, &input.RedirectURIs, &input.AllowedScopes)
	if input.ClientRecordID == "" || !validSettings(input.OrganizationID, input.ActorID, input.Name, "", input.RedirectURIs, input.AllowedScopes) {
		return Client{}, ErrInvalidInput
	}
	item, err := s.repository.UpdateClient(ctx, input)
	if err != nil {
		if errors.Is(err, ErrClientNotFound) {
			return Client{}, err
		}
		return Client{}, fmt.Errorf("修改统一登录应用失败: %w", err)
	}
	return item, nil
}

func (s *Service) ChangeClientStatus(ctx context.Context, input ChangeClientStatusInput) (Client, error) {
	if input.OrganizationID == "" || input.ActorID == "" || input.ClientRecordID == "" || (input.Status != "active" && input.Status != "disabled") {
		return Client{}, ErrInvalidInput
	}
	item, err := s.repository.ChangeClientStatus(ctx, input)
	if err != nil {
		if errors.Is(err, ErrClientNotFound) {
			return Client{}, err
		}
		return Client{}, fmt.Errorf("修改统一登录应用状态失败: %w", err)
	}
	return item, nil
}

func (s *Service) RotateClientSecret(ctx context.Context, input RotateClientSecretInput) (CreatedClient, error) {
	if input.OrganizationID == "" || input.ActorID == "" || input.ClientRecordID == "" {
		return CreatedClient{}, ErrInvalidInput
	}
	secret, err := s.newSecret()
	if err != nil {
		return CreatedClient{}, fmt.Errorf("生成 OAuth 客户端密钥失败: %w", err)
	}
	digest := sha256.Sum256([]byte(secret))
	input.ClientSecretHash = digest[:]
	input.ClientSecretPrefix = visibleSecretPrefix(secret)
	input.ClientSecret = secret
	item, err := s.repository.RotateClientSecret(ctx, input)
	if err != nil {
		if errors.Is(err, ErrClientNotFound) || errors.Is(err, ErrClientSecretUnsupported) {
			return CreatedClient{}, err
		}
		return CreatedClient{}, fmt.Errorf("轮换 OAuth 客户端密钥失败: %w", err)
	}
	return CreatedClient{Client: item, ClientSecret: secret}, nil
}

func (s *Service) RevealClientSecret(ctx context.Context, input RevealClientSecretInput) (string, error) {
	if input.OrganizationID == "" || input.ActorID == "" || input.ClientRecordID == "" {
		return "", ErrInvalidInput
	}
	value, err := s.repository.RevealClientSecret(ctx, input)
	if err != nil {
		if errors.Is(err, ErrClientNotFound) || errors.Is(err, ErrClientSecretUnsupported) || errors.Is(err, ErrClientSecretUnavailable) {
			return "", err
		}
		return "", fmt.Errorf("查看 OAuth 客户端密钥失败: %w", err)
	}
	return value, nil
}

func normalizeSettings(name *string, redirectURIs, scopes *[]string) {
	*name = strings.TrimSpace(*name)
	*redirectURIs = normalizeValues(*redirectURIs, false)
	*scopes = normalizeValues(*scopes, true)
}

func normalizeValues(values []string, lower bool) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if lower {
			value = strings.ToLower(value)
		}
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func validSettings(organizationID, actorID, name, applicationType string, redirectURIs, scopes []string) bool {
	if organizationID == "" || actorID == "" || utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 120 || len(redirectURIs) < 1 || len(redirectURIs) > 20 || len(scopes) < 1 || len(scopes) > 10 {
		return false
	}
	if applicationType != "" && applicationType != ApplicationTypeWeb && applicationType != ApplicationTypeDesktop {
		return false
	}
	for _, redirectURI := range redirectURIs {
		if !validRedirectURI(redirectURI) {
			return false
		}
	}
	hasOpenID := false
	for _, scope := range scopes {
		if _, ok := supportedScopes[scope]; !ok {
			return false
		}
		hasOpenID = hasOpenID || scope == "openid"
	}
	return hasOpenID
}

func validRedirectURI(value string) bool {
	if len(value) > 2048 {
		return false
	}
	parsed, err := url.Parse(value)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return false
	}
	if parsed.Scheme == "https" {
		return true
	}
	if parsed.Scheme != "http" {
		return false
	}
	hostname := parsed.Hostname()
	if strings.EqualFold(hostname, "localhost") {
		return true
	}
	ip := net.ParseIP(hostname)
	return ip != nil && ip.IsLoopback()
}

func randomValue(prefix string, length int) (string, error) {
	buffer := make([]byte, length)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(buffer), nil
}

func visibleSecretPrefix(secret string) string {
	const length = 30
	if len(secret) <= length {
		return secret
	}
	return secret[:length]
}
