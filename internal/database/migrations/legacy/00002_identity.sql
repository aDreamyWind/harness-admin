-- +goose Up
ALTER TABLE sessions RENAME COLUMN refresh_token_hash TO token_hash;
ALTER TABLE sessions ADD COLUMN csrf_token_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
ALTER TABLE users ADD COLUMN password_changed_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

COMMENT ON COLUMN sessions.token_hash IS '会话令牌的 SHA-256 哈希';
COMMENT ON COLUMN sessions.csrf_token_hash IS 'CSRF 令牌的 SHA-256 哈希';
COMMENT ON COLUMN sessions.last_seen_at IS '会话最后访问时间';
COMMENT ON COLUMN users.password_changed_at IS '最后一次密码变更时间';

ALTER TABLE organizations DROP CONSTRAINT organizations_code_key;
CREATE UNIQUE INDEX organizations_code_ci_key ON organizations (LOWER(code));

ALTER TABLE users DROP CONSTRAINT users_organization_id_username_key;
CREATE UNIQUE INDEX users_organization_username_ci_key ON users (organization_id, LOWER(username));

ALTER TABLE users DROP CONSTRAINT users_organization_id_email_key;
CREATE UNIQUE INDEX users_organization_email_ci_key
    ON users (organization_id, LOWER(email))
    WHERE email IS NOT NULL;

CREATE INDEX sessions_token_hash_active_idx
    ON sessions (token_hash, expires_at)
    WHERE revoked_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS sessions_token_hash_active_idx;
DROP INDEX IF EXISTS users_organization_email_ci_key;
DROP INDEX IF EXISTS users_organization_username_ci_key;
DROP INDEX IF EXISTS organizations_code_ci_key;

ALTER TABLE users ADD CONSTRAINT users_organization_id_username_key UNIQUE (organization_id, username);
ALTER TABLE users ADD CONSTRAINT users_organization_id_email_key UNIQUE (organization_id, email);
ALTER TABLE organizations ADD CONSTRAINT organizations_code_key UNIQUE (code);

ALTER TABLE users DROP COLUMN password_changed_at;
ALTER TABLE sessions DROP COLUMN last_seen_at;
ALTER TABLE sessions DROP COLUMN csrf_token_hash;
ALTER TABLE sessions RENAME COLUMN token_hash TO refresh_token_hash;
