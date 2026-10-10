package identity

import (
	"context"
	"errors"
	"testing"
	"time"
)

type repositoryStub struct {
	user           User
	findUserErr    error
	session        Session
	findSessionErr error
	savedSession   NewSession
	audits         []AuditEvent
	revokedID      string
	passwordHash   string
	passwordUpdate PasswordUpdate
	updateRevoked  int64
	sessions       []UserSession
	sessionAction  SessionActionInput
}

func (r *repositoryStub) FindUserForLogin(context.Context, string, string) (User, error) {
	return r.user, r.findUserErr
}

func (r *repositoryStub) SaveLogin(_ context.Context, session NewSession, event AuditEvent) error {
	r.savedSession = session
	r.audits = append(r.audits, event)
	return nil
}

func (r *repositoryStub) FindSessionByTokenHash(context.Context, string) (Session, error) {
	return r.session, r.findSessionErr
}

func (r *repositoryStub) RevokeSession(_ context.Context, sessionID string, event AuditEvent) error {
	r.revokedID = sessionID
	r.audits = append(r.audits, event)
	return nil
}

func (r *repositoryStub) RecordAudit(_ context.Context, event AuditEvent) error {
	r.audits = append(r.audits, event)
	return nil
}

func (r *repositoryStub) FindUserPassword(context.Context, string, string) (string, error) {
	return r.passwordHash, r.findUserErr
}

func (r *repositoryStub) UpdateOwnPassword(_ context.Context, input PasswordUpdate) (int64, error) {
	r.passwordUpdate = input
	return r.updateRevoked, nil
}

func (r *repositoryStub) ListUserSessions(context.Context, string, string) ([]UserSession, error) {
	return r.sessions, nil
}

func (r *repositoryStub) RevokeUserSession(_ context.Context, input SessionActionInput) error {
	r.sessionAction = input
	return r.findSessionErr
}

func (r *repositoryStub) RevokeOtherUserSessions(_ context.Context, input SessionActionInput) (int64, error) {
	r.sessionAction = input
	return r.updateRevoked, r.findSessionErr
}

type limiterStub struct {
	allowed  bool
	failures int
	clears   int
}

func (l *limiterStub) Allow(context.Context, string) (bool, error) { return l.allowed, nil }
func (l *limiterStub) RecordFailure(context.Context, string) error {
	l.failures++
	return nil
}
func (l *limiterStub) Clear(context.Context, string) error {
	l.clears++
	return nil
}

func TestLoginCreatesServerSideSession(t *testing.T) {
	passwordHash, err := HashPassword("A-secure-password-2026!")
	if err != nil {
		t.Fatal(err)
	}
	repository := &repositoryStub{user: User{
		ID:               "user-1",
		OrganizationID:   "org-1",
		Organization:     "测试企业",
		OrganizationCode: "default",
		Username:         "admin",
		DisplayName:      "管理员",
		PasswordHash:     passwordHash,
		Status:           "active",
	}}
	limiter := &limiterStub{allowed: true}
	service := NewService(repository, limiter, 12*time.Hour)
	fixedNow := time.Date(2026, 10, 10, 10, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return fixedNow }

	result, err := service.Login(context.Background(), LoginInput{
		OrganizationCode: "DEFAULT",
		Username:         "Admin",
		Password:         "A-secure-password-2026!",
		SourceIP:         "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("Login() 返回错误: %v", err)
	}
	if result.Token == "" || result.CSRFToken == "" {
		t.Fatal("登录结果缺少会话令牌")
	}
	if result.User.PasswordHash != "" {
		t.Fatal("登录结果不应暴露密码哈希")
	}
	if repository.savedSession.TokenHash == result.Token {
		t.Fatal("数据库中不应保存明文会话令牌")
	}
	if !repository.savedSession.ExpiresAt.Equal(fixedNow.Add(12 * time.Hour)) {
		t.Fatalf("会话过期时间不正确: %s", repository.savedSession.ExpiresAt)
	}
	if limiter.clears != 1 {
		t.Fatalf("成功登录后应清除失败计数，实际 %d", limiter.clears)
	}
}

func TestLoginRecordsInvalidCredentials(t *testing.T) {
	repository := &repositoryStub{findUserErr: ErrUserNotFound}
	limiter := &limiterStub{allowed: true}
	service := NewService(repository, limiter, time.Hour)

	_, err := service.Login(context.Background(), LoginInput{
		OrganizationCode: "default",
		Username:         "missing",
		Password:         "some-password-value",
		SourceIP:         "127.0.0.1",
	})
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("错误 = %v，期望 ErrInvalidCredentials", err)
	}
	if limiter.failures != 1 {
		t.Fatalf("失败计数 = %d，期望 1", limiter.failures)
	}
	if len(repository.audits) != 1 || repository.audits[0].Result != "failed" {
		t.Fatal("无效登录应记录失败审计")
	}
}

func TestLoginStopsWhenRateLimited(t *testing.T) {
	service := NewService(&repositoryStub{}, &limiterStub{allowed: false}, time.Hour)
	_, err := service.Login(context.Background(), LoginInput{
		OrganizationCode: "default",
		Username:         "admin",
		Password:         "some-password-value",
	})
	if !errors.Is(err, ErrTooManyAttempts) {
		t.Fatalf("错误 = %v，期望 ErrTooManyAttempts", err)
	}
}

func TestAuthenticateAndValidateCSRF(t *testing.T) {
	now := time.Date(2026, 10, 10, 10, 0, 0, 0, time.UTC)
	csrfToken := "csrf-value"
	repository := &repositoryStub{session: Session{
		ID:            "session-1",
		User:          User{ID: "user-1", Status: "active", PasswordHash: "should-not-leak"},
		CSRFTokenHash: tokenHash(csrfToken),
		ExpiresAt:     now.Add(time.Hour),
	}}
	service := NewService(repository, &limiterStub{allowed: true}, time.Hour)
	service.now = func() time.Time { return now }

	session, err := service.Authenticate(context.Background(), "session-token")
	if err != nil {
		t.Fatalf("Authenticate() 返回错误: %v", err)
	}
	if session.User.PasswordHash != "" {
		t.Fatal("认证结果不应暴露密码哈希")
	}
	if err := service.ValidateCSRF(session, csrfToken); err != nil {
		t.Fatalf("正确 CSRF 令牌验证失败: %v", err)
	}
	if !errors.Is(service.ValidateCSRF(session, "wrong"), ErrInvalidCSRF) {
		t.Fatal("错误 CSRF 令牌应被拒绝")
	}
}

func TestAuthenticateRejectsExpiredSession(t *testing.T) {
	now := time.Now().UTC()
	repository := &repositoryStub{session: Session{
		User:      User{Status: "active"},
		ExpiresAt: now.Add(-time.Minute),
	}}
	service := NewService(repository, &limiterStub{allowed: true}, time.Hour)
	service.now = func() time.Time { return now }

	if _, err := service.Authenticate(context.Background(), "expired"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("错误 = %v，期望 ErrUnauthorized", err)
	}
}

func TestChangePasswordVerifiesCurrentAndKeepsCurrentSession(t *testing.T) {
	oldHash, err := HashPassword("old-password")
	if err != nil {
		t.Fatal(err)
	}
	repository := &repositoryStub{passwordHash: oldHash, updateRevoked: 2}
	service := NewService(repository, &limiterStub{allowed: true}, time.Hour)
	revoked, err := service.ChangePassword(context.Background(), ChangePasswordInput{
		Session:         Session{ID: "session-current", User: User{ID: "user-1", OrganizationID: "org-1"}},
		CurrentPassword: "old-password",
		NewPassword:     "new-password",
	})
	if err != nil {
		t.Fatal(err)
	}
	if revoked != 2 || repository.passwordUpdate.CurrentSessionID != "session-current" {
		t.Fatalf("密码更新结果不正确: revoked=%d input=%#v", revoked, repository.passwordUpdate)
	}
	if !VerifyPassword(repository.passwordUpdate.PasswordHash, "new-password") {
		t.Fatal("新密码未正确哈希")
	}
}

func TestChangePasswordRejectsIncorrectAndUnchangedPassword(t *testing.T) {
	oldHash, err := HashPassword("old-password")
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(&repositoryStub{passwordHash: oldHash}, &limiterStub{allowed: true}, time.Hour)
	input := ChangePasswordInput{Session: Session{ID: "session-1", User: User{ID: "user-1", OrganizationID: "org-1"}}}
	input.CurrentPassword, input.NewPassword = "wrong", "new-password"
	if _, err := service.ChangePassword(context.Background(), input); !errors.Is(err, ErrCurrentPassword) {
		t.Fatalf("错误 = %v，期望 ErrCurrentPassword", err)
	}
	input.CurrentPassword, input.NewPassword = "old-password", "old-password"
	if _, err := service.ChangePassword(context.Background(), input); !errors.Is(err, ErrPasswordUnchanged) {
		t.Fatalf("错误 = %v，期望 ErrPasswordUnchanged", err)
	}
}

func TestRevokeManagedSessionProtectsCurrentSession(t *testing.T) {
	service := NewService(&repositoryStub{}, &limiterStub{allowed: true}, time.Hour)
	session := Session{ID: "session-current", User: User{ID: "user-1", OrganizationID: "org-1"}}
	if err := service.RevokeManagedSession(context.Background(), session, "session-current", "", ""); !errors.Is(err, ErrCannotRevokeCurrent) {
		t.Fatalf("错误 = %v，期望 ErrCannotRevokeCurrent", err)
	}
}
