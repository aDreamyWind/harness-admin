package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"harness-admin/internal/drama"
)

const segmentVideoScopeSQL = `
 FROM drama_storyboard_segments s
 JOIN drama_storyboard_runs r ON r.id=s.run_id
 JOIN drama_projects p ON p.id=r.project_id
 WHERE p.organization_id=$1 AND p.owner_id=$2 AND p.id=$3 AND r.source_episode_id=$4 AND r.id=$5 AND s.id=$6`

func segmentVideoArgs(s drama.SegmentVideoScope) []any {
	return []any{s.OrganizationID, s.UserID, s.ProjectID, s.EpisodeID, s.RunID, s.SegmentID}
}

func (r *DramaRepository) CreateSegmentVideo(ctx context.Context, scope drama.SegmentVideoScope, job drama.SegmentVideo) (drama.SegmentVideo, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return job, err
	}
	defer tx.Rollback(ctx)
	args := append(segmentVideoArgs(scope), job.Revision)
	var id string
	err = tx.QueryRow(ctx, "SELECT s.id"+segmentVideoScopeSQL+`
 AND r.revision=$7 AND r.status IN ('ready','confirmed') AND s.status='ready'
 AND r.script_text=(SELECT script_text FROM drama_script_episodes WHERE id=r.episode_id)
 AND NOT EXISTS(SELECT 1 FROM drama_storyboard_runs newer WHERE newer.source_episode_id=r.source_episode_id AND newer.version>r.version)
 FOR UPDATE OF p,r,s`, args...).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return job, drama.ErrStoryboardConflict
	}
	if err != nil {
		return job, err
	}
	err = tx.QueryRow(ctx, `INSERT INTO drama_segment_videos(id,segment_id,storyboard_revision,model_code,duration_seconds,reference_image_ids,status)
 VALUES($1,$2,$3,$4,$5,$6::text[]::uuid[],'running')
 ON CONFLICT (segment_id) WHERE status='running' DO NOTHING RETURNING status,created_at`,
		job.ID, id, job.Revision, job.ModelCode, job.Duration, job.ImageIDs).Scan(&job.Status, &job.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return job, drama.ErrSegmentVideoBusy
	}
	if err != nil {
		return job, err
	}
	return job, tx.Commit(ctx)
}

const segmentVideoSelect = `SELECT v.id,v.segment_id,v.storyboard_revision,v.model_code,v.duration_seconds,v.reference_image_ids::text[],v.status,v.error_message,v.created_at`

func scanSegmentVideo(row interface{ Scan(...any) error }) (drama.SegmentVideo, error) {
	var job drama.SegmentVideo
	err := row.Scan(&job.ID, &job.SegmentID, &job.Revision, &job.ModelCode, &job.Duration, &job.ImageIDs, &job.Status, &job.Error, &job.CreatedAt)
	return job, err
}

const segmentVideoReadSQL = ` FROM drama_segment_videos v JOIN drama_storyboard_segments s ON s.id=v.segment_id
 JOIN drama_storyboard_runs r ON r.id=s.run_id JOIN drama_projects p ON p.id=r.project_id
 WHERE p.organization_id=$1 AND p.owner_id=$2 AND p.id=$3 AND r.source_episode_id=$4 AND r.id=$5 AND s.id=$6`

func (r *DramaRepository) ListSegmentVideos(ctx context.Context, scope drama.SegmentVideoScope) ([]drama.SegmentVideo, error) {
	args := segmentVideoArgs(scope)
	var id string
	err := r.db.QueryRow(ctx, "SELECT s.id"+segmentVideoScopeSQL, args...).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, drama.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rows, err := r.db.Query(ctx, segmentVideoSelect+segmentVideoReadSQL+" ORDER BY v.created_at DESC,v.id DESC LIMIT 20", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := []drama.SegmentVideo{}
	for rows.Next() {
		job, err := scanSegmentVideo(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (r *DramaRepository) ReadSegmentVideo(ctx context.Context, scope drama.SegmentVideoScope, id string) (drama.SegmentVideo, error) {
	job, err := scanSegmentVideo(r.db.QueryRow(ctx, segmentVideoSelect+segmentVideoReadSQL+" AND v.id=$7", append(segmentVideoArgs(scope), id)...))
	if errors.Is(err, pgx.ErrNoRows) {
		return job, drama.ErrNotFound
	}
	return job, err
}

func (r *DramaRepository) FinishSegmentVideo(ctx context.Context, id, status, message string) error {
	tag, err := r.db.Exec(ctx, `UPDATE drama_segment_videos SET status=$2,error_message=$3,finished_at=now() WHERE id=$1 AND status='running'`, id, status, message)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("视频任务状态已变化：%w", drama.ErrStoryboardConflict)
	}
	return nil
}

func (r *DramaRepository) FailInterruptedSegmentVideos(ctx context.Context) error {
	_, err := r.db.Exec(ctx, `UPDATE drama_segment_videos SET status='failed',error_message='本地服务重启，视频生成状态已中断；请查看调用记录后重试',finished_at=now() WHERE status='running'`)
	return err
}
