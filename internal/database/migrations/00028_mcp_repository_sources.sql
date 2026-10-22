-- +goose Up
ALTER TABLE mcp_services DROP CONSTRAINT mcp_services_source_type_check;
ALTER TABLE mcp_services ADD CONSTRAINT mcp_services_source_type_check
    CHECK (source_type IN ('remote', 'github', 'gitee', 'upload'));
ALTER TABLE mcp_service_versions DROP CONSTRAINT mcp_service_versions_source_type_check;
ALTER TABLE mcp_service_versions ADD CONSTRAINT mcp_service_versions_source_type_check
    CHECK (source_type IN ('github', 'gitee', 'upload'));
ALTER TABLE mcp_service_versions ADD COLUMN description TEXT NOT NULL DEFAULT '';
COMMENT ON COLUMN mcp_services.source_type IS '服务来源：远程接入、GitHub 仓库、Gitee 仓库或上传代码包';
COMMENT ON COLUMN mcp_services.description IS '服务描述；托管服务默认取当前成功部署版本根目录的 README.md';
COMMENT ON COLUMN mcp_service_versions.source_type IS '源码来源：GitHub、Gitee 或本地上传';
COMMENT ON COLUMN mcp_service_versions.source_ref IS '完整仓库地址或上传文件名，不包含密钥';
COMMENT ON COLUMN mcp_service_versions.revision IS '解析后的固定 Git 提交号或上传包 SHA-256 摘要';
COMMENT ON COLUMN mcp_service_versions.description IS '此源码版本根目录 README.md 的内容，用于构建成功和回退时更新服务描述';
COMMENT ON TABLE mcp_service_versions IS '托管 MCP 服务的源码版本、README 描述与构建结果';

-- +goose Down
ALTER TABLE mcp_service_versions DROP COLUMN description;
ALTER TABLE mcp_service_versions DROP CONSTRAINT mcp_service_versions_source_type_check;
ALTER TABLE mcp_service_versions ADD CONSTRAINT mcp_service_versions_source_type_check
    CHECK (source_type IN ('github', 'upload'));
ALTER TABLE mcp_services DROP CONSTRAINT mcp_services_source_type_check;
ALTER TABLE mcp_services ADD CONSTRAINT mcp_services_source_type_check
    CHECK (source_type IN ('remote', 'github', 'upload'));
