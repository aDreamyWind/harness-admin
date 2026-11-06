package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"harness-admin/internal/novel"
)

type NovelRepository struct{ db *pgxpool.Pool }

func NewNovelRepository(db *pgxpool.Pool) *NovelRepository { return &NovelRepository{db: db} }

func (r *NovelRepository) List(ctx context.Context, owner novel.Owner) ([]novel.Book, error) {
	rows, err := r.db.Query(ctx, `SELECT p.id, p.title, p.chapter_count,
		COALESCE((SELECT MIN(chapter.number)::integer - 1
			FROM jsonb_array_elements(work.chapters) WITH ORDINALITY AS chapter(data, number)
			WHERE chapter.data->'final' IS DISTINCT FROM 'true'::jsonb), jsonb_array_length(work.chapters)), p.updated_at,
		COALESCE(p.workspace->>'storyMode' = 'serial' AND p.workspace->>'outline' IS NULL, false),
		COALESCE(c.revision::text, ''), COALESCE(h.source, ''), COALESCE(c.updated_at, p.updated_at)
		FROM novel_projects p LEFT JOIN novel_project_covers c ON c.project_id=p.id
		LEFT JOIN novel_project_cover_history h ON h.project_id=c.project_id AND h.revision=c.revision
		CROSS JOIN LATERAL (SELECT CASE WHEN jsonb_typeof(p.workspace->'chapters') = 'array'
			THEN p.workspace->'chapters' ELSE '[]'::jsonb END AS chapters) work
		WHERE p.organization_id=$1 AND p.owner_id=$2 ORDER BY p.updated_at DESC, p.id`, owner.OrganizationID, owner.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []novel.Book{}
	for rows.Next() {
		var item novel.Book
		var cover novel.Cover
		if err := rows.Scan(&item.ID, &item.Title, &item.ChapterCount, &item.FinalizedChapterCount, &item.UpdatedAt, &item.OpeningPending,
			&cover.Revision, &cover.Source, &cover.UpdatedAt); err != nil {
			return nil, err
		}
		if cover.Revision != "" {
			item.Cover = &cover
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *NovelRepository) Create(ctx context.Context, owner novel.Owner, p novel.Project) (novel.Project, error) {
	err := r.db.QueryRow(ctx, `INSERT INTO novel_projects
		(id, organization_id, owner_id, title, brief, chapter_count, workspace, version)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING updated_at`,
		p.ID, owner.OrganizationID, owner.UserID, p.Title, p.Brief, p.ChapterCount, p.Workspace, p.Version).
		Scan(&p.UpdatedAt)
	return p, err
}

func (r *NovelRepository) Get(ctx context.Context, owner novel.Owner, id string) (novel.Project, error) {
	var p novel.Project
	var cover novel.Cover
	err := r.db.QueryRow(ctx, `SELECT p.id, p.title, p.brief, p.chapter_count, p.workspace, p.version, p.updated_at,
		COALESCE(c.revision::text, ''), COALESCE(h.source, ''), COALESCE(c.updated_at, p.updated_at)
		FROM novel_projects p LEFT JOIN novel_project_covers c ON c.project_id=p.id
		LEFT JOIN novel_project_cover_history h ON h.project_id=c.project_id AND h.revision=c.revision
		WHERE p.organization_id=$1 AND p.owner_id=$2 AND p.id=$3`,
		owner.OrganizationID, owner.UserID, id).
		Scan(&p.ID, &p.Title, &p.Brief, &p.ChapterCount, &p.Workspace, &p.Version, &p.UpdatedAt,
			&cover.Revision, &cover.Source, &cover.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return novel.Project{}, novel.ErrNotFound
	}
	if cover.Revision != "" {
		p.Cover = &cover
	}
	return p, err
}

func (r *NovelRepository) Save(ctx context.Context, owner novel.Owner, p novel.Project) (novel.Project, error) {
	var cover novel.Cover
	err := r.db.QueryRow(ctx, `WITH saved AS (
		UPDATE novel_projects SET title=$1, workspace=$2, chapter_count=$3, version=version+1, updated_at=NOW()
		WHERE organization_id=$4 AND owner_id=$5 AND id=$6 AND version=$7
		RETURNING id, version, updated_at
	)
	SELECT saved.version, saved.updated_at, COALESCE(c.revision::text, ''), COALESCE(h.source, ''),
		COALESCE(c.updated_at, saved.updated_at)
	FROM saved LEFT JOIN novel_project_covers c ON c.project_id=saved.id
	LEFT JOIN novel_project_cover_history h ON h.project_id=c.project_id AND h.revision=c.revision`,
		p.Title, p.Workspace, p.ChapterCount, owner.OrganizationID, owner.UserID, p.ID, p.Version).
		Scan(&p.Version, &p.UpdatedAt, &cover.Revision, &cover.Source, &cover.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return novel.Project{}, novel.ErrConflict
	}
	if cover.Revision != "" {
		p.Cover = &cover
	}
	return p, err
}

func (r *NovelRepository) GetCoverRevision(ctx context.Context, owner novel.Owner, projectID string) (string, error) {
	var revision string
	err := r.db.QueryRow(ctx, `SELECT COALESCE(c.revision::text, '') FROM novel_projects p
		LEFT JOIN novel_project_covers c ON c.project_id=p.id
		WHERE p.organization_id=$1 AND p.owner_id=$2 AND p.id=$3`,
		owner.OrganizationID, owner.UserID, projectID).Scan(&revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", novel.ErrNotFound
	}
	return revision, err
}

func (r *NovelRepository) GetCover(ctx context.Context, owner novel.Owner, projectID, revision string) (novel.CoverImage, error) {
	var image novel.CoverImage
	err := r.db.QueryRow(ctx, `SELECT h.image_data, h.mime_type FROM novel_projects p
		LEFT JOIN novel_project_covers c ON c.project_id=p.id
		JOIN novel_project_cover_history h ON h.project_id=p.id AND h.revision=COALESCE(NULLIF($4, '')::uuid, c.revision)
		WHERE p.organization_id=$1 AND p.owner_id=$2 AND p.id=$3`,
		owner.OrganizationID, owner.UserID, projectID, revision).Scan(&image.Data, &image.MIME)
	if errors.Is(err, pgx.ErrNoRows) {
		return novel.CoverImage{}, novel.ErrNotFound
	}
	return image, err
}

func (r *NovelRepository) SaveCover(ctx context.Context, owner novel.Owner, projectID, previousRevision string, cover novel.Cover, image novel.CoverImage) (novel.Cover, error) {
	var saved novel.Cover
	err := pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		if err := saveNovelCoverVersion(ctx, tx, owner, projectID, cover, image); err != nil {
			return err
		}
		var err error
		saved, err = selectNovelCoverVersion(ctx, tx, owner, projectID, cover.Revision, &previousRevision)
		if errors.Is(err, pgx.ErrNoRows) {
			return novel.ErrConflict
		}
		return err
	})
	if err != nil {
		return novel.Cover{}, err
	}
	return saved, nil
}

type novelCoverQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func (r *NovelRepository) SaveCoverVersion(ctx context.Context, owner novel.Owner, projectID string, cover novel.Cover, image novel.CoverImage) error {
	return saveNovelCoverVersion(ctx, r.db, owner, projectID, cover, image)
}

func saveNovelCoverVersion(ctx context.Context, db novelCoverQueryer, owner novel.Owner, projectID string, cover novel.Cover, image novel.CoverImage) error {
	var owned bool
	err := db.QueryRow(ctx, `WITH project AS (
		SELECT id FROM novel_projects WHERE organization_id=$1 AND owner_id=$2 AND id=$3
	), archived AS (
		INSERT INTO novel_project_cover_history (project_id, revision, source, mime_type, image_data)
		SELECT id, $4, $5, $6, $7 FROM project ON CONFLICT (project_id, revision) DO NOTHING
	)
	SELECT EXISTS (SELECT 1 FROM project)`, owner.OrganizationID, owner.UserID, projectID,
		cover.Revision, cover.Source, image.MIME, image.Data).Scan(&owned)
	if err == nil && !owned {
		return novel.ErrNotFound
	}
	return err
}

func (r *NovelRepository) ListCoverHistory(ctx context.Context, owner novel.Owner, projectID string) ([]novel.CoverHistoryItem, error) {
	rows, err := r.db.Query(ctx, `SELECT COALESCE(h.revision::text, ''), COALESCE(h.source, ''),
		COALESCE(h.created_at, p.updated_at), COALESCE(h.revision=c.revision, false)
		FROM novel_projects p LEFT JOIN novel_project_cover_history h ON h.project_id=p.id
		LEFT JOIN novel_project_covers c ON c.project_id=p.id
		WHERE p.organization_id=$1 AND p.owner_id=$2 AND p.id=$3
		ORDER BY h.created_at DESC, h.revision DESC`, owner.OrganizationID, owner.UserID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []novel.CoverHistoryItem{}
	found := false
	for rows.Next() {
		var item novel.CoverHistoryItem
		if err := rows.Scan(&item.Revision, &item.Source, &item.CreatedAt, &item.Current); err != nil {
			return nil, err
		}
		found = true
		if item.Revision != "" {
			items = append(items, item)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if !found {
		return nil, novel.ErrNotFound
	}
	return items, nil
}

func (r *NovelRepository) SelectCoverVersion(ctx context.Context, owner novel.Owner, projectID, revision string) (novel.Cover, error) {
	cover, err := selectNovelCoverVersion(ctx, r.db, owner, projectID, revision, nil)
	if errors.Is(err, pgx.ErrNoRows) {
		return novel.Cover{}, novel.ErrNotFound
	}
	return cover, err
}

func selectNovelCoverVersion(ctx context.Context, db novelCoverQueryer, owner novel.Owner, projectID, revision string, previousRevision *string) (novel.Cover, error) {
	var cover novel.Cover
	err := db.QueryRow(ctx, `WITH selected AS (
		INSERT INTO novel_project_covers AS current_cover (project_id, revision)
		SELECT p.id, h.revision FROM novel_projects p JOIN novel_project_cover_history h ON h.project_id=p.id
		WHERE p.organization_id=$1 AND p.owner_id=$2 AND p.id=$3 AND h.revision=$4
		ON CONFLICT (project_id) DO UPDATE SET revision=EXCLUDED.revision,
			updated_at=CASE WHEN current_cover.revision=EXCLUDED.revision THEN current_cover.updated_at ELSE NOW() END
		WHERE $5::text IS NULL OR current_cover.revision::text=$5::text
		RETURNING project_id, revision, updated_at
	)
	SELECT s.revision, h.source, s.updated_at FROM selected s
	JOIN novel_project_cover_history h ON h.project_id=s.project_id AND h.revision=s.revision`,
		owner.OrganizationID, owner.UserID, projectID, revision, previousRevision).
		Scan(&cover.Revision, &cover.Source, &cover.UpdatedAt)
	return cover, err
}
