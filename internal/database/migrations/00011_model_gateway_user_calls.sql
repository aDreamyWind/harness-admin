-- +goose Up
ALTER TABLE model_gateway_calls
    ALTER COLUMN api_key_id DROP NOT NULL,
    ADD COLUMN auth_type VARCHAR(24) NOT NULL DEFAULT 'api_key',
    ADD COLUMN user_id UUID REFERENCES users(id),
    ADD CONSTRAINT model_gateway_calls_auth_type_check CHECK (auth_type IN ('api_key', 'user')),
    ADD CONSTRAINT model_gateway_calls_caller_check CHECK (
        (auth_type = 'api_key' AND api_key_id IS NOT NULL AND user_id IS NULL)
        OR (auth_type = 'user' AND api_key_id IS NULL AND user_id IS NOT NULL)
    );

CREATE INDEX model_gateway_calls_user_idx
    ON model_gateway_calls (user_id, created_at DESC)
    WHERE user_id IS NOT NULL;

COMMENT ON COLUMN model_gateway_calls.auth_type IS '调用身份类型：api_key 调用密钥、user OIDC 用户';
COMMENT ON COLUMN model_gateway_calls.user_id IS '使用 OIDC 访问令牌发起调用的用户标识';
COMMENT ON COLUMN model_gateway_calls.api_key_id IS '发起调用的网关密钥标识，OIDC 用户调用时为空';

-- +goose Down
DELETE FROM model_gateway_calls WHERE auth_type = 'user';
DROP INDEX IF EXISTS model_gateway_calls_user_idx;
ALTER TABLE model_gateway_calls
    DROP CONSTRAINT IF EXISTS model_gateway_calls_caller_check,
    DROP CONSTRAINT IF EXISTS model_gateway_calls_auth_type_check,
    DROP COLUMN IF EXISTS user_id,
    DROP COLUMN IF EXISTS auth_type,
    ALTER COLUMN api_key_id SET NOT NULL;
