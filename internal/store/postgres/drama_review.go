package postgres

import (
	"context"
	"fmt"

	"harness-admin/internal/drama"
)

func (r *DramaRepository) ListReviewIssues(ctx context.Context, org, owner, project, batch string) ([]drama.ReviewIssue, error) {
	if _, err := r.GetBatch(ctx, org, owner, project, batch); err != nil {
		return nil, err
	}
	rows, err := r.db.Query(ctx, `SELECT i.id,i.run_id,i.episode_number,i.context_hash,i.issue_key,i.category,i.severity,i.message,i.source_quote,i.script_quote,i.source_start,i.source_end,i.status,i.created_at,i.updated_at
 FROM drama_review_issues i JOIN drama_batches b ON b.id=i.batch_id WHERE b.id=$1 AND i.run_id=b.analysis_run_id ORDER BY i.episode_number,i.created_at,i.id`, batch)
	if err != nil {
		return nil, fmt.Errorf("读取累计审校问题失败: %w", err)
	}
	defer rows.Close()
	items := []drama.ReviewIssue{}
	for rows.Next() {
		var i drama.ReviewIssue
		if err = rows.Scan(&i.ID, &i.RunID, &i.EpisodeNumber, &i.ContextHash, &i.Key, &i.Category, &i.Severity, &i.Message, &i.SourceQuote, &i.ScriptQuote, &i.SourceStart, &i.SourceEnd, &i.Status, &i.CreatedAt, &i.UpdatedAt); err != nil {
			return nil, fmt.Errorf("读取审校问题失败: %w", err)
		}
		items = append(items, i)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("读取审校问题失败: %w", err)
	}
	return items, nil
}
func (r *DramaRepository) SaveReviewIssues(ctx context.Context, job drama.BatchAnalysisJob, issues []drama.ReviewIssue) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var valid bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM drama_batches b JOIN drama_projects p ON p.id=b.project_id WHERE p.organization_id=$1 AND p.owner_id=$2 AND p.id=$3 AND b.id=$4 AND b.analysis_run_id=$5 AND b.status='running' AND b.revision=$6 AND b.analysis_revision=$6)`, job.OrganizationID, job.OwnerID, job.Batch.ProjectID, job.Batch.ID, job.Batch.AnalysisRunID, job.Batch.AnalysisRevision).Scan(&valid)
	if err != nil {
		return fmt.Errorf("核对审校运行失败: %w", err)
	}
	if !valid {
		return drama.ErrBatchConflict
	}
	for _, i := range issues {
		if i.RunID != job.Batch.AnalysisRunID {
			return drama.ErrBatchConflict
		}
		_, err = tx.Exec(ctx, `INSERT INTO drama_review_issues(id,batch_id,run_id,episode_number,context_hash,issue_key,category,severity,message,source_quote,script_quote,source_start,source_end,status)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
 ON CONFLICT(run_id,episode_number,context_hash,issue_key) DO UPDATE SET status=EXCLUDED.status,updated_at=NOW()`, i.ID, job.Batch.ID, i.RunID, i.EpisodeNumber, i.ContextHash, i.Key, i.Category, i.Severity, i.Message, i.SourceQuote, i.ScriptQuote, i.SourceStart, i.SourceEnd, i.Status)
		if err != nil {
			return fmt.Errorf("保存累计审校问题失败: %w", err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("提交审校问题失败: %w", err)
	}
	return nil
}
func (r *DramaRepository) SaveRepairedEpisode(ctx context.Context, job drama.BatchAnalysisJob, number int, result, hash string) error {
	tag, err := r.db.Exec(ctx, `UPDATE drama_batch_steps s SET status='completed',result_text=$7,input_hash=$8,error_message='',updated_at=NOW()
 FROM drama_batches b JOIN drama_projects p ON p.id=b.project_id WHERE p.organization_id=$1 AND p.owner_id=$2 AND p.id=$3 AND b.id=$4 AND b.analysis_run_id=$5 AND b.status='running' AND b.revision=$6 AND b.analysis_revision=$6
 AND s.batch_id=b.id AND s.analysis_revision=$6 AND s.stage='director' AND s.chunk_number=$9`, job.OrganizationID, job.OwnerID, job.Batch.ProjectID, job.Batch.ID, job.Batch.AnalysisRunID, job.Batch.AnalysisRevision, result, hash, number)
	if err != nil {
		return fmt.Errorf("保存局部修正剧本失败: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return drama.ErrBatchConflict
	}
	return nil
}
