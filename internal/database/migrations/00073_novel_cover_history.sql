-- +goose Up
CREATE TABLE novel_project_cover_history (
    project_id uuid NOT NULL REFERENCES novel_projects(id) ON DELETE CASCADE,
    revision uuid NOT NULL,
    source text NOT NULL CHECK (source IN ('upload', 'ai')),
    mime_type text NOT NULL CHECK (mime_type IN ('image/jpeg', 'image/png', 'image/webp')),
    image_data bytea NOT NULL CHECK (octet_length(image_data) BETWEEN 1 AND 10485760),
    created_at timestamptz NOT NULL DEFAULT NOW(),
    PRIMARY KEY (project_id, revision)
);
CREATE INDEX novel_project_cover_history_created_idx ON novel_project_cover_history (project_id, created_at DESC, revision DESC);

COMMENT ON TABLE novel_project_cover_history IS 'AI 小说作品历次上传及成功生成的封面版本';
COMMENT ON COLUMN novel_project_cover_history.project_id IS '所属小说作品标识，删除作品时同时删除封面版本';
COMMENT ON COLUMN novel_project_cover_history.revision IS '同一作品内不可变的封面版本标识';
COMMENT ON COLUMN novel_project_cover_history.source IS '封面来源，包括作者上传和模型生成';
COMMENT ON COLUMN novel_project_cover_history.mime_type IS '根据图片实际内容识别的安全媒体类型';
COMMENT ON COLUMN novel_project_cover_history.image_data IS '经过格式及大小校验的封面图片二进制数据';
COMMENT ON COLUMN novel_project_cover_history.created_at IS '封面版本首次保存的时间';

INSERT INTO novel_project_cover_history (project_id, revision, source, mime_type, image_data, created_at)
SELECT project_id, revision, source, mime_type, image_data, updated_at FROM novel_project_covers;

ALTER TABLE novel_project_covers
    ADD CONSTRAINT novel_project_covers_history_fk FOREIGN KEY (project_id, revision)
        REFERENCES novel_project_cover_history(project_id, revision) ON DELETE CASCADE,
    DROP COLUMN source,
    DROP COLUMN mime_type,
    DROP COLUMN image_data;

COMMENT ON TABLE novel_project_covers IS 'AI 小说作品当前使用的封面版本引用';
COMMENT ON COLUMN novel_project_covers.revision IS '当前采用的封面版本标识，用于并发保护和图片地址刷新';
COMMENT ON COLUMN novel_project_covers.updated_at IS '最后一次选用当前封面的时间';

-- +goose Down
ALTER TABLE novel_project_covers
    DROP CONSTRAINT novel_project_covers_history_fk,
    ADD COLUMN source text CHECK (source IN ('upload', 'ai')),
    ADD COLUMN mime_type text CHECK (mime_type IN ('image/jpeg', 'image/png', 'image/webp')),
    ADD COLUMN image_data bytea CHECK (octet_length(image_data) BETWEEN 1 AND 10485760);

UPDATE novel_project_covers c SET source=h.source, mime_type=h.mime_type, image_data=h.image_data
FROM novel_project_cover_history h WHERE h.project_id=c.project_id AND h.revision=c.revision;

ALTER TABLE novel_project_covers
    ALTER COLUMN source SET NOT NULL,
    ALTER COLUMN mime_type SET NOT NULL,
    ALTER COLUMN image_data SET NOT NULL;

COMMENT ON TABLE novel_project_covers IS 'AI 小说作品当前使用的封面，每部作品保存一张';
COMMENT ON COLUMN novel_project_covers.revision IS '封面更换版本，用于并发保护和图片地址刷新';
COMMENT ON COLUMN novel_project_covers.source IS '封面来源，包括作者上传和模型生成';
COMMENT ON COLUMN novel_project_covers.mime_type IS '根据图片实际内容识别的安全媒体类型';
COMMENT ON COLUMN novel_project_covers.image_data IS '经过格式及大小校验的封面图片二进制数据';
COMMENT ON COLUMN novel_project_covers.updated_at IS '最后一次成功保存封面的时间';

DROP TABLE novel_project_cover_history;
