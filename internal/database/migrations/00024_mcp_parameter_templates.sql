-- +goose Up
ALTER TABLE mcp_services
    ADD COLUMN parameters JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD CONSTRAINT mcp_services_parameters_array_check CHECK (jsonb_typeof(parameters) = 'array');
COMMENT ON COLUMN mcp_services.parameters IS '上游 MCP 参数模板，记录参数位置、实际名称、显示名称及是否必填，不保存密钥值';

ALTER TABLE mcp_connections
    ADD COLUMN upstream_parameters_enc BYTEA;
COMMENT ON COLUMN mcp_connections.upstream_parameters_enc IS '连接专用上游参数值列表的加密 JSON，兼容任意请求头、URL 查询和路径参数';

-- +goose Down
ALTER TABLE mcp_connections DROP COLUMN IF EXISTS upstream_parameters_enc;
ALTER TABLE mcp_services DROP CONSTRAINT IF EXISTS mcp_services_parameters_array_check;
ALTER TABLE mcp_services DROP COLUMN IF EXISTS parameters;
