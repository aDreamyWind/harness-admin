-- +goose Up
ALTER TABLE drama_batches ADD COLUMN name varchar(80) NOT NULL DEFAULT '未命名批次',
 ADD COLUMN name_manually_edited boolean NOT NULL DEFAULT false;
COMMENT ON COLUMN drama_batches.name IS '批次剧情名称，由独立 AI 调用生成或用户手动修改，编号另行保留';
COMMENT ON COLUMN drama_batches.name_manually_edited IS '用户是否已手动命名，AI 起名不得覆盖手动名称';
ALTER TABLE drama_assets ADD COLUMN origin_batch_id uuid REFERENCES drama_batches(id) ON DELETE SET NULL;
COMMENT ON COLUMN drama_assets.origin_batch_id IS '首次提取出此资产的批次主键；历史数据按现有最早批次记录回填，名称动态关联读取';
UPDATE drama_assets a SET origin_batch_id=(
 SELECT b.id FROM drama_batch_assets ba JOIN drama_batches b ON b.id=ba.batch_id
 WHERE ba.asset_id=a.id AND b.project_id=a.project_id ORDER BY b.created_at,b.batch_number,b.id LIMIT 1
) WHERE a.origin_batch_id IS NULL;
CREATE INDEX drama_assets_origin_batch_idx ON drama_assets(origin_batch_id);
CREATE INDEX drama_episode_asset_links_asset_episode_idx ON drama_episode_asset_links(asset_id,episode_id);
COMMENT ON INDEX drama_episode_asset_links_asset_episode_idx IS '按真实资产查询其应用剧集及所属批次';
ALTER TABLE drama_batch_steps DROP CONSTRAINT drama_batch_steps_stage_check;
ALTER TABLE drama_batch_steps ADD CONSTRAINT drama_batch_steps_stage_check CHECK(stage IN ('art','story','plan','name','director','review'));
COMMENT ON COLUMN drama_batch_steps.stage IS '工作阶段：美术指导、剧情整理、分集规划、批次命名、导演或剧本审校';
-- +goose Down
ALTER TABLE drama_batch_steps DROP CONSTRAINT drama_batch_steps_stage_check;
DELETE FROM drama_batch_steps WHERE stage='name';
ALTER TABLE drama_batch_steps ADD CONSTRAINT drama_batch_steps_stage_check CHECK(stage IN ('art','story','plan','director','review'));
COMMENT ON COLUMN drama_batch_steps.stage IS '工作阶段：美术指导、剧情整理、分集规划、导演或剧本审校';
DROP INDEX drama_episode_asset_links_asset_episode_idx;
DROP INDEX drama_assets_origin_batch_idx;
ALTER TABLE drama_assets DROP COLUMN origin_batch_id;
ALTER TABLE drama_batches DROP COLUMN name,DROP COLUMN name_manually_edited;
