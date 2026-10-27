-- +goose Up
CREATE TABLE drama_projects (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    owner_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title varchar(120) NOT NULL,
    source_name varchar(255) NOT NULL DEFAULT '',
    source_text text NOT NULL,
    source_length integer NOT NULL CHECK (source_length > 0),
    source_mode varchar(16) NOT NULL CHECK (source_mode IN ('upload','generate')),
    style varchar(80) NOT NULL,
    aspect_ratio varchar(5) NOT NULL CHECK (aspect_ratio IN ('9:16', '16:9')),
    model_code varchar(80) NOT NULL,
    status varchar(16) NOT NULL CHECK (status IN ('queued','running','ready','failed')),
    phase varchar(16) NOT NULL,
    error_message varchar(500) NOT NULL DEFAULT '',
    overview jsonb NOT NULL DEFAULT '{}'::jsonb,
    characters jsonb NOT NULL DEFAULT '[]'::jsonb,
    scenes jsonb NOT NULL DEFAULT '[]'::jsonb,
    props jsonb NOT NULL DEFAULT '[]'::jsonb,
    episodes jsonb NOT NULL DEFAULT '[]'::jsonb,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW()
);
CREATE INDEX drama_projects_owner_updated_idx ON drama_projects(organization_id, owner_id, updated_at DESC);
COMMENT ON TABLE drama_projects IS 'AI 漫剧项目及上传原文和分阶段解析结果';
COMMENT ON COLUMN drama_projects.id IS '项目唯一标识';
COMMENT ON COLUMN drama_projects.organization_id IS '所属企业标识';
COMMENT ON COLUMN drama_projects.owner_id IS '创建项目的用户标识';
COMMENT ON COLUMN drama_projects.title IS '项目标题';
COMMENT ON COLUMN drama_projects.source_name IS '上传原文的文件名';
COMMENT ON COLUMN drama_projects.source_text IS '完整保存的小说或剧本原文';
COMMENT ON COLUMN drama_projects.source_length IS '原文的 Unicode 字符数';
COMMENT ON COLUMN drama_projects.source_mode IS '来源方式：上传原文或 AI 生成剧本';
COMMENT ON COLUMN drama_projects.style IS '选择的视频视觉风格';
COMMENT ON COLUMN drama_projects.aspect_ratio IS '画面比例';
COMMENT ON COLUMN drama_projects.model_code IS '剧本解析使用的模型目录编码';
COMMENT ON COLUMN drama_projects.status IS '解析任务状态';
COMMENT ON COLUMN drama_projects.phase IS '当前解析阶段';
COMMENT ON COLUMN drama_projects.error_message IS '解析失败的简体中文说明';
COMMENT ON COLUMN drama_projects.overview IS '项目概览字段';
COMMENT ON COLUMN drama_projects.characters IS '角色资产列表';
COMMENT ON COLUMN drama_projects.scenes IS '场景资产列表';
COMMENT ON COLUMN drama_projects.props IS '道具资产列表';
COMMENT ON COLUMN drama_projects.episodes IS '分集规划列表';
COMMENT ON COLUMN drama_projects.created_at IS '项目创建时间';
COMMENT ON COLUMN drama_projects.updated_at IS '项目最近修改时间';

-- +goose Down
DROP TABLE drama_projects;
