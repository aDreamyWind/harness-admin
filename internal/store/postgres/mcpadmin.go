package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"harness-admin/internal/identity"
	"harness-admin/internal/mcpadmin"
	"harness-admin/internal/secret"
)

type MCPRepository struct {
	pool   *pgxpool.Pool
	cipher *secret.Cipher
}

func NewMCPRepository(pool *pgxpool.Pool, cipher *secret.Cipher) *MCPRepository {
	return &MCPRepository{pool: pool, cipher: cipher}
}

const mcpServiceColumns = "id, code, name, description, transport, upstream_url, parameters, credentials_enc IS NOT NULL, status, created_at, updated_at"

func scanMCPService(row pgx.Row) (mcpadmin.ServiceRecord, error) {
	var item mcpadmin.ServiceRecord
	var parameters []byte
	err := row.Scan(&item.ID, &item.Code, &item.Name, &item.Description, &item.Transport, &item.UpstreamURL,
		&parameters, &item.CredentialsConfigured, &item.Status, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return item, err
	}
	err = json.Unmarshal(parameters, &item.Parameters)
	return item, err
}

func (r *MCPRepository) List(ctx context.Context, organizationID string) ([]mcpadmin.ServiceRecord, error) {
	rows, err := r.pool.Query(ctx, "SELECT "+mcpServiceColumns+" FROM mcp_services WHERE organization_id = $1 ORDER BY created_at DESC", organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]mcpadmin.ServiceRecord, 0)
	for rows.Next() {
		item, err := scanMCPService(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *MCPRepository) Create(ctx context.Context, input mcpadmin.CreateInput) (mcpadmin.ServiceRecord, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return mcpadmin.ServiceRecord{}, err
	}
	defer tx.Rollback(ctx)
	id := uuid.NewString()
	if input.Parameters == nil {
		input.Parameters = []mcpadmin.ParameterDefinition{}
	}
	parameters, err := json.Marshal(input.Parameters)
	if err != nil {
		return mcpadmin.ServiceRecord{}, err
	}
	encrypted, err := r.encryptCredentials(input.Credentials)
	if err != nil {
		return mcpadmin.ServiceRecord{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO mcp_services (id, organization_id, code, name, description, transport, upstream_url, parameters, credentials_enc, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		id, input.OrganizationID, input.Code, input.Name, input.Description, input.Transport, input.UpstreamURL, parameters, encrypted, input.UserID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return mcpadmin.ServiceRecord{}, mcpadmin.ErrConflict
		}
		return mcpadmin.ServiceRecord{}, err
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{
		OrganizationID: input.OrganizationID, ActorID: input.UserID, RequestID: input.RequestID,
		Action: "mcp.service.create", ResourceType: "mcp_service", ResourceID: id,
		Result: "success", SourceIP: input.SourceIP,
		Detail: map[string]any{"code": input.Code, "transport": input.Transport, "upstreamUrl": input.UpstreamURL},
	}); err != nil {
		return mcpadmin.ServiceRecord{}, err
	}
	item, err := getMCPService(ctx, tx, input.OrganizationID, id)
	if err != nil {
		return mcpadmin.ServiceRecord{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return mcpadmin.ServiceRecord{}, err
	}
	return item, nil
}

func (r *MCPRepository) Update(ctx context.Context, input mcpadmin.UpdateInput) (mcpadmin.ServiceRecord, error) {
	parameters := input.Parameters
	if parameters == nil {
		parameters = []mcpadmin.ParameterDefinition{}
	}
	encoded, err := json.Marshal(parameters)
	if err != nil {
		return mcpadmin.ServiceRecord{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return mcpadmin.ServiceRecord{}, err
	}
	defer tx.Rollback(ctx)
	var oldTransport, oldURL string
	err = tx.QueryRow(ctx, `SELECT transport, upstream_url FROM mcp_services
		WHERE organization_id = $1 AND id = $2 FOR UPDATE`, input.OrganizationID, input.ServiceID).Scan(&oldTransport, &oldURL)
	if errors.Is(err, pgx.ErrNoRows) {
		return mcpadmin.ServiceRecord{}, mcpadmin.ErrNotFound
	}
	if err != nil {
		return mcpadmin.ServiceRecord{}, err
	}
	tag, err := tx.Exec(ctx, `UPDATE mcp_services SET name = $3, description = $4, transport = $5,
		upstream_url = $6, parameters = $7, updated_at = NOW()
		WHERE organization_id = $1 AND id = $2`, input.OrganizationID, input.ServiceID,
		input.Name, input.Description, input.Transport, input.UpstreamURL, encoded)
	if err != nil {
		return mcpadmin.ServiceRecord{}, err
	}
	if tag.RowsAffected() == 0 {
		return mcpadmin.ServiceRecord{}, mcpadmin.ErrNotFound
	}
	if oldTransport != input.Transport || oldURL != input.UpstreamURL {
		if _, err := tx.Exec(ctx, `DELETE FROM mcp_service_tools WHERE service_id = $1`, input.ServiceID); err != nil {
			return mcpadmin.ServiceRecord{}, err
		}
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{
		OrganizationID: input.OrganizationID, ActorID: input.UserID, RequestID: input.RequestID,
		Action: "mcp.service.update", ResourceType: "mcp_service", ResourceID: input.ServiceID,
		Result: "success", SourceIP: input.SourceIP,
		Detail: map[string]any{"transport": input.Transport, "upstreamUrl": input.UpstreamURL, "parameterCount": len(parameters)},
	}); err != nil {
		return mcpadmin.ServiceRecord{}, err
	}
	item, err := getMCPService(ctx, tx, input.OrganizationID, input.ServiceID)
	if err != nil {
		return mcpadmin.ServiceRecord{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return mcpadmin.ServiceRecord{}, err
	}
	return item, nil
}

func (r *MCPRepository) Get(ctx context.Context, organizationID, id string) (mcpadmin.ServiceRecord, error) {
	return getMCPService(ctx, r.pool, organizationID, id)
}

func (r *MCPRepository) encryptCredentials(credentials mcpadmin.UpstreamCredentials) ([]byte, error) {
	items := credentials.AllParameters()
	if len(items) == 0 {
		return nil, nil
	}
	encoded, err := json.Marshal(items)
	if err != nil {
		return nil, err
	}
	return r.cipher.Seal(string(encoded))
}

func (r *MCPRepository) decryptCredentials(encrypted []byte) (mcpadmin.UpstreamCredentials, error) {
	if len(encrypted) == 0 {
		return mcpadmin.UpstreamCredentials{Parameters: []mcpadmin.ParameterValue{}}, nil
	}
	encoded, err := r.cipher.Open(encrypted)
	if err != nil {
		return mcpadmin.UpstreamCredentials{}, err
	}
	var items []mcpadmin.ParameterValue
	if err := json.Unmarshal([]byte(encoded), &items); err != nil {
		return mcpadmin.UpstreamCredentials{}, err
	}
	return mcpadmin.UpstreamCredentials{Parameters: items}, nil
}

func (r *MCPRepository) GetCredentials(ctx context.Context, organizationID, id string) (mcpadmin.UpstreamCredentials, error) {
	var encrypted []byte
	err := r.pool.QueryRow(ctx, `SELECT credentials_enc FROM mcp_services WHERE organization_id = $1 AND id = $2`, organizationID, id).Scan(&encrypted)
	if errors.Is(err, pgx.ErrNoRows) {
		return mcpadmin.UpstreamCredentials{}, mcpadmin.ErrNotFound
	}
	if err != nil {
		return mcpadmin.UpstreamCredentials{}, err
	}
	return r.decryptCredentials(encrypted)
}

func (r *MCPRepository) GetConnectionCredentials(ctx context.Context, organizationID, serviceID, connectionID string) (mcpadmin.UpstreamCredentials, error) {
	var serviceEncrypted, parametersEncrypted, authorizationEncrypted, headerEncrypted, queryEncrypted []byte
	var headerName, queryName string
	var inheritServiceCredentials bool
	err := r.pool.QueryRow(ctx, `SELECT c.inherit_service_credentials, s.credentials_enc, c.upstream_parameters_enc,
		c.upstream_authorization_enc, COALESCE(c.upstream_header_name, ''), c.upstream_header_value_enc,
		COALESCE(c.upstream_query_name, ''), c.upstream_query_value_enc
		FROM mcp_connections c JOIN mcp_services s ON s.id = c.service_id
		WHERE c.organization_id = $1 AND c.service_id = $2 AND c.id = $3
		AND c.status = 'active' AND (c.expires_at IS NULL OR c.expires_at > NOW())`,
		organizationID, serviceID, connectionID).Scan(&inheritServiceCredentials, &serviceEncrypted, &parametersEncrypted,
		&authorizationEncrypted, &headerName, &headerEncrypted, &queryName, &queryEncrypted)
	if errors.Is(err, pgx.ErrNoRows) {
		return mcpadmin.UpstreamCredentials{}, mcpadmin.ErrInvalidInput
	}
	if err != nil {
		return mcpadmin.UpstreamCredentials{}, err
	}
	defaults := mcpadmin.UpstreamCredentials{}
	if inheritServiceCredentials {
		defaults, err = r.decryptCredentials(serviceEncrypted)
		if err != nil {
			return mcpadmin.UpstreamCredentials{}, err
		}
	}
	connection, err := r.decryptCredentials(parametersEncrypted)
	if err != nil {
		return mcpadmin.UpstreamCredentials{}, err
	}
	if len(authorizationEncrypted) > 0 {
		connection.Authorization, err = r.cipher.Open(authorizationEncrypted)
		if err != nil {
			return mcpadmin.UpstreamCredentials{}, err
		}
	}
	if len(headerEncrypted) > 0 {
		connection.HeaderName = headerName
		connection.HeaderValue, err = r.cipher.Open(headerEncrypted)
		if err != nil {
			return mcpadmin.UpstreamCredentials{}, err
		}
	}
	if len(queryEncrypted) > 0 {
		connection.QueryName = queryName
		connection.QueryValue, err = r.cipher.Open(queryEncrypted)
		if err != nil {
			return mcpadmin.UpstreamCredentials{}, err
		}
	}
	return mcpadmin.MergeCredentials(defaults, connection), nil
}

func (r *MCPRepository) RevealCredentials(ctx context.Context, actor mcpadmin.Actor, id string) (mcpadmin.UpstreamCredentials, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return mcpadmin.UpstreamCredentials{}, err
	}
	defer tx.Rollback(ctx)
	var encrypted []byte
	err = tx.QueryRow(ctx, `SELECT credentials_enc FROM mcp_services WHERE organization_id = $1 AND id = $2`, actor.OrganizationID, id).Scan(&encrypted)
	if errors.Is(err, pgx.ErrNoRows) {
		return mcpadmin.UpstreamCredentials{}, mcpadmin.ErrNotFound
	}
	if err != nil {
		return mcpadmin.UpstreamCredentials{}, err
	}
	credentials, err := r.decryptCredentials(encrypted)
	if err != nil {
		return mcpadmin.UpstreamCredentials{}, err
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{OrganizationID: actor.OrganizationID, ActorID: actor.UserID,
		RequestID: actor.RequestID, Action: "mcp.service.credentials.reveal", ResourceType: "mcp_service", ResourceID: id,
		Result: "success", SourceIP: actor.SourceIP}); err != nil {
		return mcpadmin.UpstreamCredentials{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return mcpadmin.UpstreamCredentials{}, err
	}
	return credentials, nil
}

func (r *MCPRepository) UpdateCredentials(ctx context.Context, input mcpadmin.UpdateCredentialsInput) (mcpadmin.ServiceRecord, error) {
	encrypted, err := r.encryptCredentials(input.Credentials)
	if err != nil {
		return mcpadmin.ServiceRecord{}, err
	}
	var definitionsJSON any
	if input.Definitions != nil {
		encoded, err := json.Marshal(*input.Definitions)
		if err != nil {
			return mcpadmin.ServiceRecord{}, err
		}
		definitionsJSON = string(encoded)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return mcpadmin.ServiceRecord{}, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE mcp_services SET credentials_enc = $3,
		parameters = COALESCE($4::jsonb, parameters), updated_at = NOW()
		WHERE organization_id = $1 AND id = $2`, input.OrganizationID, input.ServiceID, encrypted, definitionsJSON)
	if err != nil {
		return mcpadmin.ServiceRecord{}, err
	}
	if tag.RowsAffected() == 0 {
		return mcpadmin.ServiceRecord{}, mcpadmin.ErrNotFound
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{OrganizationID: input.OrganizationID, ActorID: input.UserID,
		RequestID: input.RequestID, Action: "mcp.service.credentials.update", ResourceType: "mcp_service", ResourceID: input.ServiceID,
		Result: "success", SourceIP: input.SourceIP, Detail: map[string]any{"parameterCount": len(input.Credentials.AllParameters())}}); err != nil {
		return mcpadmin.ServiceRecord{}, err
	}
	item, err := getMCPService(ctx, tx, input.OrganizationID, input.ServiceID)
	if err != nil {
		return mcpadmin.ServiceRecord{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return mcpadmin.ServiceRecord{}, err
	}
	return item, nil
}

func getMCPService(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, organizationID, id string) (mcpadmin.ServiceRecord, error) {
	item, err := scanMCPService(q.QueryRow(ctx, "SELECT "+mcpServiceColumns+" FROM mcp_services WHERE organization_id = $1 AND id = $2", organizationID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return mcpadmin.ServiceRecord{}, mcpadmin.ErrNotFound
	}
	return item, err
}

func (r *MCPRepository) ChangeStatus(ctx context.Context, input mcpadmin.ChangeStatusInput) (mcpadmin.ServiceRecord, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return mcpadmin.ServiceRecord{}, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE mcp_services SET status = $3, updated_at = NOW()
		WHERE organization_id = $1 AND id = $2`, input.OrganizationID, input.ServiceID, input.Status)
	if err != nil {
		return mcpadmin.ServiceRecord{}, err
	}
	if tag.RowsAffected() == 0 {
		return mcpadmin.ServiceRecord{}, mcpadmin.ErrNotFound
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{
		OrganizationID: input.OrganizationID, ActorID: input.UserID, RequestID: input.RequestID,
		Action: "mcp.service.status", ResourceType: "mcp_service", ResourceID: input.ServiceID,
		Result: "success", SourceIP: input.SourceIP, Detail: map[string]any{"status": input.Status},
	}); err != nil {
		return mcpadmin.ServiceRecord{}, err
	}
	item, err := getMCPService(ctx, tx, input.OrganizationID, input.ServiceID)
	if err != nil {
		return mcpadmin.ServiceRecord{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return mcpadmin.ServiceRecord{}, err
	}
	return item, nil
}

func (r *MCPRepository) ReplaceTools(ctx context.Context, input mcpadmin.DiscoverInput, tools []mcpadmin.Tool) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// 远程发现与写入之间服务可能被停用，写入前重新检查并锁住服务。
	var status string
	if err := tx.QueryRow(ctx, `SELECT status FROM mcp_services WHERE id = $1 AND organization_id = $2 FOR UPDATE`,
		input.ServiceID, input.OrganizationID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return mcpadmin.ErrNotFound
		}
		return err
	}
	if status != "active" {
		return mcpadmin.ErrDisabled
	}
	if _, err := tx.Exec(ctx, "DELETE FROM mcp_service_tools WHERE service_id = $1", input.ServiceID); err != nil {
		return err
	}
	for _, tool := range tools {
		if _, err := tx.Exec(ctx, `INSERT INTO mcp_service_tools (service_id, name, description, input_schema)
			VALUES ($1, $2, $3, $4)`, input.ServiceID, tool.Name, tool.Description, []byte(tool.InputSchema)); err != nil {
			return err
		}
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{
		OrganizationID: input.OrganizationID, ActorID: input.UserID, RequestID: input.RequestID,
		Action: "mcp.tools.discover", ResourceType: "mcp_service", ResourceID: input.ServiceID,
		Result: "success", SourceIP: input.SourceIP, Detail: map[string]any{"toolCount": len(tools)},
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *MCPRepository) ListTools(ctx context.Context, organizationID, serviceID string) ([]mcpadmin.Tool, error) {
	rows, err := r.pool.Query(ctx, `SELECT t.name, t.description, t.input_schema, t.discovered_at
		FROM mcp_service_tools t JOIN mcp_services s ON s.id = t.service_id
		WHERE s.organization_id = $1 AND s.id = $2 ORDER BY t.name`, organizationID, serviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]mcpadmin.Tool, 0)
	for rows.Next() {
		var item mcpadmin.Tool
		if err := rows.Scan(&item.Name, &item.Description, &item.InputSchema, &item.DiscoveredAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *MCPRepository) RecordTest(ctx context.Context, input mcpadmin.TestInput, success bool) error {
	result := "success"
	if !success {
		result = "failed"
	}
	return r.recordTestAudit(ctx, input, result)
}

func (r *MCPRepository) recordTestAudit(ctx context.Context, input mcpadmin.TestInput, result string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := getMCPService(ctx, tx, input.OrganizationID, input.ServiceID); err != nil {
		return err
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{
		OrganizationID: input.OrganizationID, ActorID: input.UserID, RequestID: input.RequestID,
		Action: "mcp.tool.test", ResourceType: "mcp_service", ResourceID: input.ServiceID,
		Result: result, SourceIP: input.SourceIP, Detail: map[string]any{"tool": input.Name},
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
