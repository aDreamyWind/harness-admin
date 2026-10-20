-- +goose Up
CREATE TABLE oidc_user_consents (
    id UUID PRIMARY KEY,
    organization_id UUID NOT NULL REFERENCES organizations(id),
    client_id UUID NOT NULL,
    user_id UUID NOT NULL REFERENCES users(id),
    scopes TEXT[] NOT NULL,
    granted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at TIMESTAMPTZ,
    CONSTRAINT oidc_user_consents_client_fk
        FOREIGN KEY (organization_id, client_id) REFERENCES oidc_clients(organization_id, id),
    CONSTRAINT oidc_user_consents_org_client_user_key
        UNIQUE (organization_id, client_id, user_id),
    CONSTRAINT oidc_user_consents_scopes_check
        CHECK (CARDINALITY(scopes) BETWEEN 1 AND 10)
);

CREATE INDEX oidc_user_consents_user_active_idx
    ON oidc_user_consents (organization_id, user_id, updated_at DESC)
    WHERE revoked_at IS NULL;

COMMENT ON TABLE oidc_user_consents IS '企业用户对统一登录应用的授权记录';
COMMENT ON COLUMN oidc_user_consents.id IS '用户应用授权唯一标识';
COMMENT ON COLUMN oidc_user_consents.organization_id IS '所属企业标识';
COMMENT ON COLUMN oidc_user_consents.client_id IS '获得授权的统一登录应用内部标识';
COMMENT ON COLUMN oidc_user_consents.user_id IS '作出授权决定的企业用户标识';
COMMENT ON COLUMN oidc_user_consents.scopes IS '用户已经同意授予应用的权限范围';
COMMENT ON COLUMN oidc_user_consents.granted_at IS '本轮授权首次同意时间';
COMMENT ON COLUMN oidc_user_consents.updated_at IS '授权范围最近更新时间';
COMMENT ON COLUMN oidc_user_consents.revoked_at IS '用户撤销授权时间，未撤销时为空';

-- +goose Down
DROP TABLE IF EXISTS oidc_user_consents;
