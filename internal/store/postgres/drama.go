package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"harness-admin/internal/drama"
)

type DramaRepository struct{ db *pgxpool.Pool }

func NewDramaRepository(db *pgxpool.Pool) *DramaRepository { return &DramaRepository{db: db} }

const dramaColumns = `id, organization_id, owner_id, title, source_name, source_text, source_length, source_mode,
 style, aspect_ratio, model_code, status, phase, error_message, overview, characters, scenes, props, episodes, created_at, updated_at`

func scanDrama(row pgx.Row) (drama.Project, error) {
	var item drama.Project
	err := row.Scan(&item.ID, &item.OrganizationID, &item.OwnerID, &item.Title, &item.SourceName,
		&item.SourceText, &item.SourceLength, &item.SourceMode, &item.Style, &item.AspectRatio, &item.ModelCode,
		&item.Status, &item.Phase, &item.Error, &item.Overview, &item.Characters, &item.Scenes,
		&item.Props, &item.Episodes, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return drama.Project{}, drama.ErrNotFound
	}
	return item, err
}

func (r *DramaRepository) Create(ctx context.Context, item drama.Project) (drama.Project, error) {
	row := r.db.QueryRow(ctx, `INSERT INTO drama_projects
		(id, organization_id, owner_id, title, source_name, source_text, source_length, source_mode, style, aspect_ratio, model_code, status, phase)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING `+dramaColumns,
		item.ID, item.OrganizationID, item.OwnerID, item.Title, item.SourceName, item.SourceText,
		item.SourceLength, item.SourceMode, item.Style, item.AspectRatio, item.ModelCode, item.Status, item.Phase)
	return scanDrama(row)
}

func (r *DramaRepository) List(ctx context.Context, organizationID, ownerID string) ([]drama.Project, error) {
	rows, err := r.db.Query(ctx, `SELECT `+strings.Replace(dramaColumns, "source_text", "''::text AS source_text", 1)+` FROM drama_projects
		WHERE organization_id=$1 AND owner_id=$2 ORDER BY updated_at DESC`, organizationID, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]drama.Project, 0)
	for rows.Next() {
		item, err := scanDrama(rows)
		if err != nil {
			return nil, err
		}
		item.SourceText = ""
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *DramaRepository) Get(ctx context.Context, organizationID, ownerID, id string) (drama.Project, error) {
	item, err := scanDrama(r.db.QueryRow(ctx, `SELECT `+strings.Replace(dramaColumns, "source_text", "''::text AS source_text", 1)+` FROM drama_projects
		WHERE organization_id=$1 AND owner_id=$2 AND id=$3`, organizationID, ownerID, id))
	item.SourceText = ""
	return item, err
}

func (r *DramaRepository) GetSource(ctx context.Context, organizationID, ownerID, id string) (string, error) {
	var source string
	err := r.db.QueryRow(ctx, `SELECT source_text FROM drama_projects WHERE organization_id=$1 AND owner_id=$2 AND id=$3`, organizationID, ownerID, id).Scan(&source)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", drama.ErrNotFound
	}
	return source, err
}

func (r *DramaRepository) SaveAnalysis(ctx context.Context, organizationID, ownerID, id, status, phase string, data json.RawMessage) error {
	var result sql.NullString
	if len(data) > 0 {
		result = sql.NullString{String: string(data), Valid: true}
	}
	command, err := r.db.Exec(ctx, `UPDATE drama_projects SET status=$4, phase=$5,
		overview=CASE WHEN $5='story' AND $6::jsonb IS NOT NULL THEN $6::jsonb || overview ELSE overview END,
		characters=CASE WHEN $5='assets' AND $6::jsonb IS NOT NULL THEN COALESCE($6::jsonb->'characters','[]'::jsonb) ELSE characters END,
		scenes=CASE WHEN $5='assets' AND $6::jsonb IS NOT NULL THEN COALESCE($6::jsonb->'scenes','[]'::jsonb) ELSE scenes END,
		props=CASE WHEN $5='assets' AND $6::jsonb IS NOT NULL THEN COALESCE($6::jsonb->'props','[]'::jsonb) ELSE props END,
		episodes=CASE WHEN $5='episodes' AND $6::jsonb IS NOT NULL THEN COALESCE($6::jsonb->'episodes','[]'::jsonb) ELSE episodes END,
		updated_at=NOW() WHERE organization_id=$1 AND owner_id=$2 AND id=$3`, organizationID, ownerID, id, status, phase, result)
	if err != nil {
		return fmt.Errorf("保存解析阶段失败: %w", err)
	}
	if command.RowsAffected() == 0 {
		return drama.ErrNotFound
	}
	return nil
}

func (r *DramaRepository) FailAnalysis(ctx context.Context, organizationID, ownerID, id, message string) error {
	command, err := r.db.Exec(ctx, `UPDATE drama_projects SET status='failed', phase='failed', error_message=$4, updated_at=NOW()
		WHERE organization_id=$1 AND owner_id=$2 AND id=$3`, organizationID, ownerID, id, message)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return drama.ErrNotFound
	}
	return nil
}

func (r *DramaRepository) UpdateOverview(ctx context.Context, organizationID, ownerID, id string, overview json.RawMessage) (drama.Project, error) {
	return scanDrama(r.db.QueryRow(ctx, `UPDATE drama_projects SET overview=$4::jsonb, updated_at=NOW()
		WHERE organization_id=$1 AND owner_id=$2 AND id=$3 RETURNING `+dramaColumns,
		organizationID, ownerID, id, overview))
}

func (r *DramaRepository) FailInterrupted(ctx context.Context) error {
	_, err := r.db.Exec(ctx, `UPDATE drama_projects SET status='failed', phase='failed',
		error_message='服务重启中断了解析，请重新创建项目以继续', updated_at=NOW()
		WHERE status IN ('queued','running') AND updated_at < NOW() - INTERVAL '10 minutes'`)
	return err
}

func (r *DramaRepository) SaveGeneratedScript(ctx context.Context, organizationID, ownerID, id, script string) error {
	command, err := r.db.Exec(ctx, `UPDATE drama_projects SET source_text=$4, source_length=$5, updated_at=NOW()
		WHERE organization_id=$1 AND owner_id=$2 AND id=$3`, organizationID, ownerID, id, script, len([]rune(script)))
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return drama.ErrNotFound
	}
	return nil
}
