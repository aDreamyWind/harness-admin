package postgres

import "context"

func (r *IdentityRepository) HasPermission(ctx context.Context, userID, permissionCode string) (bool, error) {
	var allowed bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM user_roles ur
			JOIN role_permissions rp ON rp.role_id = ur.role_id
			JOIN permissions p ON p.id = rp.permission_id
			WHERE ur.user_id = $1 AND p.code = $2
		)
	`, userID, permissionCode).Scan(&allowed)
	return allowed, err
}
