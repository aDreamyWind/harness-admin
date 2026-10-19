-- +goose Up
CREATE TABLE model_subject_policies (
    organization_id UUID NOT NULL REFERENCES organizations(id),
    subject_type VARCHAR(16) NOT NULL,
    subject_id UUID NOT NULL,
    scope_mode VARCHAR(16) NOT NULL DEFAULT 'inherit',
    allowed_model_codes TEXT[],
    requests_per_minute INTEGER NOT NULL DEFAULT 0,
    monthly_token_limit BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (organization_id, subject_type, subject_id),
    CONSTRAINT model_subject_policies_type_check CHECK (subject_type IN ('department', 'user')),
    CONSTRAINT model_subject_policies_scope_check CHECK (scope_mode IN ('inherit', 'all', 'selected', 'none')),
    CONSTRAINT model_subject_policies_rpm_check CHECK (requests_per_minute BETWEEN 0 AND 100000),
    CONSTRAINT model_subject_policies_monthly_check CHECK (monthly_token_limit >= 0)
);

COMMENT ON TABLE model_subject_policies IS '部门和用户的模型访问与调用额度策略';
COMMENT ON COLUMN model_subject_policies.organization_id IS '所属企业标识';
COMMENT ON COLUMN model_subject_policies.subject_type IS '策略对象类型：department 部门、user 用户';
COMMENT ON COLUMN model_subject_policies.subject_id IS '所属部门或用户标识，由策略写入服务校验企业归属';
COMMENT ON COLUMN model_subject_policies.scope_mode IS '模型范围模式：inherit 继承、all 全部、selected 指定、none 禁用全部';
COMMENT ON COLUMN model_subject_policies.allowed_model_codes IS 'selected 模式允许使用的模型编码集合，其他模式为空数组';
COMMENT ON COLUMN model_subject_policies.requests_per_minute IS '每分钟最大请求数，0 表示不限制';
COMMENT ON COLUMN model_subject_policies.monthly_token_limit IS '按 UTC 自然月计算的 Token 上限，0 表示不限制';
COMMENT ON COLUMN model_subject_policies.created_at IS '策略创建时间';
COMMENT ON COLUMN model_subject_policies.updated_at IS '策略最后更新时间';

CREATE TABLE model_subject_monthly_usage (
    organization_id UUID NOT NULL,
    subject_type VARCHAR(16) NOT NULL,
    subject_id UUID NOT NULL,
    usage_month DATE NOT NULL,
    request_count BIGINT NOT NULL DEFAULT 0,
    total_tokens BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (organization_id, subject_type, subject_id, usage_month),
    CONSTRAINT model_subject_monthly_usage_policy_fk
        FOREIGN KEY (organization_id, subject_type, subject_id)
        REFERENCES model_subject_policies (organization_id, subject_type, subject_id) ON DELETE CASCADE,
    CONSTRAINT model_subject_monthly_usage_counts_check CHECK (request_count >= 0 AND total_tokens >= 0),
    CONSTRAINT model_subject_monthly_usage_month_check CHECK (EXTRACT(DAY FROM usage_month) = 1)
);

COMMENT ON TABLE model_subject_monthly_usage IS '部门和用户策略的月度模型调用用量';
COMMENT ON COLUMN model_subject_monthly_usage.organization_id IS '所属企业标识';
COMMENT ON COLUMN model_subject_monthly_usage.subject_type IS '用量对象类型：department 部门、user 用户';
COMMENT ON COLUMN model_subject_monthly_usage.subject_id IS '所属部门或用户标识';
COMMENT ON COLUMN model_subject_monthly_usage.usage_month IS 'UTC 统计月份的第一天';
COMMENT ON COLUMN model_subject_monthly_usage.request_count IS '当月已记录请求数';
COMMENT ON COLUMN model_subject_monthly_usage.total_tokens IS '当月已记录 Token 数';
COMMENT ON COLUMN model_subject_monthly_usage.updated_at IS '用量最后更新时间';

INSERT INTO permissions (id, code, name, resource, action, description) VALUES
    ('a1600000-0000-4000-8000-000000000001', 'model.policy.read', '查看模型策略', 'model_policy', 'read', '查看部门和用户的模型访问与额度策略'),
    ('a1600000-0000-4000-8000-000000000002', 'model.policy.write', '管理模型策略', 'model_policy', 'write', '设置或删除部门和用户的模型访问与额度策略')
ON CONFLICT (code) DO UPDATE SET name = EXCLUDED.name, resource = EXCLUDED.resource,
    action = EXCLUDED.action, description = EXCLUDED.description;

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r JOIN permissions p ON p.code IN ('model.policy.read', 'model.policy.write')
WHERE r.built_in = TRUE AND r.code = 'enterprise_admin'
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN (SELECT id FROM permissions WHERE code IN ('model.policy.read', 'model.policy.write'));
DELETE FROM permissions WHERE code IN ('model.policy.read', 'model.policy.write');
DROP TABLE IF EXISTS model_subject_monthly_usage;
DROP TABLE IF EXISTS model_subject_policies;
