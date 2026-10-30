-- +goose Up
ALTER TABLE drama_episode_asset_links ALTER COLUMN state_label TYPE text;
COMMENT ON COLUMN drama_episode_asset_links.state_label IS '项目资产在本集中的形象、时段或状态说明，多种说明按换行保留，不参与资产身份判定';
CREATE TEMP TABLE drama_episode_asset_dedup ON COMMIT DROP AS
SELECT episode_id, asset_id,
       (array_agg(id ORDER BY CASE WHEN btrim(state_label)<>'' THEN 0 ELSE 1 END,created_at,id))[1] AS keep_id,
       COALESCE(string_agg(DISTINCT NULLIF(btrim(state_label),''), E'\n' ORDER BY NULLIF(btrim(state_label),'')), '') AS states
FROM drama_episode_asset_links GROUP BY episode_id,asset_id HAVING count(*)>1;
UPDATE drama_episode_asset_links l SET state_label=d.states FROM drama_episode_asset_dedup d WHERE l.id=d.keep_id;
DELETE FROM drama_episode_asset_links l USING drama_episode_asset_dedup d
WHERE l.episode_id=d.episode_id AND l.asset_id=d.asset_id AND l.id<>d.keep_id;
ALTER TABLE drama_episode_asset_links DROP CONSTRAINT drama_episode_asset_links_episode_id_asset_id_state_label_key;
ALTER TABLE drama_episode_asset_links ADD CONSTRAINT drama_episode_asset_links_episode_asset_key UNIQUE(episode_id,asset_id);
COMMENT ON TABLE drama_episode_asset_links IS '每集对项目真实角色、场景或道具的关联，同一资产每集唯一，状态说明不创建新资产';

-- +goose Down
ALTER TABLE drama_episode_asset_links DROP CONSTRAINT drama_episode_asset_links_episode_asset_key;
ALTER TABLE drama_episode_asset_links ADD CONSTRAINT drama_episode_asset_links_episode_id_asset_id_state_label_key UNIQUE(episode_id,asset_id,state_label);
-- 保留 text 类型，避免回退时截断已经合并的状态说明。
COMMENT ON COLUMN drama_episode_asset_links.state_label IS '本集出现时的形象、时段或状态标签';
COMMENT ON TABLE drama_episode_asset_links IS '每集剧本关联的角色、场景和道具及其出现状态';
