-- +goose Up
CREATE TABLE drama_user_settings (
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    text_model_code varchar(80) NOT NULL DEFAULT '',
    image_model_code varchar(80) NOT NULL DEFAULT '',
    video_model_code varchar(80) NOT NULL DEFAULT '',
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    PRIMARY KEY (organization_id, user_id)
);
COMMENT ON TABLE drama_user_settings IS '用户在 AI 漫剧中使用的默认模型配置';
COMMENT ON COLUMN drama_user_settings.organization_id IS '所属企业标识';
COMMENT ON COLUMN drama_user_settings.user_id IS '配置所属用户标识';
COMMENT ON COLUMN drama_user_settings.text_model_code IS '剧本生成和解析使用的对话模型编码';
COMMENT ON COLUMN drama_user_settings.image_model_code IS '后续图片生成使用的模型编码';
COMMENT ON COLUMN drama_user_settings.video_model_code IS '后续视频生成使用的模型编码';
COMMENT ON COLUMN drama_user_settings.updated_at IS '配置最近修改时间';

-- +goose Down
DROP TABLE drama_user_settings;
