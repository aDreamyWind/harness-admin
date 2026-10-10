package identity

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalidCredentials  = errors.New("用户名或密码错误")
	ErrAccountDisabled     = errors.New("账号不可用")
	ErrTooManyAttempts     = errors.New("登录尝试过于频繁")
	ErrUnauthorized        = errors.New("登录状态无效")
	ErrInvalidCSRF         = errors.New("请求校验失败")
	ErrUserNotFound        = errors.New("用户不存在")
	ErrSessionNotFound     = errors.New("会话不存在")
	ErrCurrentPassword     = errors.New("当前密码不正确")
	ErrInvalidPassword     = errors.New("密码参数不正确")
	ErrPasswordUnchanged   = errors.New("新密码不能与当前密码相同")
	ErrCannotRevokeCurrent = errors.New("不能在会话管理中吊销当前会话")
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

type UserSession struct {
	ID         string
	UserAgent  string
	SourceIP   string
	ExpiresAt  time.Time
	LastSeenAt time.Time
	CreatedAt  time.Time
	Current    bool
}

type ChangePasswordInput struct {
	Session         Session
	CurrentPassword string
	NewPassword     string
	RequestID       string
	SourceIP        string
}

type PasswordUpdate struct {
	OrganizationID   string
	UserID           string
	CurrentSessionID string
	PasswordHash     string
	RequestID        string
	SourceIP         string
}

type SessionActionInput struct {
	OrganizationID   string
	UserID           string
	CurrentSessionID string
	TargetSessionID  string
	RequestID        string
	SourceIP         string
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
	FindUserPassword(ctx context.Context, organizationID, userID string) (string, error)
	UpdateOwnPassword(ctx context.Context, input PasswordUpdate) (int64, error)
	ListUserSessions(ctx context.Context, userID, currentSessionID string) ([]UserSession, error)
	RevokeUserSession(ctx context.Context, input SessionActionInput) error
	RevokeOtherUserSessions(ctx context.Context, input SessionActionInput) (int64, error)
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
