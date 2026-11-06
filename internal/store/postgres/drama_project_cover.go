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
    COALESCE(c.job_id::text,''), COALESCE(c.model_code,''),
    COALESCE(CASE WHEN c.status='running' AND c.expires_at<=NOW() THEN '封面生成中断，请重试。' ELSE c.error_message END, ''),
    COALESCE(c.cover_id::text,''),
    CASE WHEN c.candidate_data IS NOT NULL THEN c.job_id::text ELSE '' END`

func scanDramaCoverState(row pgx.Row) (drama.CoverGeneration, error) {
	var state drama.CoverGeneration
	err := row.Scan(&state.Status, &state.JobID, &state.ModelCode, &state.Error, &state.CoverID, &state.CandidateID)
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

func (r *DramaRepository) ReserveProjectCoverGeneration(ctx context.Context, scope drama.CoverScope, previousJobID, jobID, modelCode string, expiresAt time.Time) (drama.CoverGeneration, bool, error) {
	state, err := scanDramaCoverState(r.db.QueryRow(ctx, `INSERT INTO drama_project_covers AS c
		(project_id,job_id,model_code,status,expires_at)
		SELECT id,$4,$5,'running',$6 FROM drama_projects WHERE organization_id=$1 AND owner_id=$2 AND id=$3
		ON CONFLICT (project_id) DO UPDATE SET job_id=EXCLUDED.job_id,model_code=EXCLUDED.model_code,
			status='running',error_message='',expires_at=EXCLUDED.expires_at
		WHERE c.job_id::text=$7 AND c.candidate_data IS NULL AND (c.status<>'running' OR c.expires_at<=NOW())
		RETURNING `+dramaCoverStateColumns, scope.OrganizationID, scope.UserID, scope.ProjectID, jobID, modelCode, expiresAt, previousJobID))
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
	command, err := r.db.Exec(ctx, `UPDATE drama_project_covers c SET
		status=CASE WHEN $5='' THEN 'succeeded' ELSE 'failed' END,error_message=$5,
		cover_id=CASE WHEN $5='' AND c.cover_id IS NULL THEN c.job_id ELSE c.cover_id END,
		cover_data=CASE WHEN $5='' AND c.cover_id IS NULL THEN $6::bytea ELSE c.cover_data END,
		candidate_data=CASE WHEN $5='' AND c.cover_id IS NOT NULL THEN $6::bytea END
		FROM drama_projects p WHERE p.id=c.project_id AND p.organization_id=$1 AND p.owner_id=$2 AND p.id=$3
		AND c.job_id=$4 AND c.status='running' AND c.expires_at>NOW()`, scope.OrganizationID, scope.UserID, scope.ProjectID, jobID, message, image.Data)
	if err != nil {
		return fmt.Errorf("保存漫剧封面任务结果失败: %w", err)
	}
	if command.RowsAffected() == 0 {
		return drama.ErrCoverConflict
	}
	return nil
}

func (r *DramaRepository) SelectProjectCover(ctx context.Context, scope drama.CoverScope, input drama.CoverSelection) (drama.CoverGeneration, error) {
	state, err := scanDramaCoverState(r.db.QueryRow(ctx, `UPDATE drama_project_covers c SET
		cover_id=CASE WHEN $5='candidate' THEN c.job_id ELSE c.cover_id END,
		cover_data=CASE WHEN $5='candidate' AND c.candidate_data IS NOT NULL THEN c.candidate_data ELSE c.cover_data END,
		candidate_data=NULL
		FROM drama_projects p WHERE p.id=c.project_id AND p.organization_id=$1 AND p.owner_id=$2 AND p.id=$3
		AND c.job_id=$4 AND c.status='succeeded'
		AND (c.candidate_data IS NOT NULL OR ($5='candidate' AND c.cover_id=c.job_id) OR ($5='current' AND c.cover_id<>c.job_id))
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
	var data []byte
	err := r.db.QueryRow(ctx, `SELECT CASE WHEN c.cover_id=$4 THEN c.cover_data ELSE c.candidate_data END
		FROM drama_project_covers c JOIN drama_projects p ON p.id=c.project_id
		WHERE p.organization_id=$1 AND p.owner_id=$2 AND p.id=$3 AND
		(c.cover_id=$4 OR (c.job_id=$4 AND c.candidate_data IS NOT NULL))`,
		scope.OrganizationID, scope.UserID, scope.ProjectID, revision).Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		return drama.CoverImage{}, drama.ErrNotFound
	}
	if err != nil {
		return drama.CoverImage{}, fmt.Errorf("读取漫剧封面图片失败: %w", err)
	}
	return drama.CoverImage{Data: data, MIME: "image/svg+xml"}, nil
}
