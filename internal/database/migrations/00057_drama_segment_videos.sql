-- +goose Up
CREATE TABLE drama_segment_videos (
 id uuid PRIMARY KEY,
 segment_id uuid NOT NULL REFERENCES drama_storyboard_segments(id) ON DELETE CASCADE,
 storyboard_revision integer NOT NULL,
 model_code text NOT NULL,
 duration_seconds integer NOT NULL CHECK (duration_seconds BETWEEN 2 AND 30),
 reference_image_ids uuid[] NOT NULL,
 status text NOT NULL CHECK (status IN ('running','failed','succeeded')),
 error_message text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(),
 finished_at timestamptz
);
CREATE UNIQUE INDEX drama_segment_videos_one_running ON drama_segment_videos(segment_id) WHERE status='running';
CREATE INDEX drama_segment_videos_history ON drama_segment_videos(segment_id, created_at DESC);
COMMENT ON TABLE drama_segment_videos IS '漫剧分镜片段的视频生成记录';
COMMENT ON COLUMN drama_segment_videos.id IS '任务主键，亦用于定位本地视频文件';
COMMENT ON COLUMN drama_segment_videos.segment_id IS '所属分镜片段，可追溯到分镜版本';
COMMENT ON COLUMN drama_segment_videos.storyboard_revision IS '生成时分镜版本的修订号';
COMMENT ON COLUMN drama_segment_videos.model_code IS '生成时选用的视频模型编码';
COMMENT ON COLUMN drama_segment_videos.duration_seconds IS '提交给视频模型的目标秒数';
COMMENT ON COLUMN drama_segment_videos.reference_image_ids IS '按模型输入顺序保存的参考图版本主键';
COMMENT ON COLUMN drama_segment_videos.status IS '生成状态';
COMMENT ON COLUMN drama_segment_videos.error_message IS '面向用户的失败原因';
COMMENT ON COLUMN drama_segment_videos.created_at IS '创建时间';
COMMENT ON COLUMN drama_segment_videos.finished_at IS '完成或失败时间';

-- +goose Down
DROP TABLE drama_segment_videos;
