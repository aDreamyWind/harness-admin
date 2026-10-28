-- +goose Up
CREATE TABLE drama_batch_steps (
    batch_id uuid NOT NULL REFERENCES drama_batches(id) ON DELETE CASCADE,
    analysis_revision integer NOT NULL,
    stage varchar(16) NOT NULL CHECK (stage IN ('art', 'director')),
    chunk_number integer NOT NULL CHECK (chunk_number > 0),
    chunk_total integer NOT NULL CHECK (chunk_total > 0),
    status varchar(16) NOT NULL CHECK (status IN ('running', 'completed', 'failed')),
    result_text text NOT NULL DEFAULT '',
    error_message text NOT NULL DEFAULT '',
    duration_ms bigint NOT NULL DEFAULT 0,
    started_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    PRIMARY KEY (batch_id, analysis_revision, stage, chunk_number)
);
CREATE INDEX drama_batch_steps_progress_idx ON drama_batch_steps(batch_id, analysis_revision, stage, chunk_number);
COMMENT ON TABLE drama_batch_steps IS '漫剧批次美术指导和导演逐段解析的持久化进度与可恢复结果';
COMMENT ON COLUMN drama_batch_steps.batch_id IS '所属漫剧解析批次';
COMMENT ON COLUMN drama_batch_steps.analysis_revision IS '解析时的批次范围版本';
COMMENT ON COLUMN drama_batch_steps.stage IS '工作阶段：美术指导或导演';
COMMENT ON COLUMN drama_batch_steps.chunk_number IS '当前原文片段序号，从一开始';
COMMENT ON COLUMN drama_batch_steps.chunk_total IS '本批原文片段总数';
COMMENT ON COLUMN drama_batch_steps.status IS '步骤状态：运行中、已完成或失败';
COMMENT ON COLUMN drama_batch_steps.result_text IS '已校验的模型结构化结果，重试时从这里恢复';
COMMENT ON COLUMN drama_batch_steps.error_message IS '当前步骤失败原因';
COMMENT ON COLUMN drama_batch_steps.duration_ms IS '当前步骤模型调用和校验耗时，单位毫秒';
COMMENT ON COLUMN drama_batch_steps.started_at IS '步骤最近一次开始时间';
COMMENT ON COLUMN drama_batch_steps.updated_at IS '步骤最近状态变更时间';

-- +goose Down
DROP TABLE drama_batch_steps;
