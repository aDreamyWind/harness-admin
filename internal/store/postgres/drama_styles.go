package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"harness-admin/internal/drama"
)

const dramaStyleCatalogFilter = `enabled AND category='2D' AND catalog_name IS NOT NULL`

func (r *DramaRepository) ListStyles(ctx context.Context) ([]drama.StyleOption, error) {
	rows, err := r.db.Query(ctx, `SELECT id,catalog_name,category,preview_url,preview_x,preview_y
		FROM drama_visual_styles WHERE `+dramaStyleCatalogFilter+` ORDER BY sort_order,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []drama.StyleOption{}
	for rows.Next() {
		var style drama.StyleOption
		if err = rows.Scan(&style.ID, &style.Name, &style.Category, &style.PreviewURL, &style.PreviewX, &style.PreviewY); err != nil {
			return nil, err
		}
		out = append(out, style)
	}
	return out, rows.Err()
}

func (r *DramaRepository) ResolveCatalogStyle(ctx context.Context, id string) (drama.VisualStyle, error) {
	return readVisualStyle(r.db.QueryRow(ctx, `SELECT id,catalog_name,category,preview_url,preview_x,preview_y,image_prompt,video_prompt
		FROM drama_visual_styles WHERE `+dramaStyleCatalogFilter+` AND id=$1`, id))
}

func (r *DramaRepository) ResolveStyle(ctx context.Context, key string) (drama.VisualStyle, error) {
	return readVisualStyle(r.db.QueryRow(ctx, `SELECT id,COALESCE(catalog_name,name),category,preview_url,preview_x,preview_y,image_prompt,video_prompt
		FROM drama_visual_styles WHERE enabled AND (id=$1 OR name=$1 OR catalog_name=$1)
		ORDER BY CASE WHEN id=$1 THEN 0 WHEN catalog_name=$1 THEN 1 ELSE 2 END LIMIT 1`, key))
}
func readVisualStyle(row pgx.Row) (drama.VisualStyle, error) {
	var s drama.VisualStyle
	err := row.Scan(&s.ID, &s.Name, &s.Category, &s.PreviewURL, &s.PreviewX, &s.PreviewY, &s.ImagePrompt, &s.VideoPrompt)
	if errors.Is(err, pgx.ErrNoRows) {
		err = drama.ErrStyleUnavailable
	}
	return s, err
}
