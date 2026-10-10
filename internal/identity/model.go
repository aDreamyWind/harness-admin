package identity

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalidCredentials = errors.New("用户名或密码错误")
	ErrAccountDisabled    = errors.New("账号不可用")
	ErrTooManyAttempts    = errors.New("登录尝试过于频繁")
	ErrUnauthorized       = errors.New("登录状态无效")
	ErrInvalidCSRF        = errors.New("请求校验失败")
	ErrUserNotFound       = errors.New("用户不存在")
	ErrSessionNotFound    = errors.New("会话不存在")
)

type User struct {
	ID               string
	OrganizationID   string
	Organization     string
	OrganizationCode string
	Username         string
	Email            string
	DisplayName      string
	PasswordHash     string
	Status           string
}

type Session struct {
	ID            string
	User          User
	TokenHash     string
	CSRFTokenHash string
	ExpiresAt     time.Time
	RevokedAt     *time.Time
}

type NewSession struct {
	ID            string
	UserID        string
	TokenHash     string
	CSRFTokenHash string
	UserAgent     string
	SourceIP      string
	ExpiresAt     time.Time
}

type AuditEvent struct {
	OrganizationID string
	ActorID        string
	RequestID      string
	Action         string
	ResourceType   string
	ResourceID     string
	Result         string
	SourceIP       string
	Detail         map[string]any
}

type Repository interface {
	FindUserForLogin(ctx context.Context, organizationCode, username string) (User, error)
	SaveLogin(ctx context.Context, session NewSession, event AuditEvent) error
	FindSessionByTokenHash(ctx context.Context, tokenHash string) (Session, error)
	RevokeSession(ctx context.Context, sessionID string, event AuditEvent) error
	RecordAudit(ctx context.Context, event AuditEvent) error
}

type LoginLimiter interface {
	Allow(ctx context.Context, key string) (bool, error)
	RecordFailure(ctx context.Context, key string) error
	Clear(ctx context.Context, key string) error
}

type LoginInput struct {
	OrganizationCode string
	Username         string
	Password         string
	UserAgent        string
	SourceIP         string
	RequestID        string
}

type LoginResult struct {
	User      User
	Token     string
	CSRFToken string
	ExpiresAt time.Time
}

type BootstrapAdminInput struct {
	OrganizationCode string
	OrganizationName string
	Username         string
	DisplayName      string
	Email            string
	PasswordHash     string
}

type BootstrapAdminResult struct {
	OrganizationID string
	UserID         string
	Created        bool
}

type BootstrapRepository interface {
	BootstrapAdmin(ctx context.Context, input BootstrapAdminInput) (BootstrapAdminResult, error)
}
