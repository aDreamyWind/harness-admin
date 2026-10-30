-- +goose Up
ALTER TABLE drama_batches ADD COLUMN analysis_context text NOT NULL DEFAULT '';
COMMENT ON COLUMN drama_batches.analysis_context IS '本次解析的工作流版本、模型及概览快照，续跑时沿用';
ALTER TABLE drama_batch_steps DROP CONSTRAINT drama_batch_steps_stage_check;
ALTER TABLE drama_batch_steps ADD CONSTRAINT drama_batch_steps_stage_check CHECK(stage IN ('art','story','plan','director','review'));
COMMENT ON COLUMN drama_batch_steps.stage IS '工作阶段：美术指导、剧情整理、分集规划、导演或剧本审校';
COMMENT ON COLUMN drama_batch_steps.chunk_number IS '当前阶段内原文片段或剧集的序号，从一开始';
COMMENT ON COLUMN drama_batch_steps.chunk_total IS '当前阶段的原文片段或剧集总数';
CREATE TABLE drama_batch_events (
 id bigserial PRIMARY KEY,
 batch_id uuid NOT NULL REFERENCES drama_batches(id) ON DELETE CASCADE,
 analysis_revision integer NOT NULL,
 stage varchar(16) NOT NULL,
 step_number integer NOT NULL DEFAULT 0,
 kind varchar(24) NOT NULL,
 message text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT NOW()
);
CREATE INDEX drama_batch_events_cursor_idx ON drama_batch_events(batch_id,id);
COMMENT ON TABLE drama_batch_events IS '漫剧解析的真实执行事件、流式正文和调度记录';
COMMENT ON COLUMN drama_batch_events.id IS '全局递增事件序号，用于断线后续传';
COMMENT ON COLUMN drama_batch_events.batch_id IS '所属解析批次';
COMMENT ON COLUMN drama_batch_events.analysis_revision IS '事件对应的原文范围版本';
COMMENT ON COLUMN drama_batch_events.stage IS '美术、剧情整理、分集规划、导演或审校阶段';
COMMENT ON COLUMN drama_batch_events.step_number IS '阶段内的片段或剧集序号';
COMMENT ON COLUMN drama_batch_events.kind IS '开始、完成、重试、调度、流式正文或失败等事件类型';
COMMENT ON COLUMN drama_batch_events.message IS '面向用户的进度说明或增量剧本文字';
COMMENT ON COLUMN drama_batch_events.created_at IS '真实事件发生时间';
-- +goose Down
DROP TABLE drama_batch_events;
ALTER TABLE drama_batches DROP COLUMN analysis_context;
ALTER TABLE drama_batch_steps DROP CONSTRAINT drama_batch_steps_stage_check;
ALTER TABLE drama_batch_steps ADD CONSTRAINT drama_batch_steps_stage_check CHECK(stage IN ('art','director')) NOT VALID;
