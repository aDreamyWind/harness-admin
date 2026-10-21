-- +goose Up
ALTER TABLE mcp_services ADD CONSTRAINT mcp_services_id_organization_unique UNIQUE (id, organization_id);

CREATE TABLE mcp_connections (
    id UUID PRIMARY KEY,
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    service_id UUID NOT NULL,
    code VARCHAR(80) NOT NULL UNIQUE,
    name VARCHAR(120) NOT NULL,
    purpose VARCHAR(16) NOT NULL CHECK (purpose IN ('harness', 'developer')),
    auth_mode VARCHAR(16) NOT NULL CHECK (auth_mode IN ('oidc', 'bearer', 'none')),
    application_name VARCHAR(120),
    status VARCHAR(16) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    expires_at TIMESTAMPTZ,
    key_prefix VARCHAR(32),
    secret_hash BYTEA,
    encrypted_secret BYTEA,
    upstream_authorization_enc BYTEA,
    requests_per_minute INTEGER NOT NULL DEFAULT 60 CHECK (requests_per_minute BETWEEN 1 AND 100000),
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT mcp_connections_service_fk FOREIGN KEY (service_id, organization_id)
        REFERENCES mcp_services(id, organization_id) ON DELETE CASCADE,
    CONSTRAINT mcp_connections_auth_check CHECK (
        (purpose = 'harness' AND auth_mode = 'oidc' AND secret_hash IS NULL AND encrypted_secret IS NULL)
        OR (purpose = 'developer' AND auth_mode = 'bearer' AND secret_hash IS NOT NULL AND encrypted_secret IS NOT NULL)
        OR (purpose = 'developer' AND auth_mode = 'none' AND secret_hash IS NULL AND encrypted_secret IS NULL)
    )
);
CREATE INDEX mcp_connections_organization_idx ON mcp_connections (organization_id, created_at DESC);
CREATE INDEX mcp_connections_service_idx ON mcp_connections (service_id);
COMMENT ON TABLE mcp_connections IS '对外发布的独立 MCP 网关连接';
COMMENT ON COLUMN mcp_connections.id IS '连接唯一标识';
COMMENT ON COLUMN mcp_connections.organization_id IS '所属企业标识';
COMMENT ON COLUMN mcp_connections.service_id IS '对应的 MCP 服务标识';
COMMENT ON COLUMN mcp_connections.code IS '全局唯一的对外连接编码';
COMMENT ON COLUMN mcp_connections.name IS '连接显示名称';
COMMENT ON COLUMN mcp_connections.purpose IS '连接用途：harness 员工或 developer 业务应用';
COMMENT ON COLUMN mcp_connections.auth_mode IS '入口鉴权方式：oidc、bearer 或 none';
COMMENT ON COLUMN mcp_connections.application_name IS '开发者连接归属应用或项目名称，可为空';
COMMENT ON COLUMN mcp_connections.status IS '连接状态：active 启用或 disabled 停用';
COMMENT ON COLUMN mcp_connections.expires_at IS '连接到期时间，为空表示不自动过期';
COMMENT ON COLUMN mcp_connections.key_prefix IS '开发者 Bearer 密钥的非敏感前缀';
COMMENT ON COLUMN mcp_connections.secret_hash IS '开发者 Bearer 密钥的 SHA-256 校验摘要';
COMMENT ON COLUMN mcp_connections.encrypted_secret IS '用于授权管理员再次查看的加密入口密钥';
COMMENT ON COLUMN mcp_connections.upstream_authorization_enc IS '该连接专用的加密上游 Authorization 值';
COMMENT ON COLUMN mcp_connections.requests_per_minute IS '此连接允许的每分钟请求数';
COMMENT ON COLUMN mcp_connections.created_by IS '创建连接的管理员用户标识';
COMMENT ON COLUMN mcp_connections.created_at IS '创建时间';
COMMENT ON COLUMN mcp_connections.updated_at IS '最后修改时间';

CREATE TABLE mcp_connection_grants (
    id UUID PRIMARY KEY,
    connection_id UUID NOT NULL REFERENCES mcp_connections(id) ON DELETE CASCADE,
    department_id UUID REFERENCES departments(id) ON DELETE CASCADE,
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT mcp_connection_grants_subject_check CHECK ((department_id IS NULL) <> (user_id IS NULL)),
    UNIQUE (connection_id, department_id),
    UNIQUE (connection_id, user_id)
);
CREATE INDEX mcp_connection_grants_user_idx ON mcp_connection_grants (user_id, connection_id);
CREATE INDEX mcp_connection_grants_department_idx ON mcp_connection_grants (department_id, connection_id);
COMMENT ON TABLE mcp_connection_grants IS 'Harness MCP 连接的明确部门或用户授权';
COMMENT ON COLUMN mcp_connection_grants.id IS '授权记录唯一标识';
COMMENT ON COLUMN mcp_connection_grants.connection_id IS '所属 MCP 连接标识';
COMMENT ON COLUMN mcp_connection_grants.department_id IS '被授权部门标识，用户授权时为空';
COMMENT ON COLUMN mcp_connection_grants.user_id IS '被授权用户标识，部门授权时为空';
COMMENT ON COLUMN mcp_connection_grants.created_at IS '授权创建时间';

CREATE TABLE mcp_tool_calls (
    id UUID PRIMARY KEY,
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    connection_id UUID NOT NULL REFERENCES mcp_connections(id),
    service_id UUID NOT NULL REFERENCES mcp_services(id),
    tool_name TEXT NOT NULL,
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    department_id UUID REFERENCES departments(id) ON DELETE SET NULL,
    result VARCHAR(16) NOT NULL CHECK (result IN ('success', 'failed')),
    latency_ms BIGINT NOT NULL CHECK (latency_ms >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX mcp_tool_calls_stats_idx ON mcp_tool_calls (organization_id, connection_id, created_at DESC);
COMMENT ON TABLE mcp_tool_calls IS 'MCP 网关工具调用记录，不保存工具入参和返回正文';
COMMENT ON COLUMN mcp_tool_calls.id IS '调用记录唯一标识';
COMMENT ON COLUMN mcp_tool_calls.organization_id IS '所属企业标识';
COMMENT ON COLUMN mcp_tool_calls.connection_id IS '使用的对外连接标识';
COMMENT ON COLUMN mcp_tool_calls.service_id IS '调用的上游 MCP 服务标识';
COMMENT ON COLUMN mcp_tool_calls.tool_name IS '被调用的工具名称';
COMMENT ON COLUMN mcp_tool_calls.user_id IS 'Harness 员工标识；开发者连接为空';
COMMENT ON COLUMN mcp_tool_calls.department_id IS '调用时员工所在部门标识；开发者连接为空';
COMMENT ON COLUMN mcp_tool_calls.result IS '调用结果：success 成功或 failed 失败';
COMMENT ON COLUMN mcp_tool_calls.latency_ms IS '调用耗时，单位毫秒';
COMMENT ON COLUMN mcp_tool_calls.created_at IS '调用时间';

INSERT INTO permissions (id, code, name, resource, action, description) VALUES
('a1700000-0000-4000-8000-000000000003', 'mcp.connection.read', '查看 MCP 连接', 'mcp_connection', 'read', '查看当前企业的 MCP 连接和调用记录'),
('a1700000-0000-4000-8000-000000000004', 'mcp.connection.write', '管理 MCP 连接', 'mcp_connection', 'write', '创建、停用 MCP 连接并设置员工授权')
ON CONFLICT (code) DO NOTHING;
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'enterprise_admin' AND p.code IN ('mcp.connection.read', 'mcp.connection.write')
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN (SELECT id FROM permissions WHERE code IN ('mcp.connection.read', 'mcp.connection.write'));
DELETE FROM permissions WHERE code IN ('mcp.connection.read', 'mcp.connection.write');
DROP TABLE IF EXISTS mcp_tool_calls;
DROP TABLE IF EXISTS mcp_connection_grants;
DROP TABLE IF EXISTS mcp_connections;
ALTER TABLE mcp_services DROP CONSTRAINT IF EXISTS mcp_services_id_organization_unique;
