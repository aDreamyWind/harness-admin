-- +goose Up
ALTER TABLE model_gateway_calls
    ADD COLUMN department_id UUID,
    ADD COLUMN usage_reported BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN quota_exceeded BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE model_gateway_calls
SET usage_reported = TRUE
WHERE prompt_tokens > 0 OR completion_tokens > 0 OR total_tokens > 0;

CREATE INDEX model_gateway_calls_org_month_department_idx
    ON model_gateway_calls (organization_id, created_at, department_id);

COMMENT ON TABLE model_gateway_calls IS '模型网关调用记录，包含用量可靠性、超额状态与调用时部门快照';
COMMENT ON COLUMN model_gateway_calls.department_id IS '调用发生时用户所属部门标识快照；历史调用和密钥调用可能为空';
COMMENT ON COLUMN model_gateway_calls.usage_reported IS '上游是否明确返回 Token 用量；否时用量未知，不应当作零消耗';
COMMENT ON COLUMN model_gateway_calls.quota_exceeded IS '该次调用记账后是否使调用身份的月度 Token 额度超限';

-- +goose Down
DROP INDEX IF EXISTS model_gateway_calls_org_month_department_idx;
ALTER TABLE model_gateway_calls
    DROP COLUMN IF EXISTS quota_exceeded,
    DROP COLUMN IF EXISTS usage_reported,
    DROP COLUMN IF EXISTS department_id;
COMMENT ON TABLE model_gateway_calls IS '模型网关调用记录';
