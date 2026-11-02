package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"harness-admin/internal/drama"
)

// 视频列表直接投影正式剧本，不复制标题和剧本，也不把旧概览中的分集规划当作可拍摄剧集。
const dramaVideoEpisodesSQL = `WITH episodes AS (
 SELECT e.*,b.name AS batch_name,b.batch_number,
 (b.status<>'ready' OR b.revision<>b.analysis_revision) AS needs_review,
 ROW_NUMBER() OVER (ORDER BY b.batch_number,e.number,e.id) AS project_number
 FROM drama_script_episodes e JOIN drama_batches b ON b.id=e.batch_id AND b.project_id=e.project_id
 JOIN drama_projects p ON p.id=e.project_id
 WHERE p.organization_id=$1 AND p.owner_id=$2 AND p.id=$3 AND BTRIM(e.script_text)<>''
) SELECT e.id,e.project_id,e.project_number,e.number,e.batch_id,e.batch_name,e.batch_number,e.title,e.synopsis,
 e.needs_review,e.updated_at,counts.characters,counts.scenes,counts.props,
 COALESCE((SELECT CASE WHEN r.script_text<>e.script_text THEN 'stale' ELSE r.status END FROM drama_storyboard_runs r WHERE r.source_episode_id=e.id ORDER BY r.version DESC LIMIT 1),'pending')`

const dramaVideoEpisodesFromSQL = ` FROM episodes e LEFT JOIN LATERAL (
 SELECT COUNT(*) FILTER (WHERE a.kind='character') AS characters,
 COUNT(*) FILTER (WHERE a.kind='scene') AS scenes,COUNT(*) FILTER (WHERE a.kind='prop') AS props
 FROM drama_episode_asset_links l JOIN drama_assets a ON a.id=l.asset_id AND a.project_id=e.project_id
 WHERE l.episode_id=e.id
 ) counts ON true`

func scanVideoEpisode(row pgx.Row) (drama.VideoEpisodeWorkspace, error) {
	item := drama.VideoEpisodeWorkspace{Characters: []drama.EpisodeAsset{}, Scenes: []drama.EpisodeAsset{}, Props: []drama.EpisodeAsset{}}
	e := &item.Episode
	err := row.Scan(&e.ScriptEpisodeID, &e.ProjectID, &e.Number, &e.BatchEpisodeNumber, &e.Batch.ID, &e.Batch.Name, &e.Batch.Number, &e.Title, &e.Synopsis, &e.ScriptNeedsReview, &e.ScriptUpdatedAt, &e.CharacterCount, &e.SceneCount, &e.PropCount, &e.StoryboardStatus, &item.ScriptText)
	if errors.Is(err, pgx.ErrNoRows) {
		return item, drama.ErrNotFound
	}
	return item, err
}

func (r *DramaRepository) ListVideoEpisodes(ctx context.Context, org, owner, project string) ([]drama.VideoEpisode, error) {
	var exists bool
	if err := r.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM drama_projects WHERE id=$1 AND organization_id=$2 AND owner_id=$3)`, project, org, owner).Scan(&exists); err != nil {
		return nil, fmt.Errorf("核对视频分集项目失败: %w", err)
	}
	if !exists {
		return nil, drama.ErrNotFound
	}
	rows, err := r.db.Query(ctx, dramaVideoEpisodesSQL+`,''::text`+dramaVideoEpisodesFromSQL+` ORDER BY e.project_number`, org, owner, project)
	if err != nil {
		return nil, fmt.Errorf("读取视频分集失败: %w", err)
	}
	defer rows.Close()
	items := []drama.VideoEpisode{}
	for rows.Next() {
		item, err := scanVideoEpisode(rows)
		if err != nil {
			return nil, fmt.Errorf("读取视频分集失败: %w", err)
		}
		items = append(items, item.Episode)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("读取视频分集失败: %w", err)
	}
	return items, nil
}

func (r *DramaRepository) GetVideoEpisodeWorkspace(ctx context.Context, org, owner, project, episode string) (drama.VideoEpisodeWorkspace, error) {
	item, err := scanVideoEpisode(r.db.QueryRow(ctx, dramaVideoEpisodesSQL+`,e.script_text`+dramaVideoEpisodesFromSQL+` WHERE e.id=$4`, org, owner, project, episode))
	if err != nil {
		if errors.Is(err, drama.ErrNotFound) {
			return item, err
		}
		return item, fmt.Errorf("读取单集制作工作区失败: %w", err)
	}
	rows, err := r.db.Query(ctx, `SELECT l.id,l.asset_id,a.kind,a.name,a.description,a.appearance_description,a.voice_description,l.state_label
 FROM drama_episode_asset_links l JOIN drama_assets a ON a.id=l.asset_id
 JOIN drama_script_episodes e ON e.id=l.episode_id AND e.project_id=a.project_id
 JOIN drama_projects p ON p.id=e.project_id
 WHERE p.organization_id=$1 AND p.owner_id=$2 AND p.id=$3 AND e.id=$4 ORDER BY a.kind,a.name,a.id`, org, owner, project, episode)
	if err != nil {
		return item, fmt.Errorf("读取本集资产失败: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var a drama.EpisodeAsset
		if err = rows.Scan(&a.ID, &a.AssetID, &a.Kind, &a.Name, &a.Description, &a.AppearanceDescription, &a.VoiceDescription, &a.StateLabel); err != nil {
			return item, fmt.Errorf("读取本集资产失败: %w", err)
		}
		switch a.Kind {
		case "character":
			item.Characters = append(item.Characters, a)
		case "scene":
			item.Scenes = append(item.Scenes, a)
		case "prop":
			item.Props = append(item.Props, a)
		}
	}
	if err = rows.Err(); err != nil {
		return item, fmt.Errorf("读取本集资产失败: %w", err)
	}
	return item, nil
}
