-- +goose Up
CREATE TABLE organizations (
    id UUID PRIMARY KEY,
    code VARCHAR(64) NOT NULL UNIQUE,
    name VARCHAR(160) NOT NULL,
    status VARCHAR(24) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT organizations_status_check CHECK (status IN ('active', 'disabled'))
);

CREATE TABLE departments (
    id UUID PRIMARY KEY,
    organization_id UUID NOT NULL REFERENCES organizations(id),
    parent_id UUID REFERENCES departments(id),
    code VARCHAR(64) NOT NULL,
    name VARCHAR(160) NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    status VARCHAR(24) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (organization_id, code),
    CONSTRAINT departments_status_check CHECK (status IN ('active', 'disabled'))
);

CREATE TABLE users (
    id UUID PRIMARY KEY,
    organization_id UUID NOT NULL REFERENCES organizations(id),
    department_id UUID REFERENCES departments(id),
    username VARCHAR(80) NOT NULL,
    email VARCHAR(254),
    display_name VARCHAR(120) NOT NULL,
    password_hash TEXT NOT NULL,
    status VARCHAR(24) NOT NULL DEFAULT 'active',
    last_login_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (organization_id, username),
    UNIQUE (organization_id, email),
    CONSTRAINT users_status_check CHECK (status IN ('active', 'locked', 'disabled'))
);

CREATE TABLE roles (
    id UUID PRIMARY KEY,
    organization_id UUID NOT NULL REFERENCES organizations(id),
    code VARCHAR(80) NOT NULL,
    name VARCHAR(120) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    built_in BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (organization_id, code)
);

CREATE TABLE permissions (
    id UUID PRIMARY KEY,
    code VARCHAR(120) NOT NULL UNIQUE,
    name VARCHAR(120) NOT NULL,
    resource VARCHAR(80) NOT NULL,
    action VARCHAR(40) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (resource, action)
);

CREATE TABLE user_roles (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, role_id)
);

CREATE TABLE role_permissions (
    role_id UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    permission_id UUID NOT NULL REFERENCES permissions(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (role_id, permission_id)
);

CREATE TABLE sessions (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    refresh_token_hash TEXT NOT NULL UNIQUE,
    user_agent TEXT NOT NULL DEFAULT '',
    source_ip INET,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE audit_events (
    id BIGSERIAL PRIMARY KEY,
    organization_id UUID REFERENCES organizations(id),
    actor_id UUID REFERENCES users(id),
    request_id VARCHAR(80) NOT NULL DEFAULT '',
    action VARCHAR(120) NOT NULL,
    resource_type VARCHAR(80) NOT NULL,
    resource_id VARCHAR(120) NOT NULL DEFAULT '',
    result VARCHAR(24) NOT NULL,
    source_ip INET,
    detail JSONB NOT NULL DEFAULT '{}'::JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT audit_events_result_check CHECK (result IN ('success', 'denied', 'failed'))
);

CREATE INDEX departments_organization_idx ON departments (organization_id);
CREATE INDEX users_organization_idx ON users (organization_id);
CREATE INDEX users_department_idx ON users (department_id);
CREATE INDEX sessions_user_idx ON sessions (user_id);
CREATE INDEX sessions_expires_idx ON sessions (expires_at) WHERE revoked_at IS NULL;
CREATE INDEX audit_events_organization_created_idx ON audit_events (organization_id, created_at DESC);
CREATE INDEX audit_events_actor_created_idx ON audit_events (actor_id, created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS audit_events;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS role_permissions;
DROP TABLE IF EXISTS user_roles;
DROP TABLE IF EXISTS permissions;
DROP TABLE IF EXISTS roles;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS departments;
DROP TABLE IF EXISTS organizations;

