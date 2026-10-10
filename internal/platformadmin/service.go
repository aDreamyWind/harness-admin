package platformadmin

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const dummyPasswordHash = "$argon2id$v=19$m=65536,t=3,p=2$MDEyMzQ1Njc4OWFiY2RlZg$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

var codePattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{1,63}$`)

type Service struct {
	repository Repository
	limiter    LoginLimiter
	hash       PasswordHasher
	verify     PasswordVerifier
	sessionTTL time.Duration
	now        func() time.Time
}

func NewService(repository Repository, limiter LoginLimiter, hash PasswordHasher, verify PasswordVerifier, sessionTTL time.Duration) *Service {
	return &Service{repository: repository, limiter: limiter, hash: hash, verify: verify, sessionTTL: sessionTTL, now: time.Now}
}

func (s *Service) Login(ctx context.Context, input LoginInput) (LoginResult, error) {
	username := strings.ToLower(strings.TrimSpace(input.Username))
	if username == "" || input.Password == "" || len(input.Password) > 1024 {
		return LoginResult{}, ErrInvalidCredentials
	}
	allowed, err := s.limiter.Allow(ctx, username)
	if err != nil {
		return LoginResult{}, fmt.Errorf("检查平台登录频率失败: %w", err)
	}
	if !allowed {
		return LoginResult{}, ErrTooManyAttempts
	}
	admin, err := s.repository.FindAdminForLogin(ctx, username)
	if err != nil {
		if errors.Is(err, ErrUnauthorized) {
			_ = s.verify(dummyPasswordHash, input.Password)
			return LoginResult{}, s.recordFailedLogin(ctx, input, username, "admin_not_found")
		}
		return LoginResult{}, fmt.Errorf("查询平台管理员失败: %w", err)
	}
	if !s.verify(admin.PasswordHash, input.Password) {
		return LoginResult{}, s.recordFailedLogin(ctx, input, username, "password_mismatch")
	}
	if admin.Status != "active" {
		return LoginResult{}, ErrAccountDisabled
	}
	if err := s.limiter.Clear(ctx, username); err != nil {
		return LoginResult{}, fmt.Errorf("清除平台登录失败计数失败: %w", err)
	}
	token, err := randomToken(32)
	if err != nil {
		return LoginResult{}, err
	}
	csrfToken, err := randomToken(32)
	if err != nil {
		return LoginResult{}, err
	}
	expiresAt := s.now().UTC().Add(s.sessionTTL)
	sessionID := uuid.NewString()
	if err := s.repository.SaveLogin(ctx, NewSession{
		ID: sessionID, AdminID: admin.ID, TokenHash: tokenHash(token), CSRFTokenHash: tokenHash(csrfToken),
		UserAgent: input.UserAgent, SourceIP: input.SourceIP, ExpiresAt: expiresAt,
	}, AuditEvent{
		ActorID: admin.ID, RequestID: input.RequestID, Action: "platform.auth.login", ResourceType: "platform_session",
		ResourceID: sessionID, Result: "success", SourceIP: input.SourceIP, Detail: map[string]any{"username": admin.Username},
	}); err != nil {
		return LoginResult{}, fmt.Errorf("保存平台登录会话失败: %w", err)
	}
	admin.PasswordHash = ""
	return LoginResult{Admin: admin, Token: token, CSRFToken: csrfToken, ExpiresAt: expiresAt}, nil
}

func (s *Service) Authenticate(ctx context.Context, token string) (Session, error) {
	if token == "" {
		return Session{}, ErrUnauthorized
	}
	session, err := s.repository.FindSessionByTokenHash(ctx, tokenHash(token))
	if err != nil {
		if errors.Is(err, ErrUnauthorized) {
			return Session{}, ErrUnauthorized
		}
		return Session{}, fmt.Errorf("查询平台登录会话失败: %w", err)
	}
	if session.RevokedAt != nil || !session.ExpiresAt.After(s.now().UTC()) || session.Admin.Status != "active" {
		return Session{}, ErrUnauthorized
	}
	session.Admin.PasswordHash = ""
	return session, nil
}

func (s *Service) ValidateCSRF(session Session, csrfToken string) error {
	if csrfToken == "" {
		return ErrInvalidCSRF
	}
	expected, err := hex.DecodeString(session.CSRFTokenHash)
	if err != nil {
		return ErrInvalidCSRF
	}
	actual := sha256.Sum256([]byte(csrfToken))
	if subtle.ConstantTimeCompare(actual[:], expected) != 1 {
		return ErrInvalidCSRF
	}
	return nil
}

func (s *Service) Logout(ctx context.Context, session Session, requestID, sourceIP string) error {
	return s.repository.RevokeSession(ctx, session.ID, AuditEvent{
		ActorID: session.Admin.ID, RequestID: requestID, Action: "platform.auth.logout", ResourceType: "platform_session",
		ResourceID: session.ID, Result: "success", SourceIP: sourceIP, Detail: map[string]any{},
	})
}

func (s *Service) ListOrganizations(ctx context.Context, session Session) ([]Organization, error) {
	if session.Admin.ID == "" {
		return nil, ErrUnauthorized
	}
	items, err := s.repository.ListOrganizations(ctx)
	if err != nil {
		return nil, fmt.Errorf("查询企业列表失败: %w", err)
	}
	return items, nil
}

func (s *Service) CreateOrganization(ctx context.Context, input CreateOrganizationInput) (Organization, error) {
	input.Code = strings.ToLower(strings.TrimSpace(input.Code))
	input.Name = strings.TrimSpace(input.Name)
	input.AdminUsername = strings.ToLower(strings.TrimSpace(input.AdminUsername))
	input.AdminDisplayName = strings.TrimSpace(input.AdminDisplayName)
	input.AdminEmail = strings.ToLower(strings.TrimSpace(input.AdminEmail))
	if input.Session.Admin.ID == "" || !codePattern.MatchString(input.Code) || input.AdminUsername == "" ||
		utf8.RuneCountInString(input.Name) < 1 || utf8.RuneCountInString(input.Name) > 160 ||
		utf8.RuneCountInString(input.AdminUsername) > 80 || utf8.RuneCountInString(input.AdminDisplayName) < 1 ||
		utf8.RuneCountInString(input.AdminDisplayName) > 120 || input.AdminPassword == "" || len(input.AdminPassword) > 1024 {
		return Organization{}, ErrInvalidInput
	}
	if input.AdminEmail != "" {
		address, err := mail.ParseAddress(input.AdminEmail)
		if err != nil || address.Address != input.AdminEmail {
			return Organization{}, ErrInvalidInput
		}
	}
	passwordHash, err := s.hash(input.AdminPassword)
	if err != nil {
		return Organization{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	organization, err := s.repository.CreateOrganization(ctx, OrganizationCreation{
		ActorID: input.Session.Admin.ID, Code: input.Code, Name: input.Name, AdminUsername: input.AdminUsername,
		AdminDisplayName: input.AdminDisplayName, AdminEmail: input.AdminEmail, AdminPasswordHash: passwordHash,
		RequestID: input.RequestID, SourceIP: input.SourceIP,
	})
	if err != nil {
		if errors.Is(err, ErrOrganizationConflict) {
			return Organization{}, err
		}
		return Organization{}, fmt.Errorf("创建企业失败: %w", err)
	}
	return organization, nil
}

func (s *Service) UpdateOrganizationStatus(ctx context.Context, session Session, organizationID, status, requestID, sourceIP string) (Organization, error) {
	if session.Admin.ID == "" || organizationID == "" || (status != "active" && status != "disabled") {
		return Organization{}, ErrInvalidInput
	}
	organization, err := s.repository.UpdateOrganizationStatus(ctx, StatusUpdate{
		ActorID: session.Admin.ID, OrganizationID: organizationID, Status: status, RequestID: requestID, SourceIP: sourceIP,
	})
	if err != nil {
		if errors.Is(err, ErrOrganizationNotFound) {
			return Organization{}, err
		}
		return Organization{}, fmt.Errorf("修改企业状态失败: %w", err)
	}
	return organization, nil
}

func (s *Service) recordFailedLogin(ctx context.Context, input LoginInput, username, reason string) error {
	attempts, limitReached, err := s.limiter.RecordFailure(ctx, username)
	if err != nil {
		return fmt.Errorf("记录平台登录失败次数失败: %w", err)
	}
	if err := s.repository.RecordAudit(ctx, AuditEvent{
		RequestID: input.RequestID, Action: "platform.auth.login", ResourceType: "platform_session", Result: "failed",
		SourceIP: input.SourceIP, Detail: map[string]any{"username": username, "reason": reason, "attempts": attempts},
	}); err != nil {
		return fmt.Errorf("记录平台登录失败审计失败: %w", err)
	}
	if limitReached {
		return ErrTooManyAttempts
	}
	return ErrInvalidCredentials
}

func randomToken(length int) (string, error) {
	buffer := make([]byte, length)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("生成安全令牌失败: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
