package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"harness-admin/internal/tryon"
)

type TryOnRepository struct{ pool *pgxpool.Pool }

func NewTryOnRepository(pool *pgxpool.Pool) *TryOnRepository { return &TryOnRepository{pool: pool} }

func (r *TryOnRepository) ListAssets(ctx context.Context, owner tryon.Owner) ([]tryon.Asset, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, kind, name, category, created_at FROM tryon_assets
		WHERE organization_id = $1 AND owner_id = $2 ORDER BY created_at DESC, id DESC`, owner.OrganizationID, owner.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]tryon.Asset, 0)
	for rows.Next() {
		var item tryon.Asset
		if err := rows.Scan(&item.ID, &item.Kind, &item.Name, &item.Category, &item.CreatedAt); err != nil {
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

func (r *TryOnRepository) Asset(ctx context.Context, owner tryon.Owner, id string) (tryon.AssetImage, error) {
	var item tryon.AssetImage
	err := r.pool.QueryRow(ctx, `SELECT id, kind, name, category, created_at, image_mime, image_data FROM tryon_assets
		WHERE organization_id = $1 AND owner_id = $2 AND id = $3`, owner.OrganizationID, owner.UserID, id).
		Scan(&item.ID, &item.Kind, &item.Name, &item.Category, &item.CreatedAt, &item.MIME, &item.Data)
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
	rows, err := r.pool.Query(ctx, `SELECT id, person_id, garment_id, COALESCE(pose_id::text,''), model_code, status, error_message, created_at
		FROM tryon_jobs WHERE organization_id = $1 AND owner_id = $2 ORDER BY created_at DESC, id DESC LIMIT 50`,
		owner.OrganizationID, owner.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]tryon.Job, 0)
	for rows.Next() {
		var item tryon.Job
		if err := rows.Scan(&item.ID, &item.PersonID, &item.GarmentID, &item.PoseID, &item.ModelCode, &item.Status, &item.Error, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *TryOnRepository) CreateJob(ctx context.Context, owner tryon.Owner, job tryon.Job, expires time.Time) (tryon.Job, error) {
	if err := r.expireJobs(ctx, owner); err != nil {
		return tryon.Job{}, err
	}
	err := r.pool.QueryRow(ctx, `INSERT INTO tryon_jobs (id, organization_id, owner_id, person_id, garment_id, pose_id, model_code, status, expires_at)
		VALUES ($1,$2,$3,$4,$5,NULLIF($6,'')::uuid,$7,'running',$8) RETURNING status, created_at`, job.ID, owner.OrganizationID,
		owner.UserID, job.PersonID, job.GarmentID, job.PoseID, job.ModelCode, expires).Scan(&job.Status, &job.CreatedAt)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "tryon_jobs_one_running_idx" {
		err = tryon.ErrBusy
	}
	return job, err
}

func (r *TryOnRepository) FinishJob(ctx context.Context, owner tryon.Owner, id string, image tryon.Image, failure string) error {
	status := "succeeded"
	if failure != "" {
		status = "failed"
	}
	tag, err := r.pool.Exec(ctx, `UPDATE tryon_jobs SET status = $4, image_mime = NULLIF($5,''), image_data = $6, error_message = $7
		WHERE organization_id = $1 AND owner_id = $2 AND id = $3 AND status = 'running' AND expires_at > NOW()`,
		owner.OrganizationID, owner.UserID, id, status, image.MIME, image.Data, failure)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("试穿任务 %s 已过期或结束", id)
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
