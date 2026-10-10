package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"harness-admin/internal/identity"
	"harness-admin/internal/useradmin"
)

const managedUserColumns = `
	u.id, u.username, COALESCE(u.email, ''), u.display_name,
	COALESCE(u.department_id::text, ''), COALESCE(d.name, ''), u.status,
	COALESCE((
		SELECT ARRAY_AGG(r.name ORDER BY r.name)
		FROM user_roles ur
		JOIN roles r ON r.id = ur.role_id
		WHERE ur.user_id = u.id
	), '{}'::TEXT[]),
	u.last_login_at, u.created_at
`

func (r *IdentityRepository) ListUsers(ctx context.Context, filter useradmin.ListFilter) (useradmin.UserList, error) {
	searchPattern := "%" + filter.Search + "%"
	var total int64
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM users u
		WHERE u.organization_id = $1
		  AND ($2 = '' OR u.username ILIKE $3 OR u.display_name ILIKE $3 OR COALESCE(u.email, '') ILIKE $3)
		  AND ($4 = '' OR u.status = $4)
	`, filter.OrganizationID, filter.Search, searchPattern, filter.Status).Scan(&total)
	if err != nil {
		return useradmin.UserList{}, err
	}

	rows, err := r.pool.Query(ctx, `
		SELECT `+managedUserColumns+`
		FROM users u
		LEFT JOIN departments d ON d.id = u.department_id
		WHERE u.organization_id = $1
		  AND ($2 = '' OR u.username ILIKE $3 OR u.display_name ILIKE $3 OR COALESCE(u.email, '') ILIKE $3)
		  AND ($4 = '' OR u.status = $4)
		ORDER BY u.created_at DESC, u.id
		LIMIT $5 OFFSET $6
	`, filter.OrganizationID, filter.Search, searchPattern, filter.Status,
		filter.PageSize, (filter.Page-1)*filter.PageSize)
	if err != nil {
		return useradmin.UserList{}, err
	}
	defer rows.Close()

	items := make([]useradmin.User, 0)
	for rows.Next() {
		user, err := scanManagedUser(rows)
		if err != nil {
			return useradmin.UserList{}, err
		}
		items = append(items, user)
	}
	if err := rows.Err(); err != nil {
		return useradmin.UserList{}, err
	}
	return useradmin.UserList{Items: items, Total: total, Page: filter.Page, PageSize: filter.PageSize}, nil
}

func (r *IdentityRepository) CreateUser(ctx context.Context, input useradmin.NewUser) (useradmin.User, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return useradmin.User{}, err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		INSERT INTO users (
			id, organization_id, department_id, username, email, display_name, password_hash
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, input.ID, input.OrganizationID, nullableUUID(input.DepartmentID), input.Username,
		nullableString(input.Email), input.DisplayName, input.PasswordHash)
	if err != nil {
		return useradmin.User{}, translateUserWriteError(err)
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{
		OrganizationID: input.OrganizationID,
		ActorID:        input.ActorID,
		RequestID:      input.RequestID,
		Action:         "identity.user.create",
		ResourceType:   "user",
		ResourceID:     input.ID,
		Result:         "success",
		SourceIP:       input.SourceIP,
		Detail: map[string]any{
			"username": input.Username,
			"email":    input.Email,
		},
	}); err != nil {
		return useradmin.User{}, err
	}
	user, err := findManagedUser(ctx, tx, input.OrganizationID, input.ID)
	if err != nil {
		return useradmin.User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return useradmin.User{}, err
	}
	return user, nil
}

func (r *IdentityRepository) ChangeUserStatus(ctx context.Context, input useradmin.ChangeStatusInput) (useradmin.User, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return useradmin.User{}, err
	}
	defer tx.Rollback(ctx)

	var previousStatus string
	err = tx.QueryRow(ctx, `
		SELECT status FROM users
		WHERE id = $1 AND organization_id = $2
		FOR UPDATE
	`, input.UserID, input.OrganizationID).Scan(&previousStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return useradmin.User{}, useradmin.ErrUserNotFound
	}
	if err != nil {
		return useradmin.User{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE users SET status = $1, updated_at = NOW()
		WHERE id = $2 AND organization_id = $3
	`, input.Status, input.UserID, input.OrganizationID); err != nil {
		return useradmin.User{}, err
	}
	if input.Status == "disabled" {
		if _, err := tx.Exec(ctx, `
			UPDATE sessions SET revoked_at = NOW()
			WHERE user_id = $1 AND revoked_at IS NULL
		`, input.UserID); err != nil {
			return useradmin.User{}, err
		}
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{
		OrganizationID: input.OrganizationID,
		ActorID:        input.ActorID,
		RequestID:      input.RequestID,
		Action:         "identity.user.status_change",
		ResourceType:   "user",
		ResourceID:     input.UserID,
		Result:         "success",
		SourceIP:       input.SourceIP,
		Detail: map[string]any{
			"previousStatus": previousStatus,
			"status":         input.Status,
		},
	}); err != nil {
		return useradmin.User{}, err
	}
	user, err := findManagedUser(ctx, tx, input.OrganizationID, input.UserID)
	if err != nil {
		return useradmin.User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return useradmin.User{}, err
	}
	return user, nil
}

func findManagedUser(ctx context.Context, queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, organizationID, userID string) (useradmin.User, error) {
	return scanManagedUser(queryer.QueryRow(ctx, `
		SELECT `+managedUserColumns+`
		FROM users u
		LEFT JOIN departments d ON d.id = u.department_id
		WHERE u.organization_id = $1 AND u.id = $2
	`, organizationID, userID))
}

func scanManagedUser(row rowScanner) (useradmin.User, error) {
	var user useradmin.User
	err := row.Scan(
		&user.ID, &user.Username, &user.Email, &user.DisplayName,
		&user.DepartmentID, &user.DepartmentName, &user.Status, &user.RoleNames,
		&user.LastLoginAt, &user.CreatedAt,
	)
	return user, err
}

func translateUserWriteError(err error) error {
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) {
		return err
	}
	switch postgresError.Code {
	case "23505":
		return useradmin.ErrUserConflict
	case "23503":
		return useradmin.ErrInvalidDepartment
	default:
		return fmt.Errorf("写入用户失败: %w", err)
	}
}
