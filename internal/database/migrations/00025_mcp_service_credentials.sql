-- +goose Up
ALTER TABLE mcp_services
    ADD COLUMN description TEXT NOT NULL DEFAULT '',
    ADD COLUMN credentials_enc BYTEA;
COMMENT ON COLUMN mcp_services.description IS 'MCP 服务用途及能力说明';
COMMENT ON COLUMN mcp_services.credentials_enc IS '服务级上游凭证参数值列表的加密 JSON，供该服务的连接默认使用';

-- +goose Down
ALTER TABLE mcp_services DROP COLUMN IF EXISTS credentials_enc;
ALTER TABLE mcp_services DROP COLUMN IF EXISTS description;
