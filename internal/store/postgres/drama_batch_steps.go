package postgres

import (
	"context"
	"fmt"

	"harness-admin/internal/drama"
)

func (r *DramaRepository) ListBatchSteps(ctx context.Context, org, owner, project, batch string) ([]drama.BatchStep, error) {
	if _, err := r.GetBatch(ctx, org, owner, project, batch); err != nil {
		return nil, err
	}
	rows, err := r.db.Query(ctx, `SELECT s.stage,s.chunk_number,s.chunk_total,s.status,s.result_text,s.error_message,s.duration_ms,s.updated_at,s.started_at
      FROM drama_batch_steps s JOIN drama_batches b ON b.id=s.batch_id
      WHERE s.batch_id=$1 AND s.analysis_revision=b.analysis_revision
      ORDER BY CASE s.stage WHEN 'art' THEN 0 WHEN 'story' THEN 1 WHEN 'plan' THEN 2 ELSE 3 END,s.chunk_number,CASE s.stage WHEN 'director' THEN 0 ELSE 1 END`, batch)
	if err != nil {
		return nil, fmt.Errorf("读取剧本解析步骤失败: %w", err)
	}
	defer rows.Close()
	result := make([]drama.BatchStep, 0)
	for rows.Next() {
		var step drama.BatchStep
		if err = rows.Scan(&step.Stage, &step.ChunkNumber, &step.ChunkTotal, &step.Status, &step.ResultText, &step.ErrorMessage, &step.DurationMS, &step.UpdatedAt, &step.StartedAt); err != nil {
			return nil, fmt.Errorf("读取剧本解析步骤失败: %w", err)
		}
		result = append(result, step)
	}
	return result, rows.Err()
}

func (r *DramaRepository) StartBatchStep(ctx context.Context, org, owner, project, batch string, revision int, step drama.BatchStep) error {
	tag, err := r.db.Exec(ctx, `INSERT INTO drama_batch_steps(batch_id,analysis_revision,stage,chunk_number,chunk_total,status)
      SELECT b.id,$5,$6,$7,$8,'running' FROM drama_batches b JOIN drama_projects p ON p.id=b.project_id
      WHERE p.organization_id=$1 AND p.owner_id=$2 AND p.id=$3 AND b.id=$4
      AND b.status='running' AND b.revision=$5 AND b.analysis_revision=$5
      ON CONFLICT (batch_id,analysis_revision,stage,chunk_number) DO UPDATE SET
      status='running',result_text='',error_message='',chunk_total=EXCLUDED.chunk_total,duration_ms=0,started_at=NOW(),updated_at=NOW()`,
		org, owner, project, batch, revision, step.Stage, step.ChunkNumber, step.ChunkTotal)
	if err != nil {
		return fmt.Errorf("开始解析步骤失败: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return drama.ErrBatchConflict
	}
	return nil
}

func (r *DramaRepository) CompleteBatchStep(ctx context.Context, org, owner, project, batch string, revision int, step drama.BatchStep) error {
	tag, err := r.db.Exec(ctx, `UPDATE drama_batch_steps s SET status='completed',result_text=$8,error_message='',duration_ms=$9,updated_at=NOW()
      FROM drama_batches b JOIN drama_projects p ON p.id=b.project_id
      WHERE p.organization_id=$1 AND p.owner_id=$2 AND p.id=$3 AND b.id=$4
      AND b.status='running' AND b.revision=$5 AND b.analysis_revision=$5
      AND s.batch_id=b.id AND s.analysis_revision=$5 AND s.stage=$6 AND s.chunk_number=$7 AND s.status='running'`,
		org, owner, project, batch, revision, step.Stage, step.ChunkNumber, step.ResultText, step.DurationMS)
	if err != nil {
		return fmt.Errorf("保存解析步骤结果失败: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return drama.ErrBatchConflict
	}
	return nil
}

func (r *DramaRepository) FailBatchStep(ctx context.Context, job drama.BatchAnalysisJob, stage string, number int, reason string) error {
	tag, err := r.db.Exec(ctx, `UPDATE drama_batch_steps s SET status='failed',error_message=$7,updated_at=NOW()
 FROM drama_batches b JOIN drama_projects p ON p.id=b.project_id
 WHERE p.organization_id=$1 AND p.owner_id=$2 AND p.id=$3 AND b.id=$4 AND b.status='running'
 AND b.revision=$5 AND s.batch_id=b.id AND s.analysis_revision=$5 AND s.stage=$6 AND s.chunk_number=$8`, job.OrganizationID, job.OwnerID, job.Batch.ProjectID, job.Batch.ID, job.Batch.AnalysisRevision, stage, reason, number)
	if err != nil {
		return fmt.Errorf("保存失败步骤失败: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return drama.ErrBatchConflict
	}
	return nil
}
func (r *DramaRepository) AppendBatchEvent(ctx context.Context, job drama.BatchAnalysisJob, event drama.BatchEvent) error {
	tag, err := r.db.Exec(ctx, `INSERT INTO drama_batch_events(batch_id,analysis_revision,stage,step_number,kind,message)
 SELECT b.id,$5,$6,$7,$8,$9 FROM drama_batches b JOIN drama_projects p ON p.id=b.project_id
 WHERE p.organization_id=$1 AND p.owner_id=$2 AND p.id=$3 AND b.id=$4 AND b.revision=$5 AND b.analysis_revision=$5`, job.OrganizationID, job.OwnerID, job.Batch.ProjectID, job.Batch.ID, job.Batch.AnalysisRevision, event.Stage, event.StepNumber, event.Kind, event.Message)
	if err != nil {
		return fmt.Errorf("记录解析进度事件失败: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return drama.ErrBatchConflict
	}
	return nil
}
func (r *DramaRepository) ListBatchEvents(ctx context.Context, org, owner, project, batch string, after int64) ([]drama.BatchEvent, error) {
	if _, err := r.GetBatch(ctx, org, owner, project, batch); err != nil {
		return nil, err
	}
	rows, err := r.db.Query(ctx, `SELECT e.id,e.stage,e.step_number,e.kind,e.message,e.created_at FROM drama_batch_events e
 JOIN drama_batches b ON b.id=e.batch_id WHERE e.batch_id=$1 AND e.analysis_revision=b.analysis_revision AND e.id>$2 ORDER BY e.id LIMIT 100`, batch, after)
	if err != nil {
		return nil, fmt.Errorf("读取解析进度事件失败: %w", err)
	}
	defer rows.Close()
	events := make([]drama.BatchEvent, 0)
	for rows.Next() {
		var e drama.BatchEvent
		if err = rows.Scan(&e.ID, &e.Stage, &e.StepNumber, &e.Kind, &e.Message, &e.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}
