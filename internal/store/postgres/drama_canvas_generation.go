package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"harness-admin/internal/drama"
)

const canvasGenerationColumns = `
	COALESCE(j.id::text, ''), COALESCE(j.node_id::text, ''), COALESCE(j.kind, ''), COALESCE(j.model_code, ''),
	COALESCE(CASE WHEN j.status = 'running' AND j.expires_at <= NOW() THEN 'failed' ELSE j.status END, ''),
	COALESCE(j.result_media_id::text, ''),
	COALESCE(CASE WHEN j.status = 'running' AND j.expires_at <= NOW()
		THEN '生成任务已超时，请重试' ELSE j.error_message END, ''),
	j.created_at,
	CASE WHEN j.status = 'running' AND j.expires_at <= NOW() THEN j.expires_at ELSE j.finished_at END`

func scanCanvasGeneration(row interface{ Scan(...any) error }) (*drama.CanvasGenerationJob, error) {
	var job drama.CanvasGenerationJob
	var createdAt *time.Time
	err := row.Scan(&job.ID, &job.NodeID, &job.Kind, &job.ModelCode, &job.Status,
		&job.ResultMediaID, &job.ErrorMessage, &createdAt, &job.FinishedAt)
	if err != nil || job.ID == "" {
		return nil, err
	}
	job.CreatedAt = *createdAt
	return &job, nil
}

func (r *DramaRepository) ReserveCanvasGeneration(ctx context.Context, scope drama.CanvasGenerationScope, revision int, job drama.CanvasGenerationJob, expiresAt time.Time) (drama.CanvasGenerationJob, bool, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return drama.CanvasGenerationJob{}, false, err
	}
	defer tx.Rollback(ctx)

	// 与画布布局保存共用行锁，防止使用已变更的连线或素材提交新任务。
	var currentRevision int
	err = tx.QueryRow(ctx, `SELECT b.revision FROM drama_canvas_boards b
		JOIN drama_projects p ON p.id=b.project_id
		WHERE p.id=$1 AND p.organization_id=$2 AND p.owner_id=$3
		FOR UPDATE OF b`, scope.ProjectID, scope.OrganizationID, scope.UserID).Scan(&currentRevision)
	if errors.Is(err, pgx.ErrNoRows) {
		return drama.CanvasGenerationJob{}, false, drama.ErrNotFound
	}
	if err != nil {
		return drama.CanvasGenerationJob{}, false, fmt.Errorf("锁定漫剧画布版本失败: %w", err)
	}

	current, err := scanCanvasGeneration(tx.QueryRow(ctx, `SELECT `+canvasGenerationColumns+`
		FROM drama_canvas_generation_jobs j WHERE j.project_id=$1 AND j.node_id=$2
		AND j.status='running' AND j.expires_at>NOW()`, scope.ProjectID, scope.NodeID))
	if err == nil {
		return *current, false, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return drama.CanvasGenerationJob{}, false, fmt.Errorf("读取运行中的画布生成任务失败: %w", err)
	}
	if currentRevision != revision {
		return drama.CanvasGenerationJob{}, false, drama.ErrCanvasConflict
	}

	saved, err := scanCanvasGeneration(tx.QueryRow(ctx, `INSERT INTO drama_canvas_generation_jobs AS j
		(project_id,node_id,id,kind,model_code,status,expires_at)
		VALUES($1,$2,$3,$4,$5,'running',$6)
		ON CONFLICT (project_id,node_id) DO UPDATE SET id=EXCLUDED.id,kind=EXCLUDED.kind,
			model_code=EXCLUDED.model_code,status='running',error_message='',created_at=NOW(),
			expires_at=EXCLUDED.expires_at,finished_at=NULL
		RETURNING `+canvasGenerationColumns, scope.ProjectID, scope.NodeID, job.ID, job.Kind, job.ModelCode, expiresAt))
	if err != nil {
		return drama.CanvasGenerationJob{}, false, fmt.Errorf("保存画布生成任务失败: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return drama.CanvasGenerationJob{}, false, err
	}
	return *saved, true, nil
}

func (r *DramaRepository) ReadCanvasGeneration(ctx context.Context, scope drama.CanvasGenerationScope) (*drama.CanvasGenerationJob, error) {
	job, err := scanCanvasGeneration(r.db.QueryRow(ctx, `SELECT `+canvasGenerationColumns+`
		FROM drama_projects p
		LEFT JOIN drama_canvas_generation_jobs j ON j.project_id=p.id AND j.node_id=$4
		WHERE p.id=$1 AND p.organization_id=$2 AND p.owner_id=$3`,
		scope.ProjectID, scope.OrganizationID, scope.UserID, scope.NodeID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, drama.ErrNotFound
	}
	return job, err
}

func (r *DramaRepository) ListCanvasGenerations(ctx context.Context, scope drama.CanvasGenerationScope) ([]drama.CanvasGenerationJob, error) {
	rows, err := r.db.Query(ctx, `SELECT `+canvasGenerationColumns+`
		FROM drama_projects p
		LEFT JOIN drama_canvas_generation_jobs j ON j.project_id=p.id
		WHERE p.id=$1 AND p.organization_id=$2 AND p.owner_id=$3 ORDER BY j.node_id`,
		scope.ProjectID, scope.OrganizationID, scope.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := []drama.CanvasGenerationJob{}
	found := false
	for rows.Next() {
		found = true
		job, err := scanCanvasGeneration(rows)
		if err != nil {
			return nil, err
		}
		if job != nil {
			jobs = append(jobs, *job)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if !found {
		return nil, drama.ErrNotFound
	}
	return jobs, nil
}

func (r *DramaRepository) FinishCanvasGeneration(ctx context.Context, scope drama.CanvasGenerationScope, jobID, errorMessage string) error {
	tag, err := r.db.Exec(ctx, `UPDATE drama_canvas_generation_jobs j SET
		status=CASE WHEN $6::text='' THEN 'succeeded' ELSE 'failed' END,
		result_media_id=CASE WHEN $6::text='' THEN j.id ELSE j.result_media_id END,
		error_message=$6,finished_at=NOW()
		FROM drama_projects p WHERE j.project_id=p.id AND p.id=$1 AND p.organization_id=$2 AND p.owner_id=$3
		AND j.node_id=$4 AND j.id=$5 AND j.status='running' AND j.expires_at>NOW()`,
		scope.ProjectID, scope.OrganizationID, scope.UserID, scope.NodeID, jobID, errorMessage)
	if err != nil {
		return fmt.Errorf("更新画布生成任务失败: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return drama.ErrCanvasConflict
	}
	return nil
}
