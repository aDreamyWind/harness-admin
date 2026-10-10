package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Service struct {
	repository Repository
	limiter    LoginLimiter
	sessionTTL time.Duration
	now        func() time.Time
}

const dummyPasswordHash = "$argon2id$v=19$m=65536,t=3,p=2$MDEyMzQ1Njc4OWFiY2RlZg$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

func NewService(repository Repository, limiter LoginLimiter, sessionTTL time.Duration) *Service {
	return &Service{
		repository: repository,
		limiter:    limiter,
		sessionTTL: sessionTTL,
		now:        time.Now,
	}
}

func (s *Service) Login(ctx context.Context, input LoginInput) (LoginResult, error) {
	organizationCode := strings.ToLower(strings.TrimSpace(input.OrganizationCode))
	username := strings.ToLower(strings.TrimSpace(input.Username))
	if organizationCode == "" || username == "" || input.Password == "" {
		return LoginResult{}, ErrInvalidCredentials
	}

	limitKey := loginLimitKey(organizationCode, username, input.SourceIP)
	allowed, err := s.limiter.Allow(ctx, limitKey)
	if err != nil {
		return LoginResult{}, fmt.Errorf("检查登录频率失败: %w", err)
	}
	if !allowed {
		return LoginResult{}, ErrTooManyAttempts
	}

	user, err := s.repository.FindUserForLogin(ctx, organizationCode, username)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			_ = VerifyPassword(dummyPasswordHash, input.Password)
			s.recordFailedLogin(ctx, limitKey, input, "user_not_found")
			return LoginResult{}, ErrInvalidCredentials
		}
		return LoginResult{}, fmt.Errorf("查询登录用户失败: %w", err)
	}
	if !VerifyPassword(user.PasswordHash, input.Password) {
		s.recordFailedLogin(ctx, limitKey, input, "password_mismatch")
		return LoginResult{}, ErrInvalidCredentials
	}
	if user.Status != "active" {
		s.recordFailedLogin(ctx, limitKey, input, "account_"+user.Status)
		return LoginResult{}, ErrAccountDisabled
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

	err = s.repository.SaveLogin(ctx, NewSession{
		ID:            sessionID,
		UserID:        user.ID,
		TokenHash:     tokenHash(token),
		CSRFTokenHash: tokenHash(csrfToken),
		UserAgent:     input.UserAgent,
		SourceIP:      input.SourceIP,
		ExpiresAt:     expiresAt,
	}, AuditEvent{
		OrganizationID: user.OrganizationID,
		ActorID:        user.ID,
		RequestID:      input.RequestID,
		Action:         "auth.login",
		ResourceType:   "session",
		ResourceID:     sessionID,
		Result:         "success",
		SourceIP:       input.SourceIP,
		Detail:         map[string]any{"username": user.Username},
	})
	if err != nil {
		return LoginResult{}, fmt.Errorf("保存登录会话失败: %w", err)
	}
	_ = s.limiter.Clear(ctx, limitKey)

	user.PasswordHash = ""
	return LoginResult{User: user, Token: token, CSRFToken: csrfToken, ExpiresAt: expiresAt}, nil
}

func (s *Service) Authenticate(ctx context.Context, token string) (Session, error) {
	if token == "" {
		return Session{}, ErrUnauthorized
	}
	session, err := s.repository.FindSessionByTokenHash(ctx, tokenHash(token))
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return Session{}, ErrUnauthorized
		}
		return Session{}, fmt.Errorf("查询登录会话失败: %w", err)
	}
	if session.RevokedAt != nil || !session.ExpiresAt.After(s.now().UTC()) || session.User.Status != "active" {
		return Session{}, ErrUnauthorized
	}
	session.User.PasswordHash = ""
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
		OrganizationID: session.User.OrganizationID,
		ActorID:        session.User.ID,
		RequestID:      requestID,
		Action:         "auth.logout",
		ResourceType:   "session",
		ResourceID:     session.ID,
		Result:         "success",
		SourceIP:       sourceIP,
		Detail:         map[string]any{},
	})
}

func (s *Service) recordFailedLogin(ctx context.Context, limitKey string, input LoginInput, reason string) {
	_ = s.limiter.RecordFailure(ctx, limitKey)
	_ = s.repository.RecordAudit(ctx, AuditEvent{
		RequestID:    input.RequestID,
		Action:       "auth.login",
		ResourceType: "session",
		Result:       "failed",
		SourceIP:     input.SourceIP,
		Detail: map[string]any{
			"organizationCode": input.OrganizationCode,
			"username":         input.Username,
			"reason":           reason,
		},
	})
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

func loginLimitKey(organizationCode, username, sourceIP string) string {
	sum := sha256.Sum256([]byte(organizationCode + "\x00" + username + "\x00" + sourceIP))
	return "auth:login:" + hex.EncodeToString(sum[:])
}
