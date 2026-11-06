package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"harness-admin/internal/drama"
)

func lockCanvasFormMentions(ctx context.Context, tx pgx.Tx, org, user, project, form string, in drama.FormEdit) (drama.CanvasExtras, error) {
	if in.CanvasRevision == nil || *in.CanvasRevision < 1 {
		return drama.CanvasExtras{}, drama.ErrInvalidInput
	}
	var revision int
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT b.revision,b.extras FROM drama_canvas_boards b
		JOIN drama_projects p ON p.id=b.project_id
		WHERE p.id=$1 AND p.organization_id=$2 AND p.owner_id=$3 FOR UPDATE OF b`, project, org, user).Scan(&revision, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return drama.CanvasExtras{}, drama.ErrNotFound
	}
	if err != nil {
		return drama.CanvasExtras{}, fmt.Errorf("读取画布引用版本失败: %w", err)
	}
	if revision != *in.CanvasRevision {
		return drama.CanvasExtras{}, drama.ErrCanvasConflict
	}
	var extras drama.CanvasExtras
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &extras); err != nil {
			return drama.CanvasExtras{}, fmt.Errorf("读取画布引用失败: %w", err)
		}
	}
	return drama.MergeCanvasFormMentions(project, form, extras, in.MentionLinks)
}

func saveCanvasFormMentions(ctx context.Context, tx pgx.Tx, project string, extras drama.CanvasExtras) error {
	if err := validateCanvasNodeReferences(ctx, tx, project, extras); err != nil {
		return err
	}
	raw, err := json.Marshal(extras)
	if err != nil {
		return fmt.Errorf("编码画布引用失败: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE drama_canvas_boards SET extras=$2::jsonb,revision=revision+1,updated_at=now() WHERE project_id=$1`, project, raw); err != nil {
		return fmt.Errorf("保存画布引用失败: %w", err)
	}
	return nil
}
