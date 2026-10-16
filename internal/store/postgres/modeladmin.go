package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"harness-admin/internal/identity"
	"harness-admin/internal/modeladmin"
	"harness-admin/internal/secret"
)

type ModelAdminRepository struct {
	pool   *pgxpool.Pool
	cipher *secret.Cipher
}

func NewModelAdminRepository(pool *pgxpool.Pool, cipher *secret.Cipher) *ModelAdminRepository {
	return &ModelAdminRepository{pool: pool, cipher: cipher}
}

const providerColumns = `
	p.id, p.code, p.name, p.provider_kind, p.base_url, p.status, p.request_timeout_ms,
	EXISTS (SELECT 1 FROM model_provider_credentials c WHERE c.provider_connection_id = p.id),
	p.last_probe_status, p.last_probe_at, p.created_at, p.updated_at
`

func (r *ModelAdminRepository) ListProviders(ctx context.Context, organizationID string) ([]modeladmin.Provider, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+providerColumns+`
		FROM model_provider_connections p
		WHERE p.organization_id = $1
		ORDER BY p.created_at, p.name
	`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]modeladmin.Provider, 0)
	for rows.Next() {
		item, err := scanProvider(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *ModelAdminRepository) CreateProvider(ctx context.Context, input modeladmin.CreateProviderInput) (modeladmin.Provider, error) {
	ciphertext, err := r.cipher.Seal(input.APIKey)
	if err != nil {
		return modeladmin.Provider{}, err
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return modeladmin.Provider{}, err
	}
	defer tx.Rollback(ctx)
	providerID := uuid.NewString()
	_, err = tx.Exec(ctx, `
		INSERT INTO model_provider_connections (
			id, organization_id, code, name, provider_kind, base_url, request_timeout_ms
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, providerID, input.OrganizationID, input.Code, input.Name, input.ProviderKind, input.BaseURL, input.RequestTimeoutMS)
	if err != nil {
		return modeladmin.Provider{}, translateProviderWriteError(err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO model_provider_credentials (provider_connection_id, encrypted_secret)
		VALUES ($1, $2)
	`, providerID, ciphertext); err != nil {
		return modeladmin.Provider{}, err
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{
		OrganizationID: input.OrganizationID, ActorID: input.ActorID, RequestID: input.RequestID,
		Action: "model.provider.create", ResourceType: "model_provider", ResourceID: providerID,
		Result: "success", SourceIP: input.SourceIP,
		Detail: map[string]any{"code": input.Code, "name": input.Name, "providerKind": input.ProviderKind, "baseUrl": input.BaseURL},
	}); err != nil {
		return modeladmin.Provider{}, err
	}
	provider, err := findProvider(ctx, tx, input.OrganizationID, providerID)
	if err != nil {
		return modeladmin.Provider{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return modeladmin.Provider{}, err
	}
	return provider, nil
}

func (r *ModelAdminRepository) UpdateProvider(ctx context.Context, input modeladmin.UpdateProviderInput) (modeladmin.Provider, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return modeladmin.Provider{}, err
	}
	defer tx.Rollback(ctx)
	command, err := tx.Exec(ctx, `
		UPDATE model_provider_connections
		SET name = $1, provider_kind = $2, base_url = $3, request_timeout_ms = $4, updated_at = NOW()
		WHERE id = $5 AND organization_id = $6
	`, input.Name, input.ProviderKind, input.BaseURL, input.RequestTimeoutMS, input.ProviderID, input.OrganizationID)
	if err != nil {
		return modeladmin.Provider{}, err
	}
	if command.RowsAffected() == 0 {
		return modeladmin.Provider{}, modeladmin.ErrProviderNotFound
	}
	credentialRotated := input.APIKey != nil
	if credentialRotated {
		ciphertext, err := r.cipher.Seal(*input.APIKey)
		if err != nil {
			return modeladmin.Provider{}, err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO model_provider_credentials (provider_connection_id, encrypted_secret, updated_at)
			VALUES ($1, $2, NOW())
			ON CONFLICT (provider_connection_id) DO UPDATE
			SET encrypted_secret = EXCLUDED.encrypted_secret, key_version = model_provider_credentials.key_version + 1, updated_at = NOW()
		`, input.ProviderID, ciphertext); err != nil {
			return modeladmin.Provider{}, err
		}
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{
		OrganizationID: input.OrganizationID, ActorID: input.ActorID, RequestID: input.RequestID,
		Action: "model.provider.update", ResourceType: "model_provider", ResourceID: input.ProviderID,
		Result: "success", SourceIP: input.SourceIP,
		Detail: map[string]any{"name": input.Name, "providerKind": input.ProviderKind, "baseUrl": input.BaseURL, "credentialRotated": credentialRotated},
	}); err != nil {
		return modeladmin.Provider{}, err
	}
	provider, err := findProvider(ctx, tx, input.OrganizationID, input.ProviderID)
	if err != nil {
		return modeladmin.Provider{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return modeladmin.Provider{}, err
	}
	return provider, nil
}

func (r *ModelAdminRepository) ChangeProviderStatus(ctx context.Context, input modeladmin.ChangeProviderStatusInput) (modeladmin.Provider, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return modeladmin.Provider{}, err
	}
	defer tx.Rollback(ctx)
	command, err := tx.Exec(ctx, `
		UPDATE model_provider_connections SET status = $1, updated_at = NOW()
		WHERE id = $2 AND organization_id = $3
	`, input.Status, input.ProviderID, input.OrganizationID)
	if err != nil {
		return modeladmin.Provider{}, err
	}
	if command.RowsAffected() == 0 {
		return modeladmin.Provider{}, modeladmin.ErrProviderNotFound
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{
		OrganizationID: input.OrganizationID, ActorID: input.ActorID, RequestID: input.RequestID,
		Action: "model.provider.status_change", ResourceType: "model_provider", ResourceID: input.ProviderID,
		Result: "success", SourceIP: input.SourceIP, Detail: map[string]any{"status": input.Status},
	}); err != nil {
		return modeladmin.Provider{}, err
	}
	provider, err := findProvider(ctx, tx, input.OrganizationID, input.ProviderID)
	if err != nil {
		return modeladmin.Provider{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return modeladmin.Provider{}, err
	}
	return provider, nil
}

func (r *ModelAdminRepository) GetProviderCredential(ctx context.Context, organizationID, providerID string) (modeladmin.ProviderCredential, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT `+providerColumns+`, c.encrypted_secret
		FROM model_provider_connections p
		JOIN model_provider_credentials c ON c.provider_connection_id = p.id
		WHERE p.organization_id = $1 AND p.id = $2
	`, organizationID, providerID)
	var credential modeladmin.ProviderCredential
	var ciphertext []byte
	err := row.Scan(
		&credential.Provider.ID, &credential.Provider.Code, &credential.Provider.Name, &credential.Provider.ProviderKind,
		&credential.Provider.BaseURL, &credential.Provider.Status, &credential.Provider.RequestTimeoutMS,
		&credential.Provider.CredentialConfigured, &credential.Provider.LastProbeStatus, &credential.Provider.LastProbeAt,
		&credential.Provider.CreatedAt, &credential.Provider.UpdatedAt, &ciphertext,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return modeladmin.ProviderCredential{}, modeladmin.ErrProviderNotFound
	}
	if err != nil {
		return modeladmin.ProviderCredential{}, err
	}
	credential.APIKey, err = r.cipher.Open(ciphertext)
	if err != nil {
		return modeladmin.ProviderCredential{}, err
	}
	return credential, nil
}

func (r *ModelAdminRepository) RevealProviderCredential(ctx context.Context, input modeladmin.RevealProviderCredentialInput) (string, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	var ciphertext []byte
	err = tx.QueryRow(ctx, `
		SELECT c.encrypted_secret
		FROM model_provider_connections p
		JOIN model_provider_credentials c ON c.provider_connection_id = p.id
		WHERE p.organization_id = $1 AND p.id = $2
	`, input.OrganizationID, input.ProviderID).Scan(&ciphertext)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", modeladmin.ErrProviderNotFound
	}
	if err != nil {
		return "", err
	}
	value, err := r.cipher.Open(ciphertext)
	if err != nil {
		return "", err
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{
		OrganizationID: input.OrganizationID, ActorID: input.ActorID, RequestID: input.RequestID,
		Action: "model.provider.credential.reveal", ResourceType: "model_provider", ResourceID: input.ProviderID,
		Result: "success", SourceIP: input.SourceIP,
	}); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return value, nil
}

func (r *ModelAdminRepository) RecordProviderProbe(ctx context.Context, input modeladmin.ProbeInput, result modeladmin.ProbeResult, errorCode string) (modeladmin.Provider, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return modeladmin.Provider{}, err
	}
	defer tx.Rollback(ctx)
	command, err := tx.Exec(ctx, `
		UPDATE model_provider_connections
		SET last_probe_status = $1, last_probe_at = $2, updated_at = NOW()
		WHERE id = $3 AND organization_id = $4
	`, result.Status, result.CheckedAt, input.ProviderID, input.OrganizationID)
	if err != nil {
		return modeladmin.Provider{}, err
	}
	if command.RowsAffected() == 0 {
		return modeladmin.Provider{}, modeladmin.ErrProviderNotFound
	}
	auditResult := "success"
	if result.Status == "failed" {
		auditResult = "failed"
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{
		OrganizationID: input.OrganizationID, ActorID: input.ActorID, RequestID: input.RequestID,
		Action: "model.provider.probe", ResourceType: "model_provider", ResourceID: input.ProviderID,
		Result: auditResult, SourceIP: input.SourceIP,
		Detail: map[string]any{"status": result.Status, "latencyMs": result.LatencyMS, "errorCode": errorCode},
	}); err != nil {
		return modeladmin.Provider{}, err
	}
	provider, err := findProvider(ctx, tx, input.OrganizationID, input.ProviderID)
	if err != nil {
		return modeladmin.Provider{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return modeladmin.Provider{}, err
	}
	return provider, nil
}

const modelColumns = `
	m.id, m.provider_connection_id, p.name, m.code, m.upstream_model_id, m.name, m.model_type,
	m.capabilities, m.context_window_tokens, m.max_output_tokens, m.status, m.created_at, m.updated_at
`

func (r *ModelAdminRepository) ListModels(ctx context.Context, organizationID string) ([]modeladmin.CatalogModel, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+modelColumns+`
		FROM model_catalog_entries m
		JOIN model_provider_connections p ON p.id = m.provider_connection_id
		WHERE m.organization_id = $1
		ORDER BY m.created_at, m.name
	`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]modeladmin.CatalogModel, 0)
	for rows.Next() {
		item, err := scanModel(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *ModelAdminRepository) CreateModel(ctx context.Context, input modeladmin.CreateModelInput) (modeladmin.CatalogModel, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return modeladmin.CatalogModel{}, err
	}
	defer tx.Rollback(ctx)
	if err := ensureProviderOwnership(ctx, tx, input.OrganizationID, input.ProviderConnectionID); err != nil {
		return modeladmin.CatalogModel{}, err
	}
	modelID := uuid.NewString()
	_, err = tx.Exec(ctx, `
		INSERT INTO model_catalog_entries (
			id, organization_id, provider_connection_id, code, upstream_model_id, name,
			model_type, capabilities, context_window_tokens, max_output_tokens
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, modelID, input.OrganizationID, input.ProviderConnectionID, input.Code, input.UpstreamModelID,
		input.Name, input.ModelType, input.Capabilities, input.ContextWindowTokens, input.MaxOutputTokens)
	if err != nil {
		return modeladmin.CatalogModel{}, translateModelWriteError(err)
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{
		OrganizationID: input.OrganizationID, ActorID: input.ActorID, RequestID: input.RequestID,
		Action: "model.catalog.create", ResourceType: "model", ResourceID: modelID,
		Result: "success", SourceIP: input.SourceIP,
		Detail: map[string]any{"code": input.Code, "name": input.Name, "providerConnectionId": input.ProviderConnectionID, "upstreamModelId": input.UpstreamModelID},
	}); err != nil {
		return modeladmin.CatalogModel{}, err
	}
	model, err := findModel(ctx, tx, input.OrganizationID, modelID)
	if err != nil {
		return modeladmin.CatalogModel{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return modeladmin.CatalogModel{}, err
	}
	return model, nil
}

func (r *ModelAdminRepository) UpdateModel(ctx context.Context, input modeladmin.UpdateModelInput) (modeladmin.CatalogModel, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return modeladmin.CatalogModel{}, err
	}
	defer tx.Rollback(ctx)
	if err := ensureProviderOwnership(ctx, tx, input.OrganizationID, input.ProviderConnectionID); err != nil {
		return modeladmin.CatalogModel{}, err
	}
	command, err := tx.Exec(ctx, `
		UPDATE model_catalog_entries
		SET provider_connection_id = $1, upstream_model_id = $2, name = $3, model_type = $4,
		    capabilities = $5, context_window_tokens = $6, max_output_tokens = $7, updated_at = NOW()
		WHERE id = $8 AND organization_id = $9
	`, input.ProviderConnectionID, input.UpstreamModelID, input.Name, input.ModelType, input.Capabilities,
		input.ContextWindowTokens, input.MaxOutputTokens, input.ModelID, input.OrganizationID)
	if err != nil {
		return modeladmin.CatalogModel{}, translateModelWriteError(err)
	}
	if command.RowsAffected() == 0 {
		return modeladmin.CatalogModel{}, modeladmin.ErrModelNotFound
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{
		OrganizationID: input.OrganizationID, ActorID: input.ActorID, RequestID: input.RequestID,
		Action: "model.catalog.update", ResourceType: "model", ResourceID: input.ModelID,
		Result: "success", SourceIP: input.SourceIP,
		Detail: map[string]any{"name": input.Name, "providerConnectionId": input.ProviderConnectionID, "upstreamModelId": input.UpstreamModelID},
	}); err != nil {
		return modeladmin.CatalogModel{}, err
	}
	model, err := findModel(ctx, tx, input.OrganizationID, input.ModelID)
	if err != nil {
		return modeladmin.CatalogModel{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return modeladmin.CatalogModel{}, err
	}
	return model, nil
}

func (r *ModelAdminRepository) ChangeModelStatus(ctx context.Context, input modeladmin.ChangeModelStatusInput) (modeladmin.CatalogModel, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return modeladmin.CatalogModel{}, err
	}
	defer tx.Rollback(ctx)
	command, err := tx.Exec(ctx, `
		UPDATE model_catalog_entries SET status = $1, updated_at = NOW()
		WHERE id = $2 AND organization_id = $3
	`, input.Status, input.ModelID, input.OrganizationID)
	if err != nil {
		return modeladmin.CatalogModel{}, err
	}
	if command.RowsAffected() == 0 {
		return modeladmin.CatalogModel{}, modeladmin.ErrModelNotFound
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{
		OrganizationID: input.OrganizationID, ActorID: input.ActorID, RequestID: input.RequestID,
		Action: "model.catalog.status_change", ResourceType: "model", ResourceID: input.ModelID,
		Result: "success", SourceIP: input.SourceIP, Detail: map[string]any{"status": input.Status},
	}); err != nil {
		return modeladmin.CatalogModel{}, err
	}
	model, err := findModel(ctx, tx, input.OrganizationID, input.ModelID)
	if err != nil {
		return modeladmin.CatalogModel{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return modeladmin.CatalogModel{}, err
	}
	return model, nil
}

func findProvider(ctx context.Context, queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, organizationID, providerID string) (modeladmin.Provider, error) {
	provider, err := scanProvider(queryer.QueryRow(ctx, `
		SELECT `+providerColumns+` FROM model_provider_connections p
		WHERE p.organization_id = $1 AND p.id = $2
	`, organizationID, providerID))
	if errors.Is(err, pgx.ErrNoRows) {
		return modeladmin.Provider{}, modeladmin.ErrProviderNotFound
	}
	return provider, err
}

func findModel(ctx context.Context, queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, organizationID, modelID string) (modeladmin.CatalogModel, error) {
	model, err := scanModel(queryer.QueryRow(ctx, `
		SELECT `+modelColumns+`
		FROM model_catalog_entries m
		JOIN model_provider_connections p ON p.id = m.provider_connection_id
		WHERE m.organization_id = $1 AND m.id = $2
	`, organizationID, modelID))
	if errors.Is(err, pgx.ErrNoRows) {
		return modeladmin.CatalogModel{}, modeladmin.ErrModelNotFound
	}
	return model, err
}

func ensureProviderOwnership(ctx context.Context, queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, organizationID, providerID string) error {
	var found string
	err := queryer.QueryRow(ctx, `SELECT id FROM model_provider_connections WHERE organization_id = $1 AND id = $2`, organizationID, providerID).Scan(&found)
	if errors.Is(err, pgx.ErrNoRows) {
		return modeladmin.ErrProviderNotFound
	}
	return err
}

func scanProvider(row rowScanner) (modeladmin.Provider, error) {
	var provider modeladmin.Provider
	err := row.Scan(&provider.ID, &provider.Code, &provider.Name, &provider.ProviderKind, &provider.BaseURL,
		&provider.Status, &provider.RequestTimeoutMS, &provider.CredentialConfigured, &provider.LastProbeStatus,
		&provider.LastProbeAt, &provider.CreatedAt, &provider.UpdatedAt)
	return provider, err
}

func scanModel(row rowScanner) (modeladmin.CatalogModel, error) {
	var model modeladmin.CatalogModel
	err := row.Scan(&model.ID, &model.ProviderConnectionID, &model.ProviderName, &model.Code,
		&model.UpstreamModelID, &model.Name, &model.ModelType, &model.Capabilities,
		&model.ContextWindowTokens, &model.MaxOutputTokens, &model.Status, &model.CreatedAt, &model.UpdatedAt)
	return model, err
}

func translateProviderWriteError(err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" {
		return modeladmin.ErrProviderConflict
	}
	return fmt.Errorf("写入模型供应商连接失败: %w", err)
}

func translateModelWriteError(err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" {
		return modeladmin.ErrModelConflict
	}
	return fmt.Errorf("写入模型目录失败: %w", err)
}
