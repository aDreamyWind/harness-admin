package postgres

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"harness-admin/internal/drama"
	"strings"
)

func (r *DramaRepository) visualProject(ctx context.Context, tx pgx.Tx, org, user, project string) error {
	var id string
	err := tx.QueryRow(ctx, `SELECT id FROM drama_projects WHERE id=$1 AND organization_id=$2 AND owner_id=$3`, project, org, user).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return drama.ErrNotFound
	}
	return err
}
func (r *DramaRepository) ReadCanvas(ctx context.Context, org, user, project string) (drama.Canvas, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return drama.Canvas{}, err
	}
	defer tx.Rollback(ctx)
	if err = r.visualProject(ctx, tx, org, user, project); err != nil {
		return drama.Canvas{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO drama_canvas_boards(project_id) VALUES($1) ON CONFLICT DO NOTHING`, project); err != nil {
		return drama.Canvas{}, err
	}
	// 解析得到的新资产首次进入画廊时建立默认形象；后续不覆盖人工编辑。
	if _, err = tx.Exec(ctx, `INSERT INTO drama_asset_forms(id,asset_id,name,description,style,aspect_ratio,position_x,position_y,style_id)
 SELECT a.id,a.id,'默认形象',concat_ws(E'\n',NULLIF(a.description,''),NULLIF(a.appearance_description,'')),p.style,
 CASE a.kind WHEN 'character' THEN '9:16' WHEN 'scene' THEN '16:9' ELSE '1:1' END,
 CASE a.kind WHEN 'character' THEN 0 WHEN 'scene' THEN 800 ELSE 1280 END,
 (row_number() OVER(PARTITION BY a.kind ORDER BY a.created_at,a.id)-1)*CASE a.kind WHEN 'character' THEN 620 WHEN 'scene' THEN 360 ELSE 440 END,s.id
 FROM drama_assets a JOIN drama_projects p ON p.id=a.project_id
 LEFT JOIN drama_visual_styles s ON s.id=p.style_id OR(p.style_id IS NULL AND s.name=p.style) WHERE p.id=$1 ON CONFLICT DO NOTHING`, project); err != nil {
		return drama.Canvas{}, err
	}
	out := drama.Canvas{Forms: []drama.AssetForm{}}
	if err = tx.QueryRow(ctx, `SELECT viewport_x,viewport_y,viewport_zoom,revision FROM drama_canvas_boards WHERE project_id=$1`, project).Scan(&out.X, &out.Y, &out.Zoom, &out.Revision); err != nil {
		return out, err
	}
	rows, err := tx.Query(ctx, `SELECT f.id,f.asset_id,a.name,a.kind,COALESCE(f.parent_id::text,''),f.name,f.description,f.gender,f.style,f.aspect_ratio,f.position_x,f.position_y,f.revision,COALESCE(f.current_image_id::text,''),COALESCE(j.status,'idle'),COALESCE(j.error_message,''),COALESCE(f.style_id,'')
 FROM drama_asset_forms f JOIN drama_assets a ON a.id=f.asset_id
 LEFT JOIN LATERAL(SELECT status,error_message FROM drama_image_jobs WHERE form_id=f.id ORDER BY created_at DESC,id DESC LIMIT 1) j ON true
 WHERE a.project_id=$1 ORDER BY a.kind,a.created_at,a.id,f.created_at,f.id`, project)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var f drama.AssetForm
		if err = rows.Scan(&f.ID, &f.AssetID, &f.AssetName, &f.Kind, &f.ParentID, &f.Name, &f.Description, &f.Gender, &f.Style, &f.AspectRatio, &f.X, &f.Y, &f.Revision, &f.CurrentImageID, &f.ImageStatus, &f.ImageError, &f.StyleID); err != nil {
			rows.Close()
			return out, err
		}
		if f.CurrentImageID != "" {
			f.ImageURL = fmt.Sprintf("/api/v1/ai-drama/projects/%s/images/%s/content", project, f.CurrentImageID)
		}
		out.Forms = append(out.Forms, f)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}
func (r *DramaRepository) SaveCanvasLayout(ctx context.Context, org, user, project string, in drama.CanvasLayout) (int, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	if err = r.visualProject(ctx, tx, org, user, project); err != nil {
		return 0, err
	}
	var revision int
	err = tx.QueryRow(ctx, `UPDATE drama_canvas_boards SET viewport_x=$2,viewport_y=$3,viewport_zoom=$4,revision=revision+1,updated_at=now() WHERE project_id=$1 AND revision=$5 RETURNING revision`, project, in.X, in.Y, in.Zoom, in.Revision).Scan(&revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, drama.ErrCanvasConflict
	}
	if err != nil {
		return 0, err
	}
	for _, p := range in.Positions {
		tag, e := tx.Exec(ctx, `UPDATE drama_asset_forms f SET position_x=$2,position_y=$3 FROM drama_assets a WHERE f.id=$1 AND a.id=f.asset_id AND a.project_id=$4`, p.ID, p.X, p.Y, project)
		if e != nil {
			return 0, e
		}
		if tag.RowsAffected() != 1 {
			return 0, drama.ErrNotFound
		}
	}
	return revision, tx.Commit(ctx)
}
func lockVisualForm(ctx context.Context, tx pgx.Tx, org, user, project, form string) (drama.AssetForm, error) {
	var f drama.AssetForm
	err := tx.QueryRow(ctx, `SELECT f.id,f.asset_id,a.name,a.kind,COALESCE(f.parent_id::text,''),f.name,f.description,f.gender,f.style,f.aspect_ratio,f.position_x,f.position_y,f.revision,COALESCE(f.current_image_id::text,''),COALESCE(f.style_id,'') FROM drama_asset_forms f JOIN drama_assets a ON a.id=f.asset_id JOIN drama_projects p ON p.id=a.project_id WHERE f.id=$1 AND p.id=$2 AND p.organization_id=$3 AND p.owner_id=$4 FOR UPDATE OF f`, form, project, org, user).Scan(&f.ID, &f.AssetID, &f.AssetName, &f.Kind, &f.ParentID, &f.Name, &f.Description, &f.Gender, &f.Style, &f.AspectRatio, &f.X, &f.Y, &f.Revision, &f.CurrentImageID, &f.StyleID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = drama.ErrNotFound
	}
	return f, err
}
func visualBusy(ctx context.Context, tx pgx.Tx, form string) error {
	var active bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM drama_image_jobs WHERE form_id=$1 AND status IN ('queued','running'))`, form).Scan(&active)
	if err != nil {
		return err
	}
	if active {
		return drama.ErrImageBusy
	}
	return nil
}
func (r *DramaRepository) EditAssetForm(ctx context.Context, org, user, project, form string, in drama.FormEdit) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	f, err := lockVisualForm(ctx, tx, org, user, project, form)
	if err != nil {
		return err
	}
	if f.Revision != in.Revision {
		return drama.ErrCanvasConflict
	}
	if err = visualBusy(ctx, tx, form); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE drama_asset_forms SET name=$2,description=$3,gender=$4,style=$5,aspect_ratio=$6,style_id=COALESCE(NULLIF($7::text,''),(SELECT id FROM drama_visual_styles WHERE name=$5 AND enabled)),revision=revision+1,updated_at=now() WHERE id=$1`, form, in.Name, in.Description, in.Gender, in.Style, in.AspectRatio, in.StyleID)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (r *DramaRepository) DeriveAssetForm(ctx context.Context, org, user, project, parent, id string) (string, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	// 与布局保存使用相同锁顺序，避免创建派生节点与拖动保存死锁。
	if err = r.visualProject(ctx, tx, org, user, project); err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx, `UPDATE drama_canvas_boards SET revision=revision+1,updated_at=now() WHERE project_id=$1`, project); err != nil {
		return "", err
	}
	f, err := lockVisualForm(ctx, tx, org, user, project, parent)
	if err != nil {
		return "", err
	}
	var count int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM drama_asset_forms WHERE asset_id=$1`, f.AssetID).Scan(&count); err != nil {
		return "", err
	}
	if count >= 50 {
		return "", drama.ErrInvalidInput
	}
	_, err = tx.Exec(ctx, `INSERT INTO drama_asset_forms(id,asset_id,parent_id,name,description,gender,style,aspect_ratio,position_x,position_y,style_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NULLIF($11,''))`, id, f.AssetID, parent, fmt.Sprintf("形象 %d", count+1), f.Description, f.Gender, f.Style, f.AspectRatio, f.X+360, f.Y+float64(count-1)*80, f.StyleID)
	if err != nil {
		return "", err
	}
	return id, tx.Commit(ctx)
}
func (r *DramaRepository) QueueImage(ctx context.Context, org, user, project, form, model string, in drama.ImageInput) (drama.ImageJob, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return drama.ImageJob{}, err
	}
	defer tx.Rollback(ctx)
	f, err := lockVisualForm(ctx, tx, org, user, project, form)
	if err != nil {
		return drama.ImageJob{}, err
	}
	if err = visualBusy(ctx, tx, form); err != nil {
		return drama.ImageJob{}, err
	}
	job := drama.ImageJob{ID: uuid.NewString(), ProjectID: project, FormID: form, ModelCode: model, AspectRatio: f.AspectRatio, Status: "queued", OrganizationID: org, UserID: user}
	if f.ParentID != "" {
		if err = tx.QueryRow(ctx, `SELECT COALESCE(current_image_id::text,'') FROM drama_asset_forms WHERE id=$1 AND asset_id=$2`, f.ParentID, f.AssetID).Scan(&job.ReferenceID); err != nil {
			return job, err
		}
		if job.ReferenceID == "" {
			return job, drama.ErrParentImageMissing
		}
	}
	description := strings.TrimSpace(in.Prompt)
	if description == "" {
		description = f.Description
	}
	if description == "" {
		return job, drama.ErrInvalidInput
	}
	style, err := readVisualStyle(tx.QueryRow(ctx, `SELECT id,name,category,preview_url,preview_x,preview_y,image_prompt,video_prompt FROM drama_visual_styles WHERE enabled AND (id=NULLIF($1::text,'') OR ($1::text='' AND name=$2)) LIMIT 1`, f.StyleID, f.Style))
	if err != nil {
		return job, err
	}
	job.StyleID = style.ID
	job.Prompt = drama.BuildAssetImagePrompt(f, description, style, job.ReferenceID != "")
	_, err = tx.Exec(ctx, `INSERT INTO drama_image_jobs(id,form_id,model_code,form_revision,prompt,aspect_ratio,reference_id,status,style_id) VALUES($1,$2,$3,$4,$5,$6,NULLIF($7,'')::uuid,'queued',$8)`, job.ID, form, model, f.Revision, job.Prompt, job.AspectRatio, job.ReferenceID, job.StyleID)
	if err != nil {
		return job, err
	}
	return job, tx.Commit(ctx)
}
func (r *DramaRepository) ClaimImage(ctx context.Context, id string) (bool, error) {
	tag, err := r.db.Exec(ctx, `UPDATE drama_image_jobs SET status='running',updated_at=now() WHERE id=$1 AND status='queued'`, id)
	return tag.RowsAffected() == 1, err
}
func (r *DramaRepository) FinishImage(ctx context.Context, job drama.ImageJob) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE drama_image_jobs SET status=$2,error_message=$3,storage_key=$4,mime_type=$5,byte_size=$6,updated_at=now() WHERE id=$1 AND status='running'`, job.ID, job.Status, job.Error, job.StorageKey, job.MIME, job.ByteSize)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return drama.ErrCanvasConflict
	}
	if job.Status == "succeeded" {
		tag, err = tx.Exec(ctx, `UPDATE drama_asset_forms f SET current_image_id=$2,updated_at=now() FROM drama_image_jobs j WHERE f.id=$1 AND j.id=$2 AND j.form_id=f.id AND f.revision=j.form_revision`, job.FormID, job.ID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return drama.ErrCanvasConflict
		}
	}
	return tx.Commit(ctx)
}
func (r *DramaRepository) ReadImage(ctx context.Context, org, user, project, id string) (drama.ImageJob, error) {
	var job drama.ImageJob
	err := r.db.QueryRow(ctx, `SELECT j.id,j.storage_key,j.mime_type,j.byte_size FROM drama_image_jobs j JOIN drama_asset_forms f ON f.id=j.form_id JOIN drama_assets a ON a.id=f.asset_id JOIN drama_projects p ON p.id=a.project_id WHERE j.id=$1 AND p.id=$2 AND p.organization_id=$3 AND p.owner_id=$4 AND j.status='succeeded'`, id, project, org, user).Scan(&job.ID, &job.StorageKey, &job.MIME, &job.ByteSize)
	if errors.Is(err, pgx.ErrNoRows) {
		err = drama.ErrNotFound
	}
	return job, err
}
func (r *DramaRepository) FailInterruptedImages(ctx context.Context) error {
	_, err := r.db.Exec(ctx, `UPDATE drama_image_jobs SET status='failed',error_message='服务重启，图片生成已中断，请重新生成',updated_at=now() WHERE status IN ('queued','running')`)
	return err
}
