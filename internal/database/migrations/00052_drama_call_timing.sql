-- +goose Up
ALTER TABLE model_call_logs
 ADD COLUMN workflow_stage text,
 ADD COLUMN reasoning_policy text,
 ADD COLUMN requested_at timestamptz,
 ADD COLUMN headers_at timestamptz,
 ADD COLUMN first_reasoning_at timestamptz,
 ADD COLUMN first_content_at timestamptz,
 ADD COLUMN finished_at timestamptz,
 ADD COLUMN reasoning_tokens integer;
COMMENT ON TABLE model_call_logs IS '模型调用原始请求、响应、失败详情及供应商响应阶段计时日志';
COMMENT ON COLUMN model_call_logs.workflow_stage IS '业务调用阶段，空值表示旧记录或入口尚未观测';
COMMENT ON COLUMN model_call_logs.reasoning_policy IS '按实际供应商能力应用的思考策略';
COMMENT ON COLUMN model_call_logs.requested_at IS '发起供应商请求的时间';
COMMENT ON COLUMN model_call_logs.headers_at IS '收到供应商响应头的时间，失败未收到时为空';
COMMENT ON COLUMN model_call_logs.first_reasoning_at IS '首次收到思考增量的时间，不包含思考正文';
COMMENT ON COLUMN model_call_logs.first_content_at IS '首次收到正文增量的时间';
COMMENT ON COLUMN model_call_logs.finished_at IS '模型响应读取或失败结束时间，不含调用日志保存';
COMMENT ON COLUMN model_call_logs.reasoning_tokens IS '供应商报告的思考用量，未启用计时的旧记录为空';

-- +goose Down
ALTER TABLE model_call_logs
 DROP COLUMN workflow_stage,
 DROP COLUMN reasoning_policy,
 DROP COLUMN requested_at,
 DROP COLUMN headers_at,
 DROP COLUMN first_reasoning_at,
 DROP COLUMN first_content_at,
 DROP COLUMN finished_at,
 DROP COLUMN reasoning_tokens;
