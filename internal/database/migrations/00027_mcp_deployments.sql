-- +goose Up
ALTER TABLE mcp_services DROP CONSTRAINT mcp_services_transport_check;
ALTER TABLE mcp_services ADD CONSTRAINT mcp_services_transport_check CHECK (transport IN ('streamable_http', 'sse', 'stdio'));
ALTER TABLE mcp_services ADD COLUMN source_type VARCHAR(16) NOT NULL DEFAULT 'remote'
    CHECK (source_type IN ('remote', 'github', 'upload'));
ALTER TABLE mcp_services ADD COLUMN runtime_type VARCHAR(16) NOT NULL DEFAULT ''
    CHECK (runtime_type IN ('', 'node', 'python'));
ALTER TABLE mcp_services ADD COLUMN entry_file TEXT NOT NULL DEFAULT '';
ALTER TABLE mcp_services ADD COLUMN image_ref TEXT NOT NULL DEFAULT '';
ALTER TABLE mcp_services ADD COLUMN container_name TEXT NOT NULL DEFAULT '';
ALTER TABLE mcp_services ADD COLUMN deployment_status VARCHAR(16) NOT NULL DEFAULT ''
    CHECK (deployment_status IN ('', 'building', 'failed', 'stopped', 'running'));
ALTER TABLE mcp_services ADD COLUMN deployment_error TEXT NOT NULL DEFAULT '';
ALTER TABLE mcp_services ADD COLUMN current_revision TEXT NOT NULL DEFAULT '';
COMMENT ON COLUMN mcp_services.source_type IS '服务来源：远程接入、GitHub 固定提交或上传代码包';
COMMENT ON COLUMN mcp_services.runtime_type IS '托管 Stdio 服务的运行语言：Node.js 或 Python';
COMMENT ON COLUMN mcp_services.entry_file IS '容器内相对 /app 的启动脚本路径';
COMMENT ON COLUMN mcp_services.image_ref IS '当前部署版本对应的本地容器镜像标识';
COMMENT ON COLUMN mcp_services.container_name IS '由系统生成并校验归属的容器名称';
COMMENT ON COLUMN mcp_services.deployment_status IS '托管服务最近记录的构建或运行状态';
COMMENT ON COLUMN mcp_services.deployment_error IS '最近一次构建或运行失败的脱敏原因';
COMMENT ON COLUMN mcp_services.current_revision IS '当前部署版本的 Git 提交号或上传内容摘要';
COMMENT ON COLUMN mcp_services.transport IS '上游传输类型：streamable_http、sse 或托管 stdio';
COMMENT ON COLUMN mcp_services.upstream_url IS '远程上游地址；托管 Stdio 服务为空';
COMMENT ON TABLE mcp_services IS '企业接入的远程或托管 MCP 服务';

CREATE TABLE mcp_service_versions (
    id UUID PRIMARY KEY,
    service_id UUID NOT NULL REFERENCES mcp_services(id) ON DELETE CASCADE,
    source_type VARCHAR(16) NOT NULL CHECK (source_type IN ('github', 'upload')),
    source_ref TEXT NOT NULL,
    revision TEXT NOT NULL,
    runtime_type VARCHAR(16) NOT NULL CHECK (runtime_type IN ('node', 'python')),
    entry_file TEXT NOT NULL,
    image_ref TEXT NOT NULL DEFAULT '',
    build_status VARCHAR(16) NOT NULL CHECK (build_status IN ('building', 'succeeded', 'failed')),
    build_log TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at TIMESTAMPTZ
);
CREATE INDEX mcp_service_versions_service_idx ON mcp_service_versions (service_id, created_at DESC);
COMMENT ON TABLE mcp_service_versions IS '托管 MCP 服务的源码版本与构建结果';
COMMENT ON COLUMN mcp_service_versions.id IS '构建版本标识';
COMMENT ON COLUMN mcp_service_versions.service_id IS '所属 MCP 服务标识';
COMMENT ON COLUMN mcp_service_versions.source_type IS '源码来源：GitHub 或本地上传';
COMMENT ON COLUMN mcp_service_versions.source_ref IS 'GitHub owner/repo 或上传文件名，不包含密钥';
COMMENT ON COLUMN mcp_service_versions.revision IS '固定 Git 提交号或上传包 SHA-256 摘要';
COMMENT ON COLUMN mcp_service_versions.runtime_type IS '固定构建运行时';
COMMENT ON COLUMN mcp_service_versions.entry_file IS '相对源码根目录的启动脚本';
COMMENT ON COLUMN mcp_service_versions.image_ref IS '构建成功的容器镜像标识';
COMMENT ON COLUMN mcp_service_versions.build_status IS '构建状态';
COMMENT ON COLUMN mcp_service_versions.build_log IS '截断并脱敏后的构建输出';
COMMENT ON COLUMN mcp_service_versions.error_message IS '构建失败原因';
COMMENT ON COLUMN mcp_service_versions.created_at IS '构建开始时间';
COMMENT ON COLUMN mcp_service_versions.finished_at IS '构建结束时间';

-- +goose Down
DROP TABLE IF EXISTS mcp_service_versions;
ALTER TABLE mcp_services DROP COLUMN IF EXISTS current_revision;
ALTER TABLE mcp_services DROP COLUMN IF EXISTS deployment_error;
ALTER TABLE mcp_services DROP COLUMN IF EXISTS deployment_status;
ALTER TABLE mcp_services DROP COLUMN IF EXISTS container_name;
ALTER TABLE mcp_services DROP COLUMN IF EXISTS image_ref;
ALTER TABLE mcp_services DROP COLUMN IF EXISTS entry_file;
ALTER TABLE mcp_services DROP COLUMN IF EXISTS runtime_type;
ALTER TABLE mcp_services DROP COLUMN IF EXISTS source_type;
ALTER TABLE mcp_services DROP CONSTRAINT mcp_services_transport_check;
ALTER TABLE mcp_services ADD CONSTRAINT mcp_services_transport_check CHECK (transport IN ('streamable_http', 'sse'));
