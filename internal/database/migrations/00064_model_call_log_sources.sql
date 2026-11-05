-- +goose Up
ALTER TABLE model_call_logs
    DROP CONSTRAINT model_call_logs_source_check,
    ADD CONSTRAINT model_call_logs_source_check CHECK (source COLLATE "C" ~ '^[a-z][a-z0-9_]{0,31}$');

COMMENT ON TABLE model_call_logs IS '跨业务模型调用排查日志，保存请求、响应及失败详情，通过调用标识关联模型网关用量记录';
COMMENT ON COLUMN model_call_logs.source IS '发起调用的业务来源编码，首字符为小写英文字母，其余字符可用小写英文字母、数字或下划线，最多 32 个字符';
COMMENT ON COLUMN model_call_logs.request_body IS '用于排查的模型请求参数快照，已脱敏，不包含认证请求头';
COMMENT ON COLUMN model_call_logs.response_body IS '用于排查的模型响应正文，已脱敏，流式响应按事件保留';

-- +goose Down
-- 恢复旧版本的写入约束，同时保留已入库的新业务来源及其排查记录。
ALTER TABLE model_call_logs
    DROP CONSTRAINT model_call_logs_source_check,
    ADD CONSTRAINT model_call_logs_source_check CHECK (source IN ('model_gateway', 'ai_drama', 'ai_novel')) NOT VALID;

COMMENT ON TABLE model_call_logs IS '模型调用原始请求、响应与失败详情日志，通过调用标识关联模型网关用量记录';
COMMENT ON COLUMN model_call_logs.source IS '发起调用的业务入口，包括模型网关、AI 漫剧与 AI 小说';
COMMENT ON COLUMN model_call_logs.request_body IS '发往模型供应商的原始请求正文，不包含认证请求头';
COMMENT ON COLUMN model_call_logs.response_body IS '模型供应商的原始响应正文；流式响应按收到的字节保存';
