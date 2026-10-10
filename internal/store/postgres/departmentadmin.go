package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"harness-admin/internal/departmentadmin"
	"harness-admin/internal/identity"
)

const managedDepartmentColumns = `
	d.id, COALESCE(d.parent_id::text, ''), d.code, d.name, d.sort_order, d.status,
	(SELECT COUNT(*) FROM users u WHERE u.department_id = d.id), d.created_at
`

func (r *IdentityRepository) ListDepartments(ctx context.Context, organizationID string) ([]departmentadmin.Department, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+managedDepartmentColumns+`
		FROM departments d
		WHERE d.organization_id = $1
		ORDER BY d.sort_order, d.name, d.id
	`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	departments := make([]departmentadmin.Department, 0)
	for rows.Next() {
		department, err := scanManagedDepartment(rows)
		if err != nil {
			return nil, err
		}
		departments = append(departments, department)
	}
	return departments, rows.Err()
}

func (r *IdentityRepository) CreateDepartment(ctx context.Context, input departmentadmin.NewDepartment) (departmentadmin.Department, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return departmentadmin.Department{}, err
	}
	defer tx.Rollback(ctx)

	if err := validateActiveParent(ctx, tx, input.OrganizationID, input.ParentID); err != nil {
		return departmentadmin.Department{}, err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO departments (id, organization_id, parent_id, code, name, sort_order)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, input.ID, input.OrganizationID, nullableUUID(input.ParentID), input.Code, input.Name, input.SortOrder)
	if err != nil {
		return departmentadmin.Department{}, translateDepartmentWriteError(err)
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{
		OrganizationID: input.OrganizationID, ActorID: input.ActorID, RequestID: input.RequestID,
		Action: "identity.department.create", ResourceType: "department", ResourceID: input.ID,
		Result: "success", SourceIP: input.SourceIP,
		Detail: map[string]any{"code": input.Code, "name": input.Name, "parentId": input.ParentID},
	}); err != nil {
		return departmentadmin.Department{}, err
	}
	department, err := findManagedDepartment(ctx, tx, input.OrganizationID, input.ID)
	if err != nil {
		return departmentadmin.Department{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return departmentadmin.Department{}, err
	}
	return department, nil
}

func (r *IdentityRepository) UpdateDepartment(ctx context.Context, input departmentadmin.DepartmentChange) (departmentadmin.Department, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return departmentadmin.Department{}, err
	}
	defer tx.Rollback(ctx)

	var departmentID string
	err = tx.QueryRow(ctx, `
		SELECT id FROM departments
		WHERE id = $1 AND organization_id = $2
		FOR UPDATE
	`, input.DepartmentID, input.OrganizationID).Scan(&departmentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return departmentadmin.Department{}, departmentadmin.ErrDepartmentNotFound
	}
	if err != nil {
		return departmentadmin.Department{}, err
	}
	if err := validateActiveParent(ctx, tx, input.OrganizationID, input.ParentID); err != nil {
		return departmentadmin.Department{}, err
	}
	if input.ParentID != "" {
		var invalidParent bool
		if err := tx.QueryRow(ctx, `
			WITH RECURSIVE descendants AS (
				SELECT id FROM departments WHERE id = $1 AND organization_id = $2
				UNION ALL
				SELECT child.id FROM departments child
				JOIN descendants parent ON child.parent_id = parent.id
				WHERE child.organization_id = $2
			)
			SELECT EXISTS(SELECT 1 FROM descendants WHERE id::text = $3)
		`, input.DepartmentID, input.OrganizationID, input.ParentID).Scan(&invalidParent); err != nil {
			return departmentadmin.Department{}, err
		}
		if invalidParent {
			return departmentadmin.Department{}, departmentadmin.ErrInvalidParent
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE departments
		SET parent_id = $1, name = $2, sort_order = $3, updated_at = NOW()
		WHERE id = $4 AND organization_id = $5
	`, nullableUUID(input.ParentID), input.Name, input.SortOrder, input.DepartmentID, input.OrganizationID); err != nil {
		return departmentadmin.Department{}, translateDepartmentWriteError(err)
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{
		OrganizationID: input.OrganizationID, ActorID: input.ActorID, RequestID: input.RequestID,
		Action: "identity.department.update", ResourceType: "department", ResourceID: input.DepartmentID,
		Result: "success", SourceIP: input.SourceIP,
		Detail: map[string]any{"name": input.Name, "parentId": input.ParentID, "sortOrder": input.SortOrder},
	}); err != nil {
		return departmentadmin.Department{}, err
	}
	department, err := findManagedDepartment(ctx, tx, input.OrganizationID, input.DepartmentID)
	if err != nil {
		return departmentadmin.Department{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return departmentadmin.Department{}, err
	}
	return department, nil
}

func (r *IdentityRepository) ChangeDepartmentStatus(ctx context.Context, input departmentadmin.ChangeStatusInput) (departmentadmin.Department, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return departmentadmin.Department{}, err
	}
	defer tx.Rollback(ctx)

	var previousStatus string
	var parentID *string
	err = tx.QueryRow(ctx, `
		SELECT status, parent_id::text FROM departments
		WHERE id = $1 AND organization_id = $2
		FOR UPDATE
	`, input.DepartmentID, input.OrganizationID).Scan(&previousStatus, &parentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return departmentadmin.Department{}, departmentadmin.ErrDepartmentNotFound
	}
	if err != nil {
		return departmentadmin.Department{}, err
	}
	if input.Status == "active" && parentID != nil {
		if err := validateActiveParent(ctx, tx, input.OrganizationID, *parentID); err != nil {
			return departmentadmin.Department{}, err
		}
	}
	if input.Status == "disabled" {
		var activeChildren, activeUsers int
		if err := tx.QueryRow(ctx, `
			SELECT
				(SELECT COUNT(*) FROM departments WHERE parent_id = $1 AND status = 'active'),
				(SELECT COUNT(*) FROM users WHERE department_id = $1 AND status = 'active')
		`, input.DepartmentID).Scan(&activeChildren, &activeUsers); err != nil {
			return departmentadmin.Department{}, err
		}
		if activeChildren > 0 || activeUsers > 0 {
			return departmentadmin.Department{}, departmentadmin.ErrDepartmentInUse
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE departments SET status = $1, updated_at = NOW()
		WHERE id = $2 AND organization_id = $3
	`, input.Status, input.DepartmentID, input.OrganizationID); err != nil {
		return departmentadmin.Department{}, err
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{
		OrganizationID: input.OrganizationID, ActorID: input.ActorID, RequestID: input.RequestID,
		Action: "identity.department.status_change", ResourceType: "department", ResourceID: input.DepartmentID,
		Result: "success", SourceIP: input.SourceIP,
		Detail: map[string]any{"previousStatus": previousStatus, "status": input.Status},
	}); err != nil {
		return departmentadmin.Department{}, err
	}
	department, err := findManagedDepartment(ctx, tx, input.OrganizationID, input.DepartmentID)
	if err != nil {
		return departmentadmin.Department{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return departmentadmin.Department{}, err
	}
	return department, nil
}

func (r *IdentityRepository) AssignUserDepartment(ctx context.Context, input departmentadmin.AssignUserInput) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var previousDepartmentID *string
	err = tx.QueryRow(ctx, `
		SELECT department_id::text FROM users
		WHERE id = $1 AND organization_id = $2
		FOR UPDATE
	`, input.UserID, input.OrganizationID).Scan(&previousDepartmentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return departmentadmin.ErrUserNotFound
	}
	if err != nil {
		return err
	}
	if err := validateActiveParent(ctx, tx, input.OrganizationID, input.DepartmentID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE users SET department_id = $1, updated_at = NOW()
		WHERE id = $2 AND organization_id = $3
	`, nullableUUID(input.DepartmentID), input.UserID, input.OrganizationID); err != nil {
		return err
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{
		OrganizationID: input.OrganizationID, ActorID: input.ActorID, RequestID: input.RequestID,
		Action: "identity.user.department_change", ResourceType: "user", ResourceID: input.UserID,
		Result: "success", SourceIP: input.SourceIP,
		Detail: map[string]any{"previousDepartmentId": previousDepartmentID, "departmentId": input.DepartmentID},
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func validateActiveParent(ctx context.Context, queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, organizationID, parentID string) error {
	if parentID == "" {
		return nil
	}
	var exists bool
	if err := queryer.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM departments
			WHERE id = $1 AND organization_id = $2 AND status = 'active'
		)
	`, parentID, organizationID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return departmentadmin.ErrParentNotFound
	}
	return nil
}

func findManagedDepartment(ctx context.Context, queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, organizationID, departmentID string) (departmentadmin.Department, error) {
	department, err := scanManagedDepartment(queryer.QueryRow(ctx, `
		SELECT `+managedDepartmentColumns+`
		FROM departments d
		WHERE d.organization_id = $1 AND d.id = $2
	`, organizationID, departmentID))
	if errors.Is(err, pgx.ErrNoRows) {
		return departmentadmin.Department{}, departmentadmin.ErrDepartmentNotFound
	}
	return department, err
}

func scanManagedDepartment(row rowScanner) (departmentadmin.Department, error) {
	var department departmentadmin.Department
	err := row.Scan(&department.ID, &department.ParentID, &department.Code, &department.Name,
		&department.SortOrder, &department.Status, &department.UserCount, &department.CreatedAt)
	return department, err
}

func translateDepartmentWriteError(err error) error {
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) {
		return err
	}
	switch postgresError.Code {
	case "23505":
		return departmentadmin.ErrDepartmentConflict
	case "23503":
		return departmentadmin.ErrParentNotFound
	default:
		return fmt.Errorf("写入部门失败: %w", err)
	}
}
