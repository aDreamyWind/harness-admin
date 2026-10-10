package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"harness-admin/internal/identity"
	"harness-admin/internal/roleadmin"
)

const managedRoleColumns = `
	r.id, r.code, r.name, r.description, r.built_in,
	(SELECT COUNT(*) FROM user_roles ur WHERE ur.role_id = r.id),
	COALESCE((
		SELECT ARRAY_AGG(p.code ORDER BY p.resource, p.action)
		FROM role_permissions rp
		JOIN permissions p ON p.id = rp.permission_id
		WHERE rp.role_id = r.id
	), '{}'::TEXT[]),
	r.created_at
`

func (r *IdentityRepository) ListRoles(ctx context.Context, organizationID string) ([]roleadmin.Role, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+managedRoleColumns+`
		FROM roles r
		WHERE r.organization_id = $1
		ORDER BY r.built_in DESC, r.created_at, r.name
	`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	roles := make([]roleadmin.Role, 0)
	for rows.Next() {
		role, err := scanManagedRole(rows)
		if err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}
	return roles, rows.Err()
}

func (r *IdentityRepository) ListPermissions(ctx context.Context) ([]roleadmin.Permission, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, code, name, resource, action, description
		FROM permissions
		ORDER BY resource, action, code
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	permissions := make([]roleadmin.Permission, 0)
	for rows.Next() {
		var permission roleadmin.Permission
		if err := rows.Scan(&permission.ID, &permission.Code, &permission.Name, &permission.Resource, &permission.Action, &permission.Description); err != nil {
			return nil, err
		}
		permissions = append(permissions, permission)
	}
	return permissions, rows.Err()
}

func (r *IdentityRepository) CreateRole(ctx context.Context, input roleadmin.NewRole) (roleadmin.Role, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return roleadmin.Role{}, err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		INSERT INTO roles (id, organization_id, code, name, description)
		VALUES ($1, $2, $3, $4, $5)
	`, input.ID, input.OrganizationID, input.Code, input.Name, input.Description)
	if err != nil {
		return roleadmin.Role{}, translateRoleWriteError(err)
	}
	if err := replaceRolePermissions(ctx, tx, input.ID, input.PermissionCodes); err != nil {
		return roleadmin.Role{}, err
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{
		OrganizationID: input.OrganizationID, ActorID: input.ActorID, RequestID: input.RequestID,
		Action: "identity.role.create", ResourceType: "role", ResourceID: input.ID,
		Result: "success", SourceIP: input.SourceIP,
		Detail: map[string]any{"code": input.Code, "name": input.Name, "permissionCodes": input.PermissionCodes},
	}); err != nil {
		return roleadmin.Role{}, err
	}
	role, err := findManagedRole(ctx, tx, input.OrganizationID, input.ID)
	if err != nil {
		return roleadmin.Role{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return roleadmin.Role{}, err
	}
	return role, nil
}

func (r *IdentityRepository) UpdateRole(ctx context.Context, input roleadmin.RoleChange) (roleadmin.Role, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return roleadmin.Role{}, err
	}
	defer tx.Rollback(ctx)

	var builtIn bool
	err = tx.QueryRow(ctx, `
		SELECT built_in FROM roles
		WHERE id = $1 AND organization_id = $2
		FOR UPDATE
	`, input.RoleID, input.OrganizationID).Scan(&builtIn)
	if errors.Is(err, pgx.ErrNoRows) {
		return roleadmin.Role{}, roleadmin.ErrRoleNotFound
	}
	if err != nil {
		return roleadmin.Role{}, err
	}
	if builtIn {
		return roleadmin.Role{}, roleadmin.ErrBuiltInRole
	}
	if _, err := tx.Exec(ctx, `
		UPDATE roles SET name = $1, description = $2, updated_at = NOW()
		WHERE id = $3 AND organization_id = $4
	`, input.Name, input.Description, input.RoleID, input.OrganizationID); err != nil {
		return roleadmin.Role{}, err
	}
	if err := replaceRolePermissions(ctx, tx, input.RoleID, input.PermissionCodes); err != nil {
		return roleadmin.Role{}, err
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{
		OrganizationID: input.OrganizationID, ActorID: input.ActorID, RequestID: input.RequestID,
		Action: "identity.role.update", ResourceType: "role", ResourceID: input.RoleID,
		Result: "success", SourceIP: input.SourceIP,
		Detail: map[string]any{"name": input.Name, "permissionCodes": input.PermissionCodes},
	}); err != nil {
		return roleadmin.Role{}, err
	}
	role, err := findManagedRole(ctx, tx, input.OrganizationID, input.RoleID)
	if err != nil {
		return roleadmin.Role{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return roleadmin.Role{}, err
	}
	return role, nil
}

func (r *IdentityRepository) AssignUserRoles(ctx context.Context, input roleadmin.AssignUserRolesInput) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var userID string
	err = tx.QueryRow(ctx, `
		SELECT id FROM users
		WHERE id = $1 AND organization_id = $2
		FOR UPDATE
	`, input.UserID, input.OrganizationID).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return roleadmin.ErrUserNotFound
	}
	if err != nil {
		return err
	}

	if len(input.RoleIDs) > 0 {
		var roleCount int
		if err := tx.QueryRow(ctx, `
			SELECT COUNT(*) FROM roles
			WHERE organization_id = $1 AND id::text = ANY($2::text[])
		`, input.OrganizationID, input.RoleIDs).Scan(&roleCount); err != nil {
			return err
		}
		if roleCount != len(input.RoleIDs) {
			return roleadmin.ErrRoleNotFound
		}
	}

	var previousRoleIDs []string
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(ARRAY_AGG(role_id::text ORDER BY role_id), '{}'::TEXT[])
		FROM user_roles WHERE user_id = $1
	`, input.UserID).Scan(&previousRoleIDs); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM user_roles WHERE user_id = $1`, input.UserID); err != nil {
		return err
	}
	if len(input.RoleIDs) > 0 {
		if _, err := tx.Exec(ctx, `
			INSERT INTO user_roles (user_id, role_id)
			SELECT $1, id FROM roles
			WHERE organization_id = $2 AND id::text = ANY($3::text[])
		`, input.UserID, input.OrganizationID, input.RoleIDs); err != nil {
			return err
		}
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{
		OrganizationID: input.OrganizationID, ActorID: input.ActorID, RequestID: input.RequestID,
		Action: "identity.user.roles_change", ResourceType: "user", ResourceID: input.UserID,
		Result: "success", SourceIP: input.SourceIP,
		Detail: map[string]any{"previousRoleIds": previousRoleIDs, "roleIds": input.RoleIDs},
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func replaceRolePermissions(ctx context.Context, tx pgx.Tx, roleID string, permissionCodes []string) error {
	if len(permissionCodes) > 0 {
		var permissionCount int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM permissions WHERE code = ANY($1::text[])`, permissionCodes).Scan(&permissionCount); err != nil {
			return err
		}
		if permissionCount != len(permissionCodes) {
			return roleadmin.ErrPermissionNotFound
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM role_permissions WHERE role_id = $1`, roleID); err != nil {
		return err
	}
	if len(permissionCodes) == 0 {
		return nil
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO role_permissions (role_id, permission_id)
		SELECT $1, id FROM permissions WHERE code = ANY($2::text[])
	`, roleID, permissionCodes)
	return err
}

func findManagedRole(ctx context.Context, queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, organizationID, roleID string) (roleadmin.Role, error) {
	role, err := scanManagedRole(queryer.QueryRow(ctx, `
		SELECT `+managedRoleColumns+`
		FROM roles r
		WHERE r.organization_id = $1 AND r.id = $2
	`, organizationID, roleID))
	if errors.Is(err, pgx.ErrNoRows) {
		return roleadmin.Role{}, roleadmin.ErrRoleNotFound
	}
	return role, err
}

func scanManagedRole(row rowScanner) (roleadmin.Role, error) {
	var role roleadmin.Role
	err := row.Scan(&role.ID, &role.Code, &role.Name, &role.Description, &role.BuiltIn,
		&role.UserCount, &role.PermissionCodes, &role.CreatedAt)
	return role, err
}

func translateRoleWriteError(err error) error {
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) {
		return err
	}
	if postgresError.Code == "23505" {
		return roleadmin.ErrRoleConflict
	}
	return fmt.Errorf("写入角色失败: %w", err)
}
