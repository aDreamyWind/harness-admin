package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"harness-admin/internal/tryon"
)

type TryOnRepository struct{ pool *pgxpool.Pool }

func NewTryOnRepository(pool *pgxpool.Pool) *TryOnRepository { return &TryOnRepository{pool: pool} }

func (r *TryOnRepository) ListAssets(ctx context.Context, owner tryon.Owner) ([]tryon.Asset, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, kind, name, category, created_at, deleted_at IS NOT NULL FROM tryon_assets
		WHERE organization_id = $1 AND owner_id = $2 ORDER BY created_at DESC, id DESC`, owner.OrganizationID, owner.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]tryon.Asset, 0)
	for rows.Next() {
		var item tryon.Asset
		if err := rows.Scan(&item.ID, &item.Kind, &item.Name, &item.Category, &item.CreatedAt, &item.Deleted); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *TryOnRepository) SaveAsset(ctx context.Context, owner tryon.Owner, item tryon.AssetImage) (tryon.Asset, error) {
	err := r.pool.QueryRow(ctx, `INSERT INTO tryon_assets (id, organization_id, owner_id, kind, name, category, image_mime, image_data)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING created_at`, item.ID, owner.OrganizationID, owner.UserID, item.Kind,
		item.Name, item.Category, item.MIME, item.Data).Scan(&item.CreatedAt)
	return item.Asset, err
}

func (r *TryOnRepository) DeleteAsset(ctx context.Context, owner tryon.Owner, id string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// 与 CreateJob 的共享锁串行，防止删除后出现新的引用。
	tag, err := tx.Exec(ctx, `UPDATE tryon_assets SET deleted_at = NOW()
		WHERE organization_id = $1 AND owner_id = $2 AND id = $3 AND deleted_at IS NULL`,
		owner.OrganizationID, owner.UserID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return tryon.ErrNotFound
	}
	// 已引用素材保留为历史快照，未引用的图片随素材删除。
	_, err = tx.Exec(ctx, `DELETE FROM tryon_assets a WHERE a.organization_id = $1 AND a.owner_id = $2 AND a.id = $3
		AND NOT EXISTS (
			SELECT 1 FROM tryon_jobs j WHERE j.organization_id = a.organization_id AND j.owner_id = a.owner_id
				AND a.id IN (j.person_id, j.garment_id, j.pose_id)
		)
		AND NOT EXISTS (
			SELECT 1 FROM tryon_job_garments g WHERE g.organization_id = a.organization_id AND g.owner_id = a.owner_id
				AND g.garment_id = a.id
		)`, owner.OrganizationID, owner.UserID, id)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *TryOnRepository) Asset(ctx context.Context, owner tryon.Owner, id string) (tryon.AssetImage, error) {
	var item tryon.AssetImage
	err := r.pool.QueryRow(ctx, `SELECT id, kind, name, category, created_at, image_mime, image_data, deleted_at IS NOT NULL FROM tryon_assets
		WHERE organization_id = $1 AND owner_id = $2 AND id = $3`, owner.OrganizationID, owner.UserID, id).
		Scan(&item.ID, &item.Kind, &item.Name, &item.Category, &item.CreatedAt, &item.MIME, &item.Data, &item.Deleted)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tryon.ErrNotFound
	}
	return item, err
}

func (r *TryOnRepository) expireJobs(ctx context.Context, owner tryon.Owner) error {
	_, err := r.pool.Exec(ctx, `UPDATE tryon_jobs SET status = 'failed', error_message = '任务超时或服务已重启，请重新试穿'
		WHERE organization_id = $1 AND owner_id = $2 AND status = 'running' AND expires_at <= NOW()`, owner.OrganizationID, owner.UserID)
	return err
}

func (r *TryOnRepository) ListJobs(ctx context.Context, owner tryon.Owner) ([]tryon.Job, error) {
	if err := r.expireJobs(ctx, owner); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `SELECT `+tryOnJobColumns+`
		FROM tryon_jobs j WHERE j.organization_id = $1 AND j.owner_id = $2 ORDER BY j.created_at DESC, j.id DESC LIMIT 50`,
		owner.OrganizationID, owner.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]tryon.Job, 0)
	for rows.Next() {
		var item tryon.Job
		if err := rows.Scan(tryOnJobFields(&item)...); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// 老版本可能在迁移后仍有一个单件任务写入，所以保留首件衣物字段作为回退。
const tryOnJobColumns = `j.id, j.person_id, j.garment_id,
	COALESCE((SELECT ARRAY_AGG(g.garment_id::text ORDER BY g.position)
		FROM tryon_job_garments g WHERE g.job_id = j.id), ARRAY[j.garment_id::text]),
	COALESCE(j.pose_id::text, ''), j.model_code, j.status, j.error_message, j.created_at`

func tryOnJobFields(job *tryon.Job) []any {
	return []any{&job.ID, &job.PersonID, &job.GarmentID, &job.GarmentIDs, &job.PoseID,
		&job.ModelCode, &job.Status, &job.Error, &job.CreatedAt}
}

// 同一用户的任务和套装串行保存素材快照，任务提交还在锁内检查队列额度。
func lockTryOnOwner(ctx context.Context, tx pgx.Tx, owner tryon.Owner) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('tryon_jobs:' || $1::text || ':' || $2::text, 0))`,
		owner.OrganizationID, owner.UserID)
	return err
}

func (r *TryOnRepository) CreateJob(ctx context.Context, owner tryon.Owner, job tryon.Job, _ time.Time, builtinAssets []tryon.AssetImage) (tryon.Job, error) {
	if len(job.GarmentIDs) == 0 && job.GarmentID != "" {
		job.GarmentIDs = []string{job.GarmentID}
	}
	if len(job.GarmentIDs) == 0 || len(job.GarmentIDs) > tryon.MaxTryOnGarments {
		return tryon.Job{}, tryon.ErrInvalid
	}
	job.GarmentID = job.GarmentIDs[0]
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return tryon.Job{}, err
	}
	defer tx.Rollback(ctx)
	if err := lockTryOnOwner(ctx, tx, owner); err != nil {
		return tryon.Job{}, err
	}
	var active int
	err = tx.QueryRow(ctx, `SELECT COUNT(*) FROM tryon_jobs WHERE organization_id = $1 AND owner_id = $2
		AND (status = 'queued' OR (status = 'running' AND expires_at > NOW()))`,
		owner.OrganizationID, owner.UserID).Scan(&active)
	if err != nil {
		return tryon.Job{}, err
	}
	if active >= tryon.MaxPendingTryOnJobs {
		return tryon.Job{}, tryon.ErrBusy
	}

	assetIDs := append([]string{job.PersonID}, job.GarmentIDs...)
	if job.PoseID != "" {
		assetIDs = append(assetIDs, job.PoseID)
	}
	if err := persistTryOnAssets(ctx, tx, owner, assetIDs, builtinAssets); err != nil {
		return tryon.Job{}, err
	}
	err = tx.QueryRow(ctx, `INSERT INTO tryon_jobs (id, organization_id, owner_id, person_id, garment_id, pose_id, model_code, status, expires_at)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, '')::uuid, $7, 'queued', NOW()) RETURNING status, created_at`,
		job.ID, owner.OrganizationID, owner.UserID, job.PersonID, job.GarmentID, job.PoseID, job.ModelCode).
		Scan(&job.Status, &job.CreatedAt)
	if err != nil {
		return tryon.Job{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO tryon_job_garments (organization_id, owner_id, job_id, garment_id, position)
		SELECT $1, $2, $3, g.id, g.position::smallint FROM UNNEST($4::text[]::uuid[]) WITH ORDINALITY AS g(id, position)`,
		owner.OrganizationID, owner.UserID, job.ID, job.GarmentIDs)
	if err != nil {
		return tryon.Job{}, err
	}
	return job, tx.Commit(ctx)
}

func persistTryOnAssets(ctx context.Context, tx pgx.Tx, owner tryon.Owner, ids []string, builtinAssets []tryon.AssetImage) error {
	// 内置素材的快照与采用该素材的任务或套装一起提交。
	for _, asset := range builtinAssets {
		if _, err := tx.Exec(ctx, `INSERT INTO tryon_assets (id, organization_id, owner_id, kind, name, category, image_mime, image_data, created_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT (id) DO NOTHING`,
			asset.ID, owner.OrganizationID, owner.UserID, asset.Kind, asset.Name, asset.Category, asset.MIME, asset.Data, asset.CreatedAt); err != nil {
			return err
		}
	}
	// 锁住所有可用素材直到引用提交，与 DeleteAsset 串行。
	var available int
	err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM (
		SELECT id FROM tryon_assets WHERE organization_id = $1 AND owner_id = $2 AND deleted_at IS NULL
			AND id = ANY($3::text[]::uuid[])
		ORDER BY id FOR SHARE
	) AS available_assets`, owner.OrganizationID, owner.UserID, ids).Scan(&available)
	if err != nil {
		return err
	}
	if available != len(ids) {
		return tryon.ErrNotFound
	}
	return nil
}

func (r *TryOnRepository) ClaimTryOnJob(ctx context.Context, lease time.Time) (tryon.TryOnWork, error) {
	// 中断的旧领取只标记失败，避免重启后自动发起重复的模型调用。
	_, err := r.pool.Exec(ctx, `WITH expired AS (
		SELECT id FROM tryon_jobs WHERE status = 'running' AND expires_at <= NOW() FOR UPDATE SKIP LOCKED
	)
	UPDATE tryon_jobs AS j SET status = 'failed', error_message = '任务超时或服务已重启，请重新试穿'
	FROM expired WHERE j.id = expired.id`)
	if err != nil {
		return tryon.TryOnWork{}, err
	}
	var work tryon.TryOnWork
	fields := append(tryOnJobFields(&work.Job), &work.Owner.OrganizationID, &work.Owner.UserID, &work.Lease)
	err = r.pool.QueryRow(ctx, `WITH next_job AS (
		SELECT id FROM tryon_jobs WHERE status = 'queued'
		ORDER BY created_at, id LIMIT 1 FOR UPDATE SKIP LOCKED
	), claimed AS (
		UPDATE tryon_jobs AS j SET status = 'running', expires_at = $1
		FROM next_job WHERE j.id = next_job.id RETURNING j.*
	)
	SELECT `+tryOnJobColumns+`, j.organization_id, j.owner_id, j.expires_at FROM claimed j`, lease).Scan(fields...)
	if errors.Is(err, pgx.ErrNoRows) {
		return tryon.TryOnWork{}, tryon.ErrNotFound
	}
	if err != nil {
		return tryon.TryOnWork{}, err
	}
	return work, nil
}

func (r *TryOnRepository) FinishJob(ctx context.Context, owner tryon.Owner, id string, image tryon.Image, failure string) error {
	return r.finishTryOnJob(ctx, &owner, id, nil, image, failure)
}

func (r *TryOnRepository) FinishTryOnJob(ctx context.Context, id string, lease time.Time, image tryon.Image, failure string) error {
	return r.finishTryOnJob(ctx, nil, id, &lease, image, failure)
}

func (r *TryOnRepository) finishTryOnJob(ctx context.Context, owner *tryon.Owner, id string, lease *time.Time, image tryon.Image, failure string) error {
	var orgID, userID any
	if owner != nil {
		orgID, userID = owner.OrganizationID, owner.UserID
	}
	status := "succeeded"
	if failure != "" {
		status = "failed"
		image = tryon.Image{}
	}
	tag, err := r.pool.Exec(ctx, `UPDATE tryon_jobs SET status = $5, image_mime = NULLIF($6, ''), image_data = $7, error_message = $8
		WHERE id = $1 AND status = 'running' AND expires_at > NOW()
			AND ($2::timestamptz IS NULL OR expires_at = $2)
			AND ($3::uuid IS NULL OR organization_id = $3) AND ($4::uuid IS NULL OR owner_id = $4)`,
		id, lease, orgID, userID, status, image.MIME, image.Data, failure)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("试穿任务 %s 已过期或结束：%w", id, tryon.ErrNotFound)
	}
	return nil
}

func (r *TryOnRepository) Result(ctx context.Context, owner tryon.Owner, id string) (tryon.Image, error) {
	var image tryon.Image
	err := r.pool.QueryRow(ctx, `SELECT image_mime, image_data FROM tryon_jobs WHERE organization_id = $1
		AND owner_id = $2 AND id = $3 AND status = 'succeeded'`, owner.OrganizationID, owner.UserID, id).Scan(&image.MIME, &image.Data)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tryon.ErrNotFound
	}
	return image, err
}
