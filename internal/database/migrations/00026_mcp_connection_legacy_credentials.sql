-- +goose Up
ALTER TABLE mcp_connections
    ADD COLUMN inherit_service_credentials BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE mcp_connections c
SET inherit_service_credentials = TRUE
FROM mcp_services s
WHERE c.service_id = s.id AND s.credentials_enc IS NOT NULL;

COMMENT ON COLUMN mcp_connections.inherit_service_credentials IS '旧连接是否继续继承历史服务级上游参数值；新连接始终为否';

-- +goose Down
ALTER TABLE mcp_connections DROP COLUMN inherit_service_credentials;
