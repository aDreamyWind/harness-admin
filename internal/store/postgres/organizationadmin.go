package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"harness-admin/internal/identity"
	"harness-admin/internal/organizationadmin"
)

func (r *IdentityRepository) ListActiveOrganizations(ctx context.Context) ([]organizationadmin.Organization, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, code, name, status, created_at, updated_at
		FROM organizations
		WHERE status = 'active'
		ORDER BY created_at, name, id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	organizations := make([]organizationadmin.Organization, 0)
	for rows.Next() {
		organization, err := scanOrganization(rows)
		if err != nil {
			return nil, err
		}
		organizations = append(organizations, organization)
	}
	return organizations, rows.Err()
}

func (r *IdentityRepository) FindOrganization(ctx context.Context, organizationID string) (organizationadmin.Organization, error) {
	return scanOrganization(r.pool.QueryRow(ctx, `
		SELECT id, code, name, status, created_at, updated_at
		FROM organizations
		WHERE id = $1
	`, organizationID))
}

func (r *IdentityRepository) UpdateOrganization(ctx context.Context, input organizationadmin.NameUpdate) (organizationadmin.Organization, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return organizationadmin.Organization{}, err
	}
	defer tx.Rollback(ctx)

	var previousName string
	err = tx.QueryRow(ctx, `
		SELECT name
		FROM organizations
		WHERE id = $1
		FOR UPDATE
	`, input.OrganizationID).Scan(&previousName)
	if errors.Is(err, pgx.ErrNoRows) {
		return organizationadmin.Organization{}, organizationadmin.ErrOrganizationNotFound
	}
	if err != nil {
		return organizationadmin.Organization{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE organizations SET name = $1, updated_at = NOW()
		WHERE id = $2
	`, input.Name, input.OrganizationID); err != nil {
		return organizationadmin.Organization{}, err
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{
		OrganizationID: input.OrganizationID,
		ActorID:        input.ActorID,
		RequestID:      input.RequestID,
		Action:         "identity.organization.update",
		ResourceType:   "organization",
		ResourceID:     input.OrganizationID,
		Result:         "success",
		SourceIP:       input.SourceIP,
		Detail: map[string]any{
			"previousName": previousName,
			"name":         input.Name,
		},
	}); err != nil {
		return organizationadmin.Organization{}, err
	}
	organization, err := scanOrganization(tx.QueryRow(ctx, `
		SELECT id, code, name, status, created_at, updated_at
		FROM organizations
		WHERE id = $1
	`, input.OrganizationID))
	if err != nil {
		return organizationadmin.Organization{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return organizationadmin.Organization{}, err
	}
	return organization, nil
}

func scanOrganization(row rowScanner) (organizationadmin.Organization, error) {
	var organization organizationadmin.Organization
	err := row.Scan(
		&organization.ID,
		&organization.Code,
		&organization.Name,
		&organization.Status,
		&organization.CreatedAt,
		&organization.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return organizationadmin.Organization{}, organizationadmin.ErrOrganizationNotFound
	}
	return organization, err
}
