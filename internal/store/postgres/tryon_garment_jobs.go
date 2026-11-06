package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"harness-admin/internal/tryon"
)

const garmentJobColumns = "id, name, category, model_code, status, error_message, created_at"

func garmentJobFields(job *tryon.GarmentJob) []any {
	return []any{&job.ID, &job.Name, &job.Category, &job.ModelCode, &job.Status, &job.Error, &job.CreatedAt}
}

// 提交和重试使用同一把用户锁，在事务内检查额度。
func lockGarmentQueue(ctx context.Context, tx pgx.Tx, owner tryon.Owner) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('tryon_garment_jobs:' || $1::text || ':' || $2::text, 0))`,
		owner.OrganizationID, owner.UserID)
	return err
}

func checkGarmentQueueLimit(ctx context.Context, tx pgx.Tx, owner tryon.Owner, additional int) error {
	var pending int
	err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM tryon_garment_jobs
		WHERE organization_id = $1 AND owner_id = $2 AND status IN ('queued', 'running')`,
		owner.OrganizationID, owner.UserID).Scan(&pending)
	if err != nil {
		return err
	}
	if pending+additional > tryon.MaxPendingGarmentJobs {
		return tryon.ErrGarmentJobLimit
	}
	return nil
}

func (r *TryOnRepository) CreateGarmentJobs(ctx context.Context, owner tryon.Owner, model string, items []tryon.GarmentJobItem, source tryon.Image) ([]tryon.GarmentJob, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err := lockGarmentQueue(ctx, tx, owner); err != nil {
		return nil, err
	}
	if err := checkGarmentQueueLimit(ctx, tx, owner, len(items)); err != nil {
		return nil, err
	}
	batchID := uuid.NewString()
	if _, err := tx.Exec(ctx, `INSERT INTO tryon_garment_batches (id, organization_id, owner_id, source_mime, source_data)
		VALUES ($1, $2, $3, $4, $5)`, batchID, owner.OrganizationID, owner.UserID, source.MIME, source.Data); err != nil {
		return nil, err
	}
	jobs := make([]tryon.GarmentJob, 0, len(items))
	for _, item := range items {
		var coords []float64
		if item.Box != nil {
			coords = item.Box[:]
		}
		job := tryon.GarmentJob{ID: uuid.NewString(), Name: item.Name, Category: item.Category, ModelCode: model, Status: "queued"}
		err := tx.QueryRow(ctx, `INSERT INTO tryon_garment_jobs (id, organization_id, owner_id, batch_id, name, category, target_box, model_code, status)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'queued') RETURNING created_at`,
			job.ID, owner.OrganizationID, owner.UserID, batchID, item.Name, item.Category, coords, model).Scan(&job.CreatedAt)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return jobs, nil
}

func (r *TryOnRepository) ClaimGarmentJob(ctx context.Context, lease time.Time) (tryon.GarmentWork, error) {
	// 过期任务可由用户重试，避免进程异常退出后自动发起重复的模型调用。
	_, err := r.pool.Exec(ctx, `WITH expired AS (
		SELECT id FROM tryon_garment_jobs
		WHERE status = 'running' AND lease_expires_at <= NOW()
		FOR UPDATE SKIP LOCKED
	)
	UPDATE tryon_garment_jobs AS j SET status = 'failed', error_message = '生成超时或服务已重启，请重试',
		lease_expires_at = NULL, updated_at = NOW()
	FROM expired WHERE j.id = expired.id`)
	if err != nil {
		return tryon.GarmentWork{}, err
	}
	var work tryon.GarmentWork
	var coords []float64
	fields := append(garmentJobFields(&work.Job), &work.Owner.OrganizationID, &work.Owner.UserID,
		&coords, &work.Lease, &work.Source.MIME, &work.Source.Data)
	err = r.pool.QueryRow(ctx, `WITH next_job AS (
		SELECT id FROM tryon_garment_jobs WHERE status = 'queued'
		ORDER BY updated_at, id LIMIT 1 FOR UPDATE SKIP LOCKED
	), claimed AS (
		UPDATE tryon_garment_jobs AS j SET status = 'running', lease_expires_at = $1, updated_at = NOW()
		FROM next_job WHERE j.id = next_job.id RETURNING j.*
	)
	SELECT j.id, j.name, j.category, j.model_code, j.status, j.error_message, j.created_at,
		j.organization_id, j.owner_id, j.target_box, j.lease_expires_at, b.source_mime, b.source_data
	FROM claimed j JOIN tryon_garment_batches b
		ON b.id = j.batch_id AND b.organization_id = j.organization_id AND b.owner_id = j.owner_id`, lease).Scan(fields...)
	if errors.Is(err, pgx.ErrNoRows) {
		return tryon.GarmentWork{}, tryon.ErrNotFound
	}
	if err != nil {
		return tryon.GarmentWork{}, err
	}
	if coords != nil {
		if len(coords) != len(tryon.Box{}) {
			return tryon.GarmentWork{}, fmt.Errorf("衣物生成任务 %s 的目标框坐标无效", work.Job.ID)
		}
		box := tryon.Box(coords)
		work.Box = &box
	}
	return work, nil
}

func (r *TryOnRepository) FinishGarmentJob(ctx context.Context, id string, lease time.Time, result tryon.Image, failure string) error {
	status := "succeeded"
	if failure != "" {
		status = "failed"
		result = tryon.Image{}
	}
	tag, err := r.pool.Exec(ctx, `UPDATE tryon_garment_jobs SET status = $3, error_message = $4,
		image_mime = NULLIF($5, ''), image_data = $6, lease_expires_at = NULL, updated_at = NOW()
		WHERE id = $1 AND status = 'running' AND lease_expires_at = $2 AND lease_expires_at > NOW()`,
		id, lease, status, failure, result.MIME, result.Data)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return tryon.ErrGarmentJobState
	}
	return nil
}

func (r *TryOnRepository) ListGarmentJobs(ctx context.Context, owner tryon.Owner) ([]tryon.GarmentJob, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+garmentJobColumns+` FROM tryon_garment_jobs
		WHERE organization_id = $1 AND owner_id = $2 AND status <> 'saved'
		ORDER BY (status IN ('queued', 'running')) DESC, created_at DESC, id DESC LIMIT $3`,
		owner.OrganizationID, owner.UserID, tryon.MaxPendingGarmentJobs+50)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := make([]tryon.GarmentJob, 0)
	for rows.Next() {
		var job tryon.GarmentJob
		if err := rows.Scan(garmentJobFields(&job)...); err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (r *TryOnRepository) GarmentJobResult(ctx context.Context, owner tryon.Owner, id string) (tryon.Image, error) {
	var image tryon.Image
	var status string
	err := r.pool.QueryRow(ctx, `SELECT status, COALESCE(image_mime, ''), image_data FROM tryon_garment_jobs
		WHERE organization_id = $1 AND owner_id = $2 AND id = $3`, owner.OrganizationID, owner.UserID, id).
		Scan(&status, &image.MIME, &image.Data)
	if errors.Is(err, pgx.ErrNoRows) {
		return tryon.Image{}, tryon.ErrNotFound
	}
	if err != nil {
		return tryon.Image{}, err
	}
	if status != "succeeded" {
		return tryon.Image{}, tryon.ErrGarmentJobState
	}
	return image, nil
}

func (r *TryOnRepository) SaveGarmentJob(ctx context.Context, owner tryon.Owner, id string) (tryon.Asset, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return tryon.Asset{}, err
	}
	defer tx.Rollback(ctx)
	asset := tryon.Asset{ID: id, Kind: tryon.Garment}
	var status string
	var result tryon.Image
	err = tx.QueryRow(ctx, `SELECT status, name, category, COALESCE(image_mime, ''), image_data FROM tryon_garment_jobs
		WHERE organization_id = $1 AND owner_id = $2 AND id = $3 FOR UPDATE`, owner.OrganizationID, owner.UserID, id).
		Scan(&status, &asset.Name, &asset.Category, &result.MIME, &result.Data)
	if errors.Is(err, pgx.ErrNoRows) {
		return tryon.Asset{}, tryon.ErrNotFound
	}
	if err != nil {
		return tryon.Asset{}, err
	}
	switch status {
	case "saved":
		// 并发或重复确认返回同一份素材，已经删除的素材不会被恢复。
		err = tx.QueryRow(ctx, `SELECT name, category, created_at FROM tryon_assets
			WHERE organization_id = $1 AND owner_id = $2 AND id = $3 AND deleted_at IS NULL`,
			owner.OrganizationID, owner.UserID, id).Scan(&asset.Name, &asset.Category, &asset.CreatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return tryon.Asset{}, tryon.ErrNotFound
		}
	case "succeeded":
		err = tx.QueryRow(ctx, `INSERT INTO tryon_assets (id, organization_id, owner_id, kind, name, category, image_mime, image_data)
			VALUES ($1, $2, $3, 'garment', $4, $5, $6, $7) RETURNING created_at`,
			id, owner.OrganizationID, owner.UserID, asset.Name, asset.Category, result.MIME, result.Data).Scan(&asset.CreatedAt)
		if err != nil {
			return tryon.Asset{}, err
		}
		_, err = tx.Exec(ctx, `UPDATE tryon_garment_jobs SET status = 'saved', image_mime = NULL,
			image_data = NULL, updated_at = NOW() WHERE id = $1`, id)
	default:
		return tryon.Asset{}, tryon.ErrGarmentJobState
	}
	if err != nil {
		return tryon.Asset{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return tryon.Asset{}, err
	}
	return asset, nil
}

func (r *TryOnRepository) RetryGarmentJob(ctx context.Context, owner tryon.Owner, id string) (tryon.GarmentJob, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return tryon.GarmentJob{}, err
	}
	defer tx.Rollback(ctx)
	if err := lockGarmentQueue(ctx, tx, owner); err != nil {
		return tryon.GarmentJob{}, err
	}
	var status string
	err = tx.QueryRow(ctx, `SELECT status FROM tryon_garment_jobs
		WHERE organization_id = $1 AND owner_id = $2 AND id = $3 FOR UPDATE`,
		owner.OrganizationID, owner.UserID, id).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return tryon.GarmentJob{}, tryon.ErrNotFound
	}
	if err != nil {
		return tryon.GarmentJob{}, err
	}
	if status != "failed" {
		return tryon.GarmentJob{}, tryon.ErrGarmentJobState
	}
	if err := checkGarmentQueueLimit(ctx, tx, owner, 1); err != nil {
		return tryon.GarmentJob{}, err
	}
	var job tryon.GarmentJob
	err = tx.QueryRow(ctx, `UPDATE tryon_garment_jobs SET status = 'queued', error_message = '', updated_at = NOW()
		WHERE id = $1 RETURNING `+garmentJobColumns, id).Scan(garmentJobFields(&job)...)
	if err != nil {
		return tryon.GarmentJob{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return tryon.GarmentJob{}, err
	}
	return job, nil
}

func (r *TryOnRepository) PruneGarmentJobs(ctx context.Context, cutoff time.Time) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// 只移除过期的已结束任务，仍有任务的批次需要保留共享原图。
	if _, err := tx.Exec(ctx, `DELETE FROM tryon_garment_jobs
		WHERE updated_at < $1 AND status IN ('succeeded', 'failed', 'saved')`, cutoff); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM tryon_garment_batches AS b WHERE b.created_at < $1 AND NOT EXISTS (
		SELECT 1 FROM tryon_garment_jobs AS j WHERE j.batch_id = b.id
	)`, cutoff); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
