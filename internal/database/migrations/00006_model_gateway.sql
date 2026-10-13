-- +goose Up
CREATE TABLE model_gateway_api_keys (
    id UUID PRIMARY KEY,
    organization_id UUID NOT NULL REFERENCES organizations(id),
    name VARCHAR(120) NOT NULL,
    key_prefix VARCHAR(32) NOT NULL,
    secret_hash BYTEA NOT NULL,
    status VARCHAR(24) NOT NULL DEFAULT 'active',
    expires_at TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    created_by UUID NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at TIMESTAMPTZ,
    CONSTRAINT model_gateway_api_keys_status_check CHECK (status IN ('active', 'revoked')),
    CONSTRAINT model_gateway_api_keys_org_id_key UNIQUE (organization_id, id),
    CONSTRAINT model_gateway_api_keys_secret_hash_key UNIQUE (secret_hash)
);

CREATE INDEX model_gateway_api_keys_org_created_idx
    ON model_gateway_api_keys (organization_id, created_at DESC);

COMMENT ON TABLE model_gateway_api_keys IS '企业模型网关调用密钥';
COMMENT ON COLUMN model_gateway_api_keys.id IS '网关调用密钥唯一标识';
COMMENT ON COLUMN model_gateway_api_keys.organization_id IS '所属企业标识';
COMMENT ON COLUMN model_gateway_api_keys.name IS '调用密钥名称';
COMMENT ON COLUMN model_gateway_api_keys.key_prefix IS '用于识别密钥的非敏感前缀';
COMMENT ON COLUMN model_gateway_api_keys.secret_hash IS '完整调用密钥的 SHA-256 哈希';
COMMENT ON COLUMN model_gateway_api_keys.status IS '密钥状态：active 可用、revoked 已吊销';
COMMENT ON COLUMN model_gateway_api_keys.expires_at IS '密钥过期时间，为空表示永不过期';
COMMENT ON COLUMN model_gateway_api_keys.last_used_at IS '最近一次成功鉴权时间';
COMMENT ON COLUMN model_gateway_api_keys.created_by IS '创建密钥的企业用户标识';
COMMENT ON COLUMN model_gateway_api_keys.created_at IS '创建时间';
COMMENT ON COLUMN model_gateway_api_keys.updated_at IS '最后更新时间';
COMMENT ON COLUMN model_gateway_api_keys.revoked_at IS '吊销时间';

ALTER TABLE model_catalog_entries
    ADD CONSTRAINT model_catalog_entries_org_id_key UNIQUE (organization_id, id);

CREATE TABLE model_gateway_calls (
    id UUID PRIMARY KEY,
    organization_id UUID NOT NULL REFERENCES organizations(id),
    api_key_id UUID NOT NULL,
    model_catalog_entry_id UUID NOT NULL,
    provider_connection_id UUID NOT NULL,
    request_id VARCHAR(120) NOT NULL,
    model_code VARCHAR(80) NOT NULL,
    upstream_model_id VARCHAR(200) NOT NULL,
    streaming BOOLEAN NOT NULL DEFAULT FALSE,
    result VARCHAR(24) NOT NULL,
    http_status INTEGER NOT NULL,
    latency_ms BIGINT NOT NULL,
    prompt_tokens INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    total_tokens INTEGER NOT NULL DEFAULT 0,
    error_code VARCHAR(80) NOT NULL DEFAULT '',
    source_ip VARCHAR(80) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT model_gateway_calls_result_check CHECK (result IN ('success', 'failed')),
    CONSTRAINT model_gateway_calls_http_status_check CHECK (http_status BETWEEN 100 AND 599),
    CONSTRAINT model_gateway_calls_latency_check CHECK (latency_ms >= 0),
    CONSTRAINT model_gateway_calls_token_check CHECK (prompt_tokens >= 0 AND completion_tokens >= 0 AND total_tokens >= 0),
    CONSTRAINT model_gateway_calls_api_key_org_fk FOREIGN KEY (organization_id, api_key_id)
        REFERENCES model_gateway_api_keys (organization_id, id),
    CONSTRAINT model_gateway_calls_model_org_fk FOREIGN KEY (organization_id, model_catalog_entry_id)
        REFERENCES model_catalog_entries (organization_id, id),
    CONSTRAINT model_gateway_calls_provider_org_fk FOREIGN KEY (organization_id, provider_connection_id)
        REFERENCES model_provider_connections (organization_id, id)
);

CREATE INDEX model_gateway_calls_org_created_idx
    ON model_gateway_calls (organization_id, created_at DESC);
CREATE INDEX model_gateway_calls_api_key_idx
    ON model_gateway_calls (api_key_id, created_at DESC);
CREATE INDEX model_gateway_calls_model_idx
    ON model_gateway_calls (model_catalog_entry_id, created_at DESC);

COMMENT ON TABLE model_gateway_calls IS '模型网关调用记录';
COMMENT ON COLUMN model_gateway_calls.id IS '网关调用记录唯一标识';
COMMENT ON COLUMN model_gateway_calls.organization_id IS '所属企业标识';
COMMENT ON COLUMN model_gateway_calls.api_key_id IS '发起调用的网关密钥标识';
COMMENT ON COLUMN model_gateway_calls.model_catalog_entry_id IS '命中的模型目录项标识';
COMMENT ON COLUMN model_gateway_calls.provider_connection_id IS '实际使用的供应商连接标识';
COMMENT ON COLUMN model_gateway_calls.request_id IS '本次调用请求标识';
COMMENT ON COLUMN model_gateway_calls.model_code IS '调用方请求的模型编码快照';
COMMENT ON COLUMN model_gateway_calls.upstream_model_id IS '供应商侧模型标识快照';
COMMENT ON COLUMN model_gateway_calls.streaming IS '是否为流式调用';
COMMENT ON COLUMN model_gateway_calls.result IS '调用结果：success 成功、failed 失败';
COMMENT ON COLUMN model_gateway_calls.http_status IS '返回给调用方的 HTTP 状态码';
COMMENT ON COLUMN model_gateway_calls.latency_ms IS '网关处理耗时，单位毫秒';
COMMENT ON COLUMN model_gateway_calls.prompt_tokens IS '上游返回的输入 Token 数，未知时为 0';
COMMENT ON COLUMN model_gateway_calls.completion_tokens IS '上游返回的输出 Token 数，未知时为 0';
COMMENT ON COLUMN model_gateway_calls.total_tokens IS '上游返回的总 Token 数，未知时为 0';
COMMENT ON COLUMN model_gateway_calls.error_code IS '失败错误代码，成功时为空字符串';
COMMENT ON COLUMN model_gateway_calls.source_ip IS '调用方来源 IP';
COMMENT ON COLUMN model_gateway_calls.created_at IS '调用发生时间';

INSERT INTO permissions (id, code, name, resource, action, description) VALUES
    ('a1400000-0000-4000-8000-000000000001', 'model.gateway.read', '查看模型网关', 'model_gateway', 'read', '查看当前企业的网关调用密钥和调用记录'),
    ('a1400000-0000-4000-8000-000000000002', 'model.gateway.write', '管理模型网关', 'model_gateway', 'write', '创建和吊销当前企业的网关调用密钥')
ON CONFLICT (code) DO UPDATE SET
    name = EXCLUDED.name,
    resource = EXCLUDED.resource,
    action = EXCLUDED.action,
    description = EXCLUDED.description;

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
JOIN permissions p ON p.code IN ('model.gateway.read', 'model.gateway.write')
WHERE r.built_in = TRUE AND r.code = 'enterprise_admin'
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_permissions
WHERE permission_id IN (
    SELECT id FROM permissions
    WHERE code IN ('model.gateway.read', 'model.gateway.write')
);
DELETE FROM permissions
WHERE code IN ('model.gateway.read', 'model.gateway.write');
DROP TABLE IF EXISTS model_gateway_calls;
ALTER TABLE model_catalog_entries
    DROP CONSTRAINT IF EXISTS model_catalog_entries_org_id_key;
DROP TABLE IF EXISTS model_gateway_api_keys;
