package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"harness-admin/internal/identity"
	"harness-admin/internal/testseed"
)

type TestSeedRepository struct {
	pool *pgxpool.Pool
}

func NewTestSeedRepository(pool *pgxpool.Pool) *TestSeedRepository {
	return &TestSeedRepository{pool: pool}
}

func (r *TestSeedRepository) SeedOrganization(ctx context.Context, input testseed.OrganizationInput, departments []testseed.Department) (testseed.OrganizationResult, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return testseed.OrganizationResult{}, err
	}
	defer tx.Rollback(ctx)

	var organizationID string
	if err := tx.QueryRow(ctx, `
		SELECT id FROM organizations WHERE LOWER(code) = LOWER($1)
	`, input.OrganizationCode).Scan(&organizationID); err != nil {
		if err == pgx.ErrNoRows {
			return testseed.OrganizationResult{}, testseed.ErrOrganizationNotFound
		}
		return testseed.OrganizationResult{}, err
	}

	var actorID string
	if err := tx.QueryRow(ctx, `
		SELECT id FROM users
		WHERE organization_id = $1 AND LOWER(username) = LOWER($2)
	`, organizationID, input.ActorUsername).Scan(&actorID); err != nil {
		if err == pgx.ErrNoRows {
			return testseed.OrganizationResult{}, testseed.ErrActorNotFound
		}
		return testseed.OrganizationResult{}, err
	}

	departmentIDs := make(map[string]string, len(departments))
	result := testseed.OrganizationResult{OrganizationID: organizationID}
	for _, department := range departments {
		parentID := ""
		if department.ParentCode != "" {
			parentID = departmentIDs[department.ParentCode]
		}

		var departmentID string
		err := tx.QueryRow(ctx, `
			SELECT id FROM departments
			WHERE organization_id = $1 AND code = $2
		`, organizationID, department.Code).Scan(&departmentID)
		switch err {
		case nil:
			if _, err := tx.Exec(ctx, `
				UPDATE departments
				SET parent_id = $1, name = $2, sort_order = $3, status = 'active', updated_at = NOW()
				WHERE id = $4
			`, nullableUUID(parentID), department.Name, department.SortOrder, departmentID); err != nil {
				return testseed.OrganizationResult{}, fmt.Errorf("更新部门 %s 失败: %w", department.Code, err)
			}
			result.UpdatedCount++
		case pgx.ErrNoRows:
			departmentID = uuid.NewString()
			if _, err := tx.Exec(ctx, `
				INSERT INTO departments (id, organization_id, parent_id, code, name, sort_order)
				VALUES ($1, $2, $3, $4, $5, $6)
			`, departmentID, organizationID, nullableUUID(parentID), department.Code, department.Name, department.SortOrder); err != nil {
				return testseed.OrganizationResult{}, fmt.Errorf("创建部门 %s 失败: %w", department.Code, err)
			}
			result.CreatedCount++
		default:
			return testseed.OrganizationResult{}, fmt.Errorf("查询部门 %s 失败: %w", department.Code, err)
		}
		departmentIDs[department.Code] = departmentID
	}

	if err := insertAudit(ctx, tx, identity.AuditEvent{
		OrganizationID: organizationID,
		ActorID:        actorID,
		Action:         "test_data.organization_tree.seed",
		ResourceType:   "organization",
		ResourceID:     organizationID,
		Result:         "success",
		Detail: map[string]any{
			"createdCount": result.CreatedCount,
			"updatedCount": result.UpdatedCount,
		},
	}); err != nil {
		return testseed.OrganizationResult{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return testseed.OrganizationResult{}, err
	}
	return result, nil
}
