package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"harness-admin/internal/novel"
)

func (r *NovelRepository) ListMemory(ctx context.Context, owner novel.Owner, id string) ([]novel.MemoryChapter, error) {
	rows, err := r.db.Query(ctx, `SELECT chapter.number::integer, COALESCE(m.status, 'missing'), COALESCE(m.facts, '[]'::jsonb)
		FROM novel_projects p
		LEFT JOIN LATERAL (
			SELECT number FROM jsonb_array_elements(CASE WHEN jsonb_typeof(p.workspace->'chapters') = 'array'
				THEN p.workspace->'chapters' ELSE '[]'::jsonb END) WITH ORDINALITY AS chapter(data, number)
			WHERE chapter.data->'final' = 'true'::jsonb
		) chapter ON true
		LEFT JOIN novel_memory_jobs m ON m.project_id=p.id AND m.chapter=chapter.number
		WHERE p.organization_id=$1 AND p.owner_id=$2 AND p.id=$3
		ORDER BY chapter.number`, owner.OrganizationID, owner.UserID, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	chapters := []novel.MemoryChapter{}
	found := false
	for rows.Next() {
		var number *int
		var chapter novel.MemoryChapter
		if err := rows.Scan(&number, &chapter.Status, &chapter.Facts); err != nil {
			return nil, err
		}
		found = true
		if number != nil {
			chapter.Chapter = *number
			chapters = append(chapters, chapter)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if !found {
		return nil, novel.ErrNotFound
	}
	return chapters, nil
}

func (r *NovelRepository) EnqueueMemory(ctx context.Context, owner novel.Owner, id string) error {
	return pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		var workspace novel.Workspace
		err := tx.QueryRow(ctx, `SELECT workspace FROM novel_projects WHERE organization_id=$1 AND owner_id=$2 AND id=$3 FOR NO KEY UPDATE`,
			owner.OrganizationID, owner.UserID, id).Scan(&workspace)
		if errors.Is(err, pgx.ErrNoRows) {
			return novel.ErrNotFound
		}
		if err != nil {
			return err
		}
		var chapters []int
		for index, chapter := range workspace.Chapters {
			if chapter.Final {
				chapters = append(chapters, index+1)
			}
		}
		return enqueueNovelMemory(ctx, tx, id, workspace.ModelCode, chapters, nil, true)
	})
}

func enqueueNovelMemory(ctx context.Context, tx pgx.Tx, id, modelCode string, chapters, changed []int, retryFailed bool) error {
	if len(chapters) == 0 {
		return nil
	}
	_, err := tx.Exec(ctx, `INSERT INTO novel_memory_jobs (project_id, chapter, model_code)
		SELECT $1, number, $2 FROM unnest($3::integer[]) number
		ON CONFLICT (project_id, chapter) DO UPDATE SET
			id=EXCLUDED.id, model_code=EXCLUDED.model_code, status='queued', facts='[]'::jsonb,
			lease_id=NULL, lease_expires_at=NULL, updated_at=NOW()
		WHERE novel_memory_jobs.chapter=ANY($4::integer[]) OR ($5 AND novel_memory_jobs.status='failed')`,
		id, modelCode, chapters, changed, retryFailed)
	return err
}

func syncNovelMemory(ctx context.Context, tx pgx.Tx, id string, prior, current novel.Workspace) error {
	var finalized, changed, reopened []int
	for index, chapter := range current.Chapters {
		if chapter.Final {
			finalized = append(finalized, index+1)
			if index >= len(prior.Chapters) || !prior.Chapters[index].Final || prior.Chapters[index].Body != chapter.Body {
				changed = append(changed, index+1)
			}
		}
	}
	for index, chapter := range prior.Chapters {
		if chapter.Final && (index >= len(current.Chapters) || !current.Chapters[index].Final) {
			reopened = append(reopened, index+1)
		}
	}
	if len(reopened) > 0 {
		if _, err := tx.Exec(ctx, `DELETE FROM novel_memory_jobs WHERE project_id=$1 AND chapter=ANY($2::integer[])`, id, reopened); err != nil {
			return fmt.Errorf("移除已撤回定稿的故事记忆: %w", err)
		}
	}
	if len(changed) > 0 {
		if err := enqueueNovelMemory(ctx, tx, id, current.ModelCode, finalized, changed, false); err != nil {
			return fmt.Errorf("登记定稿章节的故事记忆任务: %w", err)
		}
	}
	return nil
}

func (r *NovelRepository) ClaimMemory(ctx context.Context) (novel.MemoryJob, bool, error) {
	var job novel.MemoryJob
	err := r.db.QueryRow(ctx, `WITH candidate AS (
		SELECT m.id, p.organization_id, p.owner_id, p.workspace->'chapters'->(m.chapter-1)->>'body' AS body
		FROM novel_memory_jobs m JOIN novel_projects p ON p.id=m.project_id
		WHERE (m.status='queued' OR (m.status='running' AND m.lease_expires_at<=NOW()))
			AND p.workspace->'chapters'->(m.chapter-1)->'final'='true'::jsonb
		ORDER BY m.updated_at, m.chapter, m.id LIMIT 1 FOR UPDATE OF m SKIP LOCKED
	)
	UPDATE novel_memory_jobs m SET status='running', lease_id=gen_random_uuid(), lease_expires_at=NOW()+INTERVAL '11 minutes', updated_at=NOW()
	FROM candidate WHERE m.id=candidate.id
	RETURNING m.id, candidate.organization_id, candidate.owner_id, m.project_id, m.chapter, m.model_code, COALESCE(candidate.body, ''), m.lease_id`).
		Scan(&job.ID, &job.Owner.OrganizationID, &job.Owner.UserID, &job.ProjectID, &job.Chapter, &job.ModelCode, &job.Body, &job.LeaseID)
	if errors.Is(err, pgx.ErrNoRows) {
		return novel.MemoryJob{}, false, nil
	}
	return job, err == nil, err
}

func (r *NovelRepository) FinishMemory(ctx context.Context, job novel.MemoryJob, facts []novel.MemoryFact, failed bool) error {
	status := "succeeded"
	if failed {
		status = "failed"
		facts = nil
	}
	if facts == nil {
		facts = []novel.MemoryFact{}
	}
	tag, err := r.db.Exec(ctx, `UPDATE novel_memory_jobs m SET status=$7, facts=$8,
		lease_id=NULL, lease_expires_at=NULL, updated_at=NOW()
		FROM novel_projects p WHERE m.project_id=p.id AND p.organization_id=$1 AND p.owner_id=$2
			AND m.id=$3 AND m.lease_id=$4 AND m.project_id=$5 AND m.chapter=$6
			AND m.status='running' AND m.lease_expires_at>NOW()
			AND p.workspace->'chapters'->(m.chapter-1)->'final'='true'::jsonb`,
		job.Owner.OrganizationID, job.Owner.UserID, job.ID, job.LeaseID, job.ProjectID, job.Chapter, status, facts)
	if err == nil && tag.RowsAffected() == 0 {
		return novel.ErrConflict
	}
	return err
}

func (r *NovelRepository) MemoryDecisions(ctx context.Context, owner novel.Owner, id string) (map[string]int, error) {
	rows, err := r.db.Query(ctx, `SELECT d.conflict_id, d.choice FROM novel_projects p
		LEFT JOIN novel_memory_decisions d ON d.project_id=p.id
		WHERE p.organization_id=$1 AND p.owner_id=$2 AND p.id=$3`, owner.OrganizationID, owner.UserID, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	decisions := map[string]int{}
	found := false
	for rows.Next() {
		var conflict *string
		var choice *int
		if err := rows.Scan(&conflict, &choice); err != nil {
			return nil, err
		}
		found = true
		if conflict != nil {
			decisions[*conflict] = *choice
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if !found {
		return nil, novel.ErrNotFound
	}
	return decisions, nil
}

func (r *NovelRepository) SaveMemoryDecision(ctx context.Context, owner novel.Owner, id, conflictID string, choice int) error {
	var owned, saved bool
	err := r.db.QueryRow(ctx, `WITH owned AS (
		SELECT id FROM novel_projects WHERE organization_id=$1 AND owner_id=$2 AND id=$3
	), saved AS (
		INSERT INTO novel_memory_decisions (project_id, conflict_id, choice)
		SELECT id, $4, $5 FROM owned
		ON CONFLICT (project_id, conflict_id) DO UPDATE SET choice=EXCLUDED.choice
		WHERE novel_memory_decisions.choice=EXCLUDED.choice
		RETURNING choice
	)
	SELECT EXISTS(SELECT 1 FROM owned), EXISTS(SELECT 1 FROM saved)`, owner.OrganizationID, owner.UserID, id, conflictID, choice).Scan(&owned, &saved)
	if err != nil {
		return err
	}
	if !owned {
		return novel.ErrNotFound
	}
	if !saved {
		return novel.ErrConflict
	}
	return nil
}
