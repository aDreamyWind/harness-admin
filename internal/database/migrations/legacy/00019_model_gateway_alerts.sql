-- +goose Up
CREATE TABLE model_gateway_alerts (
    id UUID PRIMARY KEY,
    organization_id UUID NOT NULL REFERENCES organizations(id),
    alert_month DATE NOT NULL,
    dedupe_key TEXT NOT NULL,
    kind VARCHAR(24) NOT NULL,
    subject_type VARCHAR(24) NOT NULL,
    subject_name TEXT NOT NULL,
    severity VARCHAR(16) NOT NULL,
    used_tokens BIGINT NOT NULL DEFAULT 0,
    token_limit BIGINT NOT NULL DEFAULT 0,
    call_count BIGINT NOT NULL DEFAULT 0,
    unknown_usage_calls BIGINT NOT NULL DEFAULT 0,
    first_detected_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    acknowledged_at TIMESTAMPTZ,
    acknowledged_by UUID REFERENCES users(id) ON DELETE SET NULL,
    resolved_at TIMESTAMPTZ,
    CONSTRAINT model_gateway_alerts_month_check CHECK (EXTRACT(DAY FROM alert_month) = 1),
    CONSTRAINT model_gateway_alerts_kind_check CHECK (kind IN ('quota', 'unknown_usage')),
    CONSTRAINT model_gateway_alerts_subject_check CHECK (subject_type IN ('department', 'user', 'department_user', 'api_key', 'model')),
    CONSTRAINT model_gateway_alerts_severity_check CHECK (severity IN ('warning', 'critical')),
    CONSTRAINT model_gateway_alerts_counts_check CHECK (used_tokens >= 0 AND token_limit >= 0 AND call_count >= 0 AND unknown_usage_calls >= 0),
    UNIQUE (organization_id, alert_month, dedupe_key)
);

CREATE INDEX model_gateway_alerts_list_idx
    ON model_gateway_alerts (organization_id, alert_month, resolved_at, severity, first_detected_at DESC);

COMMENT ON TABLE model_gateway_alerts IS '模型网关站内预警记录；同企业、月份和预警对象按去重键保留一条';
COMMENT ON COLUMN model_gateway_alerts.id IS '预警唯一标识';
COMMENT ON COLUMN model_gateway_alerts.organization_id IS '所属企业标识';
COMMENT ON COLUMN model_gateway_alerts.alert_month IS '预警对应的 UTC 自然月首日';
COMMENT ON COLUMN model_gateway_alerts.dedupe_key IS '预警类型与额度对象组成的稳定去重键';
COMMENT ON COLUMN model_gateway_alerts.kind IS '预警类型：quota 额度、unknown_usage 成功调用用量未知';
COMMENT ON COLUMN model_gateway_alerts.subject_type IS '预警对象类型：部门、用户、部门中用户、调用密钥或模型';
COMMENT ON COLUMN model_gateway_alerts.subject_name IS '对象显示名称快照';
COMMENT ON COLUMN model_gateway_alerts.severity IS '严重程度：warning 接近上限或用量未知偏多，critical 已达到额度';
COMMENT ON COLUMN model_gateway_alerts.used_tokens IS '已明确统计的当月 Token 数';
COMMENT ON COLUMN model_gateway_alerts.token_limit IS '该对象的当月 Token 上限';
COMMENT ON COLUMN model_gateway_alerts.call_count IS '该模型当月成功调用次数，仅用于用量未知预警';
COMMENT ON COLUMN model_gateway_alerts.unknown_usage_calls IS '该模型当月成功调用中未返回用量的次数';
COMMENT ON COLUMN model_gateway_alerts.first_detected_at IS '本月首次发现该预警的时间';
COMMENT ON COLUMN model_gateway_alerts.updated_at IS '预警数据最近刷新时间';
COMMENT ON COLUMN model_gateway_alerts.acknowledged_at IS '管理员确认知悉的时间；严重程度升级后清空';
COMMENT ON COLUMN model_gateway_alerts.acknowledged_by IS '确认知悉的企业用户标识';
COMMENT ON COLUMN model_gateway_alerts.resolved_at IS '预警条件不再成立的时间；重新出现时清空';

-- +goose Down
DROP TABLE IF EXISTS model_gateway_alerts;
