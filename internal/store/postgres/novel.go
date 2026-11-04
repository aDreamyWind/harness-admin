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
	rows, err := r.db.Query(ctx, `SELECT p.id, p.title, p.chapter_count, p.updated_at,
		COALESCE(c.revision::text, ''), COALESCE(c.source, ''), COALESCE(c.updated_at, p.updated_at)
		FROM novel_projects p LEFT JOIN novel_project_covers c ON c.project_id=p.id
		WHERE p.organization_id=$1 AND p.owner_id=$2 ORDER BY p.updated_at DESC, p.id`, owner.OrganizationID, owner.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []novel.Book{}
	for rows.Next() {
		var item novel.Book
		var cover novel.Cover
		if err := rows.Scan(&item.ID, &item.Title, &item.ChapterCount, &item.UpdatedAt,
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
		COALESCE(c.revision::text, ''), COALESCE(c.source, ''), COALESCE(c.updated_at, p.updated_at)
		FROM novel_projects p LEFT JOIN novel_project_covers c ON c.project_id=p.id
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
	SELECT saved.version, saved.updated_at, COALESCE(c.revision::text, ''), COALESCE(c.source, ''),
		COALESCE(c.updated_at, saved.updated_at)
	FROM saved LEFT JOIN novel_project_covers c ON c.project_id=saved.id`,
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

func (r *NovelRepository) GetCover(ctx context.Context, owner novel.Owner, projectID, revision string) (novel.CoverImage, error) {
	var image novel.CoverImage
	err := r.db.QueryRow(ctx, `SELECT c.image_data, c.mime_type FROM novel_project_covers c
		JOIN novel_projects p ON p.id=c.project_id
		WHERE p.organization_id=$1 AND p.owner_id=$2 AND p.id=$3 AND ($4='' OR c.revision::text=$4)`,
		owner.OrganizationID, owner.UserID, projectID, revision).Scan(&image.Data, &image.MIME)
	if errors.Is(err, pgx.ErrNoRows) {
		return novel.CoverImage{}, novel.ErrNotFound
	}
	return image, err
}

func (r *NovelRepository) SaveCover(ctx context.Context, owner novel.Owner, projectID, previousRevision string, cover novel.Cover, image novel.CoverImage) (novel.Cover, error) {
	err := r.db.QueryRow(ctx, `INSERT INTO novel_project_covers AS current_cover (project_id, revision, source, mime_type, image_data)
		SELECT id, $4, $5, $6, $7 FROM novel_projects WHERE organization_id=$1 AND owner_id=$2 AND id=$3
		ON CONFLICT (project_id) DO UPDATE SET revision=EXCLUDED.revision, source=EXCLUDED.source,
			mime_type=EXCLUDED.mime_type, image_data=EXCLUDED.image_data, updated_at=NOW()
		WHERE current_cover.revision::text=$8 RETURNING updated_at`,
		owner.OrganizationID, owner.UserID, projectID, cover.Revision, cover.Source, image.MIME, image.Data, previousRevision).
		Scan(&cover.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return novel.Cover{}, novel.ErrConflict
	}
	return cover, err
}
