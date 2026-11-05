-- +goose Up
CREATE TABLE drama_canvas_generation_jobs (
    project_id uuid NOT NULL REFERENCES drama_projects(id) ON DELETE CASCADE,
    node_id uuid NOT NULL,
    id uuid NOT NULL UNIQUE,
    kind varchar(16) NOT NULL CHECK (kind IN ('image', 'video')),
    model_code varchar(160) NOT NULL,
    status varchar(16) NOT NULL CHECK (status IN ('running', 'succeeded', 'failed')),
    result_media_id uuid,
    error_message text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT NOW(),
    expires_at timestamptz NOT NULL,
    finished_at timestamptz,
    PRIMARY KEY (project_id, node_id),
    CHECK ((status = 'running') = (finished_at IS NULL)),
    CHECK ((status = 'failed') = (error_message <> '')),
    CHECK (status <> 'succeeded' OR result_media_id IS NOT NULL)
);

COMMENT ON TABLE drama_canvas_generation_jobs IS '漫剧画布图片和视频节点的最近一次生成任务及最近成功素材';
COMMENT ON COLUMN drama_canvas_generation_jobs.project_id IS '画布所属项目，通过项目校验组织和所有者';
COMMENT ON COLUMN drama_canvas_generation_jobs.node_id IS '画布中的图片或视频节点标识';
COMMENT ON COLUMN drama_canvas_generation_jobs.id IS '最近一次生成任务标识，也是本次成功时的媒体文件标识';
COMMENT ON COLUMN drama_canvas_generation_jobs.kind IS '生成类型，图片或视频';
COMMENT ON COLUMN drama_canvas_generation_jobs.model_code IS '本次使用的模型目录编码';
COMMENT ON COLUMN drama_canvas_generation_jobs.status IS '本次任务状态，生成中、成功或失败';
COMMENT ON COLUMN drama_canvas_generation_jobs.result_media_id IS '最近一次成功生成的素材标识，重试和失败时保留';
COMMENT ON COLUMN drama_canvas_generation_jobs.error_message IS '失败时可显示给项目所有者的错误说明';
COMMENT ON COLUMN drama_canvas_generation_jobs.created_at IS '本次生成任务创建时间';
COMMENT ON COLUMN drama_canvas_generation_jobs.expires_at IS '本次任务租约截止时间，用于释放进程中断的任务';
COMMENT ON COLUMN drama_canvas_generation_jobs.finished_at IS '本次生成完成时间';

-- +goose Down
DROP TABLE drama_canvas_generation_jobs;
