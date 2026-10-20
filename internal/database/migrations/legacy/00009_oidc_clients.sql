-- +goose Up
CREATE TABLE oidc_clients (
    id UUID PRIMARY KEY,
    organization_id UUID NOT NULL REFERENCES organizations(id),
    name VARCHAR(120) NOT NULL,
    client_id VARCHAR(120) NOT NULL,
    client_secret_hash BYTEA NOT NULL,
    client_secret_prefix VARCHAR(40) NOT NULL,
    redirect_uris TEXT[] NOT NULL,
    allowed_scopes TEXT[] NOT NULL DEFAULT ARRAY['openid', 'profile', 'model.invoke']::TEXT[],
    status VARCHAR(24) NOT NULL DEFAULT 'active',
    created_by UUID NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT oidc_clients_org_id_key UNIQUE (organization_id, id),
    CONSTRAINT oidc_clients_client_id_key UNIQUE (client_id),
    CONSTRAINT oidc_clients_status_check CHECK (status IN ('active', 'disabled')),
    CONSTRAINT oidc_clients_redirect_uris_check CHECK (CARDINALITY(redirect_uris) BETWEEN 1 AND 20),
    CONSTRAINT oidc_clients_allowed_scopes_check CHECK (CARDINALITY(allowed_scopes) BETWEEN 1 AND 10)
);

CREATE INDEX oidc_clients_org_created_idx
    ON oidc_clients (organization_id, created_at DESC);

COMMENT ON TABLE oidc_clients IS '企业统一登录 OIDC 客户端';
COMMENT ON COLUMN oidc_clients.id IS 'OIDC 客户端唯一标识';
COMMENT ON COLUMN oidc_clients.organization_id IS '所属企业标识';
COMMENT ON COLUMN oidc_clients.name IS '接入应用名称';
COMMENT ON COLUMN oidc_clients.client_id IS '对外公开的 OAuth 客户端标识';
COMMENT ON COLUMN oidc_clients.client_secret_hash IS '客户端密钥的 SHA-256 哈希，不保存明文';
COMMENT ON COLUMN oidc_clients.client_secret_prefix IS '用于识别客户端密钥的非敏感前缀';
COMMENT ON COLUMN oidc_clients.redirect_uris IS '允许的精确登录回调地址集合';
COMMENT ON COLUMN oidc_clients.allowed_scopes IS '允许申请的 OIDC 和业务授权范围';
COMMENT ON COLUMN oidc_clients.status IS '客户端状态：active 启用、disabled 停用';
COMMENT ON COLUMN oidc_clients.created_by IS '创建客户端的企业用户标识';
COMMENT ON COLUMN oidc_clients.created_at IS '创建时间';
COMMENT ON COLUMN oidc_clients.updated_at IS '最后更新时间';

INSERT INTO permissions (id, code, name, resource, action, description) VALUES
    ('a1500000-0000-4000-8000-000000000001', 'oidc.client.read', '查看应用接入', 'oidc_client', 'read', '查看当前企业的统一登录应用'),
    ('a1500000-0000-4000-8000-000000000002', 'oidc.client.write', '管理应用接入', 'oidc_client', 'write', '创建、修改和停用当前企业的统一登录应用')
ON CONFLICT (code) DO UPDATE SET
    name = EXCLUDED.name,
    resource = EXCLUDED.resource,
    action = EXCLUDED.action,
    description = EXCLUDED.description;

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
JOIN permissions p ON p.code IN ('oidc.client.read', 'oidc.client.write')
WHERE r.built_in = TRUE AND r.code = 'enterprise_admin'
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_permissions
WHERE permission_id IN (
    SELECT id FROM permissions
    WHERE code IN ('oidc.client.read', 'oidc.client.write')
);
DELETE FROM permissions
WHERE code IN ('oidc.client.read', 'oidc.client.write');
DROP TABLE IF EXISTS oidc_clients;
