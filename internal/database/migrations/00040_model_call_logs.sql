-- +goose Up
CREATE TABLE model_call_logs (
    call_id uuid PRIMARY KEY REFERENCES model_gateway_calls(id) ON DELETE CASCADE,
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    source varchar(32) NOT NULL CHECK (source IN ('model_gateway', 'ai_drama')),
    operation varchar(40) NOT NULL,
    request_body bytea NOT NULL DEFAULT ''::bytea,
    response_body bytea NOT NULL DEFAULT ''::bytea,
    response_truncated boolean NOT NULL DEFAULT false,
    error_message text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT NOW()
);
CREATE INDEX model_call_logs_org_created_idx ON model_call_logs(organization_id, created_at DESC);
COMMENT ON TABLE model_call_logs IS '模型调用原始请求、响应与失败详情日志，通过调用标识关联模型网关用量记录';
COMMENT ON COLUMN model_call_logs.call_id IS '对应模型网关调用记录的唯一标识';
COMMENT ON COLUMN model_call_logs.organization_id IS '所属企业标识，用于排查时按企业隔离';
COMMENT ON COLUMN model_call_logs.source IS '发起调用的业务入口：模型网关或 AI 漫剧';
COMMENT ON COLUMN model_call_logs.operation IS '调用操作：chat、image、video 或视频任务查询';
COMMENT ON COLUMN model_call_logs.request_body IS '发往模型供应商的原始请求正文，不包含认证请求头';
COMMENT ON COLUMN model_call_logs.response_body IS '模型供应商的原始响应正文；流式响应按收到的字节保存';
COMMENT ON COLUMN model_call_logs.response_truncated IS '响应超过网关最大记录体积时是否截断';
COMMENT ON COLUMN model_call_logs.error_message IS '网络、超时、读取或解析失败的详细错误';
COMMENT ON COLUMN model_call_logs.created_at IS '日志创建时间';

-- +goose Down
DROP TABLE model_call_logs;
