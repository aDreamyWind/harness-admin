-- +goose Up
CREATE TABLE oidc_authorization_codes (
    id UUID PRIMARY KEY,
    organization_id UUID NOT NULL REFERENCES organizations(id),
    client_id UUID NOT NULL,
    user_id UUID NOT NULL REFERENCES users(id),
    code_hash BYTEA NOT NULL UNIQUE,
    redirect_uri TEXT NOT NULL,
    scopes TEXT[] NOT NULL,
    nonce VARCHAR(255),
    code_challenge VARCHAR(128) NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT oidc_authorization_codes_client_fk
        FOREIGN KEY (organization_id, client_id) REFERENCES oidc_clients(organization_id, id),
    CONSTRAINT oidc_authorization_codes_scopes_check CHECK (CARDINALITY(scopes) BETWEEN 1 AND 10)
);

CREATE INDEX oidc_authorization_codes_expiry_idx
    ON oidc_authorization_codes (expires_at)
    WHERE consumed_at IS NULL;

COMMENT ON TABLE oidc_authorization_codes IS 'OIDC 一次性授权码';
COMMENT ON COLUMN oidc_authorization_codes.id IS '授权码记录唯一标识';
COMMENT ON COLUMN oidc_authorization_codes.organization_id IS '所属企业标识';
COMMENT ON COLUMN oidc_authorization_codes.client_id IS '接入应用内部标识';
COMMENT ON COLUMN oidc_authorization_codes.user_id IS '授权用户标识';
COMMENT ON COLUMN oidc_authorization_codes.code_hash IS '授权码的 SHA-256 哈希，不保存明文';
COMMENT ON COLUMN oidc_authorization_codes.redirect_uri IS '授权时使用的精确回调地址';
COMMENT ON COLUMN oidc_authorization_codes.scopes IS '本次授权允许的范围';
COMMENT ON COLUMN oidc_authorization_codes.nonce IS '客户端提供的 OIDC 重放保护随机值';
COMMENT ON COLUMN oidc_authorization_codes.code_challenge IS 'PKCE S256 挑战值';
COMMENT ON COLUMN oidc_authorization_codes.expires_at IS '授权码过期时间';
COMMENT ON COLUMN oidc_authorization_codes.consumed_at IS '授权码兑换时间，非空表示已使用';
COMMENT ON COLUMN oidc_authorization_codes.created_at IS '创建时间';

CREATE TABLE oidc_refresh_tokens (
    id UUID PRIMARY KEY,
    organization_id UUID NOT NULL REFERENCES organizations(id),
    client_id UUID NOT NULL,
    user_id UUID NOT NULL REFERENCES users(id),
    token_hash BYTEA NOT NULL UNIQUE,
    scopes TEXT[] NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT oidc_refresh_tokens_client_fk
        FOREIGN KEY (organization_id, client_id) REFERENCES oidc_clients(organization_id, id),
    CONSTRAINT oidc_refresh_tokens_scopes_check CHECK (CARDINALITY(scopes) BETWEEN 1 AND 10)
);

CREATE INDEX oidc_refresh_tokens_expiry_idx
    ON oidc_refresh_tokens (expires_at)
    WHERE consumed_at IS NULL;

COMMENT ON TABLE oidc_refresh_tokens IS 'OIDC 可轮换刷新令牌';
COMMENT ON COLUMN oidc_refresh_tokens.id IS '刷新令牌记录唯一标识';
COMMENT ON COLUMN oidc_refresh_tokens.organization_id IS '所属企业标识';
COMMENT ON COLUMN oidc_refresh_tokens.client_id IS '接入应用内部标识';
COMMENT ON COLUMN oidc_refresh_tokens.user_id IS '授权用户标识';
COMMENT ON COLUMN oidc_refresh_tokens.token_hash IS '刷新令牌的 SHA-256 哈希，不保存明文';
COMMENT ON COLUMN oidc_refresh_tokens.scopes IS '刷新令牌允许续期的授权范围';
COMMENT ON COLUMN oidc_refresh_tokens.expires_at IS '刷新令牌过期时间';
COMMENT ON COLUMN oidc_refresh_tokens.consumed_at IS '刷新令牌轮换时间，非空表示已失效';
COMMENT ON COLUMN oidc_refresh_tokens.created_at IS '创建时间';

-- +goose Down
DROP TABLE IF EXISTS oidc_refresh_tokens;
DROP TABLE IF EXISTS oidc_authorization_codes;
