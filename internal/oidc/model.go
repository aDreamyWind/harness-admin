package oidc

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalidInput   = errors.New("应用接入参数不正确")
	ErrClientNotFound = errors.New("统一登录应用不存在")
	ErrInvalidClient  = errors.New("客户端认证失败")
	ErrInvalidGrant   = errors.New("授权凭据无效或已过期")
	ErrInvalidToken   = errors.New("访问令牌无效或已过期")
)

type Client struct {
	ID                 string
	OrganizationID     string
	Name               string
	ClientID           string
	ClientSecretPrefix string
	RedirectURIs       []string
	AllowedScopes      []string
	Status             string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type CreatedClient struct {
	Client
	ClientSecret string
}

type CreateClientInput struct {
	OrganizationID     string
	ActorID            string
	Name               string
	RedirectURIs       []string
	AllowedScopes      []string
	RequestID          string
	SourceIP           string
	ClientID           string
	ClientSecretHash   []byte
	ClientSecretPrefix string
}

type UpdateClientInput struct {
	OrganizationID string
	ActorID        string
	ClientRecordID string
	Name           string
	RedirectURIs   []string
	AllowedScopes  []string
	RequestID      string
	SourceIP       string
}

type ChangeClientStatusInput struct {
	OrganizationID string
	ActorID        string
	ClientRecordID string
	Status         string
	RequestID      string
	SourceIP       string
}

type RotateClientSecretInput struct {
	OrganizationID     string
	ActorID            string
	ClientRecordID     string
	ClientSecretHash   []byte
	ClientSecretPrefix string
	RequestID          string
	SourceIP           string
}

type Repository interface {
	ListClients(context.Context, string) ([]Client, error)
	CreateClient(context.Context, CreateClientInput) (Client, error)
	UpdateClient(context.Context, UpdateClientInput) (Client, error)
	ChangeClientStatus(context.Context, ChangeClientStatusInput) (Client, error)
	RotateClientSecret(context.Context, RotateClientSecretInput) (Client, error)
}

type ProtocolClient struct {
	Client
	ClientSecretHash []byte
}

type Subject struct {
	ID               string
	OrganizationID   string
	Organization     string
	OrganizationCode string
	Username         string
	DisplayName      string
	Email            string
}

type AuthorizationGrant struct {
	ID             string
	OrganizationID string
	ClientRecordID string
	UserID         string
	CodeHash       []byte
	RedirectURI    string
	Scopes         []string
	Nonce          string
	CodeChallenge  string
	ExpiresAt      time.Time
	Subject        Subject
}

type RefreshGrant struct {
	ID             string
	OrganizationID string
	ClientRecordID string
	UserID         string
	TokenHash      []byte
	Scopes         []string
	ExpiresAt      time.Time
	Subject        Subject
}

type ProviderRepository interface {
	FindProtocolClient(context.Context, string) (ProtocolClient, error)
	SaveAuthorizationGrant(context.Context, AuthorizationGrant) error
	ExchangeAuthorizationGrant(context.Context, []byte, string, string, string) (AuthorizationGrant, error)
	SaveRefreshGrant(context.Context, RefreshGrant) error
	RotateRefreshGrant(context.Context, []byte, string, RefreshGrant) (RefreshGrant, error)
	FindSubject(context.Context, string, string) (Subject, error)
}

type AuthorizeInput struct {
	Subject         Subject
	ClientID        string
	RedirectURI     string
	Scope           string
	State           string
	Nonce           string
	CodeChallenge   string
	ChallengeMethod string
}

type AuthorizationResult struct {
	RedirectURI string
}

type TokenInput struct {
	GrantType    string
	ClientID     string
	ClientSecret string
	Code         string
	RedirectURI  string
	CodeVerifier string
	RefreshToken string
}

type TokenResult struct {
	AccessToken  string
	TokenType    string
	ExpiresIn    int64
	IDToken      string
	RefreshToken string
	Scope        string
}

type UserInfo struct {
	Subject           string `json:"sub"`
	Name              string `json:"name,omitempty"`
	PreferredUsername string `json:"preferred_username,omitempty"`
	Email             string `json:"email,omitempty"`
	OrganizationID    string `json:"org_id"`
	OrganizationCode  string `json:"org_code"`
}
