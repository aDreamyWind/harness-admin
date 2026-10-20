-- +goose Up
ALTER TABLE oidc_clients
    ADD COLUMN application_type VARCHAR(24) NOT NULL DEFAULT 'web',
    ADD COLUMN token_endpoint_auth_method VARCHAR(40) NOT NULL DEFAULT 'client_secret_basic',
    ALTER COLUMN client_secret_hash DROP NOT NULL,
    ALTER COLUMN client_secret_prefix DROP NOT NULL,
    ADD CONSTRAINT oidc_clients_application_type_check
        CHECK (application_type IN ('web', 'desktop')),
    ADD CONSTRAINT oidc_clients_token_endpoint_auth_method_check
        CHECK (token_endpoint_auth_method IN ('client_secret_basic', 'none')),
    ADD CONSTRAINT oidc_clients_type_auth_method_check
        CHECK (
            (application_type = 'web' AND token_endpoint_auth_method = 'client_secret_basic'
                AND client_secret_hash IS NOT NULL AND client_secret_prefix IS NOT NULL)
            OR
            (application_type = 'desktop' AND token_endpoint_auth_method = 'none'
                AND client_secret_hash IS NULL AND client_secret_prefix IS NULL)
        );

COMMENT ON COLUMN oidc_clients.application_type IS '应用类型：web 服务端应用、desktop 桌面应用';
COMMENT ON COLUMN oidc_clients.token_endpoint_auth_method IS '令牌端点客户端认证方式：client_secret_basic 或 none';
COMMENT ON COLUMN oidc_clients.client_secret_hash IS '服务端应用客户端密钥的 SHA-256 哈希，桌面应用为空';
COMMENT ON COLUMN oidc_clients.client_secret_prefix IS '用于识别服务端应用密钥的非敏感前缀，桌面应用为空';
COMMENT ON COLUMN oidc_clients.redirect_uris IS '允许的登录回调地址集合；桌面应用的 IP 回环地址在协议校验时允许使用随机端口';

-- +goose Down
DELETE FROM oidc_refresh_tokens
WHERE client_id IN (SELECT id FROM oidc_clients WHERE application_type = 'desktop');
DELETE FROM oidc_authorization_codes
WHERE client_id IN (SELECT id FROM oidc_clients WHERE application_type = 'desktop');
DELETE FROM oidc_clients WHERE application_type = 'desktop';

ALTER TABLE oidc_clients
    DROP CONSTRAINT oidc_clients_type_auth_method_check,
    DROP CONSTRAINT oidc_clients_token_endpoint_auth_method_check,
    DROP CONSTRAINT oidc_clients_application_type_check,
    ALTER COLUMN client_secret_hash SET NOT NULL,
    ALTER COLUMN client_secret_prefix SET NOT NULL,
    DROP COLUMN token_endpoint_auth_method,
    DROP COLUMN application_type;

COMMENT ON COLUMN oidc_clients.client_secret_hash IS '客户端密钥的 SHA-256 哈希，不保存明文';
COMMENT ON COLUMN oidc_clients.client_secret_prefix IS '用于识别客户端密钥的非敏感前缀';
COMMENT ON COLUMN oidc_clients.redirect_uris IS '允许的精确登录回调地址集合';
