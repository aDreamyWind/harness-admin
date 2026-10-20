-- +goose Up
CREATE TABLE model_gateway_alert_settings (
    organization_id UUID PRIMARY KEY REFERENCES organizations(id) ON DELETE CASCADE,
    auto_refresh_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by UUID REFERENCES users(id) ON DELETE SET NULL
);

CREATE INDEX model_gateway_alert_settings_enabled_idx
    ON model_gateway_alert_settings (organization_id) WHERE auto_refresh_enabled;

COMMENT ON TABLE model_gateway_alert_settings IS '企业模型网关站内预警的后台定时刷新设置';
COMMENT ON COLUMN model_gateway_alert_settings.organization_id IS '所属企业标识';
COMMENT ON COLUMN model_gateway_alert_settings.auto_refresh_enabled IS '是否启用后台每分钟自动刷新预警；默认关闭';
COMMENT ON COLUMN model_gateway_alert_settings.updated_at IS '设置最近修改时间';
COMMENT ON COLUMN model_gateway_alert_settings.updated_by IS '最近修改设置的企业用户标识';

-- +goose Down
DROP TABLE IF EXISTS model_gateway_alert_settings;
