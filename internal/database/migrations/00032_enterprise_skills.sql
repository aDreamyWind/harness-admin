-- +goose Up
CREATE TABLE enterprise_skills (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name varchar(64) NOT NULL,
    description varchar(1024) NOT NULL,
    status varchar(16) NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published', 'disabled')),
    published_version_id uuid,
    created_by uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    UNIQUE (organization_id, name),
    UNIQUE (id, organization_id)
);
COMMENT ON TABLE enterprise_skills IS '企业 Skill 资产及当前发布状态';
COMMENT ON COLUMN enterprise_skills.id IS 'Skill 资产唯一标识';
COMMENT ON COLUMN enterprise_skills.organization_id IS '所属企业标识';
COMMENT ON COLUMN enterprise_skills.name IS '与 SKILL.md 一致的不可变 Skill 名称';
COMMENT ON COLUMN enterprise_skills.description IS '当前最新导入版本的 Skill 说明';
COMMENT ON COLUMN enterprise_skills.status IS '资产状态：草稿、已发布或已停用';
COMMENT ON COLUMN enterprise_skills.published_version_id IS '当前对员工发布的不可变版本标识';
COMMENT ON COLUMN enterprise_skills.created_by IS '最初导入的管理员用户标识';
COMMENT ON COLUMN enterprise_skills.created_at IS '资产创建时间';
COMMENT ON COLUMN enterprise_skills.updated_at IS '资产最近修改时间';

CREATE TABLE enterprise_skill_versions (
    id uuid PRIMARY KEY,
    skill_id uuid NOT NULL REFERENCES enterprise_skills(id) ON DELETE CASCADE,
    version_no integer NOT NULL CHECK (version_no > 0),
    frontmatter jsonb NOT NULL,
    manifest_digest varchar(71) NOT NULL,
    created_by uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    UNIQUE (skill_id, version_no),
    UNIQUE (skill_id, id)
);
COMMENT ON TABLE enterprise_skill_versions IS '企业 Skill 的不可变导入版本';
COMMENT ON COLUMN enterprise_skill_versions.id IS '版本唯一标识';
COMMENT ON COLUMN enterprise_skill_versions.skill_id IS '所属 Skill 资产标识';
COMMENT ON COLUMN enterprise_skill_versions.version_no IS '在同一资产内递增的版本序号';
COMMENT ON COLUMN enterprise_skill_versions.frontmatter IS 'SKILL.md 的完整 YAML 元数据映射';
COMMENT ON COLUMN enterprise_skill_versions.manifest_digest IS '按路径与文件摘要计算的版本清单摘要';
COMMENT ON COLUMN enterprise_skill_versions.created_by IS '导入此版本的管理员用户标识';
COMMENT ON COLUMN enterprise_skill_versions.created_at IS '版本导入时间';

ALTER TABLE enterprise_skills ADD CONSTRAINT enterprise_skills_published_version_fk
    FOREIGN KEY (id, published_version_id) REFERENCES enterprise_skill_versions(skill_id, id);

CREATE TABLE enterprise_skill_files (
    version_id uuid NOT NULL REFERENCES enterprise_skill_versions(id) ON DELETE CASCADE,
    path text NOT NULL,
    content bytea NOT NULL,
    mime_type varchar(120) NOT NULL,
    digest varchar(71) NOT NULL,
    size_bytes integer NOT NULL CHECK (size_bytes >= 0 AND size_bytes <= 16777216),
    PRIMARY KEY (version_id, path)
);
COMMENT ON TABLE enterprise_skill_files IS '企业 Skill 版本的不可变文件内容';
COMMENT ON COLUMN enterprise_skill_files.version_id IS '所属 Skill 版本标识';
COMMENT ON COLUMN enterprise_skill_files.path IS '相对 Skill 根目录的规范化文件路径';
COMMENT ON COLUMN enterprise_skill_files.content IS '经过导入校验的原始文件字节';
COMMENT ON COLUMN enterprise_skill_files.mime_type IS '文件读取时使用的媒体类型';
COMMENT ON COLUMN enterprise_skill_files.digest IS '原始文件字节的 SHA-256 摘要';
COMMENT ON COLUMN enterprise_skill_files.size_bytes IS '原始文件字节数';

CREATE TABLE enterprise_skill_grants (
    id uuid PRIMARY KEY,
    skill_id uuid NOT NULL REFERENCES enterprise_skills(id) ON DELETE CASCADE,
    department_id uuid REFERENCES departments(id) ON DELETE CASCADE,
    user_id uuid REFERENCES users(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    CONSTRAINT enterprise_skill_grants_subject_check CHECK ((department_id IS NULL) <> (user_id IS NULL)),
    UNIQUE (skill_id, department_id),
    UNIQUE (skill_id, user_id)
);
CREATE INDEX enterprise_skill_grants_department_idx ON enterprise_skill_grants(department_id, skill_id);
CREATE INDEX enterprise_skill_grants_user_idx ON enterprise_skill_grants(user_id, skill_id);
COMMENT ON TABLE enterprise_skill_grants IS '企业 Skill 对部门或员工的明确授权';
COMMENT ON COLUMN enterprise_skill_grants.id IS '授权记录唯一标识';
COMMENT ON COLUMN enterprise_skill_grants.skill_id IS '所属 Skill 资产标识';
COMMENT ON COLUMN enterprise_skill_grants.department_id IS '被授权部门标识，用户授权时为空';
COMMENT ON COLUMN enterprise_skill_grants.user_id IS '被授权员工标识，部门授权时为空';
COMMENT ON COLUMN enterprise_skill_grants.created_at IS '授权创建时间';

INSERT INTO permissions (id, code, name, resource, action, description) VALUES
('a1700000-0000-4000-8000-000000000005', 'skill.asset.read', '查看企业 Skill', 'skill_asset', 'read', '查看企业 Skill 资产和版本'),
('a1700000-0000-4000-8000-000000000006', 'skill.asset.write', '管理企业 Skill', 'skill_asset', 'write', '导入、发布、停用和授权企业 Skill')
ON CONFLICT (code) DO NOTHING;
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'enterprise_admin' AND p.code IN ('skill.asset.read', 'skill.asset.write')
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN (SELECT id FROM permissions WHERE code IN ('skill.asset.read', 'skill.asset.write'));
DELETE FROM permissions WHERE code IN ('skill.asset.read', 'skill.asset.write');
DROP TABLE enterprise_skill_grants;
DROP TABLE enterprise_skill_files;
ALTER TABLE enterprise_skills DROP CONSTRAINT enterprise_skills_published_version_fk;
DROP TABLE enterprise_skill_versions;
DROP TABLE enterprise_skills;
