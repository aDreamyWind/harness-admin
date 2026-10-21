-- +goose Up
CREATE TABLE mcp_services (
    id UUID PRIMARY KEY,
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    code VARCHAR(80) NOT NULL,
    name VARCHAR(120) NOT NULL,
    transport VARCHAR(24) NOT NULL CHECK (transport IN ('streamable_http', 'sse')),
    upstream_url TEXT NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (organization_id, code)
);
CREATE INDEX mcp_services_organization_idx ON mcp_services (organization_id, created_at DESC);
COMMENT ON TABLE mcp_services IS '企业登记的远程 MCP 服务';
COMMENT ON COLUMN mcp_services.id IS 'MCP 服务唯一标识';
COMMENT ON COLUMN mcp_services.organization_id IS '所属企业标识';
COMMENT ON COLUMN mcp_services.code IS '企业内唯一的 MCP 服务编码';
COMMENT ON COLUMN mcp_services.name IS 'MCP 服务显示名称';
COMMENT ON COLUMN mcp_services.transport IS '上游传输类型：streamable_http 或 sse';
COMMENT ON COLUMN mcp_services.upstream_url IS '上游 MCP 服务完整地址，不得包含凭据';
COMMENT ON COLUMN mcp_services.status IS '服务状态：active 启用、disabled 停用';
COMMENT ON COLUMN mcp_services.created_by IS '登记服务的用户标识';
COMMENT ON COLUMN mcp_services.created_at IS '创建时间';
COMMENT ON COLUMN mcp_services.updated_at IS '最后修改时间';

CREATE TABLE mcp_service_tools (
    service_id UUID NOT NULL REFERENCES mcp_services(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    input_schema JSONB NOT NULL,
    discovered_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (service_id, name)
);
COMMENT ON TABLE mcp_service_tools IS 'MCP 服务最近一次发现的工具快照';
COMMENT ON COLUMN mcp_service_tools.service_id IS '所属 MCP 服务标识';
COMMENT ON COLUMN mcp_service_tools.name IS '上游工具名称';
COMMENT ON COLUMN mcp_service_tools.description IS '上游工具说明';
COMMENT ON COLUMN mcp_service_tools.input_schema IS '上游工具输入 JSON Schema';
COMMENT ON COLUMN mcp_service_tools.discovered_at IS '最近发现工具的时间';

INSERT INTO permissions (id, code, name, resource, action, description) VALUES
('a1700000-0000-4000-8000-000000000001', 'mcp.service.read', '查看 MCP 服务', 'mcp_service', 'read', '查看当前企业的 MCP 服务和工具'),
('a1700000-0000-4000-8000-000000000002', 'mcp.service.write', '管理 MCP 服务', 'mcp_service', 'write', '登记、停用、发现和测试当前企业的 MCP 服务')
ON CONFLICT (code) DO NOTHING;
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'enterprise_admin' AND p.code IN ('mcp.service.read', 'mcp.service.write')
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN (SELECT id FROM permissions WHERE code IN ('mcp.service.read', 'mcp.service.write'));
DELETE FROM permissions WHERE code IN ('mcp.service.read', 'mcp.service.write');
DROP TABLE IF EXISTS mcp_service_tools;
DROP TABLE IF EXISTS mcp_services;
