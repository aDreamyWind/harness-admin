package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"harness-admin/internal/drama"
)

type DramaRepository struct{ db *pgxpool.Pool }

func NewDramaRepository(db *pgxpool.Pool) *DramaRepository { return &DramaRepository{db: db} }

func (r *DramaRepository) GetSettings(ctx context.Context, organizationID, userID string) (drama.Settings, error) {
	var item drama.Settings
	item.OrganizationID, item.UserID = organizationID, userID
	err := r.db.QueryRow(ctx, `SELECT text_model_code, image_model_code, video_model_code, updated_at
		FROM drama_user_settings WHERE organization_id=$1 AND user_id=$2`, organizationID, userID).
		Scan(&item.TextModelCode, &item.ImageModelCode, &item.VideoModelCode, &item.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return item, nil
	}
	if err != nil {
		return drama.Settings{}, fmt.Errorf("读取漫剧模型设置失败: %w", err)
	}
	return item, nil
}

func (r *DramaRepository) SaveSettings(ctx context.Context, item drama.Settings) (drama.Settings, error) {
	err := r.db.QueryRow(ctx, `INSERT INTO drama_user_settings
		(organization_id, user_id, text_model_code, image_model_code, video_model_code)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (organization_id, user_id) DO UPDATE SET
		text_model_code=EXCLUDED.text_model_code,
		image_model_code=EXCLUDED.image_model_code,
		video_model_code=EXCLUDED.video_model_code,
		updated_at=NOW()
		RETURNING updated_at`, item.OrganizationID, item.UserID, item.TextModelCode, item.ImageModelCode, item.VideoModelCode).
		Scan(&item.UpdatedAt)
	if err != nil {
		return drama.Settings{}, err
	}
	return item, nil
}

const omittedDramaSource = "''::text AS source_text"

var dramaColumns = dramaProjectColumns("source_text", dramaAssetJSON)

func dramaProjectColumns(sourceText, assetJSON string) string {
	return `id, organization_id, owner_id, title, source_name, ` + sourceText + `, source_length, source_mode,
 style, aspect_ratio, model_code, status, phase, error_message, protagonist, genre, synopsis, background, setting,
 COALESCE((SELECT jsonb_agg(` + assetJSON + ` ORDER BY a.created_at, a.id) FROM drama_assets a WHERE a.project_id=drama_projects.id AND a.kind='character'), '[]'::jsonb),
 COALESCE((SELECT jsonb_agg(` + assetJSON + ` ORDER BY a.created_at, a.id) FROM drama_assets a WHERE a.project_id=drama_projects.id AND a.kind='scene'), '[]'::jsonb),
 COALESCE((SELECT jsonb_agg(` + assetJSON + ` ORDER BY a.created_at, a.id) FROM drama_assets a WHERE a.project_id=drama_projects.id AND a.kind='prop'), '[]'::jsonb),
 COALESCE((SELECT jsonb_agg(jsonb_build_object('title', e.title, 'synopsis', e.synopsis) ORDER BY b.batch_number NULLS FIRST, e.number, e.created_at) FROM drama_script_episodes e LEFT JOIN drama_batches b ON b.id=e.batch_id WHERE e.project_id=drama_projects.id), '[]'::jsonb),
 created_at, updated_at,COALESCE(style_id,'')`
}

func scanDrama(row pgx.Row) (drama.Project, error) {
	var item drama.Project
	var protagonist, genre, synopsis, background, setting string
	err := row.Scan(&item.ID, &item.OrganizationID, &item.OwnerID, &item.Title, &item.SourceName,
		&item.SourceText, &item.SourceLength, &item.SourceMode, &item.Style, &item.AspectRatio, &item.ModelCode,
		&item.Status, &item.Phase, &item.Error, &protagonist, &genre, &synopsis, &background, &setting,
		&item.Characters, &item.Scenes, &item.Props, &item.Episodes, &item.CreatedAt, &item.UpdatedAt, &item.StyleID)
	if errors.Is(err, pgx.ErrNoRows) {
		return drama.Project{}, drama.ErrNotFound
	}
	if err != nil {
		return drama.Project{}, err
	}
	item.Overview = json.RawMessage(`{}`)
	if protagonist != "" || genre != "" || synopsis != "" || background != "" || setting != "" {
		item.Overview, err = json.Marshal(map[string]string{
			"videoStyle": item.Style, "styleId": item.StyleID, "aspectRatio": item.AspectRatio,
			"protagonist": protagonist, "genre": genre, "synopsis": synopsis,
			"background": background, "setting": setting,
		})
		if err != nil {
			return drama.Project{}, fmt.Errorf("构造漫剧概览失败: %w", err)
		}
	}
	return item, nil
}

func (r *DramaRepository) Create(ctx context.Context, item drama.Project) (drama.Project, error) {
	row := r.db.QueryRow(ctx, `INSERT INTO drama_projects
		(id, organization_id, owner_id, title, source_name, source_text, source_length, source_mode, style, aspect_ratio, model_code, status, phase,style_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,NULLIF($14,'')) RETURNING `+dramaColumns,
		item.ID, item.OrganizationID, item.OwnerID, item.Title, item.SourceName, item.SourceText,
		item.SourceLength, item.SourceMode, item.Style, item.AspectRatio, item.ModelCode, item.Status, item.Phase, item.StyleID)
	return scanDrama(row)
}

func (r *DramaRepository) List(ctx context.Context, organizationID, ownerID string) ([]drama.Project, error) {
	columns := dramaProjectColumns(omittedDramaSource, dramaAssetJSONWithPreview)
	rows, err := r.db.Query(ctx, `SELECT `+columns+` FROM drama_projects
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
	item, err := scanDrama(r.db.QueryRow(ctx, `SELECT `+dramaProjectColumns(omittedDramaSource, dramaAssetJSON)+` FROM drama_projects
		WHERE organization_id=$1 AND owner_id=$2 AND id=$3`, organizationID, ownerID, id))
	item.SourceText = ""
	return item, err
}

func (r *DramaRepository) RetryFailed(ctx context.Context, organizationID, ownerID, id, modelCode string) (drama.Project, error) {
	return scanDrama(r.db.QueryRow(ctx, `UPDATE drama_projects SET status='queued', phase='waiting',
		error_message='', model_code=$4, updated_at=NOW()
		WHERE organization_id=$1 AND owner_id=$2 AND id=$3 AND status='failed' AND source_mode='upload'
		RETURNING `+dramaColumns, organizationID, ownerID, id, modelCode))
}

func (r *DramaRepository) QueueOverviewReparse(ctx context.Context, organizationID, ownerID, id, modelCode string) (drama.Project, error) {
	return scanDrama(r.db.QueryRow(ctx, `UPDATE drama_projects SET status='queued', phase='waiting',
        error_message='', model_code=$4, updated_at=NOW()
        WHERE organization_id=$1 AND owner_id=$2 AND id=$3 AND status IN ('ready','failed') AND source_length > 0
        RETURNING `+dramaColumns, organizationID, ownerID, id, modelCode))
}

func (r *DramaRepository) GetSource(ctx context.Context, organizationID, ownerID, id string) (string, error) {
	var source string
	err := r.db.QueryRow(ctx, `SELECT source_text FROM drama_projects WHERE organization_id=$1 AND owner_id=$2 AND id=$3`, organizationID, ownerID, id).Scan(&source)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", drama.ErrNotFound
	}
	return source, err
}

const saveDramaAnalysisSQL = `UPDATE drama_projects SET status=$4::text, phase=$5::text,
		protagonist=CASE WHEN $5::text='story' AND $6::jsonb IS NOT NULL THEN COALESCE($6::jsonb->>'protagonist','') ELSE protagonist END,
		genre=CASE WHEN $5::text='story' AND $6::jsonb IS NOT NULL THEN COALESCE($6::jsonb->>'genre','') ELSE genre END,
		synopsis=CASE WHEN $5::text='story' AND $6::jsonb IS NOT NULL THEN COALESCE($6::jsonb->>'synopsis','') ELSE synopsis END,
		background=CASE WHEN $5::text='story' AND $6::jsonb IS NOT NULL THEN COALESCE($6::jsonb->>'background','') ELSE background END,
		setting=CASE WHEN $5::text='story' AND $6::jsonb IS NOT NULL THEN COALESCE($6::jsonb->>'setting','') ELSE setting END,
		updated_at=NOW() WHERE organization_id=$1 AND owner_id=$2 AND id=$3`

func (r *DramaRepository) SaveAnalysis(ctx context.Context, organizationID, ownerID, id, status, phase string, data json.RawMessage) error {
	var result sql.NullString
	if len(data) > 0 {
		result = sql.NullString{String: string(data), Valid: true}
	}
	command, err := r.db.Exec(ctx, saveDramaAnalysisSQL, organizationID, ownerID, id, status, phase, result)
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
	return scanDrama(r.db.QueryRow(ctx, `UPDATE drama_projects SET
		protagonist=COALESCE($4::jsonb->>'protagonist',''),
		genre=COALESCE($4::jsonb->>'genre',''),
		synopsis=COALESCE($4::jsonb->>'synopsis',''),
		background=COALESCE($4::jsonb->>'background',''),
		setting=COALESCE($4::jsonb->>'setting',''),
		style=COALESCE(NULLIF($4::jsonb->>'videoStyle',''), style),
		style_id=COALESCE(NULLIF($4::jsonb->>'styleId',''),style_id),
		aspect_ratio=COALESCE(NULLIF($4::jsonb->>'aspectRatio',''), aspect_ratio),
		updated_at=NOW()
		WHERE organization_id=$1 AND owner_id=$2 AND id=$3 RETURNING `+dramaColumns,
		organizationID, ownerID, id, overview))
}

func (r *DramaRepository) FailInterrupted(ctx context.Context) error {
	_, err := r.db.Exec(ctx, `UPDATE drama_projects SET status='failed', phase='failed',
		error_message='服务重启中断了概览解析，请在项目页重新解析', updated_at=NOW()
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
