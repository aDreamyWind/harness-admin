-- +goose Up
ALTER TABLE model_gateway_api_keys
    ADD COLUMN requests_per_minute INTEGER NOT NULL DEFAULT 60,
    ADD COLUMN monthly_token_limit BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN allowed_model_codes TEXT[] NOT NULL DEFAULT '{}'::TEXT[],
    ADD CONSTRAINT model_gateway_api_keys_rpm_check CHECK (requests_per_minute BETWEEN 0 AND 100000),
    ADD CONSTRAINT model_gateway_api_keys_monthly_token_limit_check CHECK (monthly_token_limit >= 0);

COMMENT ON COLUMN model_gateway_api_keys.requests_per_minute IS '每分钟最大请求数，0 表示不限制';
COMMENT ON COLUMN model_gateway_api_keys.monthly_token_limit IS '每月最大 Token 用量，0 表示不限制';
COMMENT ON COLUMN model_gateway_api_keys.allowed_model_codes IS '允许调用的模型编码集合，空数组表示全部已启用模型';

CREATE TABLE model_gateway_monthly_usage (
    organization_id UUID NOT NULL,
    api_key_id UUID NOT NULL,
    usage_month DATE NOT NULL,
    request_count BIGINT NOT NULL DEFAULT 0,
    total_tokens BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (organization_id, api_key_id, usage_month),
    CONSTRAINT model_gateway_monthly_usage_key_org_fk FOREIGN KEY (organization_id, api_key_id)
        REFERENCES model_gateway_api_keys (organization_id, id) ON DELETE CASCADE,
    CONSTRAINT model_gateway_monthly_usage_request_count_check CHECK (request_count >= 0),
    CONSTRAINT model_gateway_monthly_usage_total_tokens_check CHECK (total_tokens >= 0),
    CONSTRAINT model_gateway_monthly_usage_month_start_check CHECK (EXTRACT(DAY FROM usage_month) = 1)
);

CREATE INDEX model_gateway_monthly_usage_org_month_idx
    ON model_gateway_monthly_usage (organization_id, usage_month DESC);

COMMENT ON TABLE model_gateway_monthly_usage IS '模型网关调用密钥月度用量汇总';
COMMENT ON COLUMN model_gateway_monthly_usage.organization_id IS '所属企业标识';
COMMENT ON COLUMN model_gateway_monthly_usage.api_key_id IS '网关调用密钥标识';
COMMENT ON COLUMN model_gateway_monthly_usage.usage_month IS '用量月份，固定为该月第一天并按 UTC 统计';
COMMENT ON COLUMN model_gateway_monthly_usage.request_count IS '当月已记录请求数';
COMMENT ON COLUMN model_gateway_monthly_usage.total_tokens IS '当月已记录 Token 总数';
COMMENT ON COLUMN model_gateway_monthly_usage.updated_at IS '最后汇总时间';

UPDATE permissions
SET description = '创建、修改和吊销当前企业的网关调用密钥'
WHERE code = 'model.gateway.write';

-- +goose Down
UPDATE permissions
SET description = '创建和吊销当前企业的网关调用密钥'
WHERE code = 'model.gateway.write';

DROP TABLE IF EXISTS model_gateway_monthly_usage;
ALTER TABLE model_gateway_api_keys
    DROP CONSTRAINT IF EXISTS model_gateway_api_keys_monthly_token_limit_check,
    DROP CONSTRAINT IF EXISTS model_gateway_api_keys_rpm_check,
    DROP COLUMN IF EXISTS allowed_model_codes,
    DROP COLUMN IF EXISTS monthly_token_limit,
    DROP COLUMN IF EXISTS requests_per_minute;
