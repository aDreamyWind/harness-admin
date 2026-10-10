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

	allowed, err := s.limiter.Allow(ctx, organizationCode, username)
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
			if err := s.recordFailedLogin(ctx, organizationCode, username, input, "user_not_found", User{}); err != nil {
				return LoginResult{}, fmt.Errorf("记录失败登录失败: %w", err)
			}
			return LoginResult{}, ErrInvalidCredentials
		}
		return LoginResult{}, fmt.Errorf("查询登录用户失败: %w", err)
	}
	if !VerifyPassword(user.PasswordHash, input.Password) {
		if err := s.recordFailedLogin(ctx, organizationCode, username, input, "password_mismatch", user); err != nil {
			return LoginResult{}, fmt.Errorf("记录失败登录失败: %w", err)
		}
		return LoginResult{}, ErrInvalidCredentials
	}
	if user.Status == "locked" {
		if err := s.recordRejectedLogin(ctx, input, user, "account_locked"); err != nil {
			return LoginResult{}, fmt.Errorf("记录锁定账号登录失败: %w", err)
		}
		return LoginResult{}, ErrAccountLocked
	}
	if user.Status != "active" {
		if err := s.recordRejectedLogin(ctx, input, user, "account_"+user.Status); err != nil {
			return LoginResult{}, fmt.Errorf("记录不可用账号登录失败: %w", err)
		}
		return LoginResult{}, ErrAccountDisabled
	}
	if err := s.limiter.Clear(ctx, organizationCode, username); err != nil {
		return LoginResult{}, fmt.Errorf("清除登录失败计数失败: %w", err)
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

func (s *Service) ChangePassword(ctx context.Context, input ChangePasswordInput) (int64, error) {
	if input.Session.User.ID == "" || input.Session.User.OrganizationID == "" || input.Session.ID == "" ||
		input.CurrentPassword == "" || input.NewPassword == "" || len(input.CurrentPassword) > 1024 || len(input.NewPassword) > 1024 {
		return 0, ErrInvalidPassword
	}
	passwordHash, err := s.repository.FindUserPassword(ctx, input.Session.User.OrganizationID, input.Session.User.ID)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return 0, ErrUnauthorized
		}
		return 0, fmt.Errorf("查询当前密码失败: %w", err)
	}
	if !VerifyPassword(passwordHash, input.CurrentPassword) {
		return 0, ErrCurrentPassword
	}
	if VerifyPassword(passwordHash, input.NewPassword) {
		return 0, ErrPasswordUnchanged
	}
	newHash, err := HashPassword(input.NewPassword)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrInvalidPassword, err)
	}
	revoked, err := s.repository.UpdateOwnPassword(ctx, PasswordUpdate{
		OrganizationID:   input.Session.User.OrganizationID,
		UserID:           input.Session.User.ID,
		CurrentSessionID: input.Session.ID,
		PasswordHash:     newHash,
		RequestID:        input.RequestID,
		SourceIP:         input.SourceIP,
	})
	if err != nil {
		return 0, fmt.Errorf("修改密码失败: %w", err)
	}
	return revoked, nil
}

func (s *Service) ListSessions(ctx context.Context, session Session) ([]UserSession, error) {
	items, err := s.repository.ListUserSessions(ctx, session.User.ID, session.ID)
	if err != nil {
		return nil, fmt.Errorf("查询登录会话失败: %w", err)
	}
	return items, nil
}

func (s *Service) RevokeManagedSession(ctx context.Context, session Session, targetSessionID, requestID, sourceIP string) error {
	if targetSessionID == "" {
		return ErrSessionNotFound
	}
	if targetSessionID == session.ID {
		return ErrCannotRevokeCurrent
	}
	err := s.repository.RevokeUserSession(ctx, SessionActionInput{
		OrganizationID:   session.User.OrganizationID,
		UserID:           session.User.ID,
		CurrentSessionID: session.ID,
		TargetSessionID:  targetSessionID,
		RequestID:        requestID,
		SourceIP:         sourceIP,
	})
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return err
		}
		return fmt.Errorf("吊销登录会话失败: %w", err)
	}
	return nil
}

func (s *Service) RevokeOtherSessions(ctx context.Context, session Session, requestID, sourceIP string) (int64, error) {
	revoked, err := s.repository.RevokeOtherUserSessions(ctx, SessionActionInput{
		OrganizationID:   session.User.OrganizationID,
		UserID:           session.User.ID,
		CurrentSessionID: session.ID,
		RequestID:        requestID,
		SourceIP:         sourceIP,
	})
	if err != nil {
		return 0, fmt.Errorf("退出其他设备失败: %w", err)
	}
	return revoked, nil
}

func (s *Service) recordFailedLogin(
	ctx context.Context,
	organizationCode string,
	username string,
	input LoginInput,
	reason string,
	user User,
) error {
	failure, err := s.limiter.RecordFailure(ctx, organizationCode, username)
	if err != nil {
		return err
	}
	if failure.LimitReached && user.ID != "" && user.Status == "active" {
		if err := s.repository.LockUserAfterFailedLogin(ctx, AccountLockInput{
			OrganizationID: user.OrganizationID,
			UserID:         user.ID,
			Attempts:       failure.Attempts,
			RequestID:      input.RequestID,
			SourceIP:       input.SourceIP,
		}); err != nil {
			return err
		}
		if err := s.limiter.Clear(ctx, organizationCode, username); err != nil {
			return err
		}
	}
	return s.repository.RecordAudit(ctx, AuditEvent{
		OrganizationID: user.OrganizationID,
		RequestID:      input.RequestID,
		Action:         "auth.login",
		ResourceType:   "session",
		Result:         "failed",
		SourceIP:       input.SourceIP,
		Detail: map[string]any{
			"organizationCode": input.OrganizationCode,
			"username":         input.Username,
			"reason":           reason,
			"attempts":         failure.Attempts,
		},
	})
}

func (s *Service) recordRejectedLogin(ctx context.Context, input LoginInput, user User, reason string) error {
	return s.repository.RecordAudit(ctx, AuditEvent{
		OrganizationID: user.OrganizationID,
		RequestID:      input.RequestID,
		Action:         "auth.login",
		ResourceType:   "session",
		Result:         "failed",
		SourceIP:       input.SourceIP,
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
