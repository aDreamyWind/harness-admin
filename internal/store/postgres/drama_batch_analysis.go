package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"harness-admin/internal/drama"
)

func (r *DramaRepository) QueueBatchAnalysis(ctx context.Context, org, owner, project, batch string) (drama.Batch, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return drama.Batch{}, fmt.Errorf("启动剧本解析失败: %w", err)
	}
	defer tx.Rollback(ctx)
	var status string
	var revision, analysisRevision int
	err = tx.QueryRow(ctx, `SELECT b.status,b.revision,b.analysis_revision FROM drama_batches b
      JOIN drama_projects p ON p.id=b.project_id WHERE p.organization_id=$1 AND p.owner_id=$2
      AND p.id=$3 AND p.status='ready' AND b.id=$4 FOR UPDATE OF b`, org, owner, project, batch).Scan(&status, &revision, &analysisRevision)
	if errors.Is(err, pgx.ErrNoRows) {
		return drama.Batch{}, drama.ErrNotFound
	}
	if err != nil {
		return drama.Batch{}, fmt.Errorf("锁定解析批次失败: %w", err)
	}
	if status == "running" {
		return drama.Batch{}, drama.ErrBatchRunning
	}
	// 失败后沿用同一范围的已完成步骤；主动重新解析或范围变化时开启全新运行。
	if status != "failed" || analysisRevision != revision {
		if _, err = tx.Exec(ctx, `DELETE FROM drama_batch_steps WHERE batch_id=$1`, batch); err != nil {
			return drama.Batch{}, fmt.Errorf("清理旧解析步骤失败: %w", err)
		}
	}
	item, err := scanDramaBatch(tx.QueryRow(ctx, `UPDATE drama_batches b SET status='running', error_message='', analysis_revision=b.revision, updated_at=NOW()
      WHERE b.id=$1 RETURNING `+dramaBatchColumns, batch))
	if err != nil {
		return drama.Batch{}, fmt.Errorf("启动剧本解析失败: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return drama.Batch{}, fmt.Errorf("提交剧本解析任务失败: %w", err)
	}
	return item, nil
}

func (r *DramaRepository) FailBatchAnalysis(ctx context.Context, org, owner, project, batch string, revision int, reason string) error {
	tag, err := r.db.Exec(ctx, `UPDATE drama_batches b SET status='failed', error_message=$6, updated_at=NOW() FROM drama_projects p
 WHERE p.id=b.project_id AND p.organization_id=$1 AND p.owner_id=$2 AND p.id=$3 AND b.id=$4
 AND b.status='running' AND b.revision=$5 AND b.analysis_revision=$5`, org, owner, project, batch, revision, reason)
	if err != nil {
		return fmt.Errorf("保存剧本解析失败状态失败: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return drama.ErrBatchConflict
	}
	_, err = r.db.Exec(ctx, `UPDATE drama_batch_steps SET status='failed', error_message=$3, updated_at=NOW()
      WHERE batch_id=$1 AND analysis_revision=$2 AND status='running'`, batch, revision, reason)
	if err != nil {
		return fmt.Errorf("保存解析步骤失败状态失败: %w", err)
	}
	return nil
}

func (r *DramaRepository) FailInterruptedBatches(ctx context.Context) error {
	_, err := r.db.Exec(ctx, `UPDATE drama_batch_steps s SET status='failed', error_message='解析服务中断', updated_at=NOW() FROM drama_batches b WHERE b.id=s.batch_id AND b.status='running' AND s.status='running'`)
	if err != nil {
		return fmt.Errorf("恢复中断的解析步骤失败: %w", err)
	}
	_, err = r.db.Exec(ctx, `UPDATE drama_batches SET status='failed', error_message='解析服务中断，请重新点击 AI 解析', updated_at=NOW() WHERE status='running'`)
	if err != nil {
		return fmt.Errorf("恢复中断的剧本解析任务失败: %w", err)
	}
	return nil
}

func (r *DramaRepository) SaveBatchResult(ctx context.Context, org, owner, project, batch string, revision int, draft drama.BatchDraft) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var status string
	var current, analysis int
	err = tx.QueryRow(ctx, `SELECT b.status,b.revision,b.analysis_revision FROM drama_batches b JOIN drama_projects p ON p.id=b.project_id
 WHERE p.organization_id=$1 AND p.owner_id=$2 AND p.id=$3 AND b.id=$4 FOR UPDATE OF b`, org, owner, project, batch).Scan(&status, &current, &analysis)
	if errors.Is(err, pgx.ErrNoRows) {
		return drama.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("锁定解析批次失败: %w", err)
	}
	if status != "running" || current != revision || analysis != revision {
		return drama.ErrBatchConflict
	}
	// 在事务中整体替换当前批次结果，模型失败时旧结果仍保留。
	if _, err = tx.Exec(ctx, `DELETE FROM drama_batch_assets WHERE batch_id=$1`, batch); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM drama_script_episodes WHERE batch_id=$1`, batch); err != nil {
		return err
	}
	assetIDs := map[string]string{}
	upsert := func(kind, name, description, appearance, voice string) (string, error) {
		name = strings.TrimSpace(name)
		if name == "" {
			return "", nil
		}
		key := kind + "\x00" + name
		if id := assetIDs[key]; id != "" {
			if description != "" || appearance != "" || voice != "" {
				_, err := tx.Exec(ctx, `UPDATE drama_assets SET
				 description=CASE WHEN $2<>'' THEN $2 ELSE description END,
				 appearance_description=CASE WHEN $3<>'' THEN $3 ELSE appearance_description END,
				 voice_description=CASE WHEN $4<>'' THEN $4 ELSE voice_description END,
				 updated_at=NOW() WHERE id=$1`, id, description, appearance, voice)
				return id, err
			}
			return id, nil
		}
		id := uuid.NewString()
		err := tx.QueryRow(ctx, `INSERT INTO drama_assets (id,project_id,kind,name,description,appearance_description,voice_description) VALUES ($1,$2,$3,$4,$5,$6,$7)
   ON CONFLICT (project_id,kind,name) DO UPDATE SET
    description=CASE WHEN EXCLUDED.description<>'' THEN EXCLUDED.description ELSE drama_assets.description END,
    appearance_description=CASE WHEN EXCLUDED.appearance_description<>'' THEN EXCLUDED.appearance_description ELSE drama_assets.appearance_description END,
    voice_description=CASE WHEN EXCLUDED.voice_description<>'' THEN EXCLUDED.voice_description ELSE drama_assets.voice_description END,
    updated_at=NOW()
   RETURNING id`, id, project, kind, name, description, appearance, voice).Scan(&id)
		if err != nil {
			return "", fmt.Errorf("保存%s资产失败: %w", kind, err)
		}
		assetIDs[key] = id
		_, err = tx.Exec(ctx, `INSERT INTO drama_batch_assets(batch_id,asset_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, batch, id)
		return id, err
	}
	for _, group := range []struct {
		kind  string
		items []drama.AssetDraft
	}{{"character", draft.Characters}, {"scene", draft.Scenes}, {"prop", draft.Props}} {
		for _, a := range group.items {
			if _, err = upsert(group.kind, a.Name, a.Description, a.AppearanceDescription, a.VoiceDescription); err != nil {
				return err
			}
		}
	}
	for index, episode := range draft.Episodes {
		episodeID := uuid.NewString()
		_, err = tx.Exec(ctx, `INSERT INTO drama_script_episodes(id,project_id,batch_id,number,title,synopsis,script_text) VALUES($1,$2,$3,$4,$5,$6,$7)`, episodeID, project, batch, index+1, episode.Title, episode.Synopsis, episode.ScriptText)
		if err != nil {
			return fmt.Errorf("保存第%d集失败: %w", index+1, err)
		}
		for _, group := range []struct {
			kind  string
			items []drama.EpisodeAssetDraft
		}{{"character", episode.Characters}, {"scene", episode.Scenes}, {"prop", episode.Props}} {
			for _, a := range group.items {
				id, e := upsert(group.kind, a.Name, "", "", "")
				if e != nil {
					return e
				}
				if id == "" {
					continue
				}
				_, e = tx.Exec(ctx, `INSERT INTO drama_episode_asset_links(id,episode_id,asset_id,state_label) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, uuid.NewString(), episodeID, id, strings.TrimSpace(a.StateLabel))
				if e != nil {
					return fmt.Errorf("保存第%d集资产关联失败: %w", index+1, e)
				}
			}
		}
	}
	_, err = tx.Exec(ctx, `UPDATE drama_batches SET status='ready',error_message='',updated_at=NOW() WHERE id=$1`, batch)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func scanAsset(row pgx.Row) (drama.Asset, error) {
	var a drama.Asset
	err := row.Scan(&a.ID, &a.Kind, &a.Name, &a.Description, &a.AppearanceDescription, &a.VoiceDescription)
	return a, err
}
func (r *DramaRepository) ListProjectAssets(ctx context.Context, org, owner, project, kind, query string) ([]drama.Asset, error) {
	var exists bool
	err := r.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM drama_projects WHERE id=$1 AND organization_id=$2 AND owner_id=$3)`, project, org, owner).Scan(&exists)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, drama.ErrNotFound
	}
	rows, err := r.db.Query(ctx, `SELECT id,kind,name,description,appearance_description,voice_description FROM drama_assets WHERE project_id=$1 AND kind=$2 AND name ILIKE '%'||$3||'%' ORDER BY name LIMIT 200`, project, kind, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]drama.Asset, 0)
	for rows.Next() {
		a, e := scanAsset(rows)
		if e != nil {
			return nil, e
		}
		items = append(items, a)
	}
	return items, rows.Err()
}
func (r *DramaRepository) GetBatchResult(ctx context.Context, org, owner, project, batchID string) (drama.BatchResult, error) {
	batch, err := r.GetBatch(ctx, org, owner, project, batchID)
	if err != nil {
		return drama.BatchResult{}, err
	}
	result := drama.BatchResult{Batch: batch, Assets: drama.BatchAssets{Characters: []drama.Asset{}, Scenes: []drama.Asset{}, Props: []drama.Asset{}}, Episodes: []drama.ScriptEpisode{}}
	rows, err := r.db.Query(ctx, `SELECT a.id,a.kind,a.name,a.description,a.appearance_description,a.voice_description FROM drama_assets a JOIN drama_batch_assets ba ON ba.asset_id=a.id WHERE ba.batch_id=$1 ORDER BY a.kind,a.name`, batchID)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		a, e := scanAsset(rows)
		if e != nil {
			rows.Close()
			return result, e
		}
		switch a.Kind {
		case "character":
			result.Assets.Characters = append(result.Assets.Characters, a)
		case "scene":
			result.Assets.Scenes = append(result.Assets.Scenes, a)
		case "prop":
			result.Assets.Props = append(result.Assets.Props, a)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	episodes, err := r.db.Query(ctx, `SELECT id,number,title,synopsis,script_text FROM drama_script_episodes WHERE batch_id=$1 ORDER BY number`, batchID)
	if err != nil {
		return result, err
	}
	for episodes.Next() {
		var e drama.ScriptEpisode
		if err = episodes.Scan(&e.ID, &e.Number, &e.Title, &e.Synopsis, &e.ScriptText); err != nil {
			episodes.Close()
			return result, err
		}
		e.Characters = []drama.EpisodeAsset{}
		e.Scenes = []drama.EpisodeAsset{}
		e.Props = []drama.EpisodeAsset{}
		result.Episodes = append(result.Episodes, e)
	}
	err = episodes.Err()
	episodes.Close()
	if err != nil {
		return result, err
	}
	for i := range result.Episodes {
		links, e := r.db.Query(ctx, `SELECT l.id,l.asset_id,a.kind,a.name,a.description,a.appearance_description,a.voice_description,l.state_label FROM drama_episode_asset_links l JOIN drama_assets a ON a.id=l.asset_id WHERE l.episode_id=$1 ORDER BY a.kind,a.name,l.state_label`, result.Episodes[i].ID)
		if e != nil {
			return result, e
		}
		for links.Next() {
			var a drama.EpisodeAsset
			if e = links.Scan(&a.ID, &a.AssetID, &a.Kind, &a.Name, &a.Description, &a.AppearanceDescription, &a.VoiceDescription, &a.StateLabel); e != nil {
				links.Close()
				return result, e
			}
			switch a.Kind {
			case "character":
				result.Episodes[i].Characters = append(result.Episodes[i].Characters, a)
			case "scene":
				result.Episodes[i].Scenes = append(result.Episodes[i].Scenes, a)
			case "prop":
				result.Episodes[i].Props = append(result.Episodes[i].Props, a)
			}
		}
		e = links.Err()
		links.Close()
		if e != nil {
			return result, e
		}
	}
	return result, nil
}
func (r *DramaRepository) AddEpisodeAsset(ctx context.Context, org, owner, project, episode, asset, state string) (drama.EpisodeAsset, error) {
	var a drama.EpisodeAsset
	err := r.db.QueryRow(ctx, `INSERT INTO drama_episode_asset_links(id,episode_id,asset_id,state_label)
 SELECT $6,e.id,a.id,$7 FROM drama_script_episodes e JOIN drama_projects p ON p.id=e.project_id JOIN drama_assets a ON a.project_id=p.id
 WHERE p.id=$3 AND p.organization_id=$1 AND p.owner_id=$2 AND e.id=$4 AND a.id=$5
 ON CONFLICT (episode_id,asset_id,state_label) DO UPDATE SET state_label=EXCLUDED.state_label
 RETURNING id,asset_id,state_label`, org, owner, project, episode, asset, uuid.NewString(), state).Scan(&a.ID, &a.AssetID, &a.StateLabel)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, drama.ErrNotFound
	}
	if err != nil {
		return a, err
	}
	err = r.db.QueryRow(ctx, `SELECT kind,name,description,appearance_description,voice_description FROM drama_assets WHERE id=$1`, a.AssetID).Scan(&a.Kind, &a.Name, &a.Description, &a.AppearanceDescription, &a.VoiceDescription)
	return a, err
}
func (r *DramaRepository) RemoveEpisodeAsset(ctx context.Context, org, owner, project, episode, link string) error {
	tag, err := r.db.Exec(ctx, `DELETE FROM drama_episode_asset_links l USING drama_script_episodes e,drama_projects p WHERE l.id=$5 AND l.episode_id=e.id AND e.id=$4 AND e.project_id=p.id AND p.id=$3 AND p.organization_id=$1 AND p.owner_id=$2`, org, owner, project, episode, link)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return drama.ErrNotFound
	}
	return nil
}
