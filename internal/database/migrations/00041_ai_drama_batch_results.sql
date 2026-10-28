-- +goose Up
CREATE TABLE drama_assets (
    id uuid PRIMARY KEY,
    project_id uuid NOT NULL REFERENCES drama_projects(id) ON DELETE CASCADE,
    kind varchar(16) NOT NULL CHECK (kind IN ('character', 'scene', 'prop')),
    name varchar(120) NOT NULL,
    description text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    UNIQUE (project_id, kind, name)
);
CREATE INDEX drama_assets_project_kind_idx ON drama_assets(project_id, kind, name);
COMMENT ON TABLE drama_assets IS '漫剧项目的角色、场景和道具资产目录';
COMMENT ON COLUMN drama_assets.id IS '资产唯一标识';
COMMENT ON COLUMN drama_assets.project_id IS '所属漫剧项目';
COMMENT ON COLUMN drama_assets.kind IS '资产类型：角色、场景或道具';
COMMENT ON COLUMN drama_assets.name IS '资产名称，项目内同类型唯一';
COMMENT ON COLUMN drama_assets.description IS '资产文字描述';
COMMENT ON COLUMN drama_assets.created_at IS '资产创建时间';
COMMENT ON COLUMN drama_assets.updated_at IS '资产最近修改时间';

CREATE TABLE drama_batch_assets (
    batch_id uuid NOT NULL REFERENCES drama_batches(id) ON DELETE CASCADE,
    asset_id uuid NOT NULL REFERENCES drama_assets(id) ON DELETE CASCADE,
    PRIMARY KEY (batch_id, asset_id)
);
COMMENT ON TABLE drama_batch_assets IS '剧本解析批次提取或使用的项目资产关联';
COMMENT ON COLUMN drama_batch_assets.batch_id IS '所属剧本解析批次';
COMMENT ON COLUMN drama_batch_assets.asset_id IS '关联的项目资产';

CREATE TABLE drama_script_episodes (
    id uuid PRIMARY KEY,
    project_id uuid NOT NULL REFERENCES drama_projects(id) ON DELETE CASCADE,
    batch_id uuid REFERENCES drama_batches(id) ON DELETE CASCADE,
    number integer NOT NULL CHECK (number > 0),
    title varchar(120) NOT NULL,
    synopsis text NOT NULL DEFAULT '',
    script_text text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    UNIQUE (batch_id, number)
);
CREATE INDEX drama_script_episodes_project_idx ON drama_script_episodes(project_id, batch_id, number);
COMMENT ON TABLE drama_script_episodes IS '剧本解析生成的分集剧本及原有分集规划';
COMMENT ON COLUMN drama_script_episodes.id IS '剧集唯一标识';
COMMENT ON COLUMN drama_script_episodes.project_id IS '所属漫剧项目';
COMMENT ON COLUMN drama_script_episodes.batch_id IS '生成本集的解析批次；历史分集规划可为空';
COMMENT ON COLUMN drama_script_episodes.number IS '所在批次内的集序号';
COMMENT ON COLUMN drama_script_episodes.title IS '剧集标题';
COMMENT ON COLUMN drama_script_episodes.synopsis IS '剧集梗概';
COMMENT ON COLUMN drama_script_episodes.script_text IS '完整的本集剧本文本';
COMMENT ON COLUMN drama_script_episodes.created_at IS '剧集创建时间';
COMMENT ON COLUMN drama_script_episodes.updated_at IS '剧集最近修改时间';

CREATE TABLE drama_episode_asset_links (
    id uuid PRIMARY KEY,
    episode_id uuid NOT NULL REFERENCES drama_script_episodes(id) ON DELETE CASCADE,
    asset_id uuid NOT NULL REFERENCES drama_assets(id) ON DELETE CASCADE,
    state_label varchar(120) NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT NOW(),
    UNIQUE (episode_id, asset_id, state_label)
);
CREATE INDEX drama_episode_asset_links_episode_idx ON drama_episode_asset_links(episode_id);
COMMENT ON TABLE drama_episode_asset_links IS '每集剧本关联的角色、场景和道具及其出现状态';
COMMENT ON COLUMN drama_episode_asset_links.id IS '剧集资产关联唯一标识';
COMMENT ON COLUMN drama_episode_asset_links.episode_id IS '所属剧集';
COMMENT ON COLUMN drama_episode_asset_links.asset_id IS '关联的项目资产';
COMMENT ON COLUMN drama_episode_asset_links.state_label IS '本集出现时的形象、时段或状态标签';
COMMENT ON COLUMN drama_episode_asset_links.created_at IS '关联创建时间';

INSERT INTO drama_assets (id, project_id, kind, name, description)
SELECT gen_random_uuid(), project_id, kind, name, description FROM (
    SELECT DISTINCT ON (p.id, source.kind, item.value->>'name')
        p.id AS project_id, source.kind, item.value->>'name' AS name,
        COALESCE(item.value->>'description', '') AS description
    FROM drama_projects p
    CROSS JOIN LATERAL (VALUES ('character', p.characters), ('scene', p.scenes), ('prop', p.props)) AS source(kind, items)
    CROSS JOIN LATERAL jsonb_array_elements(source.items) AS item(value)
    WHERE NULLIF(item.value->>'name', '') IS NOT NULL
    ORDER BY p.id, source.kind, item.value->>'name'
) legacy_assets;
INSERT INTO drama_script_episodes (id, project_id, number, title, synopsis)
SELECT gen_random_uuid(), p.id, item.ordinality::integer,
       COALESCE(NULLIF(item.value->>'title', ''), '未命名剧集'), COALESCE(item.value->>'synopsis', '')
FROM drama_projects p CROSS JOIN LATERAL jsonb_array_elements(p.episodes) WITH ORDINALITY AS item(value, ordinality);
ALTER TABLE drama_projects DROP COLUMN characters, DROP COLUMN scenes, DROP COLUMN props, DROP COLUMN episodes;
ALTER TABLE drama_batches DROP CONSTRAINT drama_batches_status_check;
ALTER TABLE drama_batches ADD CONSTRAINT drama_batches_status_check CHECK (status IN ('pending', 'running', 'failed', 'stale', 'ready'));
ALTER TABLE drama_batches ADD COLUMN error_message text NOT NULL DEFAULT '', ADD COLUMN analysis_revision integer NOT NULL DEFAULT 0;
COMMENT ON COLUMN drama_batches.status IS '批次解析状态：待解析、进行中、失败、范围需核对或已解析';
COMMENT ON COLUMN drama_batches.error_message IS '本批剧本解析失败的详细说明';
COMMENT ON COLUMN drama_batches.analysis_revision IS '开始解析时的批次范围版本，用于阻止旧结果覆盖新范围';

-- +goose Down
ALTER TABLE drama_projects ADD COLUMN characters jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN scenes jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN props jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN episodes jsonb NOT NULL DEFAULT '[]'::jsonb;
COMMENT ON COLUMN drama_projects.characters IS '兼容旧版本的角色资产列表';
COMMENT ON COLUMN drama_projects.scenes IS '兼容旧版本的场景资产列表';
COMMENT ON COLUMN drama_projects.props IS '兼容旧版本的道具资产列表';
COMMENT ON COLUMN drama_projects.episodes IS '兼容旧版本的分集规划列表';
UPDATE drama_projects p SET
    characters = COALESCE((SELECT jsonb_agg(jsonb_build_object('name', a.name, 'description', a.description) ORDER BY a.created_at) FROM drama_assets a WHERE a.project_id=p.id AND a.kind='character'), '[]'::jsonb),
    scenes = COALESCE((SELECT jsonb_agg(jsonb_build_object('name', a.name, 'description', a.description) ORDER BY a.created_at) FROM drama_assets a WHERE a.project_id=p.id AND a.kind='scene'), '[]'::jsonb),
    props = COALESCE((SELECT jsonb_agg(jsonb_build_object('name', a.name, 'description', a.description) ORDER BY a.created_at) FROM drama_assets a WHERE a.project_id=p.id AND a.kind='prop'), '[]'::jsonb),
    episodes = COALESCE((SELECT jsonb_agg(jsonb_build_object('title', e.title, 'synopsis', e.synopsis) ORDER BY e.created_at) FROM drama_script_episodes e WHERE e.project_id=p.id), '[]'::jsonb);
ALTER TABLE drama_batches DROP COLUMN error_message, DROP COLUMN analysis_revision;
ALTER TABLE drama_batches DROP CONSTRAINT drama_batches_status_check;
ALTER TABLE drama_batches ADD CONSTRAINT drama_batches_status_check CHECK (status IN ('pending', 'stale', 'ready'));
COMMENT ON COLUMN drama_batches.status IS '批次状态：待解析、范围需核对或解析完成';
DROP TABLE drama_episode_asset_links;
DROP TABLE drama_script_episodes;
DROP TABLE drama_batch_assets;
DROP TABLE drama_assets;
