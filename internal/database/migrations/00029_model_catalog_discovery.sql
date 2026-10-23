-- +goose Up
ALTER TABLE model_provider_connections DROP CONSTRAINT model_provider_connections_kind_check;
ALTER TABLE model_provider_connections ADD CONSTRAINT model_provider_connections_kind_check
    CHECK (provider_kind IN ('openai_compatible', 'deepseek', 'aliyun_bailian', 'kimi', 'ollama', 'siliconflow', 'claude', 'gemini'));
ALTER TABLE model_catalog_entries DROP CONSTRAINT model_catalog_entries_type_check;
ALTER TABLE model_catalog_entries ADD CONSTRAINT model_catalog_entries_type_check
    CHECK (model_type IN ('chat', 'image', 'video', 'audio', 'embedding', 'rerank', 'unknown'));
COMMENT ON COLUMN model_provider_connections.provider_kind IS '供应商协议类型，决定模型发现接口和对话网关的调用方式';
COMMENT ON COLUMN model_catalog_entries.model_type IS '模型类型：对话、图片、视频、音频、向量、重排或待确认';

-- +goose Down
ALTER TABLE model_catalog_entries DROP CONSTRAINT model_catalog_entries_type_check;
ALTER TABLE model_catalog_entries ADD CONSTRAINT model_catalog_entries_type_check CHECK (model_type = 'chat');
ALTER TABLE model_provider_connections DROP CONSTRAINT model_provider_connections_kind_check;
ALTER TABLE model_provider_connections ADD CONSTRAINT model_provider_connections_kind_check
    CHECK (provider_kind IN ('openai_compatible', 'deepseek', 'aliyun_bailian', 'kimi', 'ollama'));
COMMENT ON COLUMN model_provider_connections.provider_kind IS '模型供应商协议类型';
COMMENT ON COLUMN model_catalog_entries.model_type IS '模型类型，目前仅对话';
