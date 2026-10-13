-- +goose Up
CREATE TABLE model_provider_connections (
    id UUID PRIMARY KEY,
    organization_id UUID NOT NULL REFERENCES organizations(id),
    code VARCHAR(80) NOT NULL,
    name VARCHAR(120) NOT NULL,
    provider_kind VARCHAR(40) NOT NULL,
    base_url TEXT NOT NULL,
    status VARCHAR(24) NOT NULL DEFAULT 'active',
    request_timeout_ms INTEGER NOT NULL DEFAULT 15000,
    last_probe_status VARCHAR(24) NOT NULL DEFAULT 'unknown',
    last_probe_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT model_provider_connections_kind_check CHECK (provider_kind IN ('openai_compatible')),
    CONSTRAINT model_provider_connections_status_check CHECK (status IN ('active', 'disabled')),
    CONSTRAINT model_provider_connections_timeout_check CHECK (request_timeout_ms BETWEEN 1000 AND 120000),
    CONSTRAINT model_provider_connections_probe_status_check CHECK (last_probe_status IN ('unknown', 'success', 'failed'))
);

CREATE UNIQUE INDEX model_provider_connections_org_code_ci_key
    ON model_provider_connections (organization_id, LOWER(code));
CREATE INDEX model_provider_connections_org_status_idx
    ON model_provider_connections (organization_id, status);

COMMENT ON TABLE model_provider_connections IS '企业模型供应商连接';
COMMENT ON COLUMN model_provider_connections.id IS '模型供应商连接唯一标识';
COMMENT ON COLUMN model_provider_connections.organization_id IS '所属企业标识';
COMMENT ON COLUMN model_provider_connections.code IS '企业内唯一的供应商连接代码，不区分大小写';
COMMENT ON COLUMN model_provider_connections.name IS '供应商连接显示名称';
COMMENT ON COLUMN model_provider_connections.provider_kind IS '供应商协议类型，当前支持 openai_compatible';
COMMENT ON COLUMN model_provider_connections.base_url IS '供应商 API 基础地址';
COMMENT ON COLUMN model_provider_connections.status IS '连接状态：active 启用，disabled 停用';
COMMENT ON COLUMN model_provider_connections.request_timeout_ms IS '供应商请求超时时间，单位毫秒';
COMMENT ON COLUMN model_provider_connections.last_probe_status IS '最近连接检测结果：unknown 未检测，success 成功，failed 失败';
COMMENT ON COLUMN model_provider_connections.last_probe_at IS '最近一次连接检测时间';
COMMENT ON COLUMN model_provider_connections.created_at IS '创建时间';
COMMENT ON COLUMN model_provider_connections.updated_at IS '最后更新时间';

CREATE TABLE model_provider_credentials (
    provider_connection_id UUID PRIMARY KEY REFERENCES model_provider_connections(id) ON DELETE CASCADE,
    encrypted_secret BYTEA NOT NULL,
    key_version INTEGER NOT NULL DEFAULT 1,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE model_provider_credentials IS '模型供应商加密访问凭据';
COMMENT ON COLUMN model_provider_credentials.provider_connection_id IS '所属模型供应商连接标识';
COMMENT ON COLUMN model_provider_credentials.encrypted_secret IS '使用服务端主密钥加密后的供应商访问凭据';
COMMENT ON COLUMN model_provider_credentials.key_version IS '凭据加密主密钥版本';
COMMENT ON COLUMN model_provider_credentials.updated_at IS '凭据最后更新时间';

CREATE TABLE model_catalog_entries (
    id UUID PRIMARY KEY,
    organization_id UUID NOT NULL REFERENCES organizations(id),
    provider_connection_id UUID NOT NULL REFERENCES model_provider_connections(id),
    code VARCHAR(80) NOT NULL,
    upstream_model_id VARCHAR(200) NOT NULL,
    name VARCHAR(120) NOT NULL,
    model_type VARCHAR(24) NOT NULL DEFAULT 'chat',
    capabilities TEXT[] NOT NULL DEFAULT '{}'::TEXT[],
    context_window_tokens INTEGER NOT NULL DEFAULT 0,
    max_output_tokens INTEGER NOT NULL DEFAULT 0,
    status VARCHAR(24) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT model_catalog_entries_type_check CHECK (model_type IN ('chat')),
    CONSTRAINT model_catalog_entries_status_check CHECK (status IN ('active', 'disabled')),
    CONSTRAINT model_catalog_entries_context_window_check CHECK (context_window_tokens >= 0),
    CONSTRAINT model_catalog_entries_max_output_check CHECK (max_output_tokens >= 0),
    CONSTRAINT model_catalog_entries_token_range_check CHECK (context_window_tokens = 0 OR max_output_tokens <= context_window_tokens)
);

CREATE UNIQUE INDEX model_catalog_entries_org_code_ci_key
    ON model_catalog_entries (organization_id, LOWER(code));
CREATE UNIQUE INDEX model_catalog_entries_provider_upstream_key
    ON model_catalog_entries (provider_connection_id, upstream_model_id);
CREATE INDEX model_catalog_entries_org_status_idx
    ON model_catalog_entries (organization_id, status);
CREATE INDEX model_catalog_entries_provider_idx
    ON model_catalog_entries (provider_connection_id);

COMMENT ON TABLE model_catalog_entries IS '企业模型目录';
COMMENT ON COLUMN model_catalog_entries.id IS '模型目录项唯一标识';
COMMENT ON COLUMN model_catalog_entries.organization_id IS '所属企业标识';
COMMENT ON COLUMN model_catalog_entries.provider_connection_id IS '所属模型供应商连接标识';
COMMENT ON COLUMN model_catalog_entries.code IS '企业内稳定且唯一的模型代码，不区分大小写';
COMMENT ON COLUMN model_catalog_entries.upstream_model_id IS '供应商侧模型标识';
COMMENT ON COLUMN model_catalog_entries.name IS '模型显示名称';
COMMENT ON COLUMN model_catalog_entries.model_type IS '模型类型，当前支持 chat';
COMMENT ON COLUMN model_catalog_entries.capabilities IS '模型能力代码集合，如 text、vision、tools、streaming、json';
COMMENT ON COLUMN model_catalog_entries.context_window_tokens IS '模型上下文窗口 Token 数，0 表示未知';
COMMENT ON COLUMN model_catalog_entries.max_output_tokens IS '模型最大输出 Token 数，0 表示未知';
COMMENT ON COLUMN model_catalog_entries.status IS '模型状态：active 启用，disabled 停用';
COMMENT ON COLUMN model_catalog_entries.created_at IS '创建时间';
COMMENT ON COLUMN model_catalog_entries.updated_at IS '最后更新时间';

INSERT INTO permissions (id, code, name, resource, action, description) VALUES
    ('a1300000-0000-4000-8000-000000000001', 'model.provider.read', '查看模型供应商', 'model_provider', 'read', '查看当前企业的模型供应商连接'),
    ('a1300000-0000-4000-8000-000000000002', 'model.provider.write', '管理模型供应商', 'model_provider', 'write', '创建、修改和检测当前企业的模型供应商连接'),
    ('a1300000-0000-4000-8000-000000000003', 'model.catalog.read', '查看模型目录', 'model_catalog', 'read', '查看当前企业的模型目录'),
    ('a1300000-0000-4000-8000-000000000004', 'model.catalog.write', '管理模型目录', 'model_catalog', 'write', '创建和修改当前企业的模型目录')
ON CONFLICT (code) DO UPDATE SET
    name = EXCLUDED.name,
    resource = EXCLUDED.resource,
    action = EXCLUDED.action,
    description = EXCLUDED.description;

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
JOIN permissions p ON p.code IN ('model.provider.read', 'model.provider.write', 'model.catalog.read', 'model.catalog.write')
WHERE r.built_in = TRUE AND r.code = 'enterprise_admin'
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_permissions
WHERE permission_id IN (
    SELECT id FROM permissions
    WHERE code IN ('model.provider.read', 'model.provider.write', 'model.catalog.read', 'model.catalog.write')
);
DELETE FROM permissions
WHERE code IN ('model.provider.read', 'model.provider.write', 'model.catalog.read', 'model.catalog.write');
DROP TABLE IF EXISTS model_catalog_entries;
DROP TABLE IF EXISTS model_provider_credentials;
DROP TABLE IF EXISTS model_provider_connections;
