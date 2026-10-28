package modelgateway

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalidInput            = errors.New("模型网关参数不正确")
	ErrInvalidCredential       = errors.New("模型网关调用密钥无效")
	ErrAPIKeyNotFound          = errors.New("模型网关调用密钥不存在")
	ErrAPIKeySecretUnavailable = errors.New("模型网关调用密钥尚未加密保存")
	ErrAPIKeyRevoked           = errors.New("模型网关调用密钥已吊销")
	ErrAPIKeyExpired           = errors.New("模型网关调用密钥已过期")
	ErrModelNotFound           = errors.New("请求的模型编码不存在")
	ErrModelDisabled           = errors.New("请求的模型已停用")
	ErrProviderDisabled        = errors.New("模型供应商连接已停用")
	ErrModelNotAllowed         = errors.New("当前调用身份无权访问该模型")
	ErrModelTypeMismatch       = errors.New("模型类型与调用接口不匹配")
	ErrVideoTaskNotFound       = errors.New("视频任务不存在")
	ErrRateLimited             = errors.New("调用频率超过限制")
	ErrMonthlyQuota            = errors.New("当月 Token 额度已用尽")
)

type APIKey struct {
	ID                string
	OrganizationID    string
	Name              string
	KeyPrefix         string
	Status            string
	ExpiresAt         *time.Time
	LastUsedAt        *time.Time
	RequestsPerMinute int
	MonthlyTokenLimit int64
	MonthlyTokenUsed  int64
	AllowedModelCodes []string
	AllowedModels     []AllowedModel
	CreatedAt         time.Time
	RevokedAt         *time.Time
}

type AllowedModel struct {
	Code         string `json:"code"`
	ProviderName string `json:"providerName"`
	ModelName    string `json:"modelName"`
}

type CreatedAPIKey struct {
	APIKey
	Secret string
}

type CreateAPIKeyInput struct {
	OrganizationID    string
	ActorID           string
	Name              string
	ExpiresAt         *time.Time
	RequestsPerMinute int
	MonthlyTokenLimit int64
	AllowedModelCodes []string
	RequestID         string
	SourceIP          string
	KeyPrefix         string
	SecretHash        []byte
	Secret            string
}

type RevealAPIKeySecretInput struct {
	OrganizationID string
	ActorID        string
	APIKeyID       string
	RequestID      string
	SourceIP       string
}

type UpdateAPIKeyInput struct {
	OrganizationID    string
	ActorID           string
	APIKeyID          string
	Name              string
	ExpiresAt         *time.Time
	RequestsPerMinute int
	MonthlyTokenLimit int64
	AllowedModelCodes []string
	RequestID         string
	SourceIP          string
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
	UserID         string
	DepartmentID   string
	AuthType       string
}

type Route struct {
	ModelID                    string
	ModelCode                  string
	ModelType                  string
	ModelStatus                string
	UpstreamModelID            string
	ProviderID                 string
	ProviderKind               string
	ProviderStatus             string
	ProviderBaseURL            string
	ProviderAPIKey             string
	ProviderTimeoutMS          int
	ProviderStreamUsageEnabled bool
}

type PreparedRequest struct {
	Caller    Caller
	Route     Route
	Body      []byte
	Streaming bool
}

type VideoTask struct {
	ID             string
	Caller         Caller
	Route          Route
	UpstreamTaskID string
}

type SaveVideoTaskInput struct {
	Caller         Caller
	Route          Route
	UpstreamTaskID string
	RequestID      string
	SourceIP       string
}

type Call struct {
	ID               string
	APIKeyID         string
	APIKeyName       string
	AuthType         string
	UserID           string
	UserName         string
	DepartmentID     string
	RequestID        string
	ModelCode        string
	ModelName        string
	ModelType        string
	UpstreamModelID  string
	ProviderName     string
	Streaming        bool
	Result           string
	HTTPStatus       int
	LatencyMS        int64
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	UsageReported    bool
	QuotaExceeded    bool
	ErrorCode        string
	SourceIP         string
	CreatedAt        time.Time
}

type CallQuery struct {
	OrganizationID string
	APIKeyID       string
	ModelCode      string
	Result         string
	StartedAt      *time.Time
	EndedAt        *time.Time
	Page           int
	PageSize       int
}

type CallPage struct {
	Items    []Call
	Total    int64
	Page     int
	PageSize int
}

type RecordCallInput struct {
	OrganizationID    string
	APIKeyID          string
	UserID            string
	DepartmentID      string
	AuthType          string
	ModelID           string
	ProviderID        string
	RequestID         string
	ModelCode         string
	ModelType         string
	UpstreamModelID   string
	Streaming         bool
	Result            string
	HTTPStatus        int
	LatencyMS         int64
	PromptTokens      int
	CompletionTokens  int
	TotalTokens       int
	UsageReported     bool
	ErrorCode         string
	SourceIP          string
	Source            string
	Operation         string
	RequestBody       []byte
	ResponseBody      []byte
	ResponseTruncated bool
	ErrorMessage      string
}

type UsageQuery struct {
	OrganizationID string
	Month          string
	GroupBy        string
	Page           int
	PageSize       int
}

type UsageGroup struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	CallCount         int64  `json:"callCount"`
	KnownTokens       int64  `json:"knownTokens"`
	UnknownUsageCalls int64  `json:"unknownUsageCalls"`
	OverQuotaCalls    int64  `json:"overQuotaCalls"`
}

type UsagePage struct {
	Month             string       `json:"month"`
	GroupBy           string       `json:"groupBy"`
	CallCount         int64        `json:"callCount"`
	KnownTokens       int64        `json:"knownTokens"`
	UnknownUsageCalls int64        `json:"unknownUsageCalls"`
	OverQuotaCalls    int64        `json:"overQuotaCalls"`
	Items             []UsageGroup `json:"items"`
	Total             int64        `json:"total"`
	Page              int          `json:"page"`
	PageSize          int          `json:"pageSize"`
}

type AlertCandidate struct {
	DedupeKey         string `json:"dedupeKey"`
	Kind              string `json:"kind"`
	SubjectType       string `json:"subjectType"`
	SubjectName       string `json:"subjectName"`
	UsedTokens        int64  `json:"usedTokens"`
	TokenLimit        int64  `json:"tokenLimit"`
	CallCount         int64  `json:"callCount"`
	UnknownUsageCalls int64  `json:"unknownUsageCalls"`
}

type AlertSignal struct {
	AlertCandidate
	Severity string `json:"severity"`
}

type Alert struct {
	ID    string `json:"id"`
	Month string `json:"month"`
	AlertSignal
	FirstDetectedAt time.Time  `json:"firstDetectedAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
	AcknowledgedAt  *time.Time `json:"acknowledgedAt,omitempty"`
	ResolvedAt      *time.Time `json:"resolvedAt,omitempty"`
}

type AlertQuery struct {
	OrganizationID string
	Month          string
	Status         string
	Page           int
	PageSize       int
}

type AlertPage struct {
	Month       string  `json:"month"`
	ActiveCount int64   `json:"activeCount"`
	Items       []Alert `json:"items"`
	Total       int64   `json:"total"`
	Page        int     `json:"page"`
	PageSize    int     `json:"pageSize"`
}

type AcknowledgeAlertInput struct {
	OrganizationID string
	ActorID        string
	AlertID        string
	RequestID      string
	SourceIP       string
}

type AlertSettings struct {
	AutoRefreshEnabled bool       `json:"autoRefreshEnabled"`
	UpdatedAt          *time.Time `json:"updatedAt,omitempty"`
}

type UpdateAlertSettingsInput struct {
	OrganizationID     string
	ActorID            string
	AutoRefreshEnabled bool
	RequestID          string
	SourceIP           string
}

type Repository interface {
	ListAPIKeys(context.Context, string) ([]APIKey, error)
	CreateAPIKey(context.Context, CreateAPIKeyInput) (APIKey, error)
	UpdateAPIKey(context.Context, UpdateAPIKeyInput) (APIKey, error)
	RevokeAPIKey(context.Context, RevokeAPIKeyInput) (APIKey, error)
	RevealAPIKeySecret(context.Context, RevealAPIKeySecretInput) (string, error)
	FindAPIKeyByHash(context.Context, []byte) (APIKey, error)
	TouchAPIKey(context.Context, string) error
	FindRoute(context.Context, string, string) (Route, error)
	SaveVideoTask(context.Context, SaveVideoTaskInput) (VideoTask, error)
	FindVideoTask(context.Context, Caller, string) (VideoTask, error)
	RecordCall(context.Context, RecordCallInput) error
	ListCalls(context.Context, CallQuery) (CallPage, error)
	ListUsage(context.Context, UsageQuery) (UsagePage, error)
	ListAlertCandidates(context.Context, string, string) ([]AlertCandidate, error)
	SyncAlerts(context.Context, string, string, []AlertSignal) error
	ListAlerts(context.Context, AlertQuery) (AlertPage, error)
	AcknowledgeAlert(context.Context, AcknowledgeAlertInput) (Alert, error)
	GetAlertSettings(context.Context, string) (AlertSettings, error)
	UpdateAlertSettings(context.Context, UpdateAlertSettingsInput) (AlertSettings, error)
	ListAutoRefreshOrganizations(context.Context) ([]string, error)
}

type RequestLimiter interface {
	Allow(context.Context, string, int) (bool, error)
}
