-- +goose Up
ALTER TABLE model_provider_connections
    DROP CONSTRAINT model_provider_connections_kind_check;

ALTER TABLE model_provider_connections
    ADD CONSTRAINT model_provider_connections_kind_check
    CHECK (provider_kind IN ('openai_compatible', 'deepseek', 'aliyun_bailian', 'kimi', 'ollama'));

COMMENT ON COLUMN model_provider_connections.provider_kind IS '供应商协议类型：openai_compatible OpenAI兼容、deepseek DeepSeek、aliyun_bailian 阿里云百炼、kimi Kimi、ollama Ollama';

-- +goose Down
UPDATE model_provider_connections
SET provider_kind = 'openai_compatible'
WHERE provider_kind <> 'openai_compatible';

ALTER TABLE model_provider_connections
    DROP CONSTRAINT model_provider_connections_kind_check;

ALTER TABLE model_provider_connections
    ADD CONSTRAINT model_provider_connections_kind_check
    CHECK (provider_kind IN ('openai_compatible'));

COMMENT ON COLUMN model_provider_connections.provider_kind IS '供应商协议类型，当前支持 openai_compatible';
