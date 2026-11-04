-- +goose Up
CREATE TABLE novel_projects (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    owner_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title varchar(120) NOT NULL,
    brief text NOT NULL,
    chapter_count integer NOT NULL CHECK (chapter_count BETWEEN 1 AND 30),
    workspace jsonb NOT NULL CHECK (jsonb_typeof(workspace) = 'object'),
    version integer NOT NULL DEFAULT 1 CHECK (version > 0),
    updated_at timestamptz NOT NULL DEFAULT NOW()
);
CREATE INDEX novel_projects_owner_updated_idx ON novel_projects (organization_id, owner_id, updated_at DESC);
COMMENT ON TABLE novel_projects IS '按组织和作者隔离的 AI 小说作品';
COMMENT ON COLUMN novel_projects.id IS '作品唯一标识';
COMMENT ON COLUMN novel_projects.organization_id IS '所属组织标识';
COMMENT ON COLUMN novel_projects.owner_id IS '作品作者的用户标识';
COMMENT ON COLUMN novel_projects.title IS '作品名称';
COMMENT ON COLUMN novel_projects.brief IS '作者提供的故事创意与写作要求';
COMMENT ON COLUMN novel_projects.chapter_count IS '作品计划章节总数';
COMMENT ON COLUMN novel_projects.workspace IS '包含文本模型、大纲、章节草稿、审稿意见与定稿摘要的创作状态';
COMMENT ON COLUMN novel_projects.version IS '防止并发覆盖的作品版本号';
COMMENT ON COLUMN novel_projects.updated_at IS '最后一次成功保存时间';

ALTER TABLE model_call_logs DROP CONSTRAINT model_call_logs_source_check;
ALTER TABLE model_call_logs ADD CONSTRAINT model_call_logs_source_check CHECK (source IN ('model_gateway', 'ai_drama', 'ai_novel'));
COMMENT ON COLUMN model_call_logs.source IS '发起调用的业务入口，包括模型网关、AI 漫剧与 AI 小说';

-- +goose Down
UPDATE model_call_logs SET source = 'model_gateway' WHERE source = 'ai_novel';
ALTER TABLE model_call_logs DROP CONSTRAINT model_call_logs_source_check;
ALTER TABLE model_call_logs ADD CONSTRAINT model_call_logs_source_check CHECK (source IN ('model_gateway', 'ai_drama'));
COMMENT ON COLUMN model_call_logs.source IS '发起调用的业务入口，包括模型网关与 AI 漫剧';
DROP TABLE novel_projects;
