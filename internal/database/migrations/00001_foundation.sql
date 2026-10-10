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

COMMENT ON TABLE organizations IS '企业组织';
COMMENT ON COLUMN organizations.id IS '企业唯一标识';
COMMENT ON COLUMN organizations.code IS '企业代码，用于登录和系统内识别';
COMMENT ON COLUMN organizations.name IS '企业名称';
COMMENT ON COLUMN organizations.status IS '企业状态：active 启用，disabled 停用';
COMMENT ON COLUMN organizations.created_at IS '创建时间';
COMMENT ON COLUMN organizations.updated_at IS '最后更新时间';

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

COMMENT ON TABLE departments IS '企业部门';
COMMENT ON COLUMN departments.id IS '部门唯一标识';
COMMENT ON COLUMN departments.organization_id IS '所属企业标识';
COMMENT ON COLUMN departments.parent_id IS '上级部门标识，顶级部门为空';
COMMENT ON COLUMN departments.code IS '企业内唯一的部门代码';
COMMENT ON COLUMN departments.name IS '部门名称';
COMMENT ON COLUMN departments.sort_order IS '同级部门排序值，数值越小越靠前';
COMMENT ON COLUMN departments.status IS '部门状态：active 启用，disabled 停用';
COMMENT ON COLUMN departments.created_at IS '创建时间';
COMMENT ON COLUMN departments.updated_at IS '最后更新时间';

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

COMMENT ON TABLE users IS '企业用户账号';
COMMENT ON COLUMN users.id IS '用户唯一标识';
COMMENT ON COLUMN users.organization_id IS '所属企业标识';
COMMENT ON COLUMN users.department_id IS '所属部门标识，可为空';
COMMENT ON COLUMN users.username IS '企业内唯一的登录用户名，不区分大小写';
COMMENT ON COLUMN users.email IS '企业内唯一的电子邮箱，不区分大小写';
COMMENT ON COLUMN users.display_name IS '用户显示名称';
COMMENT ON COLUMN users.password_hash IS 'Argon2id 密码哈希，不保存明文密码';
COMMENT ON COLUMN users.status IS '账号状态：active 正常，locked 锁定，disabled 停用';
COMMENT ON COLUMN users.last_login_at IS '最后一次成功登录时间';
COMMENT ON COLUMN users.created_at IS '创建时间';
COMMENT ON COLUMN users.updated_at IS '最后更新时间';

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

COMMENT ON TABLE roles IS '企业角色';
COMMENT ON COLUMN roles.id IS '角色唯一标识';
COMMENT ON COLUMN roles.organization_id IS '所属企业标识';
COMMENT ON COLUMN roles.code IS '企业内唯一的角色代码';
COMMENT ON COLUMN roles.name IS '角色名称';
COMMENT ON COLUMN roles.description IS '角色说明';
COMMENT ON COLUMN roles.built_in IS '是否为系统内置角色';
COMMENT ON COLUMN roles.created_at IS '创建时间';
COMMENT ON COLUMN roles.updated_at IS '最后更新时间';

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

COMMENT ON TABLE permissions IS '系统权限定义';
COMMENT ON COLUMN permissions.id IS '权限唯一标识';
COMMENT ON COLUMN permissions.code IS '全局唯一的权限代码';
COMMENT ON COLUMN permissions.name IS '权限名称';
COMMENT ON COLUMN permissions.resource IS '权限所控制的资源类型';
COMMENT ON COLUMN permissions.action IS '允许执行的动作';
COMMENT ON COLUMN permissions.description IS '权限说明';
COMMENT ON COLUMN permissions.created_at IS '创建时间';

CREATE TABLE user_roles (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, role_id)
);

COMMENT ON TABLE user_roles IS '用户与角色关联关系';
COMMENT ON COLUMN user_roles.user_id IS '用户标识';
COMMENT ON COLUMN user_roles.role_id IS '角色标识';
COMMENT ON COLUMN user_roles.created_at IS '角色分配时间';

CREATE TABLE role_permissions (
    role_id UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    permission_id UUID NOT NULL REFERENCES permissions(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (role_id, permission_id)
);

COMMENT ON TABLE role_permissions IS '角色与权限关联关系';
COMMENT ON COLUMN role_permissions.role_id IS '角色标识';
COMMENT ON COLUMN role_permissions.permission_id IS '权限标识';
COMMENT ON COLUMN role_permissions.created_at IS '权限分配时间';

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

COMMENT ON TABLE sessions IS '用户登录会话';
COMMENT ON COLUMN sessions.id IS '会话唯一标识';
COMMENT ON COLUMN sessions.user_id IS '会话所属用户标识';
COMMENT ON COLUMN sessions.refresh_token_hash IS '会话令牌的 SHA-256 哈希';
COMMENT ON COLUMN sessions.user_agent IS '登录客户端的 User-Agent';
COMMENT ON COLUMN sessions.source_ip IS '登录来源 IP 地址';
COMMENT ON COLUMN sessions.expires_at IS '会话过期时间';
COMMENT ON COLUMN sessions.revoked_at IS '会话吊销时间，未吊销时为空';
COMMENT ON COLUMN sessions.created_at IS '会话创建时间';

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

COMMENT ON TABLE audit_events IS '系统审计事件';
COMMENT ON COLUMN audit_events.id IS '审计事件自增标识';
COMMENT ON COLUMN audit_events.organization_id IS '事件所属企业标识，可为空';
COMMENT ON COLUMN audit_events.actor_id IS '操作用户标识，可为空';
COMMENT ON COLUMN audit_events.request_id IS '关联 HTTP 请求标识';
COMMENT ON COLUMN audit_events.action IS '审计动作代码';
COMMENT ON COLUMN audit_events.resource_type IS '被操作资源类型';
COMMENT ON COLUMN audit_events.resource_id IS '被操作资源标识';
COMMENT ON COLUMN audit_events.result IS '操作结果：success 成功，denied 拒绝，failed 失败';
COMMENT ON COLUMN audit_events.source_ip IS '操作来源 IP 地址';
COMMENT ON COLUMN audit_events.detail IS '事件扩展详情，JSON 格式';
COMMENT ON COLUMN audit_events.created_at IS '事件发生时间';

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
