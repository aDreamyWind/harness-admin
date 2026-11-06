-- +goose Up
CREATE TABLE drama_project_cover_revisions (
    project_id uuid NOT NULL REFERENCES drama_projects(id) ON DELETE CASCADE,
    revision uuid NOT NULL,
    source text NOT NULL CHECK (source IN ('ai', 'upload')),
    mime_type text NOT NULL CHECK (
        (source = 'ai' AND mime_type = 'image/svg+xml') OR
        (source = 'upload' AND mime_type IN ('image/jpeg', 'image/png', 'image/webp'))
    ),
    image_data bytea NOT NULL CHECK (octet_length(image_data) BETWEEN 1 AND 10485760),
    created_at timestamptz NOT NULL DEFAULT NOW(),
    PRIMARY KEY (project_id, revision)
);
CREATE INDEX drama_project_cover_revisions_history_idx
    ON drama_project_cover_revisions (project_id, created_at DESC, revision DESC);

INSERT INTO drama_project_cover_revisions (project_id, revision, source, mime_type, image_data)
    SELECT project_id, cover_id, 'ai', 'image/svg+xml', cover_data
    FROM drama_project_covers WHERE cover_id IS NOT NULL
    UNION ALL
    SELECT project_id, job_id, 'ai', 'image/svg+xml', candidate_data
    FROM drama_project_covers WHERE candidate_data IS NOT NULL;

ALTER TABLE drama_project_covers ADD COLUMN candidate_id uuid;
UPDATE drama_project_covers SET candidate_id = job_id WHERE candidate_data IS NOT NULL;

ALTER TABLE drama_project_covers
    DROP COLUMN cover_data,
    DROP COLUMN candidate_data,
    DROP CONSTRAINT drama_project_covers_status_check,
    ALTER COLUMN job_id DROP NOT NULL,
    ADD COLUMN instruction text NOT NULL DEFAULT '' CHECK (char_length(instruction) <= 500),
    ADD CONSTRAINT drama_project_covers_status_check CHECK (status IN ('idle', 'running', 'succeeded', 'failed')),
    ADD CONSTRAINT drama_project_covers_job_state_check CHECK ((status = 'idle') = (job_id IS NULL)),
    ADD CONSTRAINT drama_project_covers_candidate_check CHECK (candidate_id IS NULL OR
        (status = 'succeeded' AND candidate_id = job_id AND cover_id <> candidate_id)),
    ADD CONSTRAINT drama_project_covers_current_revision_fk FOREIGN KEY (project_id, cover_id)
        REFERENCES drama_project_cover_revisions (project_id, revision),
    ADD CONSTRAINT drama_project_covers_candidate_revision_fk FOREIGN KEY (project_id, candidate_id)
        REFERENCES drama_project_cover_revisions (project_id, revision);

COMMENT ON TABLE drama_project_cover_revisions IS '漫剧项目已生成或上传的封面版本，包含当前、候选及历史封面';
COMMENT ON COLUMN drama_project_cover_revisions.project_id IS '所属漫剧项目，删除项目时一并清理';
COMMENT ON COLUMN drama_project_cover_revisions.revision IS '项目内唯一的封面版本标识，AI 生成时与任务标识相同';
COMMENT ON COLUMN drama_project_cover_revisions.source IS '封面来源，包括作者上传和模型生成';
COMMENT ON COLUMN drama_project_cover_revisions.mime_type IS '根据图片内容识别的媒体类型，生成封面为系统合成的 SVG';
COMMENT ON COLUMN drama_project_cover_revisions.image_data IS '经过验证的封面图片二进制数据';
COMMENT ON COLUMN drama_project_cover_revisions.created_at IS '版本首次入库时间，升级时已存在的图片记录归档时间';
COMMENT ON TABLE drama_project_covers IS '漫剧项目当前封面指针、最新生成任务及待选择的候选封面指针';
COMMENT ON COLUMN drama_project_covers.job_id IS '最近一次生成任务标识，也是该任务结果的封面版本标识，未生成时为空';
COMMENT ON COLUMN drama_project_covers.status IS '最近一次生成的任务状态，未生成、生成中、成功或失败';
COMMENT ON COLUMN drama_project_covers.candidate_id IS '尚待用户选择的候选封面版本标识';
COMMENT ON COLUMN drama_project_covers.instruction IS '最近一次任务附加的可选画面要求';

-- +goose Down
ALTER TABLE drama_project_covers
    DROP CONSTRAINT drama_project_covers_current_revision_fk,
    DROP CONSTRAINT drama_project_covers_candidate_revision_fk,
    DROP CONSTRAINT drama_project_covers_candidate_check,
    ADD COLUMN cover_data bytea CHECK (octet_length(cover_data) BETWEEN 1 AND 10485760),
    ADD COLUMN candidate_data bytea CHECK (octet_length(candidate_data) BETWEEN 1 AND 10485760);

-- 旧版只支持系统合成的 SVG，回退时取最近可用的 AI 封面。
DELETE FROM drama_project_covers WHERE status = 'idle';
WITH legacy_covers AS (
    SELECT c.project_id, artwork.revision, artwork.image_data,
           CASE WHEN artwork.revision <> c.candidate_id THEN candidate.image_data END AS candidate_data
    FROM drama_project_covers c
    LEFT JOIN LATERAL (
        SELECT h.revision, h.image_data FROM drama_project_cover_revisions h
        WHERE h.project_id = c.project_id AND h.source = 'ai'
        ORDER BY (h.revision = c.cover_id) DESC NULLS LAST, (h.revision = c.candidate_id) ASC NULLS FIRST, h.created_at DESC, h.revision DESC
        LIMIT 1
    ) artwork ON TRUE
    LEFT JOIN drama_project_cover_revisions candidate ON candidate.project_id = c.project_id AND candidate.revision = c.candidate_id
)
UPDATE drama_project_covers c SET cover_id = h.revision, cover_data = h.image_data, candidate_data = h.candidate_data
FROM legacy_covers h WHERE h.project_id = c.project_id;

ALTER TABLE drama_project_covers
    DROP COLUMN candidate_id,
    DROP COLUMN instruction,
    DROP CONSTRAINT drama_project_covers_status_check,
    DROP CONSTRAINT drama_project_covers_job_state_check,
    ALTER COLUMN job_id SET NOT NULL,
    ADD CONSTRAINT drama_project_covers_status_check CHECK (status IN ('running', 'succeeded', 'failed')),
    ADD CONSTRAINT drama_project_covers_legacy_current_check CHECK ((cover_id IS NULL) = (cover_data IS NULL)),
    ADD CONSTRAINT drama_project_covers_legacy_candidate_check CHECK (candidate_data IS NULL OR (status = 'succeeded' AND cover_id <> job_id));

COMMENT ON TABLE drama_project_covers IS '漫剧项目当前封面、最新封面生成任务及待选择的候选封面';
COMMENT ON COLUMN drama_project_covers.job_id IS '最近一次生成任务标识，也是该任务结果的封面版本标识';
COMMENT ON COLUMN drama_project_covers.status IS '最近一次生成的任务状态，生成中、成功或失败';
COMMENT ON COLUMN drama_project_covers.cover_data IS '当前使用的封面，包含生成底图和项目标题';
COMMENT ON COLUMN drama_project_covers.candidate_data IS '候选封面，用户选择后清除';

DROP TABLE drama_project_cover_revisions;
