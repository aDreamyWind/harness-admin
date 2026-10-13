package postgres

import (
	"context"
	"errors"
	"fmt"

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
	k.last_used_at, k.created_at, k.revoked_at
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
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return modelgateway.APIKey{}, err
	}
	defer tx.Rollback(ctx)
	apiKeyID := uuid.NewString()
	_, err = tx.Exec(ctx, `
		INSERT INTO model_gateway_api_keys (
			id, organization_id, name, key_prefix, secret_hash, expires_at, created_by
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, apiKeyID, input.OrganizationID, input.Name, input.KeyPrefix, input.SecretHash, input.ExpiresAt, input.ActorID)
	if err != nil {
		return modelgateway.APIKey{}, err
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{
		OrganizationID: input.OrganizationID, ActorID: input.ActorID, RequestID: input.RequestID,
		Action: "model.gateway_api_key.create", ResourceType: "model_gateway_api_key", ResourceID: apiKeyID,
		Result: "success", SourceIP: input.SourceIP,
		Detail: map[string]any{"name": input.Name, "keyPrefix": input.KeyPrefix, "expiresAt": input.ExpiresAt},
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
		SELECT m.id, m.code, m.status, m.upstream_model_id,
		       p.id, p.status, p.base_url, p.request_timeout_ms, c.encrypted_secret
		FROM model_catalog_entries m
		JOIN model_provider_connections p
		  ON p.organization_id = m.organization_id AND p.id = m.provider_connection_id
		JOIN model_provider_credentials c ON c.provider_connection_id = p.id
		WHERE m.organization_id = $1 AND LOWER(m.code) = LOWER($2)
	`, organizationID, modelCode).Scan(
		&route.ModelID, &route.ModelCode, &route.ModelStatus, &route.UpstreamModelID,
		&route.ProviderID, &route.ProviderStatus, &route.ProviderBaseURL, &route.ProviderTimeoutMS, &encryptedSecret,
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
	_, err := r.pool.Exec(ctx, `
		INSERT INTO model_gateway_calls (
			id, organization_id, api_key_id, model_catalog_entry_id, provider_connection_id,
			request_id, model_code, upstream_model_id, streaming, result, http_status, latency_ms,
			prompt_tokens, completion_tokens, total_tokens, error_code, source_ip
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
	`, uuid.NewString(), input.OrganizationID, input.APIKeyID, input.ModelID, input.ProviderID,
		input.RequestID, input.ModelCode, input.UpstreamModelID, input.Streaming, input.Result,
		input.HTTPStatus, input.LatencyMS, input.PromptTokens, input.CompletionTokens,
		input.TotalTokens, input.ErrorCode, input.SourceIP)
	return err
}

func (r *ModelGatewayRepository) ListCalls(ctx context.Context, organizationID string, limit int) ([]modelgateway.Call, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT c.id, c.api_key_id, k.name, c.request_id, c.model_code, c.upstream_model_id,
		       c.streaming, c.result, c.http_status, c.latency_ms, c.prompt_tokens,
		       c.completion_tokens, c.total_tokens, c.error_code, c.source_ip, c.created_at
		FROM model_gateway_calls c
		JOIN model_gateway_api_keys k
		  ON k.organization_id = c.organization_id AND k.id = c.api_key_id
		WHERE c.organization_id = $1
		ORDER BY c.created_at DESC
		LIMIT $2
	`, organizationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]modelgateway.Call, 0)
	for rows.Next() {
		var item modelgateway.Call
		if err := rows.Scan(
			&item.ID, &item.APIKeyID, &item.APIKeyName, &item.RequestID, &item.ModelCode,
			&item.UpstreamModelID, &item.Streaming, &item.Result, &item.HTTPStatus, &item.LatencyMS,
			&item.PromptTokens, &item.CompletionTokens, &item.TotalTokens, &item.ErrorCode,
			&item.SourceIP, &item.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
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
	err := row.Scan(
		&item.ID, &item.OrganizationID, &item.Name, &item.KeyPrefix, &item.Status,
		&item.ExpiresAt, &item.LastUsedAt, &item.CreatedAt, &item.RevokedAt,
	)
	return item, err
}
