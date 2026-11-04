-- +goose Up
CREATE TABLE novel_project_covers (
    project_id uuid PRIMARY KEY REFERENCES novel_projects(id) ON DELETE CASCADE,
    revision uuid NOT NULL,
    source text NOT NULL CHECK (source IN ('upload', 'ai')),
    mime_type text NOT NULL CHECK (mime_type IN ('image/jpeg', 'image/png', 'image/webp')),
    image_data bytea NOT NULL CHECK (octet_length(image_data) BETWEEN 1 AND 10485760),
    updated_at timestamptz NOT NULL DEFAULT NOW()
);
COMMENT ON TABLE novel_project_covers IS 'AI 小说作品当前使用的封面，每部作品保存一张';
COMMENT ON COLUMN novel_project_covers.project_id IS '所属小说作品标识，删除作品时同时删除封面';
COMMENT ON COLUMN novel_project_covers.revision IS '封面更换版本，用于并发保护和图片地址刷新';
COMMENT ON COLUMN novel_project_covers.source IS '封面来源，包括作者上传和模型生成';
COMMENT ON COLUMN novel_project_covers.mime_type IS '根据图片实际内容识别的安全媒体类型';
COMMENT ON COLUMN novel_project_covers.image_data IS '经过格式及大小校验的封面图片二进制数据';
COMMENT ON COLUMN novel_project_covers.updated_at IS '最后一次成功保存封面的时间';

-- +goose Down
DROP TABLE novel_project_covers;
