-- +goose Up
-- 当前数据库结构基线，等价于历史迁移 00001 至 00020 的最终状态。
-- 仅供全新数据库使用；已有数据库继续沿用历史迁移版本记录。

CREATE TABLE public.audit_events (
    id bigint NOT NULL,
    organization_id uuid,
    actor_id uuid,
    request_id character varying(80) DEFAULT ''::character varying NOT NULL,
    action character varying(120) NOT NULL,
    resource_type character varying(80) NOT NULL,
    resource_id character varying(120) DEFAULT ''::character varying NOT NULL,
    result character varying(24) NOT NULL,
    source_ip inet,
    detail jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT audit_events_result_check CHECK (((result)::text = ANY ((ARRAY['success'::character varying, 'denied'::character varying, 'failed'::character varying])::text[])))
);
COMMENT ON TABLE public.audit_events IS '系统审计事件';
COMMENT ON COLUMN public.audit_events.id IS '审计事件自增标识';
COMMENT ON COLUMN public.audit_events.organization_id IS '事件所属企业标识，可为空';
COMMENT ON COLUMN public.audit_events.actor_id IS '操作用户标识，可为空';
COMMENT ON COLUMN public.audit_events.request_id IS '关联 HTTP 请求标识';
COMMENT ON COLUMN public.audit_events.action IS '审计动作代码';
COMMENT ON COLUMN public.audit_events.resource_type IS '被操作资源类型';
COMMENT ON COLUMN public.audit_events.resource_id IS '被操作资源标识';
COMMENT ON COLUMN public.audit_events.result IS '操作结果：success 成功，denied 拒绝，failed 失败';
COMMENT ON COLUMN public.audit_events.source_ip IS '操作来源 IP 地址';
COMMENT ON COLUMN public.audit_events.detail IS '事件扩展详情，JSON 格式';
COMMENT ON COLUMN public.audit_events.created_at IS '事件发生时间';
CREATE SEQUENCE public.audit_events_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;
ALTER SEQUENCE public.audit_events_id_seq OWNED BY public.audit_events.id;
CREATE TABLE public.departments (
    id uuid NOT NULL,
    organization_id uuid NOT NULL,
    parent_id uuid,
    code character varying(64) NOT NULL,
    name character varying(160) NOT NULL,
    sort_order integer DEFAULT 0 NOT NULL,
    status character varying(24) DEFAULT 'active'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT departments_status_check CHECK (((status)::text = ANY ((ARRAY['active'::character varying, 'disabled'::character varying])::text[])))
);
COMMENT ON TABLE public.departments IS '企业部门';
COMMENT ON COLUMN public.departments.id IS '部门唯一标识';
COMMENT ON COLUMN public.departments.organization_id IS '所属企业标识';
COMMENT ON COLUMN public.departments.parent_id IS '上级部门标识，顶级部门为空';
COMMENT ON COLUMN public.departments.code IS '企业内唯一的部门代码';
COMMENT ON COLUMN public.departments.name IS '部门名称';
COMMENT ON COLUMN public.departments.sort_order IS '同级部门排序值，数值越小越靠前';
COMMENT ON COLUMN public.departments.status IS '部门状态：active 启用，disabled 停用';
COMMENT ON COLUMN public.departments.created_at IS '创建时间';
COMMENT ON COLUMN public.departments.updated_at IS '最后更新时间';
CREATE TABLE public.model_catalog_entries (
    id uuid NOT NULL,
    organization_id uuid NOT NULL,
    provider_connection_id uuid NOT NULL,
    code character varying(80) NOT NULL,
    upstream_model_id character varying(200) NOT NULL,
    name character varying(120) NOT NULL,
    model_type character varying(24) DEFAULT 'chat'::character varying NOT NULL,
    capabilities text[] DEFAULT '{}'::text[] NOT NULL,
    context_window_tokens integer DEFAULT 0 NOT NULL,
    max_output_tokens integer DEFAULT 0 NOT NULL,
    status character varying(24) DEFAULT 'active'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT model_catalog_entries_context_window_check CHECK ((context_window_tokens >= 0)),
    CONSTRAINT model_catalog_entries_max_output_check CHECK ((max_output_tokens >= 0)),
    CONSTRAINT model_catalog_entries_status_check CHECK (((status)::text = ANY ((ARRAY['active'::character varying, 'disabled'::character varying])::text[]))),
    CONSTRAINT model_catalog_entries_token_range_check CHECK (((context_window_tokens = 0) OR (max_output_tokens <= context_window_tokens))),
    CONSTRAINT model_catalog_entries_type_check CHECK (((model_type)::text = 'chat'::text))
);
COMMENT ON TABLE public.model_catalog_entries IS '企业模型目录';
COMMENT ON COLUMN public.model_catalog_entries.id IS '模型目录项唯一标识';
COMMENT ON COLUMN public.model_catalog_entries.organization_id IS '所属企业标识';
COMMENT ON COLUMN public.model_catalog_entries.provider_connection_id IS '所属模型供应商连接标识';
COMMENT ON COLUMN public.model_catalog_entries.code IS '企业内稳定且唯一的模型编码，不区分大小写';
COMMENT ON COLUMN public.model_catalog_entries.upstream_model_id IS '供应商侧模型标识';
COMMENT ON COLUMN public.model_catalog_entries.name IS '模型显示名称';
COMMENT ON COLUMN public.model_catalog_entries.model_type IS '模型类型，当前支持 chat';
COMMENT ON COLUMN public.model_catalog_entries.capabilities IS '模型能力代码集合，如 text、vision、tools、streaming、json';
COMMENT ON COLUMN public.model_catalog_entries.context_window_tokens IS '模型上下文窗口 Token 数，0 表示未知';
COMMENT ON COLUMN public.model_catalog_entries.max_output_tokens IS '模型最大输出 Token 数，0 表示未知';
COMMENT ON COLUMN public.model_catalog_entries.status IS '模型状态：active 启用，disabled 停用';
COMMENT ON COLUMN public.model_catalog_entries.created_at IS '创建时间';
COMMENT ON COLUMN public.model_catalog_entries.updated_at IS '最后更新时间';
CREATE TABLE public.model_department_user_monthly_usage (
    organization_id uuid NOT NULL,
    subject_type character varying(16) DEFAULT 'department'::character varying NOT NULL,
    subject_id uuid NOT NULL,
    user_id uuid NOT NULL,
    usage_month date NOT NULL,
    request_count bigint DEFAULT 0 NOT NULL,
    total_tokens bigint DEFAULT 0 NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT model_department_user_monthly_usage_counts_check CHECK (((request_count >= 0) AND (total_tokens >= 0))),
    CONSTRAINT model_department_user_monthly_usage_month_check CHECK ((EXTRACT(day FROM usage_month) = (1)::numeric)),
    CONSTRAINT model_department_user_monthly_usage_subject_type_check CHECK (((subject_type)::text = 'department'::text))
);
COMMENT ON TABLE public.model_department_user_monthly_usage IS '部门模型策略按用户独立统计的月度用量';
COMMENT ON COLUMN public.model_department_user_monthly_usage.organization_id IS '所属企业标识';
COMMENT ON COLUMN public.model_department_user_monthly_usage.subject_type IS '固定为 department，用于关联部门模型策略';
COMMENT ON COLUMN public.model_department_user_monthly_usage.subject_id IS '部门模型策略所属部门标识';
COMMENT ON COLUMN public.model_department_user_monthly_usage.user_id IS '实际调用模型的用户标识';
COMMENT ON COLUMN public.model_department_user_monthly_usage.usage_month IS 'UTC 统计月份的第一天';
COMMENT ON COLUMN public.model_department_user_monthly_usage.request_count IS '该用户在此部门策略下当月已记录请求数';
COMMENT ON COLUMN public.model_department_user_monthly_usage.total_tokens IS '该用户在此部门策略下当月已记录 Token 数';
COMMENT ON COLUMN public.model_department_user_monthly_usage.updated_at IS '用量最后更新时间';
CREATE TABLE public.model_gateway_alert_settings (
    organization_id uuid NOT NULL,
    auto_refresh_enabled boolean DEFAULT false NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_by uuid
);
COMMENT ON TABLE public.model_gateway_alert_settings IS '企业模型网关站内预警的后台定时刷新设置';
COMMENT ON COLUMN public.model_gateway_alert_settings.organization_id IS '所属企业标识';
COMMENT ON COLUMN public.model_gateway_alert_settings.auto_refresh_enabled IS '是否启用后台每分钟自动刷新预警；默认关闭';
COMMENT ON COLUMN public.model_gateway_alert_settings.updated_at IS '设置最近修改时间';
COMMENT ON COLUMN public.model_gateway_alert_settings.updated_by IS '最近修改设置的企业用户标识';
CREATE TABLE public.model_gateway_alerts (
    id uuid NOT NULL,
    organization_id uuid NOT NULL,
    alert_month date NOT NULL,
    dedupe_key text NOT NULL,
    kind character varying(24) NOT NULL,
    subject_type character varying(24) NOT NULL,
    subject_name text NOT NULL,
    severity character varying(16) NOT NULL,
    used_tokens bigint DEFAULT 0 NOT NULL,
    token_limit bigint DEFAULT 0 NOT NULL,
    call_count bigint DEFAULT 0 NOT NULL,
    unknown_usage_calls bigint DEFAULT 0 NOT NULL,
    first_detected_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    acknowledged_at timestamp with time zone,
    acknowledged_by uuid,
    resolved_at timestamp with time zone,
    CONSTRAINT model_gateway_alerts_counts_check CHECK (((used_tokens >= 0) AND (token_limit >= 0) AND (call_count >= 0) AND (unknown_usage_calls >= 0))),
    CONSTRAINT model_gateway_alerts_kind_check CHECK (((kind)::text = ANY ((ARRAY['quota'::character varying, 'unknown_usage'::character varying])::text[]))),
    CONSTRAINT model_gateway_alerts_month_check CHECK ((EXTRACT(day FROM alert_month) = (1)::numeric)),
    CONSTRAINT model_gateway_alerts_severity_check CHECK (((severity)::text = ANY ((ARRAY['warning'::character varying, 'critical'::character varying])::text[]))),
    CONSTRAINT model_gateway_alerts_subject_check CHECK (((subject_type)::text = ANY ((ARRAY['department'::character varying, 'user'::character varying, 'department_user'::character varying, 'api_key'::character varying, 'model'::character varying])::text[])))
);
COMMENT ON TABLE public.model_gateway_alerts IS '模型网关站内预警记录；同企业、月份和预警对象按去重键保留一条';
COMMENT ON COLUMN public.model_gateway_alerts.id IS '预警唯一标识';
COMMENT ON COLUMN public.model_gateway_alerts.organization_id IS '所属企业标识';
COMMENT ON COLUMN public.model_gateway_alerts.alert_month IS '预警对应的 UTC 自然月首日';
COMMENT ON COLUMN public.model_gateway_alerts.dedupe_key IS '预警类型与额度对象组成的稳定去重键';
COMMENT ON COLUMN public.model_gateway_alerts.kind IS '预警类型：quota 额度、unknown_usage 成功调用用量未知';
COMMENT ON COLUMN public.model_gateway_alerts.subject_type IS '预警对象类型：部门、用户、部门中用户、调用密钥或模型';
COMMENT ON COLUMN public.model_gateway_alerts.subject_name IS '对象显示名称快照';
COMMENT ON COLUMN public.model_gateway_alerts.severity IS '严重程度：warning 接近上限或用量未知偏多，critical 已达到额度';
COMMENT ON COLUMN public.model_gateway_alerts.used_tokens IS '已明确统计的当月 Token 数';
COMMENT ON COLUMN public.model_gateway_alerts.token_limit IS '该对象的当月 Token 上限';
COMMENT ON COLUMN public.model_gateway_alerts.call_count IS '该模型当月成功调用次数，仅用于用量未知预警';
COMMENT ON COLUMN public.model_gateway_alerts.unknown_usage_calls IS '该模型当月成功调用中未返回用量的次数';
COMMENT ON COLUMN public.model_gateway_alerts.first_detected_at IS '本月首次发现该预警的时间';
COMMENT ON COLUMN public.model_gateway_alerts.updated_at IS '预警数据最近刷新时间';
COMMENT ON COLUMN public.model_gateway_alerts.acknowledged_at IS '管理员确认知悉的时间；严重程度升级后清空';
COMMENT ON COLUMN public.model_gateway_alerts.acknowledged_by IS '确认知悉的企业用户标识';
COMMENT ON COLUMN public.model_gateway_alerts.resolved_at IS '预警条件不再成立的时间；重新出现时清空';
CREATE TABLE public.model_gateway_api_keys (
    id uuid NOT NULL,
    organization_id uuid NOT NULL,
    name character varying(120) NOT NULL,
    key_prefix character varying(32) NOT NULL,
    secret_hash bytea NOT NULL,
    status character varying(24) DEFAULT 'active'::character varying NOT NULL,
    expires_at timestamp with time zone,
    last_used_at timestamp with time zone,
    created_by uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    revoked_at timestamp with time zone,
    requests_per_minute integer DEFAULT 60 NOT NULL,
    monthly_token_limit bigint DEFAULT 0 NOT NULL,
    allowed_model_codes text[] DEFAULT '{}'::text[] NOT NULL,
    encrypted_secret bytea,
    CONSTRAINT model_gateway_api_keys_monthly_token_limit_check CHECK ((monthly_token_limit >= 0)),
    CONSTRAINT model_gateway_api_keys_rpm_check CHECK (((requests_per_minute >= 0) AND (requests_per_minute <= 100000))),
    CONSTRAINT model_gateway_api_keys_status_check CHECK (((status)::text = ANY ((ARRAY['active'::character varying, 'revoked'::character varying])::text[])))
);
COMMENT ON TABLE public.model_gateway_api_keys IS '企业模型网关调用密钥';
COMMENT ON COLUMN public.model_gateway_api_keys.id IS '网关调用密钥唯一标识';
COMMENT ON COLUMN public.model_gateway_api_keys.organization_id IS '所属企业标识';
COMMENT ON COLUMN public.model_gateway_api_keys.name IS '调用密钥名称';
COMMENT ON COLUMN public.model_gateway_api_keys.key_prefix IS '用于识别密钥的非敏感前缀';
COMMENT ON COLUMN public.model_gateway_api_keys.secret_hash IS '完整调用密钥的 SHA-256 哈希';
COMMENT ON COLUMN public.model_gateway_api_keys.status IS '密钥状态：active 可用、revoked 已吊销';
COMMENT ON COLUMN public.model_gateway_api_keys.expires_at IS '密钥过期时间，为空表示永不过期';
COMMENT ON COLUMN public.model_gateway_api_keys.last_used_at IS '最近一次成功鉴权时间';
COMMENT ON COLUMN public.model_gateway_api_keys.created_by IS '创建密钥的企业用户标识';
COMMENT ON COLUMN public.model_gateway_api_keys.created_at IS '创建时间';
COMMENT ON COLUMN public.model_gateway_api_keys.updated_at IS '最后更新时间';
COMMENT ON COLUMN public.model_gateway_api_keys.revoked_at IS '吊销时间';
COMMENT ON COLUMN public.model_gateway_api_keys.requests_per_minute IS '每分钟最大请求数，0 表示不限制';
COMMENT ON COLUMN public.model_gateway_api_keys.monthly_token_limit IS '每月最大 Token 用量，0 表示不限制';
COMMENT ON COLUMN public.model_gateway_api_keys.allowed_model_codes IS '允许调用的模型编码集合，空数组表示全部已启用模型';
COMMENT ON COLUMN public.model_gateway_api_keys.encrypted_secret IS '使用服务端主密钥加密后的模型网关调用密钥；历史未加密记录为空';
CREATE TABLE public.model_gateway_calls (
    id uuid NOT NULL,
    organization_id uuid NOT NULL,
    api_key_id uuid,
    model_catalog_entry_id uuid NOT NULL,
    provider_connection_id uuid NOT NULL,
    request_id character varying(120) NOT NULL,
    model_code character varying(80) NOT NULL,
    upstream_model_id character varying(200) NOT NULL,
    streaming boolean DEFAULT false NOT NULL,
    result character varying(24) NOT NULL,
    http_status integer NOT NULL,
    latency_ms bigint NOT NULL,
    prompt_tokens integer DEFAULT 0 NOT NULL,
    completion_tokens integer DEFAULT 0 NOT NULL,
    total_tokens integer DEFAULT 0 NOT NULL,
    error_code character varying(80) DEFAULT ''::character varying NOT NULL,
    source_ip character varying(80) DEFAULT ''::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    auth_type character varying(24) DEFAULT 'api_key'::character varying NOT NULL,
    user_id uuid,
    department_id uuid,
    usage_reported boolean DEFAULT false NOT NULL,
    quota_exceeded boolean DEFAULT false NOT NULL,
    CONSTRAINT model_gateway_calls_auth_type_check CHECK (((auth_type)::text = ANY ((ARRAY['api_key'::character varying, 'user'::character varying])::text[]))),
    CONSTRAINT model_gateway_calls_caller_check CHECK (((((auth_type)::text = 'api_key'::text) AND (api_key_id IS NOT NULL) AND (user_id IS NULL)) OR (((auth_type)::text = 'user'::text) AND (api_key_id IS NULL) AND (user_id IS NOT NULL)))),
    CONSTRAINT model_gateway_calls_http_status_check CHECK (((http_status >= 100) AND (http_status <= 599))),
    CONSTRAINT model_gateway_calls_latency_check CHECK ((latency_ms >= 0)),
    CONSTRAINT model_gateway_calls_result_check CHECK (((result)::text = ANY ((ARRAY['success'::character varying, 'failed'::character varying])::text[]))),
    CONSTRAINT model_gateway_calls_token_check CHECK (((prompt_tokens >= 0) AND (completion_tokens >= 0) AND (total_tokens >= 0)))
);
COMMENT ON TABLE public.model_gateway_calls IS '模型网关调用记录，包含用量可靠性、超额状态与调用时部门快照';
COMMENT ON COLUMN public.model_gateway_calls.id IS '网关调用记录唯一标识';
COMMENT ON COLUMN public.model_gateway_calls.organization_id IS '所属企业标识';
COMMENT ON COLUMN public.model_gateway_calls.api_key_id IS '发起调用的网关密钥标识，OIDC 用户调用时为空';
COMMENT ON COLUMN public.model_gateway_calls.model_catalog_entry_id IS '命中的模型目录项标识';
COMMENT ON COLUMN public.model_gateway_calls.provider_connection_id IS '实际使用的供应商连接标识';
COMMENT ON COLUMN public.model_gateway_calls.request_id IS '本次调用请求标识';
COMMENT ON COLUMN public.model_gateway_calls.model_code IS '调用方请求的模型编码快照';
COMMENT ON COLUMN public.model_gateway_calls.upstream_model_id IS '供应商侧模型标识快照';
COMMENT ON COLUMN public.model_gateway_calls.streaming IS '是否为流式调用';
COMMENT ON COLUMN public.model_gateway_calls.result IS '调用结果：success 成功、failed 失败';
COMMENT ON COLUMN public.model_gateway_calls.http_status IS '返回给调用方的 HTTP 状态码';
COMMENT ON COLUMN public.model_gateway_calls.latency_ms IS '网关处理耗时，单位毫秒';
COMMENT ON COLUMN public.model_gateway_calls.prompt_tokens IS '上游返回的输入 Token 数，未知时为 0';
COMMENT ON COLUMN public.model_gateway_calls.completion_tokens IS '上游返回的输出 Token 数，未知时为 0';
COMMENT ON COLUMN public.model_gateway_calls.total_tokens IS '上游返回的总 Token 数，未知时为 0';
COMMENT ON COLUMN public.model_gateway_calls.error_code IS '失败错误代码，成功时为空字符串';
COMMENT ON COLUMN public.model_gateway_calls.source_ip IS '调用方来源 IP';
COMMENT ON COLUMN public.model_gateway_calls.created_at IS '调用发生时间';
COMMENT ON COLUMN public.model_gateway_calls.auth_type IS '调用身份类型：api_key 调用密钥、user OIDC 用户';
COMMENT ON COLUMN public.model_gateway_calls.user_id IS '使用 OIDC 访问令牌发起调用的用户标识';
COMMENT ON COLUMN public.model_gateway_calls.department_id IS '调用发生时用户所属部门标识快照；历史调用和密钥调用可能为空';
COMMENT ON COLUMN public.model_gateway_calls.usage_reported IS '上游是否明确返回 Token 用量；否时用量未知，不应当作零消耗';
COMMENT ON COLUMN public.model_gateway_calls.quota_exceeded IS '该次调用记账后是否使调用身份的月度 Token 额度超限';
CREATE TABLE public.model_gateway_monthly_usage (
    organization_id uuid NOT NULL,
    api_key_id uuid NOT NULL,
    usage_month date NOT NULL,
    request_count bigint DEFAULT 0 NOT NULL,
    total_tokens bigint DEFAULT 0 NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT model_gateway_monthly_usage_month_start_check CHECK ((EXTRACT(day FROM usage_month) = (1)::numeric)),
    CONSTRAINT model_gateway_monthly_usage_request_count_check CHECK ((request_count >= 0)),
    CONSTRAINT model_gateway_monthly_usage_total_tokens_check CHECK ((total_tokens >= 0))
);
COMMENT ON TABLE public.model_gateway_monthly_usage IS '模型网关调用密钥月度用量汇总';
COMMENT ON COLUMN public.model_gateway_monthly_usage.organization_id IS '所属企业标识';
COMMENT ON COLUMN public.model_gateway_monthly_usage.api_key_id IS '网关调用密钥标识';
COMMENT ON COLUMN public.model_gateway_monthly_usage.usage_month IS '用量月份，固定为该月第一天并按 UTC 统计';
COMMENT ON COLUMN public.model_gateway_monthly_usage.request_count IS '当月已记录请求数';
COMMENT ON COLUMN public.model_gateway_monthly_usage.total_tokens IS '当月已记录 Token 总数';
COMMENT ON COLUMN public.model_gateway_monthly_usage.updated_at IS '最后汇总时间';
CREATE TABLE public.model_provider_connections (
    id uuid NOT NULL,
    organization_id uuid NOT NULL,
    code character varying(80) NOT NULL,
    name character varying(120) NOT NULL,
    provider_kind character varying(40) NOT NULL,
    base_url text NOT NULL,
    status character varying(24) DEFAULT 'active'::character varying NOT NULL,
    request_timeout_ms integer DEFAULT 15000 NOT NULL,
    last_probe_status character varying(24) DEFAULT 'unknown'::character varying NOT NULL,
    last_probe_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    stream_usage_enabled boolean DEFAULT false NOT NULL,
    CONSTRAINT model_provider_connections_kind_check CHECK (((provider_kind)::text = ANY ((ARRAY['openai_compatible'::character varying, 'deepseek'::character varying, 'aliyun_bailian'::character varying, 'kimi'::character varying, 'ollama'::character varying])::text[]))),
    CONSTRAINT model_provider_connections_probe_status_check CHECK (((last_probe_status)::text = ANY ((ARRAY['unknown'::character varying, 'success'::character varying, 'failed'::character varying])::text[]))),
    CONSTRAINT model_provider_connections_status_check CHECK (((status)::text = ANY ((ARRAY['active'::character varying, 'disabled'::character varying])::text[]))),
    CONSTRAINT model_provider_connections_timeout_check CHECK (((request_timeout_ms >= 1000) AND (request_timeout_ms <= 120000)))
);
COMMENT ON TABLE public.model_provider_connections IS '企业模型供应商连接，包含流式用量请求设置';
COMMENT ON COLUMN public.model_provider_connections.id IS '模型供应商连接唯一标识';
COMMENT ON COLUMN public.model_provider_connections.organization_id IS '所属企业标识';
COMMENT ON COLUMN public.model_provider_connections.code IS '企业内唯一的供应商连接编码，不区分大小写';
COMMENT ON COLUMN public.model_provider_connections.name IS '供应商连接显示名称';
COMMENT ON COLUMN public.model_provider_connections.provider_kind IS '供应商协议类型：openai_compatible OpenAI兼容、deepseek DeepSeek、aliyun_bailian 阿里云百炼、kimi Kimi、ollama Ollama';
COMMENT ON COLUMN public.model_provider_connections.base_url IS '供应商 API 基础地址';
COMMENT ON COLUMN public.model_provider_connections.status IS '连接状态：active 启用，disabled 停用';
COMMENT ON COLUMN public.model_provider_connections.request_timeout_ms IS '供应商请求超时时间，单位毫秒';
COMMENT ON COLUMN public.model_provider_connections.last_probe_status IS '最近连接检测结果：unknown 未检测，success 成功，failed 失败';
COMMENT ON COLUMN public.model_provider_connections.last_probe_at IS '最近一次连接检测时间';
COMMENT ON COLUMN public.model_provider_connections.created_at IS '创建时间';
COMMENT ON COLUMN public.model_provider_connections.updated_at IS '最后更新时间';
COMMENT ON COLUMN public.model_provider_connections.stream_usage_enabled IS '启用时在流式聊天请求中向上游发送 stream_options.include_usage=true；默认关闭以兼容不支持的供应商';
CREATE TABLE public.model_provider_credentials (
    provider_connection_id uuid NOT NULL,
    encrypted_secret bytea NOT NULL,
    key_version integer DEFAULT 1 NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);
COMMENT ON TABLE public.model_provider_credentials IS '模型供应商加密访问凭据';
COMMENT ON COLUMN public.model_provider_credentials.provider_connection_id IS '所属模型供应商连接标识';
COMMENT ON COLUMN public.model_provider_credentials.encrypted_secret IS '使用服务端主密钥加密后的供应商访问凭据';
COMMENT ON COLUMN public.model_provider_credentials.key_version IS '凭据加密主密钥版本';
COMMENT ON COLUMN public.model_provider_credentials.updated_at IS '凭据最后更新时间';
CREATE TABLE public.model_subject_monthly_usage (
    organization_id uuid NOT NULL,
    subject_type character varying(16) NOT NULL,
    subject_id uuid NOT NULL,
    usage_month date NOT NULL,
    request_count bigint DEFAULT 0 NOT NULL,
    total_tokens bigint DEFAULT 0 NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT model_subject_monthly_usage_counts_check CHECK (((request_count >= 0) AND (total_tokens >= 0))),
    CONSTRAINT model_subject_monthly_usage_month_check CHECK ((EXTRACT(day FROM usage_month) = (1)::numeric))
);
COMMENT ON TABLE public.model_subject_monthly_usage IS '部门和用户策略的月度模型调用用量';
COMMENT ON COLUMN public.model_subject_monthly_usage.organization_id IS '所属企业标识';
COMMENT ON COLUMN public.model_subject_monthly_usage.subject_type IS '用量对象类型：department 部门、user 用户';
COMMENT ON COLUMN public.model_subject_monthly_usage.subject_id IS '所属部门或用户标识';
COMMENT ON COLUMN public.model_subject_monthly_usage.usage_month IS 'UTC 统计月份的第一天';
COMMENT ON COLUMN public.model_subject_monthly_usage.request_count IS '当月已记录请求数';
COMMENT ON COLUMN public.model_subject_monthly_usage.total_tokens IS '当月已记录 Token 数';
COMMENT ON COLUMN public.model_subject_monthly_usage.updated_at IS '用量最后更新时间';
CREATE TABLE public.model_subject_policies (
    organization_id uuid NOT NULL,
    subject_type character varying(16) NOT NULL,
    subject_id uuid NOT NULL,
    scope_mode character varying(16) DEFAULT 'inherit'::character varying NOT NULL,
    allowed_model_codes text[],
    requests_per_minute integer DEFAULT 0 NOT NULL,
    monthly_token_limit bigint DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    monthly_token_mode character varying(16) DEFAULT 'shared'::character varying NOT NULL,
    CONSTRAINT model_subject_policies_monthly_check CHECK ((monthly_token_limit >= 0)),
    CONSTRAINT model_subject_policies_rpm_check CHECK (((requests_per_minute >= 0) AND (requests_per_minute <= 100000))),
    CONSTRAINT model_subject_policies_scope_check CHECK (((scope_mode)::text = ANY ((ARRAY['inherit'::character varying, 'all'::character varying, 'selected'::character varying, 'none'::character varying])::text[]))),
    CONSTRAINT model_subject_policies_token_mode_check CHECK ((((monthly_token_mode)::text = ANY ((ARRAY['shared'::character varying, 'per_user'::character varying])::text[])) AND (((subject_type)::text = 'department'::text) OR ((monthly_token_mode)::text = 'shared'::text)))),
    CONSTRAINT model_subject_policies_type_check CHECK (((subject_type)::text = ANY ((ARRAY['department'::character varying, 'user'::character varying])::text[])))
);
COMMENT ON TABLE public.model_subject_policies IS '部门和用户的模型访问与调用额度策略';
COMMENT ON COLUMN public.model_subject_policies.organization_id IS '所属企业标识';
COMMENT ON COLUMN public.model_subject_policies.subject_type IS '策略对象类型：department 部门、user 用户';
COMMENT ON COLUMN public.model_subject_policies.subject_id IS '所属部门或用户标识，由策略写入服务校验企业归属';
COMMENT ON COLUMN public.model_subject_policies.scope_mode IS '模型范围模式：inherit 继承、all 全部、selected 指定、none 禁用全部';
COMMENT ON COLUMN public.model_subject_policies.allowed_model_codes IS 'selected 模式允许使用的模型编码集合，其他模式为空数组';
COMMENT ON COLUMN public.model_subject_policies.requests_per_minute IS '每名用户在此策略下的每分钟最大请求数，0 表示不限制';
COMMENT ON COLUMN public.model_subject_policies.monthly_token_limit IS '按 UTC 自然月计算的 Token 上限，0 表示不限制';
COMMENT ON COLUMN public.model_subject_policies.created_at IS '策略创建时间';
COMMENT ON COLUMN public.model_subject_policies.updated_at IS '策略最后更新时间';
COMMENT ON COLUMN public.model_subject_policies.monthly_token_mode IS '部门月度 Token 额度计算方式：shared 部门共享、per_user 每名用户独立；用户策略固定为 shared';
CREATE TABLE public.oidc_authorization_codes (
    id uuid NOT NULL,
    organization_id uuid NOT NULL,
    client_id uuid NOT NULL,
    user_id uuid NOT NULL,
    code_hash bytea NOT NULL,
    redirect_uri text NOT NULL,
    scopes text[] NOT NULL,
    nonce character varying(255),
    code_challenge character varying(128) NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    consumed_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT oidc_authorization_codes_scopes_check CHECK (((cardinality(scopes) >= 1) AND (cardinality(scopes) <= 10)))
);
COMMENT ON TABLE public.oidc_authorization_codes IS 'OIDC 一次性授权码';
COMMENT ON COLUMN public.oidc_authorization_codes.id IS '授权码记录唯一标识';
COMMENT ON COLUMN public.oidc_authorization_codes.organization_id IS '所属企业标识';
COMMENT ON COLUMN public.oidc_authorization_codes.client_id IS '接入应用内部标识';
COMMENT ON COLUMN public.oidc_authorization_codes.user_id IS '授权用户标识';
COMMENT ON COLUMN public.oidc_authorization_codes.code_hash IS '授权码的 SHA-256 哈希，不保存明文';
COMMENT ON COLUMN public.oidc_authorization_codes.redirect_uri IS '授权时使用的精确回调地址';
COMMENT ON COLUMN public.oidc_authorization_codes.scopes IS '本次授权允许的范围';
COMMENT ON COLUMN public.oidc_authorization_codes.nonce IS '客户端提供的 OIDC 重放保护随机值';
COMMENT ON COLUMN public.oidc_authorization_codes.code_challenge IS 'PKCE S256 挑战值';
COMMENT ON COLUMN public.oidc_authorization_codes.expires_at IS '授权码过期时间';
COMMENT ON COLUMN public.oidc_authorization_codes.consumed_at IS '授权码兑换时间，非空表示已使用';
COMMENT ON COLUMN public.oidc_authorization_codes.created_at IS '创建时间';
CREATE TABLE public.oidc_clients (
    id uuid NOT NULL,
    organization_id uuid NOT NULL,
    name character varying(120) NOT NULL,
    client_id character varying(120) NOT NULL,
    client_secret_hash bytea,
    client_secret_prefix character varying(40),
    redirect_uris text[] NOT NULL,
    allowed_scopes text[] DEFAULT ARRAY['openid'::text, 'profile'::text, 'model.invoke'::text] NOT NULL,
    status character varying(24) DEFAULT 'active'::character varying NOT NULL,
    created_by uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    application_type character varying(24) DEFAULT 'web'::character varying NOT NULL,
    token_endpoint_auth_method character varying(40) DEFAULT 'client_secret_basic'::character varying NOT NULL,
    client_secret_encrypted bytea,
    CONSTRAINT oidc_clients_allowed_scopes_check CHECK (((cardinality(allowed_scopes) >= 1) AND (cardinality(allowed_scopes) <= 10))),
    CONSTRAINT oidc_clients_application_type_check CHECK (((application_type)::text = ANY ((ARRAY['web'::character varying, 'desktop'::character varying])::text[]))),
    CONSTRAINT oidc_clients_redirect_uris_check CHECK (((cardinality(redirect_uris) >= 1) AND (cardinality(redirect_uris) <= 20))),
    CONSTRAINT oidc_clients_status_check CHECK (((status)::text = ANY ((ARRAY['active'::character varying, 'disabled'::character varying])::text[]))),
    CONSTRAINT oidc_clients_token_endpoint_auth_method_check CHECK (((token_endpoint_auth_method)::text = ANY ((ARRAY['client_secret_basic'::character varying, 'none'::character varying])::text[]))),
    CONSTRAINT oidc_clients_type_auth_method_check CHECK (((((application_type)::text = 'web'::text) AND ((token_endpoint_auth_method)::text = 'client_secret_basic'::text) AND (client_secret_hash IS NOT NULL) AND (client_secret_prefix IS NOT NULL)) OR (((application_type)::text = 'desktop'::text) AND ((token_endpoint_auth_method)::text = 'none'::text) AND (client_secret_hash IS NULL) AND (client_secret_prefix IS NULL))))
);
COMMENT ON TABLE public.oidc_clients IS '企业统一登录 OIDC 客户端';
COMMENT ON COLUMN public.oidc_clients.id IS 'OIDC 客户端唯一标识';
COMMENT ON COLUMN public.oidc_clients.organization_id IS '所属企业标识';
COMMENT ON COLUMN public.oidc_clients.name IS '接入应用名称';
COMMENT ON COLUMN public.oidc_clients.client_id IS '对外公开的 OAuth 客户端标识';
COMMENT ON COLUMN public.oidc_clients.client_secret_hash IS '服务端应用客户端密钥的 SHA-256 哈希，桌面应用为空';
COMMENT ON COLUMN public.oidc_clients.client_secret_prefix IS '用于识别服务端应用密钥的非敏感前缀，桌面应用为空';
COMMENT ON COLUMN public.oidc_clients.redirect_uris IS '允许的登录回调地址集合；桌面应用的 IP 回环地址在协议校验时允许使用随机端口';
COMMENT ON COLUMN public.oidc_clients.allowed_scopes IS '允许申请的 OIDC 和业务授权范围';
COMMENT ON COLUMN public.oidc_clients.status IS '客户端状态：active 启用、disabled 停用';
COMMENT ON COLUMN public.oidc_clients.created_by IS '创建客户端的企业用户标识';
COMMENT ON COLUMN public.oidc_clients.created_at IS '创建时间';
COMMENT ON COLUMN public.oidc_clients.updated_at IS '最后更新时间';
COMMENT ON COLUMN public.oidc_clients.application_type IS '应用类型：web 服务端应用、desktop 桌面应用';
COMMENT ON COLUMN public.oidc_clients.token_endpoint_auth_method IS '令牌端点客户端认证方式：client_secret_basic 或 none';
COMMENT ON COLUMN public.oidc_clients.client_secret_encrypted IS '使用服务端主密钥加密后的客户端密钥；桌面应用及历史未加密记录为空';
CREATE TABLE public.oidc_refresh_tokens (
    id uuid NOT NULL,
    organization_id uuid NOT NULL,
    client_id uuid NOT NULL,
    user_id uuid NOT NULL,
    token_hash bytea NOT NULL,
    scopes text[] NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    consumed_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT oidc_refresh_tokens_scopes_check CHECK (((cardinality(scopes) >= 1) AND (cardinality(scopes) <= 10)))
);
COMMENT ON TABLE public.oidc_refresh_tokens IS 'OIDC 可轮换刷新令牌';
COMMENT ON COLUMN public.oidc_refresh_tokens.id IS '刷新令牌记录唯一标识';
COMMENT ON COLUMN public.oidc_refresh_tokens.organization_id IS '所属企业标识';
COMMENT ON COLUMN public.oidc_refresh_tokens.client_id IS '接入应用内部标识';
COMMENT ON COLUMN public.oidc_refresh_tokens.user_id IS '授权用户标识';
COMMENT ON COLUMN public.oidc_refresh_tokens.token_hash IS '刷新令牌的 SHA-256 哈希，不保存明文';
COMMENT ON COLUMN public.oidc_refresh_tokens.scopes IS '刷新令牌允许续期的授权范围';
COMMENT ON COLUMN public.oidc_refresh_tokens.expires_at IS '刷新令牌过期时间';
COMMENT ON COLUMN public.oidc_refresh_tokens.consumed_at IS '刷新令牌轮换时间，非空表示已失效';
COMMENT ON COLUMN public.oidc_refresh_tokens.created_at IS '创建时间';
CREATE TABLE public.oidc_user_consents (
    id uuid NOT NULL,
    organization_id uuid NOT NULL,
    client_id uuid NOT NULL,
    user_id uuid NOT NULL,
    scopes text[] NOT NULL,
    granted_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    revoked_at timestamp with time zone,
    CONSTRAINT oidc_user_consents_scopes_check CHECK (((cardinality(scopes) >= 1) AND (cardinality(scopes) <= 10)))
);
COMMENT ON TABLE public.oidc_user_consents IS '企业用户对统一登录应用的授权记录';
COMMENT ON COLUMN public.oidc_user_consents.id IS '用户应用授权唯一标识';
COMMENT ON COLUMN public.oidc_user_consents.organization_id IS '所属企业标识';
COMMENT ON COLUMN public.oidc_user_consents.client_id IS '获得授权的统一登录应用内部标识';
COMMENT ON COLUMN public.oidc_user_consents.user_id IS '作出授权决定的企业用户标识';
COMMENT ON COLUMN public.oidc_user_consents.scopes IS '用户已经同意授予应用的权限范围';
COMMENT ON COLUMN public.oidc_user_consents.granted_at IS '本轮授权首次同意时间';
COMMENT ON COLUMN public.oidc_user_consents.updated_at IS '授权范围最近更新时间';
COMMENT ON COLUMN public.oidc_user_consents.revoked_at IS '用户撤销授权时间，未撤销时为空';
CREATE TABLE public.organizations (
    id uuid NOT NULL,
    code character varying(64) NOT NULL,
    name character varying(160) NOT NULL,
    status character varying(24) DEFAULT 'active'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT organizations_status_check CHECK (((status)::text = ANY ((ARRAY['active'::character varying, 'disabled'::character varying])::text[])))
);
COMMENT ON TABLE public.organizations IS '企业组织';
COMMENT ON COLUMN public.organizations.id IS '企业唯一标识';
COMMENT ON COLUMN public.organizations.code IS '企业编码，用于登录和系统内识别';
COMMENT ON COLUMN public.organizations.name IS '企业名称';
COMMENT ON COLUMN public.organizations.status IS '企业状态：active 启用，disabled 停用';
COMMENT ON COLUMN public.organizations.created_at IS '创建时间';
COMMENT ON COLUMN public.organizations.updated_at IS '最后更新时间';
CREATE TABLE public.permissions (
    id uuid NOT NULL,
    code character varying(120) NOT NULL,
    name character varying(120) NOT NULL,
    resource character varying(80) NOT NULL,
    action character varying(40) NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);
COMMENT ON TABLE public.permissions IS '系统权限定义';
COMMENT ON COLUMN public.permissions.id IS '权限唯一标识';
COMMENT ON COLUMN public.permissions.code IS '全局唯一的权限代码';
COMMENT ON COLUMN public.permissions.name IS '权限名称';
COMMENT ON COLUMN public.permissions.resource IS '权限所控制的资源类型';
COMMENT ON COLUMN public.permissions.action IS '允许执行的动作';
COMMENT ON COLUMN public.permissions.description IS '权限说明';
COMMENT ON COLUMN public.permissions.created_at IS '创建时间';
CREATE TABLE public.platform_admins (
    id uuid NOT NULL,
    username character varying(80) NOT NULL,
    display_name character varying(120) NOT NULL,
    password_hash text NOT NULL,
    status character varying(24) DEFAULT 'active'::character varying NOT NULL,
    last_login_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT platform_admins_status_check CHECK (((status)::text = ANY ((ARRAY['active'::character varying, 'disabled'::character varying])::text[])))
);
COMMENT ON TABLE public.platform_admins IS '平台管理员账号，与企业用户身份隔离';
COMMENT ON COLUMN public.platform_admins.id IS '平台管理员唯一标识';
COMMENT ON COLUMN public.platform_admins.username IS '平台管理员登录用户名，不区分大小写';
COMMENT ON COLUMN public.platform_admins.display_name IS '平台管理员显示名称';
COMMENT ON COLUMN public.platform_admins.password_hash IS 'Argon2id 密码哈希，不保存明文密码';
COMMENT ON COLUMN public.platform_admins.status IS '账号状态：active 正常，disabled 停用';
COMMENT ON COLUMN public.platform_admins.last_login_at IS '最后一次成功登录时间';
COMMENT ON COLUMN public.platform_admins.created_at IS '创建时间';
COMMENT ON COLUMN public.platform_admins.updated_at IS '最后更新时间';
CREATE TABLE public.platform_audit_events (
    id bigint NOT NULL,
    platform_admin_id uuid,
    request_id character varying(80) DEFAULT ''::character varying NOT NULL,
    action character varying(120) NOT NULL,
    resource_type character varying(80) NOT NULL,
    resource_id character varying(120) DEFAULT ''::character varying NOT NULL,
    result character varying(24) NOT NULL,
    source_ip inet,
    detail jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT platform_audit_events_result_check CHECK (((result)::text = ANY ((ARRAY['success'::character varying, 'denied'::character varying, 'failed'::character varying])::text[])))
);
COMMENT ON TABLE public.platform_audit_events IS '平台级审计事件';
COMMENT ON COLUMN public.platform_audit_events.id IS '平台审计事件自增标识';
COMMENT ON COLUMN public.platform_audit_events.platform_admin_id IS '操作平台管理员标识，可为空';
COMMENT ON COLUMN public.platform_audit_events.request_id IS '关联 HTTP 请求标识';
COMMENT ON COLUMN public.platform_audit_events.action IS '平台审计动作代码';
COMMENT ON COLUMN public.platform_audit_events.resource_type IS '被操作资源类型';
COMMENT ON COLUMN public.platform_audit_events.resource_id IS '被操作资源标识';
COMMENT ON COLUMN public.platform_audit_events.result IS '操作结果：success 成功，denied 拒绝，failed 失败';
COMMENT ON COLUMN public.platform_audit_events.source_ip IS '操作来源 IP 地址';
COMMENT ON COLUMN public.platform_audit_events.detail IS '事件扩展详情，JSON 格式';
COMMENT ON COLUMN public.platform_audit_events.created_at IS '事件发生时间';
CREATE SEQUENCE public.platform_audit_events_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;
ALTER SEQUENCE public.platform_audit_events_id_seq OWNED BY public.platform_audit_events.id;
CREATE TABLE public.platform_sessions (
    id uuid NOT NULL,
    platform_admin_id uuid NOT NULL,
    token_hash text NOT NULL,
    csrf_token_hash text NOT NULL,
    user_agent text DEFAULT ''::text NOT NULL,
    source_ip inet,
    expires_at timestamp with time zone NOT NULL,
    last_seen_at timestamp with time zone DEFAULT now() NOT NULL,
    revoked_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);
COMMENT ON TABLE public.platform_sessions IS '平台管理员登录会话';
COMMENT ON COLUMN public.platform_sessions.id IS '平台会话唯一标识';
COMMENT ON COLUMN public.platform_sessions.platform_admin_id IS '会话所属平台管理员标识';
COMMENT ON COLUMN public.platform_sessions.token_hash IS '平台会话令牌的 SHA-256 哈希';
COMMENT ON COLUMN public.platform_sessions.csrf_token_hash IS '平台会话 CSRF 令牌的 SHA-256 哈希';
COMMENT ON COLUMN public.platform_sessions.user_agent IS '登录客户端的 User-Agent';
COMMENT ON COLUMN public.platform_sessions.source_ip IS '登录来源 IP 地址';
COMMENT ON COLUMN public.platform_sessions.expires_at IS '会话过期时间';
COMMENT ON COLUMN public.platform_sessions.last_seen_at IS '会话最后访问时间';
COMMENT ON COLUMN public.platform_sessions.revoked_at IS '会话吊销时间，未吊销时为空';
COMMENT ON COLUMN public.platform_sessions.created_at IS '创建时间';
CREATE TABLE public.role_permissions (
    role_id uuid NOT NULL,
    permission_id uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);
COMMENT ON TABLE public.role_permissions IS '角色与权限关联关系';
COMMENT ON COLUMN public.role_permissions.role_id IS '角色标识';
COMMENT ON COLUMN public.role_permissions.permission_id IS '权限标识';
COMMENT ON COLUMN public.role_permissions.created_at IS '权限分配时间';
CREATE TABLE public.roles (
    id uuid NOT NULL,
    organization_id uuid NOT NULL,
    code character varying(80) NOT NULL,
    name character varying(120) NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    built_in boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);
COMMENT ON TABLE public.roles IS '企业角色';
COMMENT ON COLUMN public.roles.id IS '角色唯一标识';
COMMENT ON COLUMN public.roles.organization_id IS '所属企业标识';
COMMENT ON COLUMN public.roles.code IS '企业内唯一的角色代码';
COMMENT ON COLUMN public.roles.name IS '角色名称';
COMMENT ON COLUMN public.roles.description IS '角色说明';
COMMENT ON COLUMN public.roles.built_in IS '是否为系统内置角色';
COMMENT ON COLUMN public.roles.created_at IS '创建时间';
COMMENT ON COLUMN public.roles.updated_at IS '最后更新时间';
CREATE TABLE public.sessions (
    id uuid NOT NULL,
    user_id uuid NOT NULL,
    token_hash text NOT NULL,
    user_agent text DEFAULT ''::text NOT NULL,
    source_ip inet,
    expires_at timestamp with time zone NOT NULL,
    revoked_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    csrf_token_hash text DEFAULT ''::text NOT NULL,
    last_seen_at timestamp with time zone DEFAULT now() NOT NULL
);
COMMENT ON TABLE public.sessions IS '用户登录会话';
COMMENT ON COLUMN public.sessions.id IS '会话唯一标识';
COMMENT ON COLUMN public.sessions.user_id IS '会话所属用户标识';
COMMENT ON COLUMN public.sessions.token_hash IS '会话令牌的 SHA-256 哈希';
COMMENT ON COLUMN public.sessions.user_agent IS '登录客户端的 User-Agent';
COMMENT ON COLUMN public.sessions.source_ip IS '登录来源 IP 地址';
COMMENT ON COLUMN public.sessions.expires_at IS '会话过期时间';
COMMENT ON COLUMN public.sessions.revoked_at IS '会话吊销时间，未吊销时为空';
COMMENT ON COLUMN public.sessions.created_at IS '会话创建时间';
COMMENT ON COLUMN public.sessions.csrf_token_hash IS 'CSRF 令牌的 SHA-256 哈希';
COMMENT ON COLUMN public.sessions.last_seen_at IS '会话最后访问时间';
CREATE TABLE public.user_roles (
    user_id uuid NOT NULL,
    role_id uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);
COMMENT ON TABLE public.user_roles IS '用户与角色关联关系';
COMMENT ON COLUMN public.user_roles.user_id IS '用户标识';
COMMENT ON COLUMN public.user_roles.role_id IS '角色标识';
COMMENT ON COLUMN public.user_roles.created_at IS '角色分配时间';
CREATE TABLE public.users (
    id uuid NOT NULL,
    organization_id uuid NOT NULL,
    department_id uuid,
    username character varying(80) NOT NULL,
    email character varying(254),
    display_name character varying(120) NOT NULL,
    password_hash text NOT NULL,
    status character varying(24) DEFAULT 'active'::character varying NOT NULL,
    last_login_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    password_changed_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT users_status_check CHECK (((status)::text = ANY ((ARRAY['active'::character varying, 'locked'::character varying, 'disabled'::character varying])::text[])))
);
COMMENT ON TABLE public.users IS '企业用户账号';
COMMENT ON COLUMN public.users.id IS '用户唯一标识';
COMMENT ON COLUMN public.users.organization_id IS '所属企业标识';
COMMENT ON COLUMN public.users.department_id IS '所属部门标识，可为空';
COMMENT ON COLUMN public.users.username IS '企业内唯一的登录用户名，不区分大小写';
COMMENT ON COLUMN public.users.email IS '企业内唯一的电子邮箱，不区分大小写';
COMMENT ON COLUMN public.users.display_name IS '用户显示名称';
COMMENT ON COLUMN public.users.password_hash IS 'Argon2id 密码哈希，不保存明文密码';
COMMENT ON COLUMN public.users.status IS '账号状态：active 正常，locked 锁定，disabled 停用';
COMMENT ON COLUMN public.users.last_login_at IS '最后一次成功登录时间';
COMMENT ON COLUMN public.users.created_at IS '创建时间';
COMMENT ON COLUMN public.users.updated_at IS '最后更新时间';
COMMENT ON COLUMN public.users.password_changed_at IS '最后一次密码变更时间';
ALTER TABLE ONLY public.audit_events ALTER COLUMN id SET DEFAULT nextval('public.audit_events_id_seq'::regclass);
ALTER TABLE ONLY public.platform_audit_events ALTER COLUMN id SET DEFAULT nextval('public.platform_audit_events_id_seq'::regclass);
ALTER TABLE ONLY public.audit_events
    ADD CONSTRAINT audit_events_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.departments
    ADD CONSTRAINT departments_organization_id_code_key UNIQUE (organization_id, code);
ALTER TABLE ONLY public.departments
    ADD CONSTRAINT departments_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.model_catalog_entries
    ADD CONSTRAINT model_catalog_entries_org_id_key UNIQUE (organization_id, id);
ALTER TABLE ONLY public.model_catalog_entries
    ADD CONSTRAINT model_catalog_entries_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.model_department_user_monthly_usage
    ADD CONSTRAINT model_department_user_monthly_usage_pkey PRIMARY KEY (organization_id, subject_id, user_id, usage_month);
ALTER TABLE ONLY public.model_gateway_alert_settings
    ADD CONSTRAINT model_gateway_alert_settings_pkey PRIMARY KEY (organization_id);
ALTER TABLE ONLY public.model_gateway_alerts
    ADD CONSTRAINT model_gateway_alerts_organization_id_alert_month_dedupe_key_key UNIQUE (organization_id, alert_month, dedupe_key);
ALTER TABLE ONLY public.model_gateway_alerts
    ADD CONSTRAINT model_gateway_alerts_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.model_gateway_api_keys
    ADD CONSTRAINT model_gateway_api_keys_org_id_key UNIQUE (organization_id, id);
ALTER TABLE ONLY public.model_gateway_api_keys
    ADD CONSTRAINT model_gateway_api_keys_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.model_gateway_api_keys
    ADD CONSTRAINT model_gateway_api_keys_secret_hash_key UNIQUE (secret_hash);
ALTER TABLE ONLY public.model_gateway_calls
    ADD CONSTRAINT model_gateway_calls_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.model_gateway_monthly_usage
    ADD CONSTRAINT model_gateway_monthly_usage_pkey PRIMARY KEY (organization_id, api_key_id, usage_month);
ALTER TABLE ONLY public.model_provider_connections
    ADD CONSTRAINT model_provider_connections_org_id_key UNIQUE (organization_id, id);
ALTER TABLE ONLY public.model_provider_connections
    ADD CONSTRAINT model_provider_connections_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.model_provider_credentials
    ADD CONSTRAINT model_provider_credentials_pkey PRIMARY KEY (provider_connection_id);
ALTER TABLE ONLY public.model_subject_monthly_usage
    ADD CONSTRAINT model_subject_monthly_usage_pkey PRIMARY KEY (organization_id, subject_type, subject_id, usage_month);
ALTER TABLE ONLY public.model_subject_policies
    ADD CONSTRAINT model_subject_policies_pkey PRIMARY KEY (organization_id, subject_type, subject_id);
ALTER TABLE ONLY public.oidc_authorization_codes
    ADD CONSTRAINT oidc_authorization_codes_code_hash_key UNIQUE (code_hash);
ALTER TABLE ONLY public.oidc_authorization_codes
    ADD CONSTRAINT oidc_authorization_codes_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.oidc_clients
    ADD CONSTRAINT oidc_clients_client_id_key UNIQUE (client_id);
ALTER TABLE ONLY public.oidc_clients
    ADD CONSTRAINT oidc_clients_org_id_key UNIQUE (organization_id, id);
ALTER TABLE ONLY public.oidc_clients
    ADD CONSTRAINT oidc_clients_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.oidc_refresh_tokens
    ADD CONSTRAINT oidc_refresh_tokens_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.oidc_refresh_tokens
    ADD CONSTRAINT oidc_refresh_tokens_token_hash_key UNIQUE (token_hash);
ALTER TABLE ONLY public.oidc_user_consents
    ADD CONSTRAINT oidc_user_consents_org_client_user_key UNIQUE (organization_id, client_id, user_id);
ALTER TABLE ONLY public.oidc_user_consents
    ADD CONSTRAINT oidc_user_consents_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.organizations
    ADD CONSTRAINT organizations_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.permissions
    ADD CONSTRAINT permissions_code_key UNIQUE (code);
ALTER TABLE ONLY public.permissions
    ADD CONSTRAINT permissions_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.permissions
    ADD CONSTRAINT permissions_resource_action_key UNIQUE (resource, action);
ALTER TABLE ONLY public.platform_admins
    ADD CONSTRAINT platform_admins_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.platform_audit_events
    ADD CONSTRAINT platform_audit_events_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.platform_sessions
    ADD CONSTRAINT platform_sessions_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.platform_sessions
    ADD CONSTRAINT platform_sessions_token_hash_key UNIQUE (token_hash);
ALTER TABLE ONLY public.role_permissions
    ADD CONSTRAINT role_permissions_pkey PRIMARY KEY (role_id, permission_id);
ALTER TABLE ONLY public.roles
    ADD CONSTRAINT roles_organization_id_code_key UNIQUE (organization_id, code);
ALTER TABLE ONLY public.roles
    ADD CONSTRAINT roles_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.sessions
    ADD CONSTRAINT sessions_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.sessions
    ADD CONSTRAINT sessions_refresh_token_hash_key UNIQUE (token_hash);
ALTER TABLE ONLY public.user_roles
    ADD CONSTRAINT user_roles_pkey PRIMARY KEY (user_id, role_id);
ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_pkey PRIMARY KEY (id);
CREATE INDEX audit_events_actor_created_idx ON public.audit_events USING btree (actor_id, created_at DESC);
CREATE INDEX audit_events_organization_created_idx ON public.audit_events USING btree (organization_id, created_at DESC);
CREATE INDEX departments_organization_idx ON public.departments USING btree (organization_id);
CREATE UNIQUE INDEX model_catalog_entries_org_code_ci_key ON public.model_catalog_entries USING btree (organization_id, lower((code)::text));
CREATE INDEX model_catalog_entries_org_status_idx ON public.model_catalog_entries USING btree (organization_id, status);
CREATE INDEX model_catalog_entries_provider_idx ON public.model_catalog_entries USING btree (provider_connection_id);
CREATE UNIQUE INDEX model_catalog_entries_provider_upstream_key ON public.model_catalog_entries USING btree (provider_connection_id, upstream_model_id);
CREATE INDEX model_department_user_monthly_usage_lookup_idx ON public.model_department_user_monthly_usage USING btree (organization_id, user_id, usage_month);
CREATE INDEX model_gateway_alert_settings_enabled_idx ON public.model_gateway_alert_settings USING btree (organization_id) WHERE auto_refresh_enabled;
CREATE INDEX model_gateway_alerts_list_idx ON public.model_gateway_alerts USING btree (organization_id, alert_month, resolved_at, severity, first_detected_at DESC);
CREATE INDEX model_gateway_api_keys_org_created_idx ON public.model_gateway_api_keys USING btree (organization_id, created_at DESC);
CREATE INDEX model_gateway_calls_api_key_idx ON public.model_gateway_calls USING btree (api_key_id, created_at DESC);
CREATE INDEX model_gateway_calls_model_idx ON public.model_gateway_calls USING btree (model_catalog_entry_id, created_at DESC);
CREATE INDEX model_gateway_calls_org_created_idx ON public.model_gateway_calls USING btree (organization_id, created_at DESC);
CREATE INDEX model_gateway_calls_org_month_department_idx ON public.model_gateway_calls USING btree (organization_id, created_at, department_id);
CREATE INDEX model_gateway_calls_user_idx ON public.model_gateway_calls USING btree (user_id, created_at DESC) WHERE (user_id IS NOT NULL);
CREATE INDEX model_gateway_monthly_usage_org_month_idx ON public.model_gateway_monthly_usage USING btree (organization_id, usage_month DESC);
CREATE UNIQUE INDEX model_provider_connections_org_code_ci_key ON public.model_provider_connections USING btree (organization_id, lower((code)::text));
CREATE INDEX model_provider_connections_org_status_idx ON public.model_provider_connections USING btree (organization_id, status);
CREATE INDEX oidc_authorization_codes_expiry_idx ON public.oidc_authorization_codes USING btree (expires_at) WHERE (consumed_at IS NULL);
CREATE INDEX oidc_clients_org_created_idx ON public.oidc_clients USING btree (organization_id, created_at DESC);
CREATE INDEX oidc_refresh_tokens_expiry_idx ON public.oidc_refresh_tokens USING btree (expires_at) WHERE (consumed_at IS NULL);
CREATE INDEX oidc_user_consents_user_active_idx ON public.oidc_user_consents USING btree (organization_id, user_id, updated_at DESC) WHERE (revoked_at IS NULL);
CREATE UNIQUE INDEX organizations_code_ci_key ON public.organizations USING btree (lower((code)::text));
CREATE UNIQUE INDEX platform_admins_username_ci_key ON public.platform_admins USING btree (lower((username)::text));
CREATE INDEX platform_audit_events_actor_created_idx ON public.platform_audit_events USING btree (platform_admin_id, created_at DESC);
CREATE INDEX platform_audit_events_created_idx ON public.platform_audit_events USING btree (created_at DESC);
CREATE INDEX platform_sessions_admin_idx ON public.platform_sessions USING btree (platform_admin_id);
CREATE INDEX platform_sessions_expires_idx ON public.platform_sessions USING btree (expires_at) WHERE (revoked_at IS NULL);
CREATE INDEX sessions_expires_idx ON public.sessions USING btree (expires_at) WHERE (revoked_at IS NULL);
CREATE INDEX sessions_token_hash_active_idx ON public.sessions USING btree (token_hash, expires_at) WHERE (revoked_at IS NULL);
CREATE INDEX sessions_user_idx ON public.sessions USING btree (user_id);
CREATE INDEX users_department_idx ON public.users USING btree (department_id);
CREATE UNIQUE INDEX users_organization_email_ci_key ON public.users USING btree (organization_id, lower((email)::text)) WHERE (email IS NOT NULL);
CREATE INDEX users_organization_idx ON public.users USING btree (organization_id);
CREATE UNIQUE INDEX users_organization_username_ci_key ON public.users USING btree (organization_id, lower((username)::text));
ALTER TABLE ONLY public.audit_events
    ADD CONSTRAINT audit_events_actor_id_fkey FOREIGN KEY (actor_id) REFERENCES public.users(id);
ALTER TABLE ONLY public.audit_events
    ADD CONSTRAINT audit_events_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id);
ALTER TABLE ONLY public.departments
    ADD CONSTRAINT departments_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id);
ALTER TABLE ONLY public.departments
    ADD CONSTRAINT departments_parent_id_fkey FOREIGN KEY (parent_id) REFERENCES public.departments(id);
ALTER TABLE ONLY public.model_catalog_entries
    ADD CONSTRAINT model_catalog_entries_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id);
ALTER TABLE ONLY public.model_catalog_entries
    ADD CONSTRAINT model_catalog_entries_provider_org_fk FOREIGN KEY (organization_id, provider_connection_id) REFERENCES public.model_provider_connections(organization_id, id);
ALTER TABLE ONLY public.model_department_user_monthly_usage
    ADD CONSTRAINT model_department_user_monthly_usage_policy_fk FOREIGN KEY (organization_id, subject_type, subject_id) REFERENCES public.model_subject_policies(organization_id, subject_type, subject_id) ON DELETE CASCADE;
ALTER TABLE ONLY public.model_department_user_monthly_usage
    ADD CONSTRAINT model_department_user_monthly_usage_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.model_gateway_alert_settings
    ADD CONSTRAINT model_gateway_alert_settings_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.model_gateway_alert_settings
    ADD CONSTRAINT model_gateway_alert_settings_updated_by_fkey FOREIGN KEY (updated_by) REFERENCES public.users(id) ON DELETE SET NULL;
ALTER TABLE ONLY public.model_gateway_alerts
    ADD CONSTRAINT model_gateway_alerts_acknowledged_by_fkey FOREIGN KEY (acknowledged_by) REFERENCES public.users(id) ON DELETE SET NULL;
ALTER TABLE ONLY public.model_gateway_alerts
    ADD CONSTRAINT model_gateway_alerts_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id);
ALTER TABLE ONLY public.model_gateway_api_keys
    ADD CONSTRAINT model_gateway_api_keys_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id);
ALTER TABLE ONLY public.model_gateway_api_keys
    ADD CONSTRAINT model_gateway_api_keys_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id);
ALTER TABLE ONLY public.model_gateway_calls
    ADD CONSTRAINT model_gateway_calls_api_key_org_fk FOREIGN KEY (organization_id, api_key_id) REFERENCES public.model_gateway_api_keys(organization_id, id);
ALTER TABLE ONLY public.model_gateway_calls
    ADD CONSTRAINT model_gateway_calls_model_org_fk FOREIGN KEY (organization_id, model_catalog_entry_id) REFERENCES public.model_catalog_entries(organization_id, id);
ALTER TABLE ONLY public.model_gateway_calls
    ADD CONSTRAINT model_gateway_calls_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id);
ALTER TABLE ONLY public.model_gateway_calls
    ADD CONSTRAINT model_gateway_calls_provider_org_fk FOREIGN KEY (organization_id, provider_connection_id) REFERENCES public.model_provider_connections(organization_id, id);
ALTER TABLE ONLY public.model_gateway_calls
    ADD CONSTRAINT model_gateway_calls_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id);
ALTER TABLE ONLY public.model_gateway_monthly_usage
    ADD CONSTRAINT model_gateway_monthly_usage_key_org_fk FOREIGN KEY (organization_id, api_key_id) REFERENCES public.model_gateway_api_keys(organization_id, id) ON DELETE CASCADE;
ALTER TABLE ONLY public.model_provider_connections
    ADD CONSTRAINT model_provider_connections_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id);
ALTER TABLE ONLY public.model_provider_credentials
    ADD CONSTRAINT model_provider_credentials_provider_connection_id_fkey FOREIGN KEY (provider_connection_id) REFERENCES public.model_provider_connections(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.model_subject_monthly_usage
    ADD CONSTRAINT model_subject_monthly_usage_policy_fk FOREIGN KEY (organization_id, subject_type, subject_id) REFERENCES public.model_subject_policies(organization_id, subject_type, subject_id) ON DELETE CASCADE;
ALTER TABLE ONLY public.model_subject_policies
    ADD CONSTRAINT model_subject_policies_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id);
ALTER TABLE ONLY public.oidc_authorization_codes
    ADD CONSTRAINT oidc_authorization_codes_client_fk FOREIGN KEY (organization_id, client_id) REFERENCES public.oidc_clients(organization_id, id);
ALTER TABLE ONLY public.oidc_authorization_codes
    ADD CONSTRAINT oidc_authorization_codes_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id);
ALTER TABLE ONLY public.oidc_authorization_codes
    ADD CONSTRAINT oidc_authorization_codes_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id);
ALTER TABLE ONLY public.oidc_clients
    ADD CONSTRAINT oidc_clients_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id);
ALTER TABLE ONLY public.oidc_clients
    ADD CONSTRAINT oidc_clients_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id);
ALTER TABLE ONLY public.oidc_refresh_tokens
    ADD CONSTRAINT oidc_refresh_tokens_client_fk FOREIGN KEY (organization_id, client_id) REFERENCES public.oidc_clients(organization_id, id);
ALTER TABLE ONLY public.oidc_refresh_tokens
    ADD CONSTRAINT oidc_refresh_tokens_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id);
ALTER TABLE ONLY public.oidc_refresh_tokens
    ADD CONSTRAINT oidc_refresh_tokens_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id);
ALTER TABLE ONLY public.oidc_user_consents
    ADD CONSTRAINT oidc_user_consents_client_fk FOREIGN KEY (organization_id, client_id) REFERENCES public.oidc_clients(organization_id, id);
ALTER TABLE ONLY public.oidc_user_consents
    ADD CONSTRAINT oidc_user_consents_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id);
ALTER TABLE ONLY public.oidc_user_consents
    ADD CONSTRAINT oidc_user_consents_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id);
ALTER TABLE ONLY public.platform_audit_events
    ADD CONSTRAINT platform_audit_events_platform_admin_id_fkey FOREIGN KEY (platform_admin_id) REFERENCES public.platform_admins(id);
ALTER TABLE ONLY public.platform_sessions
    ADD CONSTRAINT platform_sessions_platform_admin_id_fkey FOREIGN KEY (platform_admin_id) REFERENCES public.platform_admins(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.role_permissions
    ADD CONSTRAINT role_permissions_permission_id_fkey FOREIGN KEY (permission_id) REFERENCES public.permissions(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.role_permissions
    ADD CONSTRAINT role_permissions_role_id_fkey FOREIGN KEY (role_id) REFERENCES public.roles(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.roles
    ADD CONSTRAINT roles_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id);
ALTER TABLE ONLY public.sessions
    ADD CONSTRAINT sessions_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.user_roles
    ADD CONSTRAINT user_roles_role_id_fkey FOREIGN KEY (role_id) REFERENCES public.roles(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.user_roles
    ADD CONSTRAINT user_roles_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_department_id_fkey FOREIGN KEY (department_id) REFERENCES public.departments(id);
ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id);

-- 内置权限定义。角色授权由初始化流程按企业创建。
INSERT INTO public.permissions (id, code, name, resource, action, description) VALUES ('a1300000-0000-4000-8000-000000000001', 'model.provider.read', '查看模型供应商', 'model_provider', 'read', '查看当前企业的模型供应商连接');
INSERT INTO public.permissions (id, code, name, resource, action, description) VALUES ('a1300000-0000-4000-8000-000000000002', 'model.provider.write', '管理模型供应商', 'model_provider', 'write', '创建、修改和检测当前企业的模型供应商连接');
INSERT INTO public.permissions (id, code, name, resource, action, description) VALUES ('a1300000-0000-4000-8000-000000000003', 'model.catalog.read', '查看模型目录', 'model_catalog', 'read', '查看当前企业的模型目录');
INSERT INTO public.permissions (id, code, name, resource, action, description) VALUES ('a1300000-0000-4000-8000-000000000004', 'model.catalog.write', '管理模型目录', 'model_catalog', 'write', '创建和修改当前企业的模型目录');
INSERT INTO public.permissions (id, code, name, resource, action, description) VALUES ('a1400000-0000-4000-8000-000000000001', 'model.gateway.read', '查看模型网关', 'model_gateway', 'read', '查看当前企业的网关调用密钥和调用记录');
INSERT INTO public.permissions (id, code, name, resource, action, description) VALUES ('a1400000-0000-4000-8000-000000000002', 'model.gateway.write', '管理模型网关', 'model_gateway', 'write', '创建、修改和吊销当前企业的网关调用密钥');
INSERT INTO public.permissions (id, code, name, resource, action, description) VALUES ('a1500000-0000-4000-8000-000000000001', 'oidc.client.read', '查看应用接入', 'oidc_client', 'read', '查看当前企业的统一登录应用');
INSERT INTO public.permissions (id, code, name, resource, action, description) VALUES ('a1500000-0000-4000-8000-000000000002', 'oidc.client.write', '管理应用接入', 'oidc_client', 'write', '创建、修改和停用当前企业的统一登录应用');
INSERT INTO public.permissions (id, code, name, resource, action, description) VALUES ('a1600000-0000-4000-8000-000000000001', 'model.policy.read', '查看模型策略', 'model_policy', 'read', '查看部门和用户的模型访问与额度策略');
INSERT INTO public.permissions (id, code, name, resource, action, description) VALUES ('a1600000-0000-4000-8000-000000000002', 'model.policy.write', '管理模型策略', 'model_policy', 'write', '设置或删除部门和用户的模型访问与额度策略');

