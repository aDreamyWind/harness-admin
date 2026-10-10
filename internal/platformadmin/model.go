package platformadmin

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalidCredentials   = errors.New("用户名或密码错误")
	ErrAccountDisabled      = errors.New("平台账号不可用")
	ErrTooManyAttempts      = errors.New("登录尝试过于频繁")
	ErrUnauthorized         = errors.New("平台登录状态无效")
	ErrInvalidCSRF          = errors.New("请求校验失败")
	ErrOrganizationConflict = errors.New("企业编码已存在")
	ErrOrganizationNotFound = errors.New("企业不存在")
	ErrInvalidInput         = errors.New("企业参数不正确")
)

type Admin struct {
	ID           string
	Username     string
	DisplayName  string
	PasswordHash string
	Status       string
}

type Session struct {
	ID            string
	Admin         Admin
	TokenHash     string
	CSRFTokenHash string
	ExpiresAt     time.Time
	RevokedAt     *time.Time
}

type NewSession struct {
	ID            string
	AdminID       string
	TokenHash     string
	CSRFTokenHash string
	UserAgent     string
	SourceIP      string
	ExpiresAt     time.Time
}

type LoginInput struct {
	Username  string
	Password  string
	UserAgent string
	SourceIP  string
	RequestID string
}

type LoginResult struct {
	Admin     Admin
	Token     string
	CSRFToken string
	ExpiresAt time.Time
}

type Organization struct {
	ID            string
	Code          string
	Name          string
	Status        string
	AdminUsername string
	UserCount     int64
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type CreateOrganizationInput struct {
	Session          Session
	Code             string
	Name             string
	AdminUsername    string
	AdminDisplayName string
	AdminEmail       string
	AdminPassword    string
	RequestID        string
	SourceIP         string
}

type OrganizationCreation struct {
	ActorID           string
	Code              string
	Name              string
	AdminUsername     string
	AdminDisplayName  string
	AdminEmail        string
	AdminPasswordHash string
	RequestID         string
	SourceIP          string
}

type StatusUpdate struct {
	ActorID        string
	OrganizationID string
	Status         string
	RequestID      string
	SourceIP       string
}

type AuditEvent struct {
	ActorID      string
	RequestID    string
	Action       string
	ResourceType string
	ResourceID   string
	Result       string
	SourceIP     string
	Detail       map[string]any
}

type BootstrapInput struct {
	Username     string
	DisplayName  string
	PasswordHash string
}

type BootstrapResult struct {
	AdminID string
	Created bool
}

type Repository interface {
	FindAdminForLogin(context.Context, string) (Admin, error)
	SaveLogin(context.Context, NewSession, AuditEvent) error
	FindSessionByTokenHash(context.Context, string) (Session, error)
	RevokeSession(context.Context, string, AuditEvent) error
	RecordAudit(context.Context, AuditEvent) error
	ListOrganizations(context.Context) ([]Organization, error)
	CreateOrganization(context.Context, OrganizationCreation) (Organization, error)
	UpdateOrganizationStatus(context.Context, StatusUpdate) (Organization, error)
	BootstrapPlatformAdmin(context.Context, BootstrapInput) (BootstrapResult, error)
}

type LoginLimiter interface {
	Allow(context.Context, string) (bool, error)
	RecordFailure(context.Context, string) (attempts int64, limitReached bool, err error)
	Clear(context.Context, string) error
}

type PasswordHasher func(string) (string, error)
type PasswordVerifier func(string, string) bool
