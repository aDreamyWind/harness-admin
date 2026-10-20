package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"harness-admin/internal/identity"
	"harness-admin/internal/modelgateway"
)

const alertColumns = `id, alert_month, dedupe_key, kind, subject_type, subject_name, severity,
	used_tokens, token_limit, call_count, unknown_usage_calls, first_detected_at,
	updated_at, acknowledged_at, resolved_at`

func (r *ModelGatewayRepository) ListAlertCandidates(ctx context.Context, organizationID, month string) ([]modelgateway.AlertCandidate, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT 'quota:' || p.subject_type || ':' || p.subject_id::TEXT, 'quota', p.subject_type,
			COALESCE(u.display_name, d.name, p.subject_id::TEXT), COALESCE(mu.total_tokens, 0),
			p.monthly_token_limit, 0::BIGINT, 0::BIGINT
		FROM model_subject_policies p
		LEFT JOIN users u ON p.subject_type = 'user' AND u.organization_id = p.organization_id AND u.id = p.subject_id
		LEFT JOIN departments d ON p.subject_type = 'department' AND d.organization_id = p.organization_id AND d.id = p.subject_id
		LEFT JOIN model_subject_monthly_usage mu ON mu.organization_id = p.organization_id
			AND mu.subject_type = p.subject_type AND mu.subject_id = p.subject_id AND mu.usage_month = $2::DATE
		WHERE p.organization_id = $1 AND p.monthly_token_limit > 0
			AND (p.subject_type = 'user' OR p.monthly_token_mode = 'shared')
		UNION ALL
		SELECT 'quota:department_user:' || p.subject_id::TEXT || ':' || du.user_id::TEXT,
			'quota', 'department_user', COALESCE(d.name, p.subject_id::TEXT) || ' / ' || u.display_name,
			du.total_tokens, p.monthly_token_limit, 0::BIGINT, 0::BIGINT
		FROM model_subject_policies p
		JOIN model_department_user_monthly_usage du ON du.organization_id = p.organization_id
			AND du.subject_id = p.subject_id AND du.usage_month = $2::DATE
		JOIN users u ON u.organization_id = p.organization_id AND u.id = du.user_id
			AND u.department_id = p.subject_id AND u.status = 'active'
		LEFT JOIN departments d ON d.organization_id = p.organization_id AND d.id = p.subject_id
		WHERE p.organization_id = $1 AND p.subject_type = 'department'
			AND p.monthly_token_mode = 'per_user' AND p.monthly_token_limit > 0
		UNION ALL
		SELECT 'quota:api_key:' || k.id::TEXT, 'quota', 'api_key', k.name,
			COALESCE(mu.total_tokens, 0), k.monthly_token_limit, 0::BIGINT, 0::BIGINT
		FROM model_gateway_api_keys k
		LEFT JOIN model_gateway_monthly_usage mu ON mu.organization_id = k.organization_id
			AND mu.api_key_id = k.id AND mu.usage_month = $2::DATE
		WHERE k.organization_id = $1 AND k.status = 'active'
			AND (k.expires_at IS NULL OR k.expires_at > NOW()) AND k.monthly_token_limit > 0
		UNION ALL
		SELECT 'unknown_usage:model:' || c.model_code, 'unknown_usage', 'model', c.model_code,
			0::BIGINT, 0::BIGINT, COUNT(*)::BIGINT,
			COUNT(*) FILTER (WHERE NOT c.usage_reported)::BIGINT
		FROM model_gateway_calls c
		WHERE c.organization_id = $1 AND c.result = 'success'
			AND c.created_at >= $2::DATE AND c.created_at < $2::DATE + INTERVAL '1 month'
		GROUP BY c.model_code
		ORDER BY 1
	`, organizationID, month+"-01")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]modelgateway.AlertCandidate, 0)
	for rows.Next() {
		var item modelgateway.AlertCandidate
		if err := rows.Scan(&item.DedupeKey, &item.Kind, &item.SubjectType, &item.SubjectName,
			&item.UsedTokens, &item.TokenLimit, &item.CallCount, &item.UnknownUsageCalls); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *ModelGatewayRepository) SyncAlerts(ctx context.Context, organizationID, month string, signals []modelgateway.AlertSignal) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	keys := make([]string, 0, len(signals))
	for _, signal := range signals {
		keys = append(keys, signal.DedupeKey)
		_, err = tx.Exec(ctx, `INSERT INTO model_gateway_alerts (
			id, organization_id, alert_month, dedupe_key, kind, subject_type, subject_name, severity,
			used_tokens, token_limit, call_count, unknown_usage_calls
		) VALUES ($1, $2, $3::DATE, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (organization_id, alert_month, dedupe_key) DO UPDATE SET
			subject_name = EXCLUDED.subject_name, severity = EXCLUDED.severity,
			used_tokens = EXCLUDED.used_tokens, token_limit = EXCLUDED.token_limit,
			call_count = EXCLUDED.call_count, unknown_usage_calls = EXCLUDED.unknown_usage_calls,
			acknowledged_at = CASE WHEN model_gateway_alerts.resolved_at IS NOT NULL
				OR (model_gateway_alerts.severity = 'warning' AND EXCLUDED.severity = 'critical')
				THEN NULL ELSE model_gateway_alerts.acknowledged_at END,
			acknowledged_by = CASE WHEN model_gateway_alerts.resolved_at IS NOT NULL
				OR (model_gateway_alerts.severity = 'warning' AND EXCLUDED.severity = 'critical')
				THEN NULL ELSE model_gateway_alerts.acknowledged_by END,
			resolved_at = NULL, updated_at = NOW()
		WHERE model_gateway_alerts.resolved_at IS NOT NULL OR
			(model_gateway_alerts.subject_name, model_gateway_alerts.severity,
			model_gateway_alerts.used_tokens, model_gateway_alerts.token_limit,
			model_gateway_alerts.call_count, model_gateway_alerts.unknown_usage_calls)
			IS DISTINCT FROM
			(EXCLUDED.subject_name, EXCLUDED.severity,
			EXCLUDED.used_tokens, EXCLUDED.token_limit,
			EXCLUDED.call_count, EXCLUDED.unknown_usage_calls)`, uuid.NewString(), organizationID, month+"-01",
			signal.DedupeKey, signal.Kind, signal.SubjectType, signal.SubjectName, signal.Severity,
			signal.UsedTokens, signal.TokenLimit, signal.CallCount, signal.UnknownUsageCalls)
		if err != nil {
			return fmt.Errorf("保存预警 %q 失败: %w", signal.DedupeKey, err)
		}
	}
	_, err = tx.Exec(ctx, `UPDATE model_gateway_alerts SET resolved_at = NOW(), updated_at = NOW()
		WHERE organization_id = $1 AND alert_month = $2::DATE AND resolved_at IS NULL
			AND NOT (dedupe_key = ANY($3::TEXT[]))`, organizationID, month+"-01", keys)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *ModelGatewayRepository) ListAlerts(ctx context.Context, query modelgateway.AlertQuery) (modelgateway.AlertPage, error) {
	page := modelgateway.AlertPage{Month: query.Month, Page: query.Page, PageSize: query.PageSize, Items: []modelgateway.Alert{}}
	month := query.Month + "-01"
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM model_gateway_alerts
		WHERE organization_id = $1 AND alert_month = $2::DATE AND resolved_at IS NULL`, query.OrganizationID, month).Scan(&page.ActiveCount); err != nil {
		return modelgateway.AlertPage{}, err
	}
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM model_gateway_alerts
		WHERE organization_id = $1 AND alert_month = $2::DATE
			AND ($3 = 'all' OR resolved_at IS NULL)`, query.OrganizationID, month, query.Status).Scan(&page.Total); err != nil {
		return modelgateway.AlertPage{}, err
	}
	rows, err := r.pool.Query(ctx, `SELECT `+alertColumns+` FROM model_gateway_alerts
		WHERE organization_id = $1 AND alert_month = $2::DATE
			AND ($3 = 'all' OR resolved_at IS NULL)
		ORDER BY resolved_at NULLS FIRST, CASE severity WHEN 'critical' THEN 0 ELSE 1 END,
			acknowledged_at NULLS FIRST, first_detected_at DESC, id DESC
		LIMIT $4 OFFSET $5`, query.OrganizationID, month, query.Status, query.PageSize, (query.Page-1)*query.PageSize)
	if err != nil {
		return modelgateway.AlertPage{}, err
	}
	defer rows.Close()
	for rows.Next() {
		item, err := scanGatewayAlert(rows)
		if err != nil {
			return modelgateway.AlertPage{}, err
		}
		page.Items = append(page.Items, item)
	}
	return page, rows.Err()
}

func (r *ModelGatewayRepository) AcknowledgeAlert(ctx context.Context, input modelgateway.AcknowledgeAlertInput) (modelgateway.Alert, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return modelgateway.Alert{}, err
	}
	defer tx.Rollback(ctx)
	item, err := scanGatewayAlert(tx.QueryRow(ctx, `UPDATE model_gateway_alerts
		SET acknowledged_at = COALESCE(acknowledged_at, NOW()),
			acknowledged_by = COALESCE(acknowledged_by, $3::UUID)
		WHERE organization_id = $1 AND id = $2 AND resolved_at IS NULL
		RETURNING `+alertColumns, input.OrganizationID, input.AlertID, input.ActorID))
	if errors.Is(err, pgx.ErrNoRows) {
		return modelgateway.Alert{}, modelgateway.ErrAlertNotFound
	}
	if err != nil {
		return modelgateway.Alert{}, err
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{
		OrganizationID: input.OrganizationID, ActorID: input.ActorID, RequestID: input.RequestID,
		Action: "model.gateway_alert.acknowledge", ResourceType: "model_gateway_alert", ResourceID: input.AlertID,
		Result: "success", SourceIP: input.SourceIP,
		Detail: map[string]any{"dedupeKey": item.DedupeKey, "severity": item.Severity},
	}); err != nil {
		return modelgateway.Alert{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return modelgateway.Alert{}, err
	}
	return item, nil
}

func scanGatewayAlert(row interface{ Scan(...any) error }) (modelgateway.Alert, error) {
	var item modelgateway.Alert
	var month time.Time
	err := row.Scan(&item.ID, &month, &item.DedupeKey, &item.Kind, &item.SubjectType,
		&item.SubjectName, &item.Severity, &item.UsedTokens, &item.TokenLimit,
		&item.CallCount, &item.UnknownUsageCalls, &item.FirstDetectedAt, &item.UpdatedAt,
		&item.AcknowledgedAt, &item.ResolvedAt)
	if err != nil {
		return modelgateway.Alert{}, err
	}
	item.Month = month.Format("2006-01")
	return item, nil
}
