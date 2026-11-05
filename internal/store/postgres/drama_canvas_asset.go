package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"harness-admin/internal/drama"
)

func (r *DramaRepository) CreateCanvasAsset(ctx context.Context, org, user, project, id string, in drama.CanvasAssetInput) (drama.CanvasAssetResult, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return drama.CanvasAssetResult{}, err
	}
	defer tx.Rollback(ctx)
	if err := r.visualProject(ctx, tx, org, user, project); err != nil {
		return drama.CanvasAssetResult{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO drama_canvas_boards(project_id) VALUES($1) ON CONFLICT DO NOTHING`, project); err != nil {
		return drama.CanvasAssetResult{}, fmt.Errorf("初始化漫剧画布失败: %w", err)
	}
	// 先锁定画布版本，与保存布局和派生形象的锁顺序保持一致。
	result := drama.CanvasAssetResult{ID: id}
	err = tx.QueryRow(ctx, `UPDATE drama_canvas_boards SET revision=revision+1,updated_at=now()
		WHERE project_id=$1 AND revision=$2 RETURNING revision`, project, in.Revision).Scan(&result.Revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return drama.CanvasAssetResult{}, drama.ErrCanvasConflict
	}
	if err != nil {
		return drama.CanvasAssetResult{}, fmt.Errorf("更新漫剧画布版本失败: %w", err)
	}
	tag, err := tx.Exec(ctx, `INSERT INTO drama_assets(id,project_id,kind,name,description) VALUES($1,$2,$3,$4,$5)
		ON CONFLICT (project_id,kind,name) DO NOTHING`, id, project, in.Kind, in.Name, in.Description)
	if err != nil {
		return drama.CanvasAssetResult{}, fmt.Errorf("创建漫剧资产失败: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return drama.CanvasAssetResult{}, drama.ErrCanvasAssetNameExists
	}
	_, err = tx.Exec(ctx, `INSERT INTO drama_asset_forms(id,asset_id,name,description,style,aspect_ratio,position_x,position_y,style_id)
		SELECT $1,$1,'默认形象',$4,p.style,
		       CASE $3 WHEN 'character' THEN '9:16' WHEN 'scene' THEN '16:9' ELSE '1:1' END,$5,$6,s.id
		FROM drama_projects p
		LEFT JOIN drama_visual_styles s ON s.id=p.style_id OR (p.style_id IS NULL AND s.name=p.style)
		WHERE p.id=$2`, id, project, in.Kind, in.Description, in.X, in.Y)
	if err != nil {
		return drama.CanvasAssetResult{}, fmt.Errorf("创建漫剧资产默认形象失败: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return drama.CanvasAssetResult{}, err
	}
	return result, nil
}
