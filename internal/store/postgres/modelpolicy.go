package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"harness-admin/internal/identity"
	"harness-admin/internal/modelgateway"
)

type ModelPolicyRepository struct{ pool *pgxpool.Pool }

func NewModelPolicyRepository(pool *pgxpool.Pool) *ModelPolicyRepository {
	return &ModelPolicyRepository{pool: pool}
}

const policyColumns = `p.organization_id, p.subject_type, p.subject_id,
	COALESCE(u.display_name, d.name, ''), p.scope_mode, p.allowed_model_codes, p.requests_per_minute,
	p.monthly_token_limit, p.monthly_token_mode, COALESCE(mu.total_tokens, 0), p.created_at, p.updated_at`

const policyJoins = `
	LEFT JOIN users u ON p.subject_type = 'user' AND u.organization_id = p.organization_id AND u.id = p.subject_id
	LEFT JOIN departments d ON p.subject_type = 'department' AND d.organization_id = p.organization_id AND d.id = p.subject_id
	LEFT JOIN model_subject_monthly_usage mu ON mu.organization_id = p.organization_id
		AND mu.subject_type = p.subject_type AND mu.subject_id = p.subject_id
		AND mu.usage_month = DATE_TRUNC('month', NOW() AT TIME ZONE 'UTC')::DATE`

func (r *ModelPolicyRepository) ListPolicies(ctx context.Context, organizationID string) ([]modelgateway.SubjectPolicy, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+policyColumns+` FROM model_subject_policies p `+policyJoins+`
		WHERE p.organization_id = $1 ORDER BY p.subject_type, p.updated_at DESC, p.subject_id`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]modelgateway.SubjectPolicy, 0)
	for rows.Next() {
		item, err := scanSubjectPolicy(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *ModelPolicyRepository) SavePolicy(ctx context.Context, input modelgateway.SavePolicyInput) (modelgateway.SubjectPolicy, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return modelgateway.SubjectPolicy{}, err
	}
	defer tx.Rollback(ctx)
	table := "users"
	if input.SubjectType == "department" {
		table = "departments"
	}
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM `+table+` WHERE organization_id = $1 AND id = $2)`, input.OrganizationID, input.SubjectID).Scan(&exists); err != nil {
		return modelgateway.SubjectPolicy{}, err
	}
	if !exists {
		return modelgateway.SubjectPolicy{}, modelgateway.ErrPolicySubjectNotFound
	}
	if input.AllowedModelCodes != nil {
		var missing int
		err := tx.QueryRow(ctx, `
			SELECT COUNT(*) FROM unnest($2::TEXT[]) code
			WHERE NOT EXISTS (SELECT 1 FROM model_catalog_entries m
				JOIN model_provider_connections p ON p.id = m.provider_connection_id
				WHERE m.organization_id = $1 AND LOWER(m.code) = code
				AND ((m.model_type = 'chat' AND p.provider_kind NOT IN ('claude', 'gemini'))
				  OR (m.model_type = 'image' AND p.provider_kind IN ('openai_compatible', 'aliyun_bailian'))
				  OR (m.model_type = 'video' AND p.provider_kind = 'aliyun_bailian')))
		`, input.OrganizationID, input.AllowedModelCodes).Scan(&missing)
		if err != nil {
			return modelgateway.SubjectPolicy{}, err
		}
		if missing > 0 {
			return modelgateway.SubjectPolicy{}, modelgateway.ErrModelNotFound
		}
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO model_subject_policies (
			organization_id, subject_type, subject_id, scope_mode, allowed_model_codes, requests_per_minute, monthly_token_limit, monthly_token_mode
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (organization_id, subject_type, subject_id) DO UPDATE SET
			scope_mode = EXCLUDED.scope_mode,
			allowed_model_codes = EXCLUDED.allowed_model_codes,
			requests_per_minute = EXCLUDED.requests_per_minute,
			monthly_token_limit = EXCLUDED.monthly_token_limit,
			monthly_token_mode = EXCLUDED.monthly_token_mode,
			updated_at = NOW()
	`, input.OrganizationID, input.SubjectType, input.SubjectID, input.ScopeMode, input.AllowedModelCodes, input.RequestsPerMinute, input.MonthlyTokenLimit, input.MonthlyTokenMode)
	if err != nil {
		return modelgateway.SubjectPolicy{}, err
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{
		OrganizationID: input.OrganizationID, ActorID: input.ActorID, RequestID: input.RequestID,
		Action: "model.policy.save", ResourceType: "model_policy", ResourceID: input.SubjectID,
		Result: "success", SourceIP: input.SourceIP,
		Detail: map[string]any{"subjectType": input.SubjectType, "scopeMode": input.ScopeMode, "allowedModelCodes": input.AllowedModelCodes,
			"requestsPerMinute": input.RequestsPerMinute, "monthlyTokenLimit": input.MonthlyTokenLimit, "monthlyTokenMode": input.MonthlyTokenMode},
	}); err != nil {
		return modelgateway.SubjectPolicy{}, err
	}
	item, err := findSubjectPolicy(ctx, tx, input.OrganizationID, input.SubjectType, input.SubjectID)
	if err != nil {
		return modelgateway.SubjectPolicy{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return modelgateway.SubjectPolicy{}, err
	}
	return item, nil
}

func (r *ModelPolicyRepository) SaveDepartmentPolicies(ctx context.Context, input modelgateway.SaveDepartmentPoliciesInput) ([]modelgateway.SubjectPolicy, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var existing int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM departments
		WHERE organization_id = $1 AND id IN (SELECT value::UUID FROM unnest($2::TEXT[]) AS value)`, input.OrganizationID, input.SubjectIDs).Scan(&existing); err != nil {
		return nil, err
	}
	if existing != len(input.SubjectIDs) {
		return nil, modelgateway.ErrPolicySubjectNotFound
	}
	if len(input.AllowedModelCodes) > 0 {
		var missing int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM unnest($2::TEXT[]) code
			WHERE NOT EXISTS (SELECT 1 FROM model_catalog_entries m
				JOIN model_provider_connections p ON p.id = m.provider_connection_id
				WHERE m.organization_id = $1 AND LOWER(m.code) = code
				AND ((m.model_type = 'chat' AND p.provider_kind NOT IN ('claude', 'gemini'))
				  OR (m.model_type = 'image' AND p.provider_kind IN ('openai_compatible', 'aliyun_bailian'))
				  OR (m.model_type = 'video' AND p.provider_kind = 'aliyun_bailian')))`,
			input.OrganizationID, input.AllowedModelCodes).Scan(&missing); err != nil {
			return nil, err
		}
		if missing > 0 {
			return nil, modelgateway.ErrModelNotFound
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO model_subject_policies (
		organization_id, subject_type, subject_id, scope_mode, allowed_model_codes, requests_per_minute, monthly_token_limit, monthly_token_mode
	) SELECT $1, 'department', selected.id::UUID, $3, $4, $5, $6, $7
	FROM unnest($2::TEXT[]) AS selected(id)
		ON CONFLICT (organization_id, subject_type, subject_id) DO UPDATE SET
			scope_mode = EXCLUDED.scope_mode,
			allowed_model_codes = EXCLUDED.allowed_model_codes,
			requests_per_minute = EXCLUDED.requests_per_minute,
			monthly_token_limit = EXCLUDED.monthly_token_limit,
			monthly_token_mode = EXCLUDED.monthly_token_mode,
			updated_at = NOW()`, input.OrganizationID, input.SubjectIDs, input.ScopeMode, input.AllowedModelCodes,
		input.RequestsPerMinute, input.MonthlyTokenLimit, input.MonthlyTokenMode)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT `+policyColumns+` FROM model_subject_policies p `+policyJoins+`
		WHERE p.organization_id = $1 AND p.subject_type = 'department'
		AND p.subject_id IN (SELECT value::UUID FROM unnest($2::TEXT[]) AS value)
		ORDER BY p.subject_id`, input.OrganizationID, input.SubjectIDs)
	if err != nil {
		return nil, err
	}
	items := make([]modelgateway.SubjectPolicy, 0, len(input.SubjectIDs))
	for rows.Next() {
		item, err := scanSubjectPolicy(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if err := insertAudit(ctx, tx, identity.AuditEvent{
		OrganizationID: input.OrganizationID, ActorID: input.ActorID, RequestID: input.RequestID,
		Action: "model.policy.batch_save", ResourceType: "model_policy", ResourceID: "departments",
		Result: "success", SourceIP: input.SourceIP,
		Detail: map[string]any{"subjectIds": input.SubjectIDs, "scopeMode": input.ScopeMode,
			"allowedModelCodes": input.AllowedModelCodes, "requestsPerMinute": input.RequestsPerMinute,
			"monthlyTokenLimit": input.MonthlyTokenLimit, "monthlyTokenMode": input.MonthlyTokenMode},
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *ModelPolicyRepository) DeletePolicy(ctx context.Context, input modelgateway.DeletePolicyInput) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	command, err := tx.Exec(ctx, `DELETE FROM model_subject_policies
		WHERE organization_id = $1 AND subject_type = $2 AND subject_id = $3`, input.OrganizationID, input.SubjectType, input.SubjectID)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return modelgateway.ErrPolicyNotFound
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{
		OrganizationID: input.OrganizationID, ActorID: input.ActorID, RequestID: input.RequestID,
		Action: "model.policy.delete", ResourceType: "model_policy", ResourceID: input.SubjectID,
		Result: "success", SourceIP: input.SourceIP, Detail: map[string]any{"subjectType": input.SubjectType},
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *ModelPolicyRepository) FindUserPolicy(ctx context.Context, organizationID, userID string) (modelgateway.UserPolicySnapshot, error) {
	var snapshot modelgateway.UserPolicySnapshot
	err := r.pool.QueryRow(ctx, `SELECT COALESCE(u.department_id::TEXT, '') FROM users u
		JOIN organizations o ON o.id = u.organization_id
		WHERE u.organization_id = $1 AND u.id = $2 AND u.status = 'active' AND o.status = 'active'`, organizationID, userID).Scan(&snapshot.DepartmentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return modelgateway.UserPolicySnapshot{}, modelgateway.ErrInvalidCredential
	}
	if err != nil {
		return modelgateway.UserPolicySnapshot{}, err
	}
	userPolicy, err := findSubjectPolicy(ctx, r.pool, organizationID, "user", userID)
	if err != nil && !errors.Is(err, modelgateway.ErrPolicyNotFound) {
		return modelgateway.UserPolicySnapshot{}, err
	}
	if err == nil {
		snapshot.UserPolicy = &userPolicy
	}
	if snapshot.DepartmentID != "" {
		policy, err := findSubjectPolicy(ctx, r.pool, organizationID, "department", snapshot.DepartmentID)
		if err != nil && !errors.Is(err, modelgateway.ErrPolicyNotFound) {
			return modelgateway.UserPolicySnapshot{}, err
		}
		if err == nil {
			if policy.MonthlyTokenMode == "per_user" {
				err = r.pool.QueryRow(ctx, `SELECT COALESCE(SUM(total_tokens), 0)::BIGINT FROM model_department_user_monthly_usage
					WHERE organization_id = $1 AND subject_id = $2 AND user_id = $3
					AND usage_month = DATE_TRUNC('month', NOW() AT TIME ZONE 'UTC')::DATE`, organizationID, snapshot.DepartmentID, userID).Scan(&policy.MonthlyTokenUsed)
				if err != nil {
					return modelgateway.UserPolicySnapshot{}, err
				}
			}
			snapshot.DepartmentPolicy = &policy
		}
	}
	return snapshot, nil
}

func (r *ModelPolicyRepository) ListAvailableModels(ctx context.Context, organizationID string) ([]modelgateway.AvailableModel, error) {
	rows, err := r.pool.Query(ctx, `SELECT m.code, m.name, m.model_type, m.capabilities, m.created_at, m.upstream_model_id, p.provider_kind FROM model_catalog_entries m
		JOIN model_provider_connections p ON p.organization_id = m.organization_id AND p.id = m.provider_connection_id
		WHERE m.organization_id = $1 AND m.status = 'active' AND p.status = 'active'
		  AND ((m.model_type = 'chat' AND p.provider_kind NOT IN ('claude', 'gemini'))
		    OR (m.model_type = 'image' AND p.provider_kind IN ('openai_compatible', 'aliyun_bailian'))
		    OR (m.model_type = 'video' AND p.provider_kind = 'aliyun_bailian'))
		ORDER BY m.code`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]modelgateway.AvailableModel, 0)
	for rows.Next() {
		var item modelgateway.AvailableModel
		if err := rows.Scan(&item.Code, &item.Name, &item.ModelType, &item.Capabilities, &item.CreatedAt, &item.UpstreamModelID, &item.ProviderKind); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func findSubjectPolicy(ctx context.Context, queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, organizationID, subjectType, subjectID string) (modelgateway.SubjectPolicy, error) {
	item, err := scanSubjectPolicy(queryer.QueryRow(ctx, `SELECT `+policyColumns+` FROM model_subject_policies p `+policyJoins+`
		WHERE p.organization_id = $1 AND p.subject_type = $2 AND p.subject_id = $3`, organizationID, subjectType, subjectID))
	if errors.Is(err, pgx.ErrNoRows) {
		return modelgateway.SubjectPolicy{}, modelgateway.ErrPolicyNotFound
	}
	return item, err
}

func scanSubjectPolicy(row interface{ Scan(...any) error }) (modelgateway.SubjectPolicy, error) {
	var item modelgateway.SubjectPolicy
	err := row.Scan(&item.OrganizationID, &item.SubjectType, &item.SubjectID, &item.SubjectName,
		&item.ScopeMode, &item.AllowedModelCodes, &item.RequestsPerMinute, &item.MonthlyTokenLimit, &item.MonthlyTokenMode, &item.MonthlyTokenUsed,
		&item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return modelgateway.SubjectPolicy{}, fmt.Errorf("读取模型策略失败: %w", err)
	}
	return item, nil
}
