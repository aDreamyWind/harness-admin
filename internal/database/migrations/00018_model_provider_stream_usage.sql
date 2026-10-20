-- +goose Up
ALTER TABLE model_provider_connections
    ADD COLUMN stream_usage_enabled BOOLEAN NOT NULL DEFAULT FALSE;

COMMENT ON TABLE model_provider_connections IS '企业模型供应商连接，包含流式用量请求设置';
COMMENT ON COLUMN model_provider_connections.stream_usage_enabled IS '启用时在流式聊天请求中向上游发送 stream_options.include_usage=true；默认关闭以兼容不支持的供应商';

-- +goose Down
ALTER TABLE model_provider_connections
    DROP COLUMN stream_usage_enabled;
COMMENT ON TABLE model_provider_connections IS '企业模型供应商连接';
