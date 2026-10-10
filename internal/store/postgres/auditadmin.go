package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"harness-admin/internal/auditadmin"
)

const auditEventColumns = `
	a.id, COALESCE(a.actor_id::text, ''), COALESCE(u.username, ''), COALESCE(u.display_name, ''),
	a.request_id, a.action, a.resource_type, a.resource_id, a.result,
	COALESCE(HOST(a.source_ip), ''), a.detail, a.created_at
`

func (r *IdentityRepository) ListAuditEvents(ctx context.Context, filter auditadmin.ListFilter) (auditadmin.EventList, error) {
	searchPattern := "%" + filter.Search + "%"
	var total int64
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM audit_events a
		LEFT JOIN users u ON u.id = a.actor_id
		WHERE a.organization_id = $1
		  AND ($2 = '' OR a.action ILIKE $3 OR a.resource_type ILIKE $3 OR a.resource_id ILIKE $3
		       OR a.request_id ILIKE $3 OR COALESCE(u.username, '') ILIKE $3 OR COALESCE(u.display_name, '') ILIKE $3)
		  AND ($4 = '' OR a.action = $4)
		  AND ($5 = '' OR a.result = $5)
		  AND ($6::timestamptz IS NULL OR a.created_at >= $6)
		  AND ($7::timestamptz IS NULL OR a.created_at <= $7)
	`, filter.OrganizationID, filter.Search, searchPattern, filter.Action, filter.Result, filter.StartedAt, filter.EndedAt).Scan(&total)
	if err != nil {
		return auditadmin.EventList{}, err
	}

	rows, err := r.pool.Query(ctx, `
		SELECT `+auditEventColumns+`
		FROM audit_events a
		LEFT JOIN users u ON u.id = a.actor_id
		WHERE a.organization_id = $1
		  AND ($2 = '' OR a.action ILIKE $3 OR a.resource_type ILIKE $3 OR a.resource_id ILIKE $3
		       OR a.request_id ILIKE $3 OR COALESCE(u.username, '') ILIKE $3 OR COALESCE(u.display_name, '') ILIKE $3)
		  AND ($4 = '' OR a.action = $4)
		  AND ($5 = '' OR a.result = $5)
		  AND ($6::timestamptz IS NULL OR a.created_at >= $6)
		  AND ($7::timestamptz IS NULL OR a.created_at <= $7)
		ORDER BY a.created_at DESC, a.id DESC
		LIMIT $8 OFFSET $9
	`, filter.OrganizationID, filter.Search, searchPattern, filter.Action, filter.Result,
		filter.StartedAt, filter.EndedAt, filter.PageSize, (filter.Page-1)*filter.PageSize)
	if err != nil {
		return auditadmin.EventList{}, err
	}
	defer rows.Close()

	items := make([]auditadmin.Event, 0)
	for rows.Next() {
		event, err := scanAuditEvent(rows)
		if err != nil {
			return auditadmin.EventList{}, err
		}
		items = append(items, event)
	}
	if err := rows.Err(); err != nil {
		return auditadmin.EventList{}, err
	}
	return auditadmin.EventList{Items: items, Total: total, Page: filter.Page, PageSize: filter.PageSize}, nil
}

func (r *IdentityRepository) FindAuditEvent(ctx context.Context, organizationID string, eventID int64) (auditadmin.Event, error) {
	event, err := scanAuditEvent(r.pool.QueryRow(ctx, `
		SELECT `+auditEventColumns+`
		FROM audit_events a
		LEFT JOIN users u ON u.id = a.actor_id
		WHERE a.organization_id = $1 AND a.id = $2
	`, organizationID, eventID))
	if errors.Is(err, pgx.ErrNoRows) {
		return auditadmin.Event{}, auditadmin.ErrEventNotFound
	}
	return event, err
}

func scanAuditEvent(row rowScanner) (auditadmin.Event, error) {
	var event auditadmin.Event
	var detail []byte
	err := row.Scan(
		&event.ID, &event.ActorID, &event.ActorUsername, &event.ActorDisplayName,
		&event.RequestID, &event.Action, &event.ResourceType, &event.ResourceID,
		&event.Result, &event.SourceIP, &detail, &event.CreatedAt,
	)
	if err != nil {
		return auditadmin.Event{}, err
	}
	if err := json.Unmarshal(detail, &event.Detail); err != nil {
		return auditadmin.Event{}, err
	}
	return event, nil
}
