-- +goose Up
ALTER TABLE model_subject_policies
    ADD COLUMN monthly_token_mode VARCHAR(16) NOT NULL DEFAULT 'shared';

ALTER TABLE model_subject_policies
    ADD CONSTRAINT model_subject_policies_token_mode_check
    CHECK (monthly_token_mode IN ('shared', 'per_user') AND (subject_type = 'department' OR monthly_token_mode = 'shared'));

COMMENT ON COLUMN model_subject_policies.monthly_token_mode IS '部门月度 Token 额度计算方式：shared 部门共享、per_user 每名用户独立；用户策略固定为 shared';
COMMENT ON COLUMN model_subject_policies.requests_per_minute IS '每名用户在此策略下的每分钟最大请求数，0 表示不限制';

CREATE TABLE model_department_user_monthly_usage (
    organization_id UUID NOT NULL,
    subject_type VARCHAR(16) NOT NULL DEFAULT 'department' CHECK (subject_type = 'department'),
    subject_id UUID NOT NULL,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    usage_month DATE NOT NULL,
    request_count BIGINT NOT NULL DEFAULT 0,
    total_tokens BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (organization_id, subject_id, user_id, usage_month),
    CONSTRAINT model_department_user_monthly_usage_policy_fk
        FOREIGN KEY (organization_id, subject_type, subject_id)
        REFERENCES model_subject_policies (organization_id, subject_type, subject_id) ON DELETE CASCADE,
    CONSTRAINT model_department_user_monthly_usage_counts_check CHECK (request_count >= 0 AND total_tokens >= 0),
    CONSTRAINT model_department_user_monthly_usage_month_check CHECK (EXTRACT(DAY FROM usage_month) = 1)
);

CREATE INDEX model_department_user_monthly_usage_lookup_idx
    ON model_department_user_monthly_usage (organization_id, user_id, usage_month);

COMMENT ON TABLE model_department_user_monthly_usage IS '部门模型策略按用户独立统计的月度用量';
COMMENT ON COLUMN model_department_user_monthly_usage.organization_id IS '所属企业标识';
COMMENT ON COLUMN model_department_user_monthly_usage.subject_type IS '固定为 department，用于关联部门模型策略';
COMMENT ON COLUMN model_department_user_monthly_usage.subject_id IS '部门模型策略所属部门标识';
COMMENT ON COLUMN model_department_user_monthly_usage.user_id IS '实际调用模型的用户标识';
COMMENT ON COLUMN model_department_user_monthly_usage.usage_month IS 'UTC 统计月份的第一天';
COMMENT ON COLUMN model_department_user_monthly_usage.request_count IS '该用户在此部门策略下当月已记录请求数';
COMMENT ON COLUMN model_department_user_monthly_usage.total_tokens IS '该用户在此部门策略下当月已记录 Token 数';
COMMENT ON COLUMN model_department_user_monthly_usage.updated_at IS '用量最后更新时间';

-- +goose Down
DROP TABLE IF EXISTS model_department_user_monthly_usage;
ALTER TABLE model_subject_policies DROP CONSTRAINT IF EXISTS model_subject_policies_token_mode_check;
ALTER TABLE model_subject_policies DROP COLUMN IF EXISTS monthly_token_mode;
COMMENT ON COLUMN model_subject_policies.requests_per_minute IS '每分钟最大请求数，0 表示不限制';
