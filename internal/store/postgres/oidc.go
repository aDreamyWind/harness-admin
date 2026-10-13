package postgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"harness-admin/internal/identity"
	"harness-admin/internal/oidc"
)

type OIDCRepository struct {
	pool *pgxpool.Pool
}

func NewOIDCRepository(pool *pgxpool.Pool) *OIDCRepository {
	return &OIDCRepository{pool: pool}
}

const oidcClientColumns = `
	c.id, c.organization_id, c.name, c.client_id, c.client_secret_prefix,
	c.redirect_uris, c.allowed_scopes, c.status, c.created_at, c.updated_at
`

func (r *OIDCRepository) ListClients(ctx context.Context, organizationID string) ([]oidc.Client, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+oidcClientColumns+`
		FROM oidc_clients c
		WHERE c.organization_id = $1
		ORDER BY c.created_at DESC
	`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]oidc.Client, 0)
	for rows.Next() {
		item, err := scanOIDCClient(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *OIDCRepository) CreateClient(ctx context.Context, input oidc.CreateClientInput) (oidc.Client, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return oidc.Client{}, err
	}
	defer tx.Rollback(ctx)
	clientRecordID := uuid.NewString()
	_, err = tx.Exec(ctx, `
		INSERT INTO oidc_clients (
			id, organization_id, name, client_id, client_secret_hash, client_secret_prefix,
			redirect_uris, allowed_scopes, created_by
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, clientRecordID, input.OrganizationID, input.Name, input.ClientID, input.ClientSecretHash,
		input.ClientSecretPrefix, input.RedirectURIs, input.AllowedScopes, input.ActorID)
	if err != nil {
		return oidc.Client{}, err
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{
		OrganizationID: input.OrganizationID, ActorID: input.ActorID, RequestID: input.RequestID,
		Action: "oidc.client.create", ResourceType: "oidc_client", ResourceID: clientRecordID,
		Result: "success", SourceIP: input.SourceIP,
		Detail: map[string]any{"name": input.Name, "clientId": input.ClientID,
			"redirectUris": input.RedirectURIs, "allowedScopes": input.AllowedScopes},
	}); err != nil {
		return oidc.Client{}, err
	}
	item, err := findOIDCClient(ctx, tx, input.OrganizationID, clientRecordID)
	if err != nil {
		return oidc.Client{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return oidc.Client{}, err
	}
	return item, nil
}

func (r *OIDCRepository) UpdateClient(ctx context.Context, input oidc.UpdateClientInput) (oidc.Client, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return oidc.Client{}, err
	}
	defer tx.Rollback(ctx)
	command, err := tx.Exec(ctx, `
		UPDATE oidc_clients
		SET name = $1, redirect_uris = $2, allowed_scopes = $3, updated_at = NOW()
		WHERE organization_id = $4 AND id = $5
	`, input.Name, input.RedirectURIs, input.AllowedScopes, input.OrganizationID, input.ClientRecordID)
	if err != nil {
		return oidc.Client{}, err
	}
	if command.RowsAffected() == 0 {
		return oidc.Client{}, oidc.ErrClientNotFound
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{
		OrganizationID: input.OrganizationID, ActorID: input.ActorID, RequestID: input.RequestID,
		Action: "oidc.client.update", ResourceType: "oidc_client", ResourceID: input.ClientRecordID,
		Result: "success", SourceIP: input.SourceIP,
		Detail: map[string]any{"name": input.Name, "redirectUris": input.RedirectURIs, "allowedScopes": input.AllowedScopes},
	}); err != nil {
		return oidc.Client{}, err
	}
	item, err := findOIDCClient(ctx, tx, input.OrganizationID, input.ClientRecordID)
	if err != nil {
		return oidc.Client{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return oidc.Client{}, err
	}
	return item, nil
}

func (r *OIDCRepository) ChangeClientStatus(ctx context.Context, input oidc.ChangeClientStatusInput) (oidc.Client, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return oidc.Client{}, err
	}
	defer tx.Rollback(ctx)
	command, err := tx.Exec(ctx, `
		UPDATE oidc_clients SET status = $1, updated_at = NOW()
		WHERE organization_id = $2 AND id = $3
	`, input.Status, input.OrganizationID, input.ClientRecordID)
	if err != nil {
		return oidc.Client{}, err
	}
	if command.RowsAffected() == 0 {
		return oidc.Client{}, oidc.ErrClientNotFound
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{
		OrganizationID: input.OrganizationID, ActorID: input.ActorID, RequestID: input.RequestID,
		Action: "oidc.client.status.update", ResourceType: "oidc_client", ResourceID: input.ClientRecordID,
		Result: "success", SourceIP: input.SourceIP, Detail: map[string]any{"status": input.Status},
	}); err != nil {
		return oidc.Client{}, err
	}
	item, err := findOIDCClient(ctx, tx, input.OrganizationID, input.ClientRecordID)
	if err != nil {
		return oidc.Client{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return oidc.Client{}, err
	}
	return item, nil
}

func (r *OIDCRepository) RotateClientSecret(ctx context.Context, input oidc.RotateClientSecretInput) (oidc.Client, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return oidc.Client{}, err
	}
	defer tx.Rollback(ctx)
	command, err := tx.Exec(ctx, `
		UPDATE oidc_clients
		SET client_secret_hash = $1, client_secret_prefix = $2, updated_at = NOW()
		WHERE organization_id = $3 AND id = $4
	`, input.ClientSecretHash, input.ClientSecretPrefix, input.OrganizationID, input.ClientRecordID)
	if err != nil {
		return oidc.Client{}, err
	}
	if command.RowsAffected() == 0 {
		return oidc.Client{}, oidc.ErrClientNotFound
	}
	if err := insertAudit(ctx, tx, identity.AuditEvent{
		OrganizationID: input.OrganizationID, ActorID: input.ActorID, RequestID: input.RequestID,
		Action: "oidc.client.secret.rotate", ResourceType: "oidc_client", ResourceID: input.ClientRecordID,
		Result: "success", SourceIP: input.SourceIP,
	}); err != nil {
		return oidc.Client{}, err
	}
	item, err := findOIDCClient(ctx, tx, input.OrganizationID, input.ClientRecordID)
	if err != nil {
		return oidc.Client{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return oidc.Client{}, err
	}
	return item, nil
}

func findOIDCClient(ctx context.Context, queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, organizationID, clientRecordID string) (oidc.Client, error) {
	item, err := scanOIDCClient(queryer.QueryRow(ctx, `
		SELECT `+oidcClientColumns+`
		FROM oidc_clients c
		WHERE c.organization_id = $1 AND c.id = $2
	`, organizationID, clientRecordID))
	if errors.Is(err, pgx.ErrNoRows) {
		return oidc.Client{}, oidc.ErrClientNotFound
	}
	return item, err
}

func scanOIDCClient(row interface{ Scan(...any) error }) (oidc.Client, error) {
	var item oidc.Client
	err := row.Scan(
		&item.ID, &item.OrganizationID, &item.Name, &item.ClientID, &item.ClientSecretPrefix,
		&item.RedirectURIs, &item.AllowedScopes, &item.Status, &item.CreatedAt, &item.UpdatedAt,
	)
	return item, err
}

func (r *OIDCRepository) FindProtocolClient(ctx context.Context, clientID string) (oidc.ProtocolClient, error) {
	var item oidc.ProtocolClient
	err := r.pool.QueryRow(ctx, `
		SELECT `+oidcClientColumns+`, c.client_secret_hash
		FROM oidc_clients c
		WHERE c.client_id = $1
	`, clientID).Scan(
		&item.ID, &item.OrganizationID, &item.Name, &item.ClientID, &item.ClientSecretPrefix,
		&item.RedirectURIs, &item.AllowedScopes, &item.Status, &item.CreatedAt, &item.UpdatedAt,
		&item.ClientSecretHash,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return oidc.ProtocolClient{}, oidc.ErrClientNotFound
	}
	return item, err
}

func (r *OIDCRepository) SaveAuthorizationGrant(ctx context.Context, grant oidc.AuthorizationGrant) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO oidc_authorization_codes (
			id, organization_id, client_id, user_id, code_hash, redirect_uri,
			scopes, nonce, code_challenge, expires_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''), $9, $10)
	`, grant.ID, grant.OrganizationID, grant.ClientRecordID, grant.UserID, grant.CodeHash,
		grant.RedirectURI, grant.Scopes, grant.Nonce, grant.CodeChallenge, grant.ExpiresAt)
	return err
}

func (r *OIDCRepository) ExchangeAuthorizationGrant(ctx context.Context, codeHash []byte, clientRecordID, redirectURI, codeChallenge string) (oidc.AuthorizationGrant, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return oidc.AuthorizationGrant{}, err
	}
	defer tx.Rollback(ctx)
	var grant oidc.AuthorizationGrant
	err = tx.QueryRow(ctx, `
		SELECT a.id, a.organization_id, a.client_id, a.user_id, a.redirect_uri,
		       a.scopes, COALESCE(a.nonce, ''), a.code_challenge, a.expires_at,
		       u.id, u.organization_id, o.name, o.code, u.username, u.display_name,
		       COALESCE(u.email, '')
		FROM oidc_authorization_codes a
		JOIN oidc_clients c ON c.organization_id = a.organization_id AND c.id = a.client_id
		JOIN users u ON u.organization_id = a.organization_id AND u.id = a.user_id
		JOIN organizations o ON o.id = u.organization_id
		WHERE a.code_hash = $1 AND a.client_id = $2 AND a.redirect_uri = $3
		  AND a.code_challenge = $4 AND a.consumed_at IS NULL AND a.expires_at > NOW()
		  AND c.status = 'active' AND u.status = 'active' AND o.status = 'active'
		FOR UPDATE OF a
	`, codeHash, clientRecordID, redirectURI, codeChallenge).Scan(
		&grant.ID, &grant.OrganizationID, &grant.ClientRecordID, &grant.UserID, &grant.RedirectURI,
		&grant.Scopes, &grant.Nonce, &grant.CodeChallenge, &grant.ExpiresAt,
		&grant.Subject.ID, &grant.Subject.OrganizationID, &grant.Subject.Organization,
		&grant.Subject.OrganizationCode, &grant.Subject.Username, &grant.Subject.DisplayName, &grant.Subject.Email,
	)
	if err != nil {
		return oidc.AuthorizationGrant{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE oidc_authorization_codes SET consumed_at = NOW() WHERE id = $1`, grant.ID); err != nil {
		return oidc.AuthorizationGrant{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return oidc.AuthorizationGrant{}, err
	}
	return grant, nil
}

func (r *OIDCRepository) SaveRefreshGrant(ctx context.Context, grant oidc.RefreshGrant) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO oidc_refresh_tokens (
			id, organization_id, client_id, user_id, token_hash, scopes, expires_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, grant.ID, grant.OrganizationID, grant.ClientRecordID, grant.UserID, grant.TokenHash, grant.Scopes, grant.ExpiresAt)
	return err
}

func (r *OIDCRepository) RotateRefreshGrant(ctx context.Context, oldHash []byte, clientRecordID string, replacement oidc.RefreshGrant) (oidc.RefreshGrant, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return oidc.RefreshGrant{}, err
	}
	defer tx.Rollback(ctx)
	var grant oidc.RefreshGrant
	err = tx.QueryRow(ctx, `
		SELECT r.id, r.organization_id, r.client_id, r.user_id, r.scopes, r.expires_at,
		       u.id, u.organization_id, o.name, o.code, u.username, u.display_name,
		       COALESCE(u.email, '')
		FROM oidc_refresh_tokens r
		JOIN oidc_clients c ON c.organization_id = r.organization_id AND c.id = r.client_id
		JOIN users u ON u.organization_id = r.organization_id AND u.id = r.user_id
		JOIN organizations o ON o.id = u.organization_id
		WHERE r.token_hash = $1 AND r.client_id = $2 AND r.consumed_at IS NULL
		  AND r.expires_at > NOW() AND c.status = 'active'
		  AND u.status = 'active' AND o.status = 'active'
		FOR UPDATE OF r
	`, oldHash, clientRecordID).Scan(
		&grant.ID, &grant.OrganizationID, &grant.ClientRecordID, &grant.UserID, &grant.Scopes, &grant.ExpiresAt,
		&grant.Subject.ID, &grant.Subject.OrganizationID, &grant.Subject.Organization,
		&grant.Subject.OrganizationCode, &grant.Subject.Username, &grant.Subject.DisplayName, &grant.Subject.Email,
	)
	if err != nil {
		return oidc.RefreshGrant{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE oidc_refresh_tokens SET consumed_at = NOW() WHERE id = $1`, grant.ID); err != nil {
		return oidc.RefreshGrant{}, err
	}
	replacement.OrganizationID = grant.OrganizationID
	replacement.ClientRecordID = grant.ClientRecordID
	replacement.UserID = grant.UserID
	replacement.Scopes = grant.Scopes
	if replacement.ExpiresAt.After(grant.ExpiresAt) {
		replacement.ExpiresAt = grant.ExpiresAt
	}
	if _, err = tx.Exec(ctx, `
		INSERT INTO oidc_refresh_tokens (
			id, organization_id, client_id, user_id, token_hash, scopes, expires_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, replacement.ID, replacement.OrganizationID, replacement.ClientRecordID, replacement.UserID,
		replacement.TokenHash, replacement.Scopes, replacement.ExpiresAt); err != nil {
		return oidc.RefreshGrant{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return oidc.RefreshGrant{}, err
	}
	grant.ID = replacement.ID
	grant.TokenHash = replacement.TokenHash
	grant.ExpiresAt = replacement.ExpiresAt
	return grant, nil
}

func (r *OIDCRepository) FindSubject(ctx context.Context, organizationID, userID string) (oidc.Subject, error) {
	var subject oidc.Subject
	err := r.pool.QueryRow(ctx, `
		SELECT u.id, u.organization_id, o.name, o.code, u.username, u.display_name, COALESCE(u.email, '')
		FROM users u
		JOIN organizations o ON o.id = u.organization_id
		WHERE u.organization_id = $1 AND u.id = $2
		  AND u.status = 'active' AND o.status = 'active'
	`, organizationID, userID).Scan(
		&subject.ID, &subject.OrganizationID, &subject.Organization, &subject.OrganizationCode,
		&subject.Username, &subject.DisplayName, &subject.Email,
	)
	return subject, err
}
