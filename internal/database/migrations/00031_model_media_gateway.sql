-- +goose Up
ALTER TABLE model_gateway_calls
    ADD COLUMN model_type varchar(16) NOT NULL DEFAULT 'chat',
    ADD CONSTRAINT model_gateway_calls_model_type_check CHECK (model_type IN ('chat', 'image', 'video'));
COMMENT ON COLUMN model_gateway_calls.model_type IS '本次调用的模型类型：对话、图片或视频';

CREATE TABLE model_gateway_video_tasks (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES organizations(id),
    auth_type varchar(16) NOT NULL CHECK (auth_type IN ('api_key', 'user')),
    api_key_id uuid,
    user_id uuid REFERENCES users(id),
    model_catalog_entry_id uuid NOT NULL,
    provider_connection_id uuid NOT NULL,
    upstream_task_id varchar(200) NOT NULL,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    CONSTRAINT model_gateway_video_tasks_caller_check CHECK (
        (auth_type = 'api_key' AND api_key_id IS NOT NULL AND user_id IS NULL) OR
        (auth_type = 'user' AND api_key_id IS NULL AND user_id IS NOT NULL)
    ),
    CONSTRAINT model_gateway_video_tasks_key_fk FOREIGN KEY (organization_id, api_key_id)
        REFERENCES model_gateway_api_keys(organization_id, id),
    CONSTRAINT model_gateway_video_tasks_model_fk FOREIGN KEY (organization_id, model_catalog_entry_id)
        REFERENCES model_catalog_entries(organization_id, id),
    CONSTRAINT model_gateway_video_tasks_provider_fk FOREIGN KEY (organization_id, provider_connection_id)
        REFERENCES model_provider_connections(organization_id, id)
);
CREATE INDEX model_gateway_video_tasks_owner_idx
    ON model_gateway_video_tasks (organization_id, auth_type, api_key_id, user_id, created_at DESC);
COMMENT ON TABLE model_gateway_video_tasks IS '视频生成网关任务与供应商异步任务的归属映射';
COMMENT ON COLUMN model_gateway_video_tasks.id IS '对调用方公开的网关视频任务标识';
COMMENT ON COLUMN model_gateway_video_tasks.organization_id IS '所属企业标识';
COMMENT ON COLUMN model_gateway_video_tasks.auth_type IS '任务发起身份类型：调用密钥或用户';
COMMENT ON COLUMN model_gateway_video_tasks.api_key_id IS '发起任务的网关调用密钥标识';
COMMENT ON COLUMN model_gateway_video_tasks.user_id IS '发起任务的用户标识';
COMMENT ON COLUMN model_gateway_video_tasks.model_catalog_entry_id IS '生成视频使用的模型目录项标识';
COMMENT ON COLUMN model_gateway_video_tasks.provider_connection_id IS '生成视频使用的供应商连接标识';
COMMENT ON COLUMN model_gateway_video_tasks.upstream_task_id IS '供应商返回的异步任务标识';
COMMENT ON COLUMN model_gateway_video_tasks.created_at IS '任务提交时间';

-- +goose Down
DROP TABLE model_gateway_video_tasks;
ALTER TABLE model_gateway_calls DROP CONSTRAINT model_gateway_calls_model_type_check;
ALTER TABLE model_gateway_calls DROP COLUMN model_type;
