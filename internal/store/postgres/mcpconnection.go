package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"harness-admin/internal/identity"
	"harness-admin/internal/mcpadmin"
	"harness-admin/internal/mcpconnection"
	"harness-admin/internal/secret"
)

type MCPConnectionRepository struct {
	pool   *pgxpool.Pool
	cipher *secret.Cipher
}

func NewMCPConnectionRepository(pool *pgxpool.Pool, cipher *secret.Cipher) *MCPConnectionRepository {
	return &MCPConnectionRepository{pool: pool, cipher: cipher}
}

const mcpConnectionColumns = `
	c.id, c.service_id, s.name, c.code, c.name, c.purpose, c.auth_mode, COALESCE(c.application_name, ''),
	c.status, c.expires_at, COALESCE(c.key_prefix, ''), c.upstream_authorization_enc IS NOT NULL,
	c.upstream_header_value_enc IS NOT NULL, c.upstream_query_value_enc IS NOT NULL,
	c.upstream_parameters_enc IS NOT NULL OR c.upstream_authorization_enc IS NOT NULL OR
		c.upstream_header_value_enc IS NOT NULL OR c.upstream_query_value_enc IS NOT NULL,
	c.requests_per_minute,
	ARRAY(SELECT g.department_id::text FROM mcp_connection_grants g WHERE g.connection_id = c.id AND g.department_id IS NOT NULL ORDER BY g.department_id),
	ARRAY(SELECT g.user_id::text FROM mcp_connection_grants g WHERE g.connection_id = c.id AND g.user_id IS NOT NULL ORDER BY g.user_id),
	c.created_at, c.updated_at
`

func scanMCPConnection(row pgx.Row) (mcpconnection.Connection, error) {
	var item mcpconnection.Connection
	err := row.Scan(&item.ID, &item.ServiceID, &item.ServiceName, &item.Code, &item.Name, &item.Purpose,
		&item.AuthMode, &item.ApplicationName, &item.Status, &item.ExpiresAt, &item.KeyPrefix,
		&item.UpstreamAuthConfigured, &item.UpstreamHeaderConfigured, &item.UpstreamQueryConfigured,
		&item.UpstreamConfigured,
		&item.RequestsPerMinute, &item.DepartmentIDs, &item.UserIDs,
		&item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return mcpconnection.Connection{}, mcpconnection.ErrNotFound
	}
	return item, err
}

func (r *MCPConnectionRepository) List(ctx context.Context, organizationID string) ([]mcpconnection.Connection, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+mcpConnectionColumns+`
		FROM mcp_connections c JOIN mcp_services s ON s.id = c.service_id
		WHERE c.organization_id = $1 ORDER BY c.created_at DESC`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]mcpconnection.Connection, 0)
	for rows.Next() {
		item, err := scanMCPConnection(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *MCPConnectionRepository) Create(ctx context.Context, input mcpconnection.CreateInput) (mcpconnection.Connection, error) {
	var encryptedSecret, encryptedUpstream, encryptedHeader, encryptedQuery, encryptedParameters []byte
	var err error
	if input.Secret != "" {
		encryptedSecret, err = r.cipher.Seal(input.Secret)
		if err != nil {
			return mcpconnection.Connection{}, err
		}
	}
	if input.UpstreamAuthorization != "" {
		encryptedUpstream, err = r.cipher.Seal(input.UpstreamAuthorization)
		if err != nil {
			return mcpconnection.Connection{}, err
		}
	}
	if input.UpstreamHeaderValue != "" {
		encryptedHeader, err = r.cipher.Seal(input.UpstreamHeaderValue)
		if err != nil {
			return mcpconnection.Connection{}, err
		}
	}
	if input.UpstreamQueryValue != "" {
		encryptedQuery, err = r.cipher.Seal(input.UpstreamQueryValue)
		if err != nil {
			return mcpconnection.Connection{}, err
		}
	}
	if len(input.UpstreamParameters) > 0 {
		encoded, err := json.Marshal(input.UpstreamParameters)
		if err != nil {
			return mcpconnection.Connection{}, err
		}
		encryptedParameters, err = r.cipher.Seal(string(encoded))
		if err != nil {
			return mcpconnection.Connection{}, err
		}
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return mcpconnection.Connection{}, err
	}
	defer tx.Rollback(ctx)
	var serviceStatus string
	var parametersJSON []byte
	err = tx.QueryRow(ctx, `SELECT status, parameters FROM mcp_services WHERE id = $1 AND organization_id = $2 FOR UPDATE`,
		input.ServiceID, input.OrganizationID).Scan(&serviceStatus, &parametersJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return mcpconnection.Connection{}, mcpadmin.ErrNotFound
	}
	if err != nil {
		return mcpconnection.Connection{}, err
	}
	if serviceStatus != "active" {
		return mcpconnection.Connection{}, mcpadmin.ErrDisabled
	}
	var definitions []mcpadmin.ParameterDefinition
	if err := json.Unmarshal(parametersJSON, &definitions); err != nil {
		return mcpconnection.Connection{}, err
	}
	connectionCredentials := mcpadmin.UpstreamCredentials{
		Authorization: input.UpstreamAuthorization, HeaderName: input.UpstreamHeaderName,
		HeaderValue: input.UpstreamHeaderValue, QueryName: input.UpstreamQueryName,
		QueryValue: input.UpstreamQueryValue, Parameters: input.UpstreamParameters,
	}
	if !mcpadmin.HasRequiredParameters(definitions, connectionCredentials) {
		return mcpconnection.Connection{}, mcpconnection.ErrInvalidInput
	}
	id := uuid.NewString()
	_, err = tx.Exec(ctx, `INSERT INTO mcp_connections
		(id, organization_id, service_id, code, name, purpose, auth_mode, application_name, expires_at,
		 key_prefix, secret_hash, encrypted_secret, upstream_authorization_enc,
		 upstream_header_name, upstream_header_value_enc, upstream_query_name, upstream_query_value_enc,
		 upstream_parameters_enc, requests_per_minute, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)`,
		id, input.OrganizationID, input.ServiceID, input.Code, input.Name, input.Purpose, input.AuthMode,
		mcpNullableString(input.ApplicationName), input.ExpiresAt, mcpNullableString(input.KeyPrefix),
		nullableBytes(input.SecretHash), nullableBytes(encryptedSecret), nullableBytes(encryptedUpstream),
		mcpNullableString(input.UpstreamHeaderName), nullableBytes(encryptedHeader),
		mcpNullableString(input.UpstreamQueryName), nullableBytes(encryptedQuery), nullableBytes(encryptedParameters),
		input.RequestsPerMinute, input.UserID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return mcpconnection.Connection{}, mcpconnection.ErrConflict
		}
		return mcpconnection.Connection{}, err
	}
	if err := insertMCPGrants(ctx, tx, id, input.OrganizationID, input.DepartmentIDs, input.UserIDs); err != nil {
		return mcpconnection.Connection{}, err
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{
		OrganizationID: input.OrganizationID, ActorID: input.UserID, RequestID: input.RequestID,
		Action: "mcp.connection.create", ResourceType: "mcp_connection", ResourceID: id,
		Result: "success", SourceIP: input.SourceIP,
		Detail: map[string]any{"code": input.Code, "purpose": input.Purpose, "authMode": input.AuthMode,
			"departmentCount": len(input.DepartmentIDs), "userCount": len(input.UserIDs)},
	}); err != nil {
		return mcpconnection.Connection{}, err
	}
	item, err := getMCPConnection(ctx, tx, input.OrganizationID, id)
	if err != nil {
		return mcpconnection.Connection{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return mcpconnection.Connection{}, err
	}
	return item, nil
}

func insertMCPGrants(ctx context.Context, tx pgx.Tx, id, organizationID string, departmentIDs, userIDs []string) error {
	for _, departmentID := range departmentIDs {
		tag, err := tx.Exec(ctx, `INSERT INTO mcp_connection_grants (id, connection_id, department_id)
			SELECT $1,$2,d.id FROM departments d WHERE d.id = $3 AND d.organization_id = $4 AND d.status = 'active'
			ON CONFLICT DO NOTHING`, uuid.NewString(), id, departmentID, organizationID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return mcpconnection.ErrInvalidInput
		}
	}
	for _, userID := range userIDs {
		tag, err := tx.Exec(ctx, `INSERT INTO mcp_connection_grants (id, connection_id, user_id)
			SELECT $1,$2,u.id FROM users u WHERE u.id = $3 AND u.organization_id = $4 AND u.status = 'active'
			ON CONFLICT DO NOTHING`, uuid.NewString(), id, userID, organizationID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return mcpconnection.ErrInvalidInput
		}
	}
	return nil
}

func (r *MCPConnectionRepository) ReplaceGrants(ctx context.Context, input mcpconnection.GrantsInput) (mcpconnection.Connection, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return mcpconnection.Connection{}, err
	}
	defer tx.Rollback(ctx)
	var purpose string
	err = tx.QueryRow(ctx, `SELECT purpose FROM mcp_connections WHERE id = $1 AND organization_id = $2 FOR UPDATE`,
		input.ConnectionID, input.OrganizationID).Scan(&purpose)
	if errors.Is(err, pgx.ErrNoRows) {
		return mcpconnection.Connection{}, mcpconnection.ErrNotFound
	}
	if err != nil {
		return mcpconnection.Connection{}, err
	}
	if purpose != "harness" {
		return mcpconnection.Connection{}, mcpconnection.ErrInvalidInput
	}
	if _, err := tx.Exec(ctx, `DELETE FROM mcp_connection_grants WHERE connection_id = $1`, input.ConnectionID); err != nil {
		return mcpconnection.Connection{}, err
	}
	if err := insertMCPGrants(ctx, tx, input.ConnectionID, input.OrganizationID, input.DepartmentIDs, input.UserIDs); err != nil {
		return mcpconnection.Connection{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE mcp_connections SET updated_at = NOW() WHERE id = $1`, input.ConnectionID); err != nil {
		return mcpconnection.Connection{}, err
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{
		OrganizationID: input.OrganizationID, ActorID: input.UserID, RequestID: input.RequestID,
		Action: "mcp.connection.grants.replace", ResourceType: "mcp_connection", ResourceID: input.ConnectionID,
		Result: "success", SourceIP: input.SourceIP,
		Detail: map[string]any{"departmentCount": len(input.DepartmentIDs), "userCount": len(input.UserIDs)},
	}); err != nil {
		return mcpconnection.Connection{}, err
	}
	item, err := getMCPConnection(ctx, tx, input.OrganizationID, input.ConnectionID)
	if err != nil {
		return mcpconnection.Connection{}, err
	}
	return item, tx.Commit(ctx)
}

func getMCPConnection(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, organizationID, id string) (mcpconnection.Connection, error) {
	return scanMCPConnection(q.QueryRow(ctx, `SELECT `+mcpConnectionColumns+`
		FROM mcp_connections c JOIN mcp_services s ON s.id = c.service_id
		WHERE c.organization_id = $1 AND c.id = $2`, organizationID, id))
}

func (r *MCPConnectionRepository) GetByCode(ctx context.Context, code string) (mcpconnection.GatewayConnection, error) {
	var item mcpconnection.GatewayConnection
	var encryptedUpstream, encryptedHeader, encryptedQuery, encryptedParameters, encryptedService []byte
	var inheritServiceCredentials bool
	err := r.pool.QueryRow(ctx, `SELECT `+mcpConnectionColumns+`,
		c.organization_id, s.status, s.upstream_url, s.transport, c.secret_hash, c.upstream_authorization_enc,
		COALESCE(c.upstream_header_name, ''), c.upstream_header_value_enc,
		COALESCE(c.upstream_query_name, ''), c.upstream_query_value_enc, c.upstream_parameters_enc,
		c.inherit_service_credentials, s.credentials_enc,
		s.source_type, s.runtime_type, s.entry_file, s.container_name, s.deployment_status
		FROM mcp_connections c JOIN mcp_services s ON s.id = c.service_id
		JOIN organizations o ON o.id = c.organization_id
		WHERE c.code = $1 AND o.status = 'active'`, code).
		Scan(&item.ID, &item.ServiceID, &item.ServiceName, &item.Code, &item.Name, &item.Purpose,
			&item.AuthMode, &item.ApplicationName, &item.Status, &item.ExpiresAt, &item.KeyPrefix,
			&item.UpstreamAuthConfigured, &item.UpstreamHeaderConfigured, &item.UpstreamQueryConfigured,
			&item.UpstreamConfigured,
			&item.RequestsPerMinute, &item.DepartmentIDs, &item.UserIDs,
			&item.CreatedAt, &item.UpdatedAt, &item.OrganizationID, &item.ServiceStatus,
			&item.UpstreamURL, &item.Transport, &item.SecretHash, &encryptedUpstream,
			&item.UpstreamHeaderName, &encryptedHeader, &item.UpstreamQueryName, &encryptedQuery,
			&encryptedParameters, &inheritServiceCredentials, &encryptedService,
			&item.SourceType, &item.RuntimeType, &item.EntryFile, &item.ContainerName, &item.DeploymentStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return mcpconnection.GatewayConnection{}, mcpconnection.ErrNotFound
	}
	if err != nil {
		return mcpconnection.GatewayConnection{}, err
	}
	if len(encryptedUpstream) > 0 {
		item.UpstreamAuthorization, err = r.cipher.Open(encryptedUpstream)
		if err != nil {
			return mcpconnection.GatewayConnection{}, err
		}
	}
	if len(encryptedHeader) > 0 {
		item.UpstreamHeaderValue, err = r.cipher.Open(encryptedHeader)
		if err != nil {
			return mcpconnection.GatewayConnection{}, err
		}
	}
	if len(encryptedQuery) > 0 {
		item.UpstreamQueryValue, err = r.cipher.Open(encryptedQuery)
		if err != nil {
			return mcpconnection.GatewayConnection{}, err
		}
	}
	if len(encryptedParameters) > 0 {
		encoded, err := r.cipher.Open(encryptedParameters)
		if err != nil {
			return mcpconnection.GatewayConnection{}, err
		}
		if err := json.Unmarshal([]byte(encoded), &item.UpstreamParameters); err != nil {
			return mcpconnection.GatewayConnection{}, err
		}
	}
	if inheritServiceCredentials {
		serviceCredentials, err := NewMCPRepository(r.pool, r.cipher).decryptCredentials(encryptedService)
		if err != nil {
			return mcpconnection.GatewayConnection{}, err
		}
		item.ServiceParameters = serviceCredentials.Parameters
	}
	tools, err := NewMCPRepository(r.pool, r.cipher).ListTools(ctx, item.OrganizationID, item.ServiceID)
	if err != nil {
		return mcpconnection.GatewayConnection{}, err
	}
	item.Tools = tools
	return item, nil
}

func (r *MCPConnectionRepository) GetByID(ctx context.Context, organizationID, id string) (mcpconnection.GatewayConnection, error) {
	var code string
	err := r.pool.QueryRow(ctx, `SELECT code FROM mcp_connections WHERE organization_id = $1 AND id = $2`, organizationID, id).Scan(&code)
	if errors.Is(err, pgx.ErrNoRows) {
		return mcpconnection.GatewayConnection{}, mcpconnection.ErrNotFound
	}
	if err != nil {
		return mcpconnection.GatewayConnection{}, err
	}
	item, err := r.GetByCode(ctx, code)
	if err != nil {
		return mcpconnection.GatewayConnection{}, err
	}
	if item.OrganizationID != organizationID || item.ID != id {
		return mcpconnection.GatewayConnection{}, mcpconnection.ErrNotFound
	}
	return item, nil
}

func (r *MCPConnectionRepository) ChangeStatus(ctx context.Context, actor mcpconnection.Actor, id, status string) (mcpconnection.Connection, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return mcpconnection.Connection{}, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE mcp_connections SET status = $3, updated_at = NOW()
		WHERE organization_id = $1 AND id = $2`, actor.OrganizationID, id, status)
	if err != nil {
		return mcpconnection.Connection{}, err
	}
	if tag.RowsAffected() == 0 {
		return mcpconnection.Connection{}, mcpconnection.ErrNotFound
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{
		OrganizationID: actor.OrganizationID, ActorID: actor.UserID, RequestID: actor.RequestID,
		Action: "mcp.connection.status", ResourceType: "mcp_connection", ResourceID: id,
		Result: "success", SourceIP: actor.SourceIP, Detail: map[string]any{"status": status},
	}); err != nil {
		return mcpconnection.Connection{}, err
	}
	item, err := getMCPConnection(ctx, tx, actor.OrganizationID, id)
	if err != nil {
		return mcpconnection.Connection{}, err
	}
	return item, tx.Commit(ctx)
}

func (r *MCPConnectionRepository) RotateSecret(ctx context.Context, actor mcpconnection.Actor, id, value string, digest []byte, prefix string) error {
	encrypted, err := r.cipher.Seal(value)
	if err != nil {
		return err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE mcp_connections SET secret_hash = $3, encrypted_secret = $4,
		key_prefix = $5, updated_at = NOW() WHERE organization_id = $1 AND id = $2
		AND purpose = 'developer' AND auth_mode = 'bearer'`, actor.OrganizationID, id, digest, encrypted, prefix)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return mcpconnection.ErrNotFound
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{
		OrganizationID: actor.OrganizationID, ActorID: actor.UserID, RequestID: actor.RequestID,
		Action: "mcp.connection.secret.rotate", ResourceType: "mcp_connection", ResourceID: id,
		Result: "success", SourceIP: actor.SourceIP,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *MCPConnectionRepository) UpdateUpstreamCredentials(ctx context.Context, actor mcpconnection.Actor, id string, credentials mcpadmin.UpstreamCredentials) (mcpconnection.Connection, error) {
	items := credentials.AllParameters()
	var encrypted []byte
	if len(items) > 0 {
		encoded, err := json.Marshal(items)
		if err != nil {
			return mcpconnection.Connection{}, err
		}
		value, err := r.cipher.Seal(string(encoded))
		if err != nil {
			return mcpconnection.Connection{}, err
		}
		encrypted = value
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return mcpconnection.Connection{}, err
	}
	defer tx.Rollback(ctx)
	var definitionsJSON []byte
	err = tx.QueryRow(ctx, `SELECT s.parameters FROM mcp_connections c
		JOIN mcp_services s ON s.id = c.service_id
		WHERE c.organization_id = $1 AND c.id = $2 FOR UPDATE OF c`, actor.OrganizationID, id).Scan(&definitionsJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return mcpconnection.Connection{}, mcpconnection.ErrNotFound
	}
	if err != nil {
		return mcpconnection.Connection{}, err
	}
	var definitions []mcpadmin.ParameterDefinition
	if err := json.Unmarshal(definitionsJSON, &definitions); err != nil {
		return mcpconnection.Connection{}, err
	}
	if !mcpadmin.HasRequiredParameters(definitions, credentials) {
		return mcpconnection.Connection{}, mcpconnection.ErrInvalidInput
	}
	tag, err := tx.Exec(ctx, `UPDATE mcp_connections SET upstream_authorization_enc = NULL,
		upstream_header_name = NULL, upstream_header_value_enc = NULL,
		upstream_query_name = NULL, upstream_query_value_enc = NULL,
		upstream_parameters_enc = $3, inherit_service_credentials = FALSE, updated_at = NOW()
		WHERE organization_id = $1 AND id = $2`, actor.OrganizationID, id, nullableBytes(encrypted))
	if err != nil {
		return mcpconnection.Connection{}, err
	}
	if tag.RowsAffected() == 0 {
		return mcpconnection.Connection{}, mcpconnection.ErrNotFound
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{
		OrganizationID: actor.OrganizationID, ActorID: actor.UserID, RequestID: actor.RequestID,
		Action: "mcp.connection.upstream_credentials.update", ResourceType: "mcp_connection", ResourceID: id,
		Result: "success", SourceIP: actor.SourceIP,
		Detail: map[string]any{"parameterCount": len(items)},
	}); err != nil {
		return mcpconnection.Connection{}, err
	}
	item, err := getMCPConnection(ctx, tx, actor.OrganizationID, id)
	if err != nil {
		return mcpconnection.Connection{}, err
	}
	return item, tx.Commit(ctx)
}

func (r *MCPConnectionRepository) RevealSecret(ctx context.Context, actor mcpconnection.Actor, id string) (string, error) {
	return r.revealCredential(ctx, actor, id, false)
}

func (r *MCPConnectionRepository) RevealUpstreamAuthorization(ctx context.Context, actor mcpconnection.Actor, id string) (string, error) {
	return r.revealCredential(ctx, actor, id, true)
}

func (r *MCPConnectionRepository) RevealUpstreamCredentials(ctx context.Context, actor mcpconnection.Actor, id string) (mcpadmin.UpstreamCredentials, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return mcpadmin.UpstreamCredentials{}, err
	}
	defer tx.Rollback(ctx)
	var authorization, header, query, encryptedParameters []byte
	var result mcpadmin.UpstreamCredentials
	err = tx.QueryRow(ctx, `SELECT upstream_authorization_enc, COALESCE(upstream_header_name, ''), upstream_header_value_enc,
		COALESCE(upstream_query_name, ''), upstream_query_value_enc, upstream_parameters_enc FROM mcp_connections
		WHERE organization_id = $1 AND id = $2`, actor.OrganizationID, id).
		Scan(&authorization, &result.HeaderName, &header, &result.QueryName, &query, &encryptedParameters)
	if errors.Is(err, pgx.ErrNoRows) {
		return mcpadmin.UpstreamCredentials{}, mcpconnection.ErrNotFound
	}
	if err != nil {
		return mcpadmin.UpstreamCredentials{}, err
	}
	if len(authorization) > 0 {
		result.Authorization, err = r.cipher.Open(authorization)
		if err != nil {
			return mcpadmin.UpstreamCredentials{}, err
		}
	}
	if len(header) > 0 {
		result.HeaderValue, err = r.cipher.Open(header)
		if err != nil {
			return mcpadmin.UpstreamCredentials{}, err
		}
	}
	if len(query) > 0 {
		result.QueryValue, err = r.cipher.Open(query)
		if err != nil {
			return mcpadmin.UpstreamCredentials{}, err
		}
	}
	if len(encryptedParameters) > 0 {
		encoded, err := r.cipher.Open(encryptedParameters)
		if err != nil {
			return mcpadmin.UpstreamCredentials{}, err
		}
		if err := json.Unmarshal([]byte(encoded), &result.Parameters); err != nil {
			return mcpadmin.UpstreamCredentials{}, err
		}
	} else {
		result.Parameters = result.AllParameters()
	}
	if len(result.Parameters) == 0 {
		return mcpadmin.UpstreamCredentials{}, mcpconnection.ErrSecretUnavailable
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{
		OrganizationID: actor.OrganizationID, ActorID: actor.UserID, RequestID: actor.RequestID,
		Action: "mcp.connection.upstream_credentials.reveal", ResourceType: "mcp_connection", ResourceID: id,
		Result: "success", SourceIP: actor.SourceIP,
	}); err != nil {
		return mcpadmin.UpstreamCredentials{}, err
	}
	return result, tx.Commit(ctx)
}

func (r *MCPConnectionRepository) revealCredential(ctx context.Context, actor mcpconnection.Actor, id string, upstream bool) (string, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	var ciphertext []byte
	action := "mcp.connection.secret.reveal"
	if upstream {
		action = "mcp.connection.upstream_authorization.reveal"
		err = tx.QueryRow(ctx, `SELECT upstream_authorization_enc FROM mcp_connections WHERE organization_id = $1 AND id = $2`,
			actor.OrganizationID, id).Scan(&ciphertext)
	} else {
		err = tx.QueryRow(ctx, `SELECT encrypted_secret FROM mcp_connections WHERE organization_id = $1 AND id = $2`,
			actor.OrganizationID, id).Scan(&ciphertext)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return "", mcpconnection.ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if len(ciphertext) == 0 {
		return "", mcpconnection.ErrSecretUnavailable
	}
	value, err := r.cipher.Open(ciphertext)
	if err != nil {
		return "", err
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{
		OrganizationID: actor.OrganizationID, ActorID: actor.UserID, RequestID: actor.RequestID,
		Action: action, ResourceType: "mcp_connection", ResourceID: id,
		Result: "success", SourceIP: actor.SourceIP,
	}); err != nil {
		return "", err
	}
	return value, tx.Commit(ctx)
}

func (r *MCPConnectionRepository) AllowedUser(ctx context.Context, connectionID, organizationID, userID string) (string, bool, error) {
	var departmentID string
	var allowed bool
	err := r.pool.QueryRow(ctx, `SELECT COALESCE(u.department_id::text, ''), EXISTS (
		SELECT 1 FROM mcp_connection_grants g
		WHERE g.connection_id = $1 AND (g.user_id = u.id OR
		  (g.department_id = u.department_id AND EXISTS (
		    SELECT 1 FROM departments d WHERE d.id = u.department_id AND d.status = 'active'))))
		FROM users u WHERE u.id = $2 AND u.organization_id = $3 AND u.status = 'active'`,
		connectionID, userID, organizationID).Scan(&departmentID, &allowed)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	return departmentID, allowed, err
}

func (r *MCPConnectionRepository) ListForUser(ctx context.Context, organizationID, userID string) ([]mcpconnection.Connection, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+mcpConnectionColumns+`
		FROM mcp_connections c JOIN mcp_services s ON s.id = c.service_id
		JOIN users u ON u.organization_id = c.organization_id AND u.id = $2 AND u.status = 'active'
		WHERE c.organization_id = $1 AND c.purpose = 'harness' AND c.status = 'active'
		  AND s.status = 'active' AND (c.expires_at IS NULL OR c.expires_at > NOW())
		  AND EXISTS (SELECT 1 FROM mcp_connection_grants g WHERE g.connection_id = c.id
		    AND (g.user_id = u.id OR (g.department_id = u.department_id AND EXISTS (
		      SELECT 1 FROM departments d WHERE d.id = u.department_id AND d.status = 'active'))))
		ORDER BY c.name`, organizationID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]mcpconnection.Connection, 0)
	for rows.Next() {
		item, err := scanMCPConnection(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *MCPConnectionRepository) RecordCall(ctx context.Context, record mcpconnection.CallRecord) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO mcp_tool_calls
		(id, organization_id, connection_id, service_id, tool_name, user_id, department_id, result, latency_ms)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		uuid.NewString(), record.OrganizationID, record.ConnectionID, record.ServiceID,
		record.ToolName, mcpNullableString(record.UserID), mcpNullableString(record.DepartmentID),
		record.Result, record.LatencyMS)
	if err != nil {
		return fmt.Errorf("记录 MCP 工具调用失败: %w", err)
	}
	return nil
}

func (r *MCPConnectionRepository) ListUsage(ctx context.Context, organizationID string) ([]mcpconnection.Usage, error) {
	rows, err := r.pool.Query(ctx, `SELECT tc.connection_id::text, tc.tool_name,
		COALESCE(tc.user_id::text, ''), COALESCE(u.display_name, ''),
		COALESCE(tc.department_id::text, ''), COALESCE(d.name, ''),
		COUNT(*)::bigint, COUNT(*) FILTER (WHERE tc.result = 'failed')::bigint,
		COALESCE(AVG(tc.latency_ms)::bigint, 0)
		FROM mcp_tool_calls tc
		LEFT JOIN users u ON u.id = tc.user_id
		LEFT JOIN departments d ON d.id = tc.department_id
		WHERE tc.organization_id = $1
		GROUP BY tc.connection_id, tc.tool_name, tc.user_id, u.display_name, tc.department_id, d.name
		ORDER BY COUNT(*) DESC, tc.tool_name LIMIT 500`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]mcpconnection.Usage, 0)
	for rows.Next() {
		var item mcpconnection.Usage
		if err := rows.Scan(&item.ConnectionID, &item.ToolName, &item.UserID, &item.UserName,
			&item.DepartmentID, &item.DepartmentName, &item.Calls, &item.Failures, &item.AverageMS); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func mcpNullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableBytes(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}
