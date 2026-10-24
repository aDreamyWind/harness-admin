-- +goose Up
ALTER TABLE enterprise_skill_versions
    ADD COLUMN source_type varchar(16) NOT NULL DEFAULT 'upload'
        CHECK (source_type IN ('upload', 'github')),
    ADD COLUMN source_ref text NOT NULL DEFAULT '',
    ADD COLUMN source_revision varchar(40) NOT NULL DEFAULT '',
    ADD COLUMN source_path text NOT NULL DEFAULT '';
ALTER TABLE enterprise_skill_versions
    ADD CONSTRAINT enterprise_skill_versions_github_source_check
    CHECK (source_type = 'upload' OR
           (source_ref <> '' AND source_revision ~ '^[0-9a-f]{40}$'));
COMMENT ON COLUMN enterprise_skill_versions.source_type IS '版本导入来源：本地上传或 GitHub 仓库';
COMMENT ON COLUMN enterprise_skill_versions.source_ref IS 'GitHub 来源仓库的规范化 HTTPS 地址，本地上传时为空';
COMMENT ON COLUMN enterprise_skill_versions.source_revision IS 'GitHub 来源解析后的固定完整提交号，本地上传时为空';
COMMENT ON COLUMN enterprise_skill_versions.source_path IS 'Skill 在 GitHub 仓库内的相对目录，本地上传时为空';

-- +goose Down
ALTER TABLE enterprise_skill_versions DROP CONSTRAINT enterprise_skill_versions_github_source_check;
ALTER TABLE enterprise_skill_versions
    DROP COLUMN source_path,
    DROP COLUMN source_revision,
    DROP COLUMN source_ref,
    DROP COLUMN source_type;
