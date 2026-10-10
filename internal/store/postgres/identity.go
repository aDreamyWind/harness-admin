package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"harness-admin/internal/identity"
)

type IdentityRepository struct {
	pool *pgxpool.Pool
}

func NewIdentityRepository(pool *pgxpool.Pool) *IdentityRepository {
	return &IdentityRepository{pool: pool}
}

func (r *IdentityRepository) FindUserForLogin(ctx context.Context, organizationCode, username string) (identity.User, error) {
	user, err := scanUser(r.pool.QueryRow(ctx, `
		SELECT u.id, u.organization_id, o.name, o.code, u.username,
		       COALESCE(u.email, ''), u.display_name, u.password_hash, u.status,
		       ARRAY(
		           SELECT DISTINCT p.code
		           FROM user_roles ur
		           JOIN role_permissions rp ON rp.role_id = ur.role_id
		           JOIN permissions p ON p.id = rp.permission_id
		           WHERE ur.user_id = u.id
		           ORDER BY p.code
		       )
		FROM users u
		JOIN organizations o ON o.id = u.organization_id
		WHERE LOWER(o.code) = LOWER($1) AND LOWER(u.username) = LOWER($2)
	`, organizationCode, username))
	if err == pgx.ErrNoRows {
		return identity.User{}, identity.ErrUserNotFound
	}
	return user, err
}

func (r *IdentityRepository) SaveLogin(ctx context.Context, session identity.NewSession, event identity.AuditEvent) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		INSERT INTO sessions (
			id, user_id, token_hash, csrf_token_hash, user_agent, source_ip, expires_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, session.ID, session.UserID, session.TokenHash, session.CSRFTokenHash,
		session.UserAgent, nullableIP(session.SourceIP), session.ExpiresAt)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE users SET last_login_at = NOW(), updated_at = NOW() WHERE id = $1`, session.UserID); err != nil {
		return err
	}
	if err = insertAudit(ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *IdentityRepository) FindSessionByTokenHash(ctx context.Context, tokenHash string) (identity.Session, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT s.id, s.token_hash, s.csrf_token_hash, s.expires_at, s.revoked_at,
		       u.id, u.organization_id, o.name, o.code, u.username,
		       COALESCE(u.email, ''), u.display_name, u.status,
		       ARRAY(
		           SELECT DISTINCT p.code
		           FROM user_roles ur
		           JOIN role_permissions rp ON rp.role_id = ur.role_id
		           JOIN permissions p ON p.id = rp.permission_id
		           WHERE ur.user_id = u.id
		           ORDER BY p.code
		       )
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		JOIN organizations o ON o.id = u.organization_id
		WHERE s.token_hash = $1
	`, tokenHash)

	var session identity.Session
	err := row.Scan(
		&session.ID, &session.TokenHash, &session.CSRFTokenHash, &session.ExpiresAt, &session.RevokedAt,
		&session.User.ID, &session.User.OrganizationID, &session.User.Organization,
		&session.User.OrganizationCode, &session.User.Username, &session.User.Email,
		&session.User.DisplayName, &session.User.Status, &session.User.PermissionCodes,
	)
	if err == pgx.ErrNoRows {
		return identity.Session{}, identity.ErrSessionNotFound
	}
	if err == nil {
		if _, updateErr := r.pool.Exec(ctx, `
			UPDATE sessions SET last_seen_at = NOW()
			WHERE id = $1 AND last_seen_at < NOW() - INTERVAL '1 minute'
		`, session.ID); updateErr != nil {
			return identity.Session{}, updateErr
		}
	}
	return session, err
}

func (r *IdentityRepository) RevokeSession(ctx context.Context, sessionID string, event identity.AuditEvent) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	command, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at = NOW() WHERE id = $1 AND revoked_at IS NULL`, sessionID)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return identity.ErrUnauthorized
	}
	if err := insertAudit(ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *IdentityRepository) RecordAudit(ctx context.Context, event identity.AuditEvent) error {
	return insertAudit(ctx, r.pool, event)
}

func (r *IdentityRepository) FindUserPassword(ctx context.Context, organizationID, userID string) (string, error) {
	var passwordHash string
	err := r.pool.QueryRow(ctx, `
		SELECT password_hash FROM users WHERE organization_id = $1 AND id = $2
	`, organizationID, userID).Scan(&passwordHash)
	if err == pgx.ErrNoRows {
		return "", identity.ErrUserNotFound
	}
	return passwordHash, err
}

func (r *IdentityRepository) UpdateOwnPassword(ctx context.Context, input identity.PasswordUpdate) (int64, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	command, err := tx.Exec(ctx, `
		UPDATE users SET password_hash = $1, password_changed_at = NOW(), updated_at = NOW()
		WHERE organization_id = $2 AND id = $3
	`, input.PasswordHash, input.OrganizationID, input.UserID)
	if err != nil {
		return 0, err
	}
	if command.RowsAffected() == 0 {
		return 0, identity.ErrUserNotFound
	}
	revokedCommand, err := tx.Exec(ctx, `
		UPDATE sessions SET revoked_at = NOW()
		WHERE user_id = $1 AND id <> $2 AND revoked_at IS NULL
	`, input.UserID, input.CurrentSessionID)
	if err != nil {
		return 0, err
	}
	revoked := revokedCommand.RowsAffected()
	if err := insertAudit(ctx, tx, identity.AuditEvent{
		OrganizationID: input.OrganizationID,
		ActorID:        input.UserID,
		RequestID:      input.RequestID,
		Action:         "auth.password.change",
		ResourceType:   "user",
		ResourceID:     input.UserID,
		Result:         "success",
		SourceIP:       input.SourceIP,
		Detail:         map[string]any{"revokedOtherSessions": revoked},
	}); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return revoked, nil
}

func (r *IdentityRepository) ListUserSessions(ctx context.Context, userID, currentSessionID string) ([]identity.UserSession, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, user_agent, COALESCE(HOST(source_ip), ''), expires_at, last_seen_at, created_at,
		       id = $2
		FROM sessions
		WHERE user_id = $1 AND revoked_at IS NULL AND expires_at > NOW()
		ORDER BY (id = $2) DESC, last_seen_at DESC, created_at DESC
	`, userID, currentSessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]identity.UserSession, 0)
	for rows.Next() {
		var item identity.UserSession
		if err := rows.Scan(&item.ID, &item.UserAgent, &item.SourceIP, &item.ExpiresAt, &item.LastSeenAt, &item.CreatedAt, &item.Current); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *IdentityRepository) RevokeUserSession(ctx context.Context, input identity.SessionActionInput) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	command, err := tx.Exec(ctx, `
		UPDATE sessions SET revoked_at = NOW()
		WHERE id = $1 AND user_id = $2 AND id <> $3 AND revoked_at IS NULL AND expires_at > NOW()
	`, input.TargetSessionID, input.UserID, input.CurrentSessionID)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return identity.ErrSessionNotFound
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{
		OrganizationID: input.OrganizationID,
		ActorID:        input.UserID,
		RequestID:      input.RequestID,
		Action:         "auth.session.revoke",
		ResourceType:   "session",
		ResourceID:     input.TargetSessionID,
		Result:         "success",
		SourceIP:       input.SourceIP,
		Detail:         map[string]any{},
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *IdentityRepository) RevokeOtherUserSessions(ctx context.Context, input identity.SessionActionInput) (int64, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	command, err := tx.Exec(ctx, `
		UPDATE sessions SET revoked_at = NOW()
		WHERE user_id = $1 AND id <> $2 AND revoked_at IS NULL
	`, input.UserID, input.CurrentSessionID)
	if err != nil {
		return 0, err
	}
	revoked := command.RowsAffected()
	if err := insertAudit(ctx, tx, identity.AuditEvent{
		OrganizationID: input.OrganizationID,
		ActorID:        input.UserID,
		RequestID:      input.RequestID,
		Action:         "auth.session.revoke_others",
		ResourceType:   "session",
		ResourceID:     input.CurrentSessionID,
		Result:         "success",
		SourceIP:       input.SourceIP,
		Detail:         map[string]any{"revokedSessions": revoked},
	}); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return revoked, nil
}

func (r *IdentityRepository) BootstrapAdmin(ctx context.Context, input identity.BootstrapAdminInput) (identity.BootstrapAdminResult, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return identity.BootstrapAdminResult{}, err
	}
	defer tx.Rollback(ctx)

	organizationID := uuid.NewString()
	if err := tx.QueryRow(ctx, `
		INSERT INTO organizations (id, code, name)
		VALUES ($1, $2, $3)
		ON CONFLICT ((LOWER(code))) DO UPDATE SET name = EXCLUDED.name, updated_at = NOW()
		RETURNING id
	`, organizationID, input.OrganizationCode, input.OrganizationName).Scan(&organizationID); err != nil {
		return identity.BootstrapAdminResult{}, fmt.Errorf("创建企业失败: %w", err)
	}

	var existingUserID string
	err = tx.QueryRow(ctx, `
		SELECT id FROM users
		WHERE organization_id = $1 AND LOWER(username) = LOWER($2)
	`, organizationID, input.Username).Scan(&existingUserID)
	if err == nil {
		return identity.BootstrapAdminResult{OrganizationID: organizationID, UserID: existingUserID, Created: false}, tx.Commit(ctx)
	}
	if !errorsIsNoRows(err) {
		return identity.BootstrapAdminResult{}, err
	}

	roleID := uuid.NewString()
	if err := tx.QueryRow(ctx, `
		INSERT INTO roles (id, organization_id, code, name, description, built_in)
		VALUES ($1, $2, 'system_admin', '系统管理员', '拥有当前企业全部管理权限', TRUE)
		ON CONFLICT (organization_id, code) DO UPDATE SET name = EXCLUDED.name
		RETURNING id
	`, roleID, organizationID).Scan(&roleID); err != nil {
		return identity.BootstrapAdminResult{}, fmt.Errorf("创建管理员角色失败: %w", err)
	}

	permissions := []struct{ code, name, resource, action string }{
		{"iam.user.read", "查看用户", "user", "read"},
		{"iam.user.write", "管理用户", "user", "write"},
		{"iam.department.read", "查看部门", "department", "read"},
		{"iam.department.write", "管理部门", "department", "write"},
		{"iam.role.read", "查看角色", "role", "read"},
		{"iam.role.write", "管理角色", "role", "write"},
		{"audit.event.read", "查看审计事件", "audit_event", "read"},
	}
	for _, permission := range permissions {
		permissionID := uuid.NewString()
		if err := tx.QueryRow(ctx, `
			INSERT INTO permissions (id, code, name, resource, action)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (code) DO UPDATE SET name = EXCLUDED.name
			RETURNING id
		`, permissionID, permission.code, permission.name, permission.resource, permission.action).Scan(&permissionID); err != nil {
			return identity.BootstrapAdminResult{}, fmt.Errorf("创建权限 %s 失败: %w", permission.code, err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO role_permissions (role_id, permission_id)
			VALUES ($1, $2) ON CONFLICT DO NOTHING
		`, roleID, permissionID); err != nil {
			return identity.BootstrapAdminResult{}, fmt.Errorf("分配权限 %s 失败: %w", permission.code, err)
		}
	}

	userID := uuid.NewString()
	if _, err := tx.Exec(ctx, `
		INSERT INTO users (
			id, organization_id, username, email, display_name, password_hash
		) VALUES ($1, $2, $3, $4, $5, $6)
	`, userID, organizationID, input.Username, nullableString(input.Email), input.DisplayName, input.PasswordHash); err != nil {
		return identity.BootstrapAdminResult{}, fmt.Errorf("创建管理员失败: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO user_roles (user_id, role_id) VALUES ($1, $2)`, userID, roleID); err != nil {
		return identity.BootstrapAdminResult{}, fmt.Errorf("分配管理员角色失败: %w", err)
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{
		OrganizationID: organizationID,
		ActorID:        userID,
		Action:         "identity.bootstrap_admin",
		ResourceType:   "user",
		ResourceID:     userID,
		Result:         "success",
		Detail:         map[string]any{"username": input.Username},
	}); err != nil {
		return identity.BootstrapAdminResult{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return identity.BootstrapAdminResult{}, err
	}
	return identity.BootstrapAdminResult{OrganizationID: organizationID, UserID: userID, Created: true}, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

type auditExecer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func scanUser(row rowScanner) (identity.User, error) {
	var user identity.User
	err := row.Scan(
		&user.ID, &user.OrganizationID, &user.Organization, &user.OrganizationCode,
		&user.Username, &user.Email, &user.DisplayName, &user.PasswordHash, &user.Status,
		&user.PermissionCodes,
	)
	return user, err
}

func insertAudit(ctx context.Context, executor auditExecer, event identity.AuditEvent) error {
	detail, err := json.Marshal(event.Detail)
	if err != nil {
		return fmt.Errorf("编码审计详情失败: %w", err)
	}
	_, err = executor.Exec(ctx, `
		INSERT INTO audit_events (
			organization_id, actor_id, request_id, action, resource_type,
			resource_id, result, source_ip, detail
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, nullableUUID(event.OrganizationID), nullableUUID(event.ActorID), event.RequestID,
		event.Action, event.ResourceType, event.ResourceID, event.Result,
		nullableIP(event.SourceIP), detail)
	return err
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.TrimSpace(value)
}

func nullableUUID(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableIP(value string) any {
	host := value
	if parsedHost, _, err := net.SplitHostPort(value); err == nil {
		host = parsedHost
	}
	if net.ParseIP(host) == nil {
		return nil
	}
	return host
}

func errorsIsNoRows(err error) bool {
	return err == pgx.ErrNoRows
}
