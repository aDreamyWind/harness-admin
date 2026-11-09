-- +goose Up
CREATE TABLE novel_memory_jobs (
    project_id uuid NOT NULL REFERENCES novel_projects(id) ON DELETE CASCADE,
    chapter integer NOT NULL CHECK (chapter BETWEEN 1 AND 500),
    id uuid NOT NULL UNIQUE DEFAULT gen_random_uuid(),
    model_code text NOT NULL DEFAULT '',
    status text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'succeeded', 'failed')),
    facts jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(facts) = 'array'),
    lease_id uuid,
    lease_expires_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    PRIMARY KEY (project_id, chapter),
    CHECK (status = 'succeeded' OR facts = '[]'::jsonb),
    CHECK (CASE WHEN status = 'running' THEN lease_id IS NOT NULL AND lease_expires_at IS NOT NULL
                ELSE lease_id IS NULL AND lease_expires_at IS NULL END)
);
CREATE INDEX novel_memory_jobs_claim_idx ON novel_memory_jobs (updated_at, chapter, id)
    WHERE status IN ('queued', 'running');

COMMENT ON TABLE novel_memory_jobs IS '小说已定稿章节的记忆提取任务及有原文证据的事实';
COMMENT ON COLUMN novel_memory_jobs.project_id IS '所属小说作品标识，删除作品时同时删除记忆任务及事实';
COMMENT ON COLUMN novel_memory_jobs.chapter IS '从第一章开始的定稿章号';
COMMENT ON COLUMN novel_memory_jobs.id IS '当前任务标识，改动定稿正文或重新提交失败任务时更换';
COMMENT ON COLUMN novel_memory_jobs.model_code IS '入队时作品使用的文本模型编码，未配置时为空';
COMMENT ON COLUMN novel_memory_jobs.status IS '提取状态，包括待领取、执行中、成功和失败';
COMMENT ON COLUMN novel_memory_jobs.facts IS '提取成功后保存的事实数组，每条事实引用本章原文';
COMMENT ON COLUMN novel_memory_jobs.lease_id IS '当前领取凭证，每次领取时更换，防止旧执行者写回';
COMMENT ON COLUMN novel_memory_jobs.lease_expires_at IS '当前领取凭证的截止时间，到期后可以重新领取';
COMMENT ON COLUMN novel_memory_jobs.updated_at IS '最后一次入队、领取或保存结果的时间';

CREATE TABLE novel_memory_decisions (
    project_id uuid NOT NULL REFERENCES novel_projects(id) ON DELETE CASCADE,
    conflict_id text NOT NULL CHECK (conflict_id ~ '^[0-9a-f]{64}$'),
    choice smallint NOT NULL CHECK (choice IN (0, 1)),
    PRIMARY KEY (project_id, conflict_id)
);

COMMENT ON TABLE novel_memory_decisions IS '小说作者对故事记忆中冲突事实的取舍记录';
COMMENT ON COLUMN novel_memory_decisions.project_id IS '所属小说作品标识，删除作品时同时删除取舍记录';
COMMENT ON COLUMN novel_memory_decisions.conflict_id IS '根据两份冲突证据及其章号生成的摘要，证据变动后旧决定不适用';
COMMENT ON COLUMN novel_memory_decisions.choice IS '作者选用的候选序号，从零开始';

-- +goose Down
DROP TABLE novel_memory_decisions;
DROP TABLE novel_memory_jobs;
