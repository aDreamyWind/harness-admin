package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"harness-admin/internal/drama"
)

const dramaBatchColumns = `b.id, b.project_id, b.batch_number, b.start_offset, b.end_offset,
 b.selection_mode, b.previous_batch_id, b.boundary, b.status, b.error_message, b.revision, b.analysis_revision, b.created_at, b.updated_at`

func scanDramaBatch(row pgx.Row) (drama.Batch, error) {
	var item drama.Batch
	var previous sql.NullString
	err := row.Scan(&item.ID, &item.ProjectID, &item.Number, &item.StartOffset, &item.EndOffset,
		&item.SelectionMode, &previous, &item.Boundary, &item.Status, &item.ErrorMessage, &item.Revision, &item.AnalysisRevision, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return drama.Batch{}, drama.ErrNotFound
	}
	if err != nil {
		return drama.Batch{}, err
	}
	if previous.Valid {
		item.PreviousBatchID = previous.String
	}
	return item, nil
}

func (r *DramaRepository) GetSourceRange(ctx context.Context, organizationID, ownerID, projectID string, start, limit int) (drama.SourceRange, error) {
	var result drama.SourceRange
	err := r.db.QueryRow(ctx, `SELECT substr(source_text, $4::integer + 1, $5::integer), source_length
		FROM drama_projects WHERE organization_id=$1 AND owner_id=$2 AND id=$3`,
		organizationID, ownerID, projectID, start, limit).Scan(&result.Text, &result.TotalLength)
	if errors.Is(err, pgx.ErrNoRows) {
		return drama.SourceRange{}, drama.ErrNotFound
	}
	if err != nil {
		return drama.SourceRange{}, fmt.Errorf("读取剧本原文区间失败: %w", err)
	}
	return result, nil
}

func (r *DramaRepository) ListBatches(ctx context.Context, organizationID, ownerID, projectID string) ([]drama.Batch, error) {
	rows, err := r.db.Query(ctx, `SELECT `+dramaBatchColumns+` FROM drama_batches b
		JOIN drama_projects p ON p.id=b.project_id
		WHERE p.organization_id=$1 AND p.owner_id=$2 AND p.id=$3 ORDER BY b.batch_number`, organizationID, ownerID, projectID)
	if err != nil {
		return nil, fmt.Errorf("读取剧本批次失败: %w", err)
	}
	defer rows.Close()
	items := make([]drama.Batch, 0)
	for rows.Next() {
		item, err := scanDramaBatch(rows)
		if err != nil {
			return nil, fmt.Errorf("读取剧本批次失败: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("读取剧本批次失败: %w", err)
	}
	return items, nil
}

func (r *DramaRepository) GetBatch(ctx context.Context, organizationID, ownerID, projectID, batchID string) (drama.Batch, error) {
	item, err := scanDramaBatch(r.db.QueryRow(ctx, `SELECT `+dramaBatchColumns+` FROM drama_batches b
		JOIN drama_projects p ON p.id=b.project_id
		WHERE p.organization_id=$1 AND p.owner_id=$2 AND p.id=$3 AND b.id=$4`, organizationID, ownerID, projectID, batchID))
	if err != nil && !errors.Is(err, drama.ErrNotFound) {
		return drama.Batch{}, fmt.Errorf("读取剧本批次失败: %w", err)
	}
	return item, err
}

func (r *DramaRepository) CreateBatch(ctx context.Context, organizationID, ownerID string, item drama.Batch) (drama.Batch, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return drama.Batch{}, fmt.Errorf("创建剧本批次失败: %w", err)
	}
	defer tx.Rollback(ctx)
	var status string
	err = tx.QueryRow(ctx, `SELECT status FROM drama_projects WHERE organization_id=$1 AND owner_id=$2 AND id=$3 FOR UPDATE`,
		organizationID, ownerID, item.ProjectID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return drama.Batch{}, drama.ErrNotFound
	}
	if err != nil {
		return drama.Batch{}, fmt.Errorf("创建剧本批次失败: %w", err)
	}
	if status != "ready" {
		return drama.Batch{}, drama.ErrBatchNotReady
	}
	var previous any
	if item.PreviousBatchID != "" {
		var previousEnd int
		err = tx.QueryRow(ctx, `SELECT end_offset FROM drama_batches WHERE id=$1 AND project_id=$2 FOR UPDATE`, item.PreviousBatchID, item.ProjectID).Scan(&previousEnd)
		if errors.Is(err, pgx.ErrNoRows) {
			return drama.Batch{}, drama.ErrNotFound
		}
		if err != nil {
			return drama.Batch{}, fmt.Errorf("检查承接批次失败: %w", err)
		}
		if previousEnd != item.StartOffset {
			return drama.Batch{}, drama.ErrBatchConflict
		}
		previous = item.PreviousBatchID
	}
	var previousID sql.NullString
	err = tx.QueryRow(ctx, `INSERT INTO drama_batches
		(id, project_id, batch_number, start_offset, end_offset, selection_mode, previous_batch_id, boundary, status)
		VALUES ($1, $2, (SELECT COALESCE(MAX(batch_number), 0) + 1 FROM drama_batches WHERE project_id=$2), $3, $4, $5, $6, $7, 'pending')
		RETURNING id, project_id, batch_number, start_offset, end_offset, selection_mode,
		previous_batch_id, boundary, status, error_message, revision, analysis_revision, created_at, updated_at`,
		item.ID, item.ProjectID, item.StartOffset, item.EndOffset, item.SelectionMode, previous, item.Boundary).
		Scan(&item.ID, &item.ProjectID, &item.Number, &item.StartOffset, &item.EndOffset,
			&item.SelectionMode, &previousID, &item.Boundary, &item.Status, &item.ErrorMessage, &item.Revision, &item.AnalysisRevision, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return drama.Batch{}, fmt.Errorf("创建剧本批次失败: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return drama.Batch{}, fmt.Errorf("提交剧本批次失败: %w", err)
	}
	return item, nil
}

func (r *DramaRepository) UpdateBatchRange(ctx context.Context, organizationID, ownerID, projectID, batchID string, change drama.BatchRangeChange) (drama.Batch, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return drama.Batch{}, fmt.Errorf("修改批次范围失败: %w", err)
	}
	defer tx.Rollback(ctx)
	var item drama.Batch
	var previous sql.NullString
	err = tx.QueryRow(ctx, `UPDATE drama_batches b SET start_offset=$5, end_offset=$6, boundary='manual',
		status=CASE WHEN b.status IN ('ready','running','failed') THEN 'stale' ELSE b.status END,
		selection_mode=CASE WHEN b.start_offset<>$5 THEN 'manual' ELSE b.selection_mode END,
		previous_batch_id=CASE WHEN b.start_offset<>$5 THEN NULL ELSE b.previous_batch_id END,
		revision=b.revision+1, updated_at=NOW()
		FROM drama_projects p WHERE p.id=b.project_id AND p.organization_id=$1 AND p.owner_id=$2
		AND p.id=$3 AND b.id=$4 AND b.revision=$7
		RETURNING b.id, b.project_id, b.batch_number, b.start_offset, b.end_offset,
		b.selection_mode, b.previous_batch_id, b.boundary, b.status, b.error_message, b.revision, b.analysis_revision, b.created_at, b.updated_at`,
		organizationID, ownerID, projectID, batchID, change.StartOffset, change.EndOffset, change.Revision).
		Scan(&item.ID, &item.ProjectID, &item.Number, &item.StartOffset, &item.EndOffset,
			&item.SelectionMode, &previous, &item.Boundary, &item.Status, &item.ErrorMessage, &item.Revision, &item.AnalysisRevision, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		if _, lookupErr := r.GetBatch(ctx, organizationID, ownerID, projectID, batchID); lookupErr != nil {
			return drama.Batch{}, lookupErr
		}
		return drama.Batch{}, drama.ErrBatchConflict
	}
	if err != nil {
		return drama.Batch{}, fmt.Errorf("修改批次范围失败: %w", err)
	}
	if previous.Valid {
		item.PreviousBatchID = previous.String
	}
	_, err = tx.Exec(ctx, `WITH RECURSIVE dependent AS (
		SELECT id FROM drama_batches WHERE previous_batch_id=$1
		UNION ALL
		SELECT b.id FROM drama_batches b JOIN dependent d ON b.previous_batch_id=d.id
	)
	UPDATE drama_batches SET status='stale', updated_at=NOW() WHERE id IN (SELECT id FROM dependent)`, batchID)
	if err != nil {
		return drama.Batch{}, fmt.Errorf("标记后续批次需核对失败: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return drama.Batch{}, fmt.Errorf("提交批次范围修改失败: %w", err)
	}
	return item, nil
}
