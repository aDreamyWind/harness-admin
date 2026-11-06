-- +goose Up
CREATE TABLE drama_project_covers (
    project_id uuid PRIMARY KEY REFERENCES drama_projects(id) ON DELETE CASCADE,
    job_id uuid NOT NULL UNIQUE,
    model_code varchar(160) NOT NULL,
    status varchar(16) NOT NULL CHECK (status IN ('running', 'succeeded', 'failed')),
    error_message text NOT NULL DEFAULT '',
    expires_at timestamptz NOT NULL,
    cover_id uuid,
    cover_data bytea CHECK (octet_length(cover_data) BETWEEN 1 AND 10485760),
    candidate_data bytea CHECK (octet_length(candidate_data) BETWEEN 1 AND 10485760),
    CHECK ((status = 'failed') = (error_message <> '')),
    CHECK ((cover_id IS NULL) = (cover_data IS NULL)),
    CHECK (status <> 'succeeded' OR cover_id IS NOT NULL),
    CHECK (candidate_data IS NULL OR (status = 'succeeded' AND cover_id <> job_id))
);

COMMENT ON TABLE drama_project_covers IS '漫剧项目当前封面、最新封面生成任务及待选择的候选封面';
COMMENT ON COLUMN drama_project_covers.project_id IS '所属漫剧项目，删除项目时一并清理';
COMMENT ON COLUMN drama_project_covers.job_id IS '最近一次生成任务标识，也是该任务结果的封面版本标识';
COMMENT ON COLUMN drama_project_covers.model_code IS '最近一次生成使用的图片模型目录编码';
COMMENT ON COLUMN drama_project_covers.status IS '最近一次生成的任务状态，生成中、成功或失败';
COMMENT ON COLUMN drama_project_covers.error_message IS '失败时可向项目所有者显示的说明';
COMMENT ON COLUMN drama_project_covers.expires_at IS '生成任务最迟完成时间，用于释放中断任务';
COMMENT ON COLUMN drama_project_covers.cover_id IS '当前使用的封面版本标识';
COMMENT ON COLUMN drama_project_covers.cover_data IS '当前使用的封面，包含生成底图和项目标题';
COMMENT ON COLUMN drama_project_covers.candidate_data IS '候选封面，用户选择后清除';

-- +goose Down
DROP TABLE drama_project_covers;
