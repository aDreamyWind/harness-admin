-- +goose Up
ALTER TABLE mcp_connections
    ADD COLUMN upstream_header_name VARCHAR(64),
    ADD COLUMN upstream_header_value_enc BYTEA,
    ADD COLUMN upstream_query_name VARCHAR(64),
    ADD COLUMN upstream_query_value_enc BYTEA,
    ADD CONSTRAINT mcp_connections_header_pair_check CHECK (
        (upstream_header_name IS NULL) = (upstream_header_value_enc IS NULL)),
    ADD CONSTRAINT mcp_connections_query_pair_check CHECK (
        (upstream_query_name IS NULL) = (upstream_query_value_enc IS NULL));
COMMENT ON COLUMN mcp_connections.upstream_header_name IS '连接专用上游 API Key 请求头名称';
COMMENT ON COLUMN mcp_connections.upstream_header_value_enc IS '连接专用上游 API Key 请求头值密文';
COMMENT ON COLUMN mcp_connections.upstream_query_name IS '连接专用上游 API Key URL 参数名称';
COMMENT ON COLUMN mcp_connections.upstream_query_value_enc IS '连接专用上游 API Key URL 参数值密文';

-- +goose Down
ALTER TABLE mcp_connections
    DROP CONSTRAINT IF EXISTS mcp_connections_header_pair_check,
    DROP CONSTRAINT IF EXISTS mcp_connections_query_pair_check,
    DROP COLUMN IF EXISTS upstream_header_name,
    DROP COLUMN IF EXISTS upstream_header_value_enc,
    DROP COLUMN IF EXISTS upstream_query_name,
    DROP COLUMN IF EXISTS upstream_query_value_enc;
