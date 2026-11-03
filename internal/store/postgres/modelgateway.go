package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"harness-admin/internal/identity"
	"harness-admin/internal/modelgateway"
	"harness-admin/internal/secret"
)

type ModelGatewayRepository struct {
	pool   *pgxpool.Pool
	cipher *secret.Cipher
}

func NewModelGatewayRepository(pool *pgxpool.Pool, cipher *secret.Cipher) *ModelGatewayRepository {
	return &ModelGatewayRepository{pool: pool, cipher: cipher}
}

const gatewayAPIKeyColumns = `
	k.id, k.organization_id, k.name, k.key_prefix, k.status, k.expires_at,
	k.last_used_at, k.requests_per_minute, k.monthly_token_limit,
	COALESCE((
		SELECT u.total_tokens FROM model_gateway_monthly_usage u
		WHERE u.organization_id = k.organization_id AND u.api_key_id = k.id
		  AND u.usage_month = DATE_TRUNC('month', NOW() AT TIME ZONE 'UTC')::DATE
	), 0), k.allowed_model_codes,
	COALESCE((
		SELECT jsonb_agg(jsonb_build_object(
			'code', scope.code,
			'providerName', COALESCE(p.name, ''),
			'modelName', COALESCE(m.name, '')
		) ORDER BY scope.ordinality)
		FROM unnest(k.allowed_model_codes) WITH ORDINALITY AS scope(code, ordinality)
		LEFT JOIN model_catalog_entries m
		  ON m.organization_id = k.organization_id AND m.code = scope.code
		LEFT JOIN model_provider_connections p
		  ON p.organization_id = k.organization_id AND p.id = m.provider_connection_id
	), '[]'::jsonb), k.created_at, k.revoked_at
`

func (r *ModelGatewayRepository) ListAPIKeys(ctx context.Context, organizationID string) ([]modelgateway.APIKey, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+gatewayAPIKeyColumns+`
		FROM model_gateway_api_keys k
		WHERE k.organization_id = $1
		ORDER BY k.created_at DESC
	`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]modelgateway.APIKey, 0)
	for rows.Next() {
		item, err := scanGatewayAPIKey(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *ModelGatewayRepository) CreateAPIKey(ctx context.Context, input modelgateway.CreateAPIKeyInput) (modelgateway.APIKey, error) {
	ciphertext, err := r.cipher.Seal(input.Secret)
	if err != nil {
		return modelgateway.APIKey{}, err
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return modelgateway.APIKey{}, err
	}
	defer tx.Rollback(ctx)
	apiKeyID := uuid.NewString()
	_, err = tx.Exec(ctx, `
		INSERT INTO model_gateway_api_keys (
			id, organization_id, name, key_prefix, secret_hash, expires_at, created_by,
			requests_per_minute, monthly_token_limit, allowed_model_codes, encrypted_secret
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`, apiKeyID, input.OrganizationID, input.Name, input.KeyPrefix, input.SecretHash, input.ExpiresAt,
		input.ActorID, input.RequestsPerMinute, input.MonthlyTokenLimit, input.AllowedModelCodes, ciphertext)
	if err != nil {
		return modelgateway.APIKey{}, err
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{
		OrganizationID: input.OrganizationID, ActorID: input.ActorID, RequestID: input.RequestID,
		Action: "model.gateway_api_key.create", ResourceType: "model_gateway_api_key", ResourceID: apiKeyID,
		Result: "success", SourceIP: input.SourceIP,
		Detail: map[string]any{"name": input.Name, "keyPrefix": input.KeyPrefix, "expiresAt": input.ExpiresAt,
			"requestsPerMinute": input.RequestsPerMinute, "monthlyTokenLimit": input.MonthlyTokenLimit,
			"allowedModelCodes": input.AllowedModelCodes},
	}); err != nil {
		return modelgateway.APIKey{}, err
	}
	item, err := findGatewayAPIKey(ctx, tx, input.OrganizationID, apiKeyID)
	if err != nil {
		return modelgateway.APIKey{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return modelgateway.APIKey{}, err
	}
	return item, nil
}

func (r *ModelGatewayRepository) RevealAPIKeySecret(ctx context.Context, input modelgateway.RevealAPIKeySecretInput) (string, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	var ciphertext []byte
	err = tx.QueryRow(ctx, `
		SELECT encrypted_secret
		FROM model_gateway_api_keys
		WHERE organization_id = $1 AND id = $2
	`, input.OrganizationID, input.APIKeyID).Scan(&ciphertext)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", modelgateway.ErrAPIKeyNotFound
	}
	if err != nil {
		return "", err
	}
	if len(ciphertext) == 0 {
		return "", modelgateway.ErrAPIKeySecretUnavailable
	}
	value, err := r.cipher.Open(ciphertext)
	if err != nil {
		return "", err
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{
		OrganizationID: input.OrganizationID, ActorID: input.ActorID, RequestID: input.RequestID,
		Action: "model.gateway_api_key.secret.reveal", ResourceType: "model_gateway_api_key", ResourceID: input.APIKeyID,
		Result: "success", SourceIP: input.SourceIP,
	}); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return value, nil
}

func (r *ModelGatewayRepository) UpdateAPIKey(ctx context.Context, input modelgateway.UpdateAPIKeyInput) (modelgateway.APIKey, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return modelgateway.APIKey{}, err
	}
	defer tx.Rollback(ctx)
	command, err := tx.Exec(ctx, `
		UPDATE model_gateway_api_keys
		SET name = $1, expires_at = $2, requests_per_minute = $3,
		    monthly_token_limit = $4, allowed_model_codes = $5, updated_at = NOW()
		WHERE organization_id = $6 AND id = $7
	`, input.Name, input.ExpiresAt, input.RequestsPerMinute, input.MonthlyTokenLimit,
		input.AllowedModelCodes, input.OrganizationID, input.APIKeyID)
	if err != nil {
		return modelgateway.APIKey{}, err
	}
	if command.RowsAffected() == 0 {
		return modelgateway.APIKey{}, modelgateway.ErrAPIKeyNotFound
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{
		OrganizationID: input.OrganizationID, ActorID: input.ActorID, RequestID: input.RequestID,
		Action: "model.gateway_api_key.update", ResourceType: "model_gateway_api_key", ResourceID: input.APIKeyID,
		Result: "success", SourceIP: input.SourceIP,
		Detail: map[string]any{"name": input.Name, "expiresAt": input.ExpiresAt,
			"requestsPerMinute": input.RequestsPerMinute, "monthlyTokenLimit": input.MonthlyTokenLimit,
			"allowedModelCodes": input.AllowedModelCodes},
	}); err != nil {
		return modelgateway.APIKey{}, err
	}
	item, err := findGatewayAPIKey(ctx, tx, input.OrganizationID, input.APIKeyID)
	if err != nil {
		return modelgateway.APIKey{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return modelgateway.APIKey{}, err
	}
	return item, nil
}

func (r *ModelGatewayRepository) RevokeAPIKey(ctx context.Context, input modelgateway.RevokeAPIKeyInput) (modelgateway.APIKey, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return modelgateway.APIKey{}, err
	}
	defer tx.Rollback(ctx)
	command, err := tx.Exec(ctx, `
		UPDATE model_gateway_api_keys
		SET status = 'revoked', revoked_at = COALESCE(revoked_at, NOW()), updated_at = NOW()
		WHERE organization_id = $1 AND id = $2
	`, input.OrganizationID, input.APIKeyID)
	if err != nil {
		return modelgateway.APIKey{}, err
	}
	if command.RowsAffected() == 0 {
		return modelgateway.APIKey{}, modelgateway.ErrAPIKeyNotFound
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{
		OrganizationID: input.OrganizationID, ActorID: input.ActorID, RequestID: input.RequestID,
		Action: "model.gateway_api_key.revoke", ResourceType: "model_gateway_api_key", ResourceID: input.APIKeyID,
		Result: "success", SourceIP: input.SourceIP,
	}); err != nil {
		return modelgateway.APIKey{}, err
	}
	item, err := findGatewayAPIKey(ctx, tx, input.OrganizationID, input.APIKeyID)
	if err != nil {
		return modelgateway.APIKey{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return modelgateway.APIKey{}, err
	}
	return item, nil
}

func (r *ModelGatewayRepository) FindAPIKeyByHash(ctx context.Context, secretHash []byte) (modelgateway.APIKey, error) {
	item, err := scanGatewayAPIKey(r.pool.QueryRow(ctx, `
		SELECT `+gatewayAPIKeyColumns+`
		FROM model_gateway_api_keys k
		JOIN organizations o ON o.id = k.organization_id
		WHERE k.secret_hash = $1 AND o.status = 'active'
	`, secretHash))
	if errors.Is(err, pgx.ErrNoRows) {
		return modelgateway.APIKey{}, modelgateway.ErrAPIKeyNotFound
	}
	return item, err
}

func (r *ModelGatewayRepository) TouchAPIKey(ctx context.Context, apiKeyID string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE model_gateway_api_keys SET last_used_at = NOW(), updated_at = NOW() WHERE id = $1
	`, apiKeyID)
	return err
}

func (r *ModelGatewayRepository) FindRoute(ctx context.Context, organizationID, modelCode string) (modelgateway.Route, error) {
	var route modelgateway.Route
	var encryptedSecret []byte
	err := r.pool.QueryRow(ctx, `
		SELECT m.id, m.code, m.model_type, m.status, m.upstream_model_id,
		       p.id, p.provider_kind, p.status, p.base_url, p.request_timeout_ms, p.stream_usage_enabled, c.encrypted_secret
		FROM model_catalog_entries m
		JOIN model_provider_connections p
		  ON p.organization_id = m.organization_id AND p.id = m.provider_connection_id
		JOIN model_provider_credentials c ON c.provider_connection_id = p.id
		WHERE m.organization_id = $1 AND LOWER(m.code) = LOWER($2)
		  AND ((m.model_type = 'chat' AND p.provider_kind NOT IN ('claude', 'gemini'))
		    OR (m.model_type = 'image' AND p.provider_kind IN ('openai_compatible', 'aliyun_bailian'))
		    OR (m.model_type = 'video' AND p.provider_kind = 'aliyun_bailian'))
	`, organizationID, modelCode).Scan(
		&route.ModelID, &route.ModelCode, &route.ModelType, &route.ModelStatus, &route.UpstreamModelID,
		&route.ProviderID, &route.ProviderKind, &route.ProviderStatus, &route.ProviderBaseURL, &route.ProviderTimeoutMS, &route.ProviderStreamUsageEnabled, &encryptedSecret,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return modelgateway.Route{}, modelgateway.ErrModelNotFound
	}
	if err != nil {
		return modelgateway.Route{}, err
	}
	route.ProviderAPIKey, err = r.cipher.Open(encryptedSecret)
	if err != nil {
		return modelgateway.Route{}, fmt.Errorf("解密模型供应商凭据失败: %w", err)
	}
	return route, nil
}

func (r *ModelGatewayRepository) RecordCall(ctx context.Context, input modelgateway.RecordCallInput) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	callID := uuid.NewString()
	_, err = tx.Exec(ctx, `
		INSERT INTO model_gateway_calls (
			id, organization_id, api_key_id, auth_type, user_id, model_catalog_entry_id, provider_connection_id,
			request_id, model_code, model_type, upstream_model_id, streaming, result, http_status, latency_ms,
			prompt_tokens, completion_tokens, total_tokens, error_code, source_ip, department_id, usage_reported
		) VALUES ($1, $2, NULLIF($3, '')::UUID, $4, NULLIF($5, '')::UUID, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, NULLIF($21, '')::UUID, $22)
	`, callID, input.OrganizationID, input.APIKeyID, input.AuthType, input.UserID, input.ModelID, input.ProviderID,
		input.RequestID, input.ModelCode, input.ModelType, input.UpstreamModelID, input.Streaming, input.Result,
		input.HTTPStatus, input.LatencyMS, input.PromptTokens, input.CompletionTokens,
		input.TotalTokens, input.ErrorCode, input.SourceIP, input.DepartmentID, input.UsageReported)
	if err != nil {
		return err
	}
	if err := insertModelCallLog(ctx, tx, callID, input); err != nil {
		return err
	}
	if input.AuthType == "api_key" {
		_, err = tx.Exec(ctx, `
		INSERT INTO model_gateway_monthly_usage (
			organization_id, api_key_id, usage_month, request_count, total_tokens
		) VALUES ($1, $2, DATE_TRUNC('month', NOW() AT TIME ZONE 'UTC')::DATE, 1, $3)
		ON CONFLICT (organization_id, api_key_id, usage_month) DO UPDATE
		SET request_count = model_gateway_monthly_usage.request_count + 1,
		    total_tokens = model_gateway_monthly_usage.total_tokens + EXCLUDED.total_tokens,
		    updated_at = NOW()
		`, input.OrganizationID, input.APIKeyID, input.TotalTokens)
		if err != nil {
			return err
		}
	} else if input.AuthType == "user" {
		_, err = tx.Exec(ctx, `INSERT INTO model_subject_monthly_usage (
			organization_id, subject_type, subject_id, usage_month, request_count, total_tokens
		) SELECT p.organization_id, p.subject_type, p.subject_id,
			DATE_TRUNC('month', NOW() AT TIME ZONE 'UTC')::DATE, 1, $3
		FROM model_subject_policies p
		WHERE p.organization_id = $1 AND p.subject_type = 'user' AND p.subject_id = $2
		ON CONFLICT (organization_id, subject_type, subject_id, usage_month) DO UPDATE
		SET request_count = model_subject_monthly_usage.request_count + 1,
			total_tokens = model_subject_monthly_usage.total_tokens + EXCLUDED.total_tokens,
			updated_at = NOW()`, input.OrganizationID, input.UserID, input.TotalTokens)
		if err != nil {
			return err
		}
		if input.DepartmentID != "" {
			_, err = tx.Exec(ctx, `INSERT INTO model_subject_monthly_usage (
				organization_id, subject_type, subject_id, usage_month, request_count, total_tokens
			) SELECT p.organization_id, p.subject_type, p.subject_id,
				DATE_TRUNC('month', NOW() AT TIME ZONE 'UTC')::DATE, 1, $3
			FROM model_subject_policies p
			WHERE p.organization_id = $1 AND p.subject_type = 'department' AND p.subject_id = $2
			ON CONFLICT (organization_id, subject_type, subject_id, usage_month) DO UPDATE
			SET request_count = model_subject_monthly_usage.request_count + 1,
				total_tokens = model_subject_monthly_usage.total_tokens + EXCLUDED.total_tokens,
				updated_at = NOW()`, input.OrganizationID, input.DepartmentID, input.TotalTokens)
			if err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `INSERT INTO model_department_user_monthly_usage (
				organization_id, subject_type, subject_id, user_id, usage_month, request_count, total_tokens
			) SELECT p.organization_id, p.subject_type, p.subject_id, $3,
				DATE_TRUNC('month', NOW() AT TIME ZONE 'UTC')::DATE, 1, $4
			FROM model_subject_policies p
			WHERE p.organization_id = $1 AND p.subject_type = 'department' AND p.subject_id = $2
			ON CONFLICT (organization_id, subject_id, user_id, usage_month) DO UPDATE
			SET request_count = model_department_user_monthly_usage.request_count + 1,
				total_tokens = model_department_user_monthly_usage.total_tokens + EXCLUDED.total_tokens,
				updated_at = NOW()`, input.OrganizationID, input.DepartmentID, input.UserID, input.TotalTokens)
			if err != nil {
				return err
			}
		}
	}
	var exceeded bool
	if input.AuthType == "api_key" {
		err = tx.QueryRow(ctx, `SELECT k.monthly_token_limit > 0 AND u.total_tokens > k.monthly_token_limit
			FROM model_gateway_api_keys k
			JOIN model_gateway_monthly_usage u ON u.organization_id = k.organization_id AND u.api_key_id = k.id
				AND u.usage_month = DATE_TRUNC('month', NOW() AT TIME ZONE 'UTC')::DATE
			WHERE k.organization_id = $1 AND k.id = $2`, input.OrganizationID, input.APIKeyID).Scan(&exceeded)
	} else {
		err = tx.QueryRow(ctx, `SELECT
			EXISTS (SELECT 1 FROM model_subject_policies p
				JOIN model_subject_monthly_usage u ON u.organization_id = p.organization_id
					AND u.subject_type = p.subject_type AND u.subject_id = p.subject_id
					AND u.usage_month = DATE_TRUNC('month', NOW() AT TIME ZONE 'UTC')::DATE
				WHERE p.organization_id = $1 AND p.subject_type = 'user' AND p.subject_id = $2
					AND p.monthly_token_limit > 0 AND u.total_tokens > p.monthly_token_limit)
			OR EXISTS (SELECT 1 FROM model_subject_policies p
				JOIN model_subject_monthly_usage u ON u.organization_id = p.organization_id
					AND u.subject_type = p.subject_type AND u.subject_id = p.subject_id
					AND u.usage_month = DATE_TRUNC('month', NOW() AT TIME ZONE 'UTC')::DATE
				WHERE p.organization_id = $1 AND p.subject_type = 'department' AND p.subject_id = NULLIF($3, '')::UUID
					AND p.monthly_token_mode = 'shared' AND p.monthly_token_limit > 0
					AND u.total_tokens > p.monthly_token_limit)
			OR EXISTS (SELECT 1 FROM model_subject_policies p
				JOIN model_department_user_monthly_usage u ON u.organization_id = p.organization_id
					AND u.subject_id = p.subject_id AND u.user_id = $2
					AND u.usage_month = DATE_TRUNC('month', NOW() AT TIME ZONE 'UTC')::DATE
				WHERE p.organization_id = $1 AND p.subject_type = 'department' AND p.subject_id = NULLIF($3, '')::UUID
					AND p.monthly_token_mode = 'per_user' AND p.monthly_token_limit > 0
					AND u.total_tokens > p.monthly_token_limit)`, input.OrganizationID, input.UserID, input.DepartmentID).Scan(&exceeded)
	}
	if err != nil {
		return err
	}
	if exceeded {
		if _, err := tx.Exec(ctx, `UPDATE model_gateway_calls SET quota_exceeded = TRUE
			WHERE organization_id = $1 AND id = $2`, input.OrganizationID, callID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func insertModelCallLog(ctx context.Context, tx pgx.Tx, callID string, input modelgateway.RecordCallInput) error {
	var stage, policy, requested, headers, reasoning, content, finished, tokens any
	if t := input.Timing; t != nil {
		stage, policy, requested, headers, reasoning, content, finished, tokens = t.Stage, t.ReasoningPolicy, t.RequestedAt, t.HeadersAt, t.FirstReasoningAt, t.FirstContentAt, t.FinishedAt, t.ReasoningTokens
	}
	_, err := tx.Exec(ctx, `INSERT INTO model_call_logs
  (call_id, organization_id, source, operation, request_body, response_body, response_truncated, error_message,
  workflow_stage,reasoning_policy,requested_at,headers_at,first_reasoning_at,first_content_at,finished_at,reasoning_tokens,segment_video_id)
  VALUES ($1,$2,$3,$4,COALESCE($5::bytea,''::bytea),COALESCE($6::bytea,''::bytea),$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,NULLIF($17,'')::uuid)`, callID, input.OrganizationID, input.Source,
		input.Operation, input.RequestBody, input.ResponseBody, input.ResponseTruncated, input.ErrorMessage, stage, policy, requested, headers, reasoning, content, finished, tokens, input.SegmentVideoID)
	if err != nil {
		return fmt.Errorf("保存模型调用正文失败: %w", err)
	}
	return nil
}

func (r *ModelGatewayRepository) ListCalls(ctx context.Context, query modelgateway.CallQuery) (modelgateway.CallPage, error) {
	const filters = `
		WHERE c.organization_id = $1
		  AND ($2 = '' OR c.api_key_id::TEXT = $2)
		  AND ($3 = '' OR LOWER(c.model_code) = LOWER($3))
		  AND ($4 = '' OR c.result = $4)
		  AND ($5::TIMESTAMPTZ IS NULL OR c.created_at >= $5)
		  AND ($6::TIMESTAMPTZ IS NULL OR c.created_at <= $6)
	`
	arguments := []any{query.OrganizationID, query.APIKeyID, query.ModelCode, query.Result, query.StartedAt, query.EndedAt}
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM model_gateway_calls c `+filters, arguments...).Scan(&total); err != nil {
		return modelgateway.CallPage{}, err
	}
	offset := (query.Page - 1) * query.PageSize
	rows, err := r.pool.Query(ctx, `
		SELECT c.id, COALESCE(c.api_key_id::TEXT, ''), COALESCE(k.name, ''),
		       c.auth_type, COALESCE(c.user_id::TEXT, ''), COALESCE(u.display_name, ''), COALESCE(c.department_id::TEXT, ''),
		       c.request_id, c.model_code, COALESCE(m.name, c.model_code), c.model_type, c.upstream_model_id,
		       COALESCE(p.name, ''),
		       c.streaming, c.result, c.http_status, c.latency_ms, c.prompt_tokens,
		       c.completion_tokens, c.total_tokens, c.usage_reported, c.quota_exceeded,
		       c.error_code, c.source_ip, c.created_at,
  COALESCE(l.workflow_stage,''),COALESCE(l.reasoning_policy,''),l.requested_at,l.headers_at,l.first_reasoning_at,l.first_content_at,l.finished_at,l.reasoning_tokens
		FROM model_gateway_calls c
  LEFT JOIN model_call_logs l ON l.call_id=c.id AND l.organization_id=c.organization_id
		LEFT JOIN model_gateway_api_keys k
		  ON k.organization_id = c.organization_id AND k.id = c.api_key_id
		LEFT JOIN users u
		  ON u.organization_id = c.organization_id AND u.id = c.user_id
		LEFT JOIN model_catalog_entries m
		  ON m.organization_id = c.organization_id AND m.id = c.model_catalog_entry_id
		LEFT JOIN model_provider_connections p
		  ON p.organization_id = c.organization_id AND p.id = c.provider_connection_id
		`+filters+`
		ORDER BY c.created_at DESC, c.id DESC
		LIMIT $7 OFFSET $8
	`, append(arguments, query.PageSize, offset)...)
	if err != nil {
		return modelgateway.CallPage{}, err
	}
	defer rows.Close()
	items := make([]modelgateway.Call, 0)
	for rows.Next() {
		var item modelgateway.Call
		timing := modelgateway.CallTiming{}
		var requested, finished *time.Time
		if err := rows.Scan(
			&item.ID, &item.APIKeyID, &item.APIKeyName, &item.AuthType, &item.UserID, &item.UserName, &item.DepartmentID,
			&item.RequestID, &item.ModelCode, &item.ModelName, &item.ModelType,
			&item.UpstreamModelID, &item.ProviderName, &item.Streaming, &item.Result, &item.HTTPStatus, &item.LatencyMS,
			&item.PromptTokens, &item.CompletionTokens, &item.TotalTokens, &item.UsageReported, &item.QuotaExceeded, &item.ErrorCode,
			&item.SourceIP, &item.CreatedAt,
			&timing.Stage, &timing.ReasoningPolicy, &requested, &timing.HeadersAt, &timing.FirstReasoningAt, &timing.FirstContentAt, &finished, &timing.ReasoningTokens,
		); err != nil {
			return modelgateway.CallPage{}, err
		}
		if requested != nil && finished != nil {
			timing.RequestedAt = *requested
			timing.FinishedAt = *finished
			item.Timing = &timing
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return modelgateway.CallPage{}, err
	}
	return modelgateway.CallPage{Items: items, Total: total, Page: query.Page, PageSize: query.PageSize}, nil
}

func (r *ModelGatewayRepository) ListUsage(ctx context.Context, query modelgateway.UsageQuery) (modelgateway.UsagePage, error) {
	month, err := time.Parse("2006-01", query.Month)
	if err != nil {
		return modelgateway.UsagePage{}, err
	}
	end := month.AddDate(0, 1, 0)
	filter := `WHERE c.organization_id = $1 AND c.created_at >= $2 AND c.created_at < $3`
	if query.GroupBy == "department" || query.GroupBy == "user" {
		filter += ` AND c.auth_type = 'user'`
	}
	page := modelgateway.UsagePage{
		Month: query.Month, GroupBy: query.GroupBy, Items: []modelgateway.UsageGroup{},
		Page: query.Page, PageSize: query.PageSize,
	}
	err = r.pool.QueryRow(ctx, `SELECT COUNT(*), COALESCE(SUM(c.total_tokens) FILTER (WHERE c.usage_reported), 0)::BIGINT,
		COUNT(*) FILTER (WHERE NOT c.usage_reported), COUNT(*) FILTER (WHERE c.quota_exceeded)
		FROM model_gateway_calls c `+filter, query.OrganizationID, month, end).Scan(
		&page.CallCount, &page.KnownTokens, &page.UnknownUsageCalls, &page.OverQuotaCalls)
	if err != nil {
		return modelgateway.UsagePage{}, err
	}
	var groupID, groupName, joins string
	switch query.GroupBy {
	case "department":
		groupID = `COALESCE(c.department_id::TEXT, '')`
		groupName = `COALESCE(MAX(d.name), '未记录部门')`
		joins = `LEFT JOIN departments d ON d.organization_id = c.organization_id AND d.id = c.department_id`
	case "user":
		groupID = `COALESCE(c.user_id::TEXT, '')`
		groupName = `COALESCE(MAX(u.display_name), '已删除用户')`
		joins = `LEFT JOIN users u ON u.organization_id = c.organization_id AND u.id = c.user_id`
	case "model":
		groupID = `c.model_code`
		groupName = `COALESCE(MAX(m.name), MAX(c.model_code))`
		joins = `LEFT JOIN model_catalog_entries m ON m.organization_id = c.organization_id AND LOWER(m.code) = LOWER(c.model_code)`
	default:
		return modelgateway.UsagePage{}, modelgateway.ErrInvalidInput
	}
	source := ` FROM model_gateway_calls c ` + joins + ` ` + filter
	err = r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM (SELECT `+groupID+source+` GROUP BY 1) groups`,
		query.OrganizationID, month, end).Scan(&page.Total)
	if err != nil {
		return modelgateway.UsagePage{}, err
	}
	rows, err := r.pool.Query(ctx, `SELECT `+groupID+`, `+groupName+`, COUNT(*),
		COALESCE(SUM(c.total_tokens) FILTER (WHERE c.usage_reported), 0)::BIGINT,
		COUNT(*) FILTER (WHERE NOT c.usage_reported), COUNT(*) FILTER (WHERE c.quota_exceeded)`+source+`
		GROUP BY 1 ORDER BY 4 DESC, 3 DESC, 2 ASC LIMIT $4 OFFSET $5`,
		query.OrganizationID, month, end, query.PageSize, (query.Page-1)*query.PageSize)
	if err != nil {
		return modelgateway.UsagePage{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var item modelgateway.UsageGroup
		if err := rows.Scan(&item.ID, &item.Name, &item.CallCount, &item.KnownTokens,
			&item.UnknownUsageCalls, &item.OverQuotaCalls); err != nil {
			return modelgateway.UsagePage{}, err
		}
		page.Items = append(page.Items, item)
	}
	return page, rows.Err()
}

func findGatewayAPIKey(ctx context.Context, queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, organizationID, apiKeyID string) (modelgateway.APIKey, error) {
	item, err := scanGatewayAPIKey(queryer.QueryRow(ctx, `
		SELECT `+gatewayAPIKeyColumns+`
		FROM model_gateway_api_keys k
		WHERE k.organization_id = $1 AND k.id = $2
	`, organizationID, apiKeyID))
	if errors.Is(err, pgx.ErrNoRows) {
		return modelgateway.APIKey{}, modelgateway.ErrAPIKeyNotFound
	}
	return item, err
}

func scanGatewayAPIKey(row interface{ Scan(...any) error }) (modelgateway.APIKey, error) {
	var item modelgateway.APIKey
	var allowedModelsJSON []byte
	err := row.Scan(
		&item.ID, &item.OrganizationID, &item.Name, &item.KeyPrefix, &item.Status,
		&item.ExpiresAt, &item.LastUsedAt, &item.RequestsPerMinute, &item.MonthlyTokenLimit,
		&item.MonthlyTokenUsed, &item.AllowedModelCodes, &allowedModelsJSON, &item.CreatedAt, &item.RevokedAt,
	)
	if err != nil {
		return modelgateway.APIKey{}, err
	}
	if err := json.Unmarshal(allowedModelsJSON, &item.AllowedModels); err != nil {
		return modelgateway.APIKey{}, fmt.Errorf("解析调用密钥可用模型名称失败: %w", err)
	}
	return item, nil
}
