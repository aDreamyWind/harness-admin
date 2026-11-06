package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"harness-admin/internal/drama"
)

const dramaCoverStateColumns = `
    COALESCE(CASE WHEN c.status='running' AND c.expires_at<=NOW() THEN 'failed' ELSE c.status END, 'idle'),
    COALESCE(c.job_id::text,''), COALESCE(c.model_code,''), COALESCE(c.instruction,''),
    COALESCE(CASE WHEN c.status='running' AND c.expires_at<=NOW() THEN '封面生成中断，请重试。' ELSE c.error_message END, ''),
    COALESCE(c.cover_id::text,''),
    COALESCE(c.candidate_id::text,'')`

func scanDramaCoverState(row pgx.Row) (drama.CoverGeneration, error) {
	var state drama.CoverGeneration
	err := row.Scan(&state.Status, &state.JobID, &state.ModelCode, &state.Instruction, &state.Error, &state.CoverID, &state.CandidateID)
	return state, err
}

func (r *DramaRepository) GetProjectCoverGeneration(ctx context.Context, scope drama.CoverScope) (drama.CoverGeneration, error) {
	state, err := scanDramaCoverState(r.db.QueryRow(ctx, `SELECT `+dramaCoverStateColumns+`
		FROM drama_projects p LEFT JOIN drama_project_covers c ON c.project_id=p.id
		WHERE p.organization_id=$1 AND p.owner_id=$2 AND p.id=$3`, scope.OrganizationID, scope.UserID, scope.ProjectID))
	if errors.Is(err, pgx.ErrNoRows) {
		return drama.CoverGeneration{}, drama.ErrNotFound
	}
	if err != nil {
		return drama.CoverGeneration{}, fmt.Errorf("读取漫剧封面状态失败: %w", err)
	}
	return state, nil
}

func (r *DramaRepository) ReserveProjectCoverGeneration(ctx context.Context, scope drama.CoverScope, previousJobID, jobID string, input drama.CoverGenerationInput, expiresAt time.Time) (drama.CoverGeneration, bool, error) {
	state, err := scanDramaCoverState(r.db.QueryRow(ctx, `INSERT INTO drama_project_covers AS c
		(project_id,job_id,model_code,status,expires_at,instruction)
		SELECT id,$4,$5,'running',$6,$7 FROM drama_projects WHERE organization_id=$1 AND owner_id=$2 AND id=$3
		ON CONFLICT (project_id) DO UPDATE SET job_id=EXCLUDED.job_id,model_code=EXCLUDED.model_code,
			status='running',error_message='',expires_at=EXCLUDED.expires_at,instruction=EXCLUDED.instruction
		WHERE COALESCE(c.job_id::text,'')=$8 AND c.candidate_id IS NULL AND (c.status<>'running' OR c.expires_at<=NOW())
		RETURNING `+dramaCoverStateColumns, scope.OrganizationID, scope.UserID, scope.ProjectID, jobID, input.ModelCode, expiresAt, input.Instruction, previousJobID))
	if errors.Is(err, pgx.ErrNoRows) {
		state, err = r.GetProjectCoverGeneration(ctx, scope)
		return state, false, err
	}
	if err != nil {
		return drama.CoverGeneration{}, false, fmt.Errorf("创建漫剧封面任务失败: %w", err)
	}
	return state, true, nil
}

func (r *DramaRepository) FinishProjectCoverGeneration(ctx context.Context, scope drama.CoverScope, jobID string, image drama.CoverImage, message string) error {
	var updated bool
	err := r.db.QueryRow(ctx, `WITH finished AS (
		UPDATE drama_project_covers c SET
			status=CASE WHEN $5='' THEN 'succeeded' ELSE 'failed' END,error_message=$5,
			cover_id=CASE WHEN $5='' THEN COALESCE(c.cover_id,c.job_id) ELSE c.cover_id END,
			candidate_id=CASE WHEN $5='' AND c.cover_id IS NOT NULL THEN c.job_id END
		FROM drama_projects p WHERE p.id=c.project_id AND p.organization_id=$1 AND p.owner_id=$2 AND p.id=$3
		AND c.job_id=$4 AND c.status='running' AND c.expires_at>NOW()
		RETURNING c.project_id,c.job_id
	), archived AS (
		INSERT INTO drama_project_cover_revisions (project_id,revision,source,mime_type,image_data)
		SELECT project_id,job_id,'ai',$6,$7 FROM finished WHERE $5=''
	)
	SELECT EXISTS (SELECT 1 FROM finished)`, scope.OrganizationID, scope.UserID, scope.ProjectID, jobID, message, image.MIME, image.Data).Scan(&updated)
	if err != nil {
		return fmt.Errorf("保存漫剧封面任务结果失败: %w", err)
	}
	if !updated {
		return drama.ErrCoverConflict
	}
	return nil
}

func (r *DramaRepository) SelectProjectCover(ctx context.Context, scope drama.CoverScope, input drama.CoverSelection) (drama.CoverGeneration, error) {
	state, err := scanDramaCoverState(r.db.QueryRow(ctx, `UPDATE drama_project_covers c SET
		cover_id=CASE WHEN $5='candidate' THEN c.job_id ELSE c.cover_id END,
		candidate_id=NULL
		FROM drama_projects p WHERE p.id=c.project_id AND p.organization_id=$1 AND p.owner_id=$2 AND p.id=$3
		AND c.job_id=$4 AND c.status='succeeded'
		AND (c.candidate_id IS NOT NULL OR ($5='candidate' AND c.cover_id=c.job_id) OR ($5='current' AND c.cover_id<>c.job_id))
		RETURNING `+dramaCoverStateColumns, scope.OrganizationID, scope.UserID, scope.ProjectID, input.JobID, input.Choice))
	if errors.Is(err, pgx.ErrNoRows) {
		if _, err := r.GetProjectCoverGeneration(ctx, scope); err != nil {
			return drama.CoverGeneration{}, err
		}
		return drama.CoverGeneration{}, drama.ErrCoverConflict
	}
	if err != nil {
		return drama.CoverGeneration{}, fmt.Errorf("确认漫剧封面失败: %w", err)
	}
	return state, nil
}

func (r *DramaRepository) ReadProjectCoverImage(ctx context.Context, scope drama.CoverScope, revision string) (drama.CoverImage, error) {
	var image drama.CoverImage
	err := r.db.QueryRow(ctx, `SELECT h.image_data,h.mime_type
		FROM drama_project_cover_revisions h JOIN drama_projects p ON p.id=h.project_id
		WHERE p.organization_id=$1 AND p.owner_id=$2 AND p.id=$3 AND h.revision=$4`,
		scope.OrganizationID, scope.UserID, scope.ProjectID, revision).Scan(&image.Data, &image.MIME)
	if errors.Is(err, pgx.ErrNoRows) {
		return drama.CoverImage{}, drama.ErrNotFound
	}
	if err != nil {
		return drama.CoverImage{}, fmt.Errorf("读取漫剧封面图片失败: %w", err)
	}
	return image, nil
}

func (r *DramaRepository) ListProjectCoverHistory(ctx context.Context, scope drama.CoverScope) ([]drama.CoverRevision, error) {
	rows, err := r.db.Query(ctx, `SELECT COALESCE(h.revision::text,''), COALESCE(h.source,''),
		COALESCE(h.created_at,'epoch'), COALESCE(h.revision=c.cover_id,false)
		FROM drama_projects p LEFT JOIN drama_project_cover_revisions h ON h.project_id=p.id
		LEFT JOIN drama_project_covers c ON c.project_id=p.id
		WHERE p.organization_id=$1 AND p.owner_id=$2 AND p.id=$3
		ORDER BY h.created_at DESC,h.revision DESC`, scope.OrganizationID, scope.UserID, scope.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("查询漫剧封面历史失败: %w", err)
	}
	defer rows.Close()
	items := make([]drama.CoverRevision, 0)
	found := false
	for rows.Next() {
		found = true
		var item drama.CoverRevision
		if err := rows.Scan(&item.Revision, &item.Source, &item.CreatedAt, &item.Current); err != nil {
			return nil, fmt.Errorf("解析漫剧封面历史失败: %w", err)
		}
		if item.Revision != "" {
			items = append(items, item)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("读取漫剧封面历史失败: %w", err)
	}
	if !found {
		return nil, drama.ErrNotFound
	}
	return items, nil
}

func (r *DramaRepository) ApplyProjectCoverHistory(ctx context.Context, scope drama.CoverScope, revision string) (drama.CoverGeneration, error) {
	state, err := scanDramaCoverState(r.db.QueryRow(ctx, `UPDATE drama_project_covers c SET
		cover_id=h.revision, candidate_id=CASE WHEN c.candidate_id<>h.revision THEN c.candidate_id END
		FROM drama_projects p JOIN drama_project_cover_revisions h ON h.project_id=p.id
		WHERE c.project_id=p.id AND p.organization_id=$1 AND p.owner_id=$2 AND p.id=$3 AND h.revision=$4
		RETURNING `+dramaCoverStateColumns, scope.OrganizationID, scope.UserID, scope.ProjectID, revision))
	if errors.Is(err, pgx.ErrNoRows) {
		return drama.CoverGeneration{}, drama.ErrNotFound
	}
	if err != nil {
		return drama.CoverGeneration{}, fmt.Errorf("采用漫剧历史封面失败: %w", err)
	}
	return state, nil
}

func (r *DramaRepository) SaveProjectCoverUpload(ctx context.Context, scope drama.CoverScope, revision string, image drama.CoverImage) (drama.CoverGeneration, error) {
	state, err := scanDramaCoverState(r.db.QueryRow(ctx, `WITH archived AS (
		INSERT INTO drama_project_cover_revisions (project_id,revision,source,mime_type,image_data)
		SELECT id,$4,'upload',$5,$6 FROM drama_projects WHERE organization_id=$1 AND owner_id=$2 AND id=$3
		RETURNING project_id,revision
	)
	INSERT INTO drama_project_covers AS c (project_id,model_code,status,expires_at,cover_id)
	SELECT project_id,'','idle',NOW(),revision FROM archived
	ON CONFLICT (project_id) DO UPDATE SET cover_id=EXCLUDED.cover_id
	RETURNING `+dramaCoverStateColumns, scope.OrganizationID, scope.UserID, scope.ProjectID, revision, image.MIME, image.Data))
	if errors.Is(err, pgx.ErrNoRows) {
		return drama.CoverGeneration{}, drama.ErrNotFound
	}
	if err != nil {
		return drama.CoverGeneration{}, fmt.Errorf("保存上传的漫剧封面失败: %w", err)
	}
	return state, nil
}
