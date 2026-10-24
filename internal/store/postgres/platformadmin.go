package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"harness-admin/internal/platformadmin"
)

type PlatformAdminRepository struct {
	pool *pgxpool.Pool
}

func NewPlatformAdminRepository(pool *pgxpool.Pool) *PlatformAdminRepository {
	return &PlatformAdminRepository{pool: pool}
}

func (r *PlatformAdminRepository) FindAdminForLogin(ctx context.Context, username string) (platformadmin.Admin, error) {
	var admin platformadmin.Admin
	err := r.pool.QueryRow(ctx, `
		SELECT id, username, display_name, password_hash, status
		FROM platform_admins
		WHERE LOWER(username) = LOWER($1)
	`, username).Scan(&admin.ID, &admin.Username, &admin.DisplayName, &admin.PasswordHash, &admin.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return platformadmin.Admin{}, platformadmin.ErrUnauthorized
	}
	return admin, err
}

func (r *PlatformAdminRepository) SaveLogin(ctx context.Context, session platformadmin.NewSession, event platformadmin.AuditEvent) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		INSERT INTO platform_sessions (
			id, platform_admin_id, token_hash, csrf_token_hash, user_agent, source_ip, expires_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, session.ID, session.AdminID, session.TokenHash, session.CSRFTokenHash,
		session.UserAgent, nullableIP(session.SourceIP), session.ExpiresAt); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE platform_admins SET last_login_at = NOW(), updated_at = NOW() WHERE id = $1`, session.AdminID); err != nil {
		return err
	}
	if err := insertPlatformAudit(ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *PlatformAdminRepository) FindSessionByTokenHash(ctx context.Context, tokenHash string) (platformadmin.Session, error) {
	var session platformadmin.Session
	err := r.pool.QueryRow(ctx, `
		SELECT s.id, s.token_hash, s.csrf_token_hash, s.expires_at, s.revoked_at,
		       a.id, a.username, a.display_name, a.status
		FROM platform_sessions s
		JOIN platform_admins a ON a.id = s.platform_admin_id
		WHERE s.token_hash = $1
	`, tokenHash).Scan(&session.ID, &session.TokenHash, &session.CSRFTokenHash, &session.ExpiresAt, &session.RevokedAt,
		&session.Admin.ID, &session.Admin.Username, &session.Admin.DisplayName, &session.Admin.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return platformadmin.Session{}, platformadmin.ErrUnauthorized
	}
	if err == nil {
		_, err = r.pool.Exec(ctx, `
			UPDATE platform_sessions SET last_seen_at = NOW()
			WHERE id = $1 AND last_seen_at < NOW() - INTERVAL '1 minute'
		`, session.ID)
	}
	return session, err
}

func (r *PlatformAdminRepository) RevokeSession(ctx context.Context, sessionID string, event platformadmin.AuditEvent) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	command, err := tx.Exec(ctx, `UPDATE platform_sessions SET revoked_at = NOW() WHERE id = $1 AND revoked_at IS NULL`, sessionID)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return platformadmin.ErrUnauthorized
	}
	if err := insertPlatformAudit(ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *PlatformAdminRepository) RecordAudit(ctx context.Context, event platformadmin.AuditEvent) error {
	return insertPlatformAudit(ctx, r.pool, event)
}

func (r *PlatformAdminRepository) ListOrganizations(ctx context.Context) ([]platformadmin.Organization, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT o.id, o.code, o.name, o.status,
		       COALESCE((
		           SELECT u.username
		           FROM users u
		           JOIN user_roles ur ON ur.user_id = u.id
		           JOIN roles r ON r.id = ur.role_id
		           WHERE u.organization_id = o.id AND r.code = 'enterprise_admin'
		           ORDER BY u.created_at, u.id LIMIT 1
		       ), ''),
		       (SELECT COUNT(*) FROM users u WHERE u.organization_id = o.id),
		       o.created_at, o.updated_at
		FROM organizations o
		ORDER BY o.created_at DESC, o.id DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]platformadmin.Organization, 0)
	for rows.Next() {
		var item platformadmin.Organization
		if err := rows.Scan(&item.ID, &item.Code, &item.Name, &item.Status, &item.AdminUsername,
			&item.UserCount, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PlatformAdminRepository) CreateOrganization(ctx context.Context, input platformadmin.OrganizationCreation) (platformadmin.Organization, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return platformadmin.Organization{}, err
	}
	defer tx.Rollback(ctx)
	organizationID := uuid.NewString()
	if _, err := tx.Exec(ctx, `INSERT INTO organizations (id, code, name) VALUES ($1, $2, $3)`, organizationID, input.Code, input.Name); err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "23505" {
			return platformadmin.Organization{}, platformadmin.ErrOrganizationConflict
		}
		return platformadmin.Organization{}, err
	}
	roleID, err := ensureEnterpriseAdminRole(ctx, tx, organizationID)
	if err != nil {
		return platformadmin.Organization{}, err
	}
	userID := uuid.NewString()
	if _, err := tx.Exec(ctx, `
		INSERT INTO users (id, organization_id, username, email, display_name, password_hash)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, userID, organizationID, input.AdminUsername, nullableString(input.AdminEmail), input.AdminDisplayName, input.AdminPasswordHash); err != nil {
		return platformadmin.Organization{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO user_roles (user_id, role_id) VALUES ($1, $2)`, userID, roleID); err != nil {
		return platformadmin.Organization{}, err
	}
	if err := insertPlatformAudit(ctx, tx, platformadmin.AuditEvent{
		ActorID: input.ActorID, RequestID: input.RequestID, Action: "platform.organization.create",
		ResourceType: "organization", ResourceID: organizationID, Result: "success", SourceIP: input.SourceIP,
		Detail: map[string]any{"code": input.Code, "name": input.Name, "adminUsername": input.AdminUsername, "adminUserId": userID},
	}); err != nil {
		return platformadmin.Organization{}, err
	}
	var organization platformadmin.Organization
	if err := tx.QueryRow(ctx, `
		SELECT id, code, name, status, $2::text, 1::bigint, created_at, updated_at
		FROM organizations WHERE id = $1
	`, organizationID, input.AdminUsername).Scan(&organization.ID, &organization.Code, &organization.Name, &organization.Status,
		&organization.AdminUsername, &organization.UserCount, &organization.CreatedAt, &organization.UpdatedAt); err != nil {
		return platformadmin.Organization{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return platformadmin.Organization{}, err
	}
	return organization, nil
}

func (r *PlatformAdminRepository) UpdateOrganizationStatus(ctx context.Context, input platformadmin.StatusUpdate) (platformadmin.Organization, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return platformadmin.Organization{}, err
	}
	defer tx.Rollback(ctx)
	command, err := tx.Exec(ctx, `UPDATE organizations SET status = $1, updated_at = NOW() WHERE id = $2`, input.Status, input.OrganizationID)
	if err != nil {
		return platformadmin.Organization{}, err
	}
	if command.RowsAffected() == 0 {
		return platformadmin.Organization{}, platformadmin.ErrOrganizationNotFound
	}
	revokedSessions := int64(0)
	if input.Status == "disabled" {
		revoked, err := tx.Exec(ctx, `
			UPDATE sessions SET revoked_at = NOW()
			WHERE revoked_at IS NULL
			  AND user_id IN (SELECT id FROM users WHERE organization_id = $1)
		`, input.OrganizationID)
		if err != nil {
			return platformadmin.Organization{}, err
		}
		revokedSessions = revoked.RowsAffected()
	}
	if err := insertPlatformAudit(ctx, tx, platformadmin.AuditEvent{
		ActorID: input.ActorID, RequestID: input.RequestID, Action: "platform.organization.status_change",
		ResourceType: "organization", ResourceID: input.OrganizationID, Result: "success", SourceIP: input.SourceIP,
		Detail: map[string]any{"status": input.Status, "revokedSessions": revokedSessions},
	}); err != nil {
		return platformadmin.Organization{}, err
	}
	organization, err := scanPlatformOrganization(tx.QueryRow(ctx, `
		SELECT o.id, o.code, o.name, o.status,
		       COALESCE((SELECT u.username FROM users u JOIN user_roles ur ON ur.user_id = u.id JOIN roles r ON r.id = ur.role_id
		                 WHERE u.organization_id = o.id AND r.code = 'enterprise_admin' ORDER BY u.created_at LIMIT 1), ''),
		       (SELECT COUNT(*) FROM users u WHERE u.organization_id = o.id), o.created_at, o.updated_at
		FROM organizations o WHERE o.id = $1
	`, input.OrganizationID))
	if err != nil {
		return platformadmin.Organization{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return platformadmin.Organization{}, err
	}
	return organization, nil
}

func (r *PlatformAdminRepository) BootstrapPlatformAdmin(ctx context.Context, input platformadmin.BootstrapInput) (platformadmin.BootstrapResult, error) {
	adminID := uuid.NewString()
	var created bool
	err := r.pool.QueryRow(ctx, `
		INSERT INTO platform_admins (id, username, display_name, password_hash)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT ((LOWER(username))) DO UPDATE SET display_name = EXCLUDED.display_name, updated_at = NOW()
		RETURNING id, (xmax = 0)
	`, adminID, input.Username, input.DisplayName, input.PasswordHash).Scan(&adminID, &created)
	if err != nil {
		return platformadmin.BootstrapResult{}, err
	}
	return platformadmin.BootstrapResult{AdminID: adminID, Created: created}, nil
}

func ensureEnterpriseAdminRole(ctx context.Context, tx pgx.Tx, organizationID string) (string, error) {
	roleID := uuid.NewString()
	if err := tx.QueryRow(ctx, `
		INSERT INTO roles (id, organization_id, code, name, description, built_in)
		VALUES ($1, $2, 'enterprise_admin', '企业管理员', '拥有当前企业全部管理权限', TRUE)
		ON CONFLICT (organization_id, code) DO UPDATE SET name = EXCLUDED.name, description = EXCLUDED.description
		RETURNING id
	`, roleID, organizationID).Scan(&roleID); err != nil {
		return "", fmt.Errorf("创建企业管理员角色失败: %w", err)
	}
	permissions := []struct{ code, name, resource, action string }{
		{"iam.organization.read", "查看企业设置", "organization", "read"},
		{"iam.organization.write", "管理企业设置", "organization", "write"},
		{"iam.user.read", "查看用户", "user", "read"},
		{"iam.user.write", "管理用户", "user", "write"},
		{"iam.department.read", "查看部门", "department", "read"},
		{"iam.department.write", "管理部门", "department", "write"},
		{"iam.role.read", "查看角色", "role", "read"},
		{"iam.role.write", "管理角色", "role", "write"},
		{"audit.event.read", "查看审计事件", "audit_event", "read"},
		{"model.provider.read", "查看模型供应商", "model_provider", "read"},
		{"model.provider.write", "管理模型供应商", "model_provider", "write"},
		{"model.catalog.read", "查看模型目录", "model_catalog", "read"},
		{"model.catalog.write", "管理模型目录", "model_catalog", "write"},
		{"model.gateway.read", "查看模型网关", "model_gateway", "read"},
		{"model.gateway.write", "管理模型网关", "model_gateway", "write"},
		{"oidc.client.read", "查看应用接入", "oidc_client", "read"},
		{"oidc.client.write", "管理应用接入", "oidc_client", "write"},
		{"model.policy.read", "查看模型策略", "model_policy", "read"},
		{"model.policy.write", "管理模型策略", "model_policy", "write"},
		{"mcp.service.read", "查看 MCP 服务", "mcp_service", "read"},
		{"mcp.service.write", "管理 MCP 服务", "mcp_service", "write"},
		{"mcp.connection.read", "查看 MCP 连接", "mcp_connection", "read"},
		{"mcp.connection.write", "管理 MCP 连接", "mcp_connection", "write"},
		{"skill.asset.read", "查看企业 Skill", "skill_asset", "read"},
		{"skill.asset.write", "管理企业 Skill", "skill_asset", "write"},
	}
	for _, permission := range permissions {
		permissionID := uuid.NewString()
		if err := tx.QueryRow(ctx, `
			INSERT INTO permissions (id, code, name, resource, action)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (code) DO UPDATE SET name = EXCLUDED.name
			RETURNING id
		`, permissionID, permission.code, permission.name, permission.resource, permission.action).Scan(&permissionID); err != nil {
			return "", fmt.Errorf("创建权限 %s 失败: %w", permission.code, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO role_permissions (role_id, permission_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, roleID, permissionID); err != nil {
			return "", err
		}
	}
	return roleID, nil
}

func scanPlatformOrganization(row rowScanner) (platformadmin.Organization, error) {
	var organization platformadmin.Organization
	err := row.Scan(&organization.ID, &organization.Code, &organization.Name, &organization.Status,
		&organization.AdminUsername, &organization.UserCount, &organization.CreatedAt, &organization.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return platformadmin.Organization{}, platformadmin.ErrOrganizationNotFound
	}
	return organization, err
}

func insertPlatformAudit(ctx context.Context, executor auditExecer, event platformadmin.AuditEvent) error {
	detail, err := json.Marshal(event.Detail)
	if err != nil {
		return err
	}
	_, err = executor.Exec(ctx, `
		INSERT INTO platform_audit_events (
			platform_admin_id, request_id, action, resource_type, resource_id, result, source_ip, detail
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, nullableUUID(event.ActorID), event.RequestID, event.Action, event.ResourceType, event.ResourceID,
		event.Result, nullableIP(event.SourceIP), detail)
	return err
}
