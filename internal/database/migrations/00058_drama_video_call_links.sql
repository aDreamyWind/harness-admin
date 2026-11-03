-- +goose Up
ALTER TABLE model_call_logs ADD COLUMN segment_video_id uuid REFERENCES drama_segment_videos(id) ON DELETE SET NULL;
COMMENT ON COLUMN model_call_logs.segment_video_id IS '发起此次模型提交或状态查询的漫剧片段视频任务';
CREATE INDEX model_call_logs_segment_video ON model_call_logs(segment_video_id) WHERE segment_video_id IS NOT NULL;

-- +goose Down
DROP INDEX model_call_logs_segment_video;
ALTER TABLE model_call_logs DROP COLUMN segment_video_id;
