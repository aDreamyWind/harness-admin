package modeladmin

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalidInput     = errors.New("模型接入参数不正确")
	ErrProviderNotFound = errors.New("模型供应商连接不存在")
	ErrProviderConflict = errors.New("模型供应商连接编码已存在")
	ErrModelNotFound    = errors.New("模型不存在")
	ErrModelConflict    = errors.New("模型编码或上游模型标识已存在")
	ErrProviderDisabled = errors.New("模型供应商连接已停用")
	ErrProbeFailed      = errors.New("模型供应商连接检测失败")
)

type Provider struct {
	ID                   string
	Code                 string
	Name                 string
	ProviderKind         string
	BaseURL              string
	Status               string
	RequestTimeoutMS     int
	CredentialConfigured bool
	LastProbeStatus      string
	LastProbeAt          *time.Time
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type CatalogModel struct {
	ID                   string
	ProviderConnectionID string
	ProviderName         string
	Code                 string
	UpstreamModelID      string
	Name                 string
	ModelType            string
	Capabilities         []string
	ContextWindowTokens  int
	MaxOutputTokens      int
	Status               string
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type CreateProviderInput struct {
	OrganizationID   string
	ActorID          string
	Code             string
	Name             string
	ProviderKind     string
	BaseURL          string
	APIKey           string
	RequestTimeoutMS int
	RequestID        string
	SourceIP         string
}

type UpdateProviderInput struct {
	OrganizationID   string
	ActorID          string
	ProviderID       string
	Name             string
	ProviderKind     string
	BaseURL          string
	APIKey           *string
	RequestTimeoutMS int
	RequestID        string
	SourceIP         string
}

type ChangeProviderStatusInput struct {
	OrganizationID string
	ActorID        string
	ProviderID     string
	Status         string
	RequestID      string
	SourceIP       string
}

type CreateModelInput struct {
	OrganizationID       string
	ActorID              string
	ProviderConnectionID string
	Code                 string
	UpstreamModelID      string
	Name                 string
	ModelType            string
	Capabilities         []string
	ContextWindowTokens  int
	MaxOutputTokens      int
	RequestID            string
	SourceIP             string
}

type UpdateModelInput struct {
	OrganizationID       string
	ActorID              string
	ModelID              string
	ProviderConnectionID string
	UpstreamModelID      string
	Name                 string
	ModelType            string
	Capabilities         []string
	ContextWindowTokens  int
	MaxOutputTokens      int
	RequestID            string
	SourceIP             string
}

type ChangeModelStatusInput struct {
	OrganizationID string
	ActorID        string
	ModelID        string
	Status         string
	RequestID      string
	SourceIP       string
}

type ProbeInput struct {
	OrganizationID string
	ActorID        string
	ProviderID     string
	RequestID      string
	SourceIP       string
}

type ProbeResult struct {
	Status    string
	LatencyMS int64
	CheckedAt time.Time
}

type ProviderCredential struct {
	Provider Provider
	APIKey   string
}

type Repository interface {
	ListProviders(context.Context, string) ([]Provider, error)
	CreateProvider(context.Context, CreateProviderInput) (Provider, error)
	UpdateProvider(context.Context, UpdateProviderInput) (Provider, error)
	ChangeProviderStatus(context.Context, ChangeProviderStatusInput) (Provider, error)
	GetProviderCredential(context.Context, string, string) (ProviderCredential, error)
	RecordProviderProbe(context.Context, ProbeInput, ProbeResult, string) (Provider, error)
	ListModels(context.Context, string) ([]CatalogModel, error)
	CreateModel(context.Context, CreateModelInput) (CatalogModel, error)
	UpdateModel(context.Context, UpdateModelInput) (CatalogModel, error)
	ChangeModelStatus(context.Context, ChangeModelStatusInput) (CatalogModel, error)
}

type ProviderProber interface {
	Probe(context.Context, ProviderCredential) (ProbeResult, error)
}
