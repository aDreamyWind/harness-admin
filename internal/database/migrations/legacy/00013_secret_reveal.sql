-- +goose Up
ALTER TABLE oidc_clients
    ADD COLUMN client_secret_encrypted BYTEA;

ALTER TABLE model_gateway_api_keys
    ADD COLUMN encrypted_secret BYTEA;

COMMENT ON COLUMN oidc_clients.client_secret_encrypted IS '使用服务端主密钥加密后的客户端密钥；桌面应用及历史未加密记录为空';
COMMENT ON COLUMN model_gateway_api_keys.encrypted_secret IS '使用服务端主密钥加密后的模型网关调用密钥；历史未加密记录为空';

-- +goose Down
ALTER TABLE model_gateway_api_keys
    DROP COLUMN encrypted_secret;

ALTER TABLE oidc_clients
    DROP COLUMN client_secret_encrypted;
