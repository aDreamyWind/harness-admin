package modelgateway

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalidInput      = errors.New("模型网关参数不正确")
	ErrInvalidCredential = errors.New("模型网关调用密钥无效")
	ErrAPIKeyNotFound    = errors.New("模型网关调用密钥不存在")
	ErrAPIKeyRevoked     = errors.New("模型网关调用密钥已吊销")
	ErrAPIKeyExpired     = errors.New("模型网关调用密钥已过期")
	ErrModelNotFound     = errors.New("请求的模型编码不存在")
	ErrModelDisabled     = errors.New("请求的模型已停用")
	ErrProviderDisabled  = errors.New("模型供应商连接已停用")
)

type APIKey struct {
	ID             string
	OrganizationID string
	Name           string
	KeyPrefix      string
	Status         string
	ExpiresAt      *time.Time
	LastUsedAt     *time.Time
	CreatedAt      time.Time
	RevokedAt      *time.Time
}

type CreatedAPIKey struct {
	APIKey
	Secret string
}

type CreateAPIKeyInput struct {
	OrganizationID string
	ActorID        string
	Name           string
	ExpiresAt      *time.Time
	RequestID      string
	SourceIP       string
	KeyPrefix      string
	SecretHash     []byte
}

type RevokeAPIKeyInput struct {
	OrganizationID string
	ActorID        string
	APIKeyID       string
	RequestID      string
	SourceIP       string
}

type Caller struct {
	OrganizationID string
	APIKeyID       string
}

type Route struct {
	ModelID           string
	ModelCode         string
	ModelStatus       string
	UpstreamModelID   string
	ProviderID        string
	ProviderStatus    string
	ProviderBaseURL   string
	ProviderAPIKey    string
	ProviderTimeoutMS int
}

type PreparedRequest struct {
	Caller    Caller
	Route     Route
	Body      []byte
	Streaming bool
}

type Call struct {
	ID               string
	APIKeyID         string
	APIKeyName       string
	RequestID        string
	ModelCode        string
	UpstreamModelID  string
	Streaming        bool
	Result           string
	HTTPStatus       int
	LatencyMS        int64
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	ErrorCode        string
	SourceIP         string
	CreatedAt        time.Time
}

type RecordCallInput struct {
	OrganizationID   string
	APIKeyID         string
	ModelID          string
	ProviderID       string
	RequestID        string
	ModelCode        string
	UpstreamModelID  string
	Streaming        bool
	Result           string
	HTTPStatus       int
	LatencyMS        int64
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	ErrorCode        string
	SourceIP         string
}

type Repository interface {
	ListAPIKeys(context.Context, string) ([]APIKey, error)
	CreateAPIKey(context.Context, CreateAPIKeyInput) (APIKey, error)
	RevokeAPIKey(context.Context, RevokeAPIKeyInput) (APIKey, error)
	FindAPIKeyByHash(context.Context, []byte) (APIKey, error)
	TouchAPIKey(context.Context, string) error
	FindRoute(context.Context, string, string) (Route, error)
	RecordCall(context.Context, RecordCallInput) error
	ListCalls(context.Context, string, int) ([]Call, error)
}
