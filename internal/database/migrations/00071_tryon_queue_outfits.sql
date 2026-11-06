-- +goose Up
DROP INDEX tryon_jobs_one_running_idx;
ALTER TABLE tryon_jobs DROP CONSTRAINT tryon_jobs_status_check;
ALTER TABLE tryon_jobs ADD CONSTRAINT tryon_jobs_status_check CHECK (status IN ('queued', 'running', 'succeeded', 'failed'));
ALTER TABLE tryon_jobs ADD CONSTRAINT tryon_jobs_owner_id_unique UNIQUE (organization_id, owner_id, id);

CREATE INDEX tryon_jobs_queue_idx ON tryon_jobs (created_at, id) WHERE status = 'queued';
CREATE INDEX tryon_jobs_lease_idx ON tryon_jobs (expires_at) WHERE status = 'running';

CREATE TABLE tryon_job_garments (
    organization_id uuid NOT NULL,
    owner_id uuid NOT NULL,
    job_id uuid NOT NULL,
    garment_id uuid NOT NULL,
    position smallint NOT NULL CHECK (position BETWEEN 1 AND 4),
    PRIMARY KEY (job_id, position),
    UNIQUE (job_id, garment_id),
    FOREIGN KEY (organization_id, owner_id, job_id)
        REFERENCES tryon_jobs (organization_id, owner_id, id) ON DELETE CASCADE,
    FOREIGN KEY (organization_id, owner_id, garment_id)
        REFERENCES tryon_assets (organization_id, owner_id, id)
);
CREATE INDEX tryon_job_garments_asset_idx ON tryon_job_garments (organization_id, owner_id, garment_id);
INSERT INTO tryon_job_garments (organization_id, owner_id, job_id, garment_id, position)
    SELECT organization_id, owner_id, id, garment_id, 1 FROM tryon_jobs;

CREATE TABLE tryon_outfits (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    owner_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name varchar(80) NOT NULL CHECK (btrim(name) <> ''),
    garment_ids uuid[] NOT NULL CHECK (cardinality(garment_ids) BETWEEN 2 AND 4 AND array_position(garment_ids, NULL) IS NULL),
    created_at timestamptz NOT NULL DEFAULT NOW()
);
CREATE INDEX tryon_outfits_owner_idx ON tryon_outfits (organization_id, owner_id, created_at DESC, id DESC);

COMMENT ON TABLE tryon_jobs IS '可持久化排队的 AI 试衣任务及结果，记录生成使用的参考素材和模型';
COMMENT ON COLUMN tryon_jobs.garment_id IS '兼容单件试穿的首件衣物标识，完整衣物顺序见试穿衣物关联';
COMMENT ON COLUMN tryon_jobs.status IS '任务状态，分为排队、生成中、成功和失败';
COMMENT ON COLUMN tryon_jobs.expires_at IS '领取后本次生成的到期时间，排队时记录入队时间';
COMMENT ON TABLE tryon_job_garments IS '试穿任务采用的衣物及输入顺序，保留历史素材引用';
COMMENT ON COLUMN tryon_job_garments.organization_id IS '任务所属组织标识';
COMMENT ON COLUMN tryon_job_garments.owner_id IS '任务发起用户标识';
COMMENT ON COLUMN tryon_job_garments.job_id IS '试穿任务标识';
COMMENT ON COLUMN tryon_job_garments.garment_id IS '试穿采用的衣物素材标识';
COMMENT ON COLUMN tryon_job_garments.position IS '衣物在试穿输入中的顺序，从一开始';
COMMENT ON TABLE tryon_outfits IS '用户保存的有序衣物套装，可查看其中已失效的素材引用';
COMMENT ON COLUMN tryon_outfits.id IS '套装唯一标识';
COMMENT ON COLUMN tryon_outfits.organization_id IS '套装所属组织标识';
COMMENT ON COLUMN tryon_outfits.owner_id IS '创建套装的用户标识';
COMMENT ON COLUMN tryon_outfits.name IS '套装显示名称';
COMMENT ON COLUMN tryon_outfits.garment_ids IS '套装采用的有序衣物标识，衣物被删除后保留标识供识别失效项';
COMMENT ON COLUMN tryon_outfits.created_at IS '套装保存时间';

-- +goose Down
DROP TABLE tryon_outfits;
DROP TABLE tryon_job_garments;
DROP INDEX tryon_jobs_queue_idx;
DROP INDEX tryon_jobs_lease_idx;
ALTER TABLE tryon_jobs DROP CONSTRAINT tryon_jobs_owner_id_unique;
UPDATE tryon_jobs SET status = 'failed', error_message = '服务回滚导致任务中断，请重新试穿'
    WHERE status IN ('queued', 'running');
ALTER TABLE tryon_jobs DROP CONSTRAINT tryon_jobs_status_check;
ALTER TABLE tryon_jobs ADD CONSTRAINT tryon_jobs_status_check CHECK (status IN ('running', 'succeeded', 'failed'));
CREATE UNIQUE INDEX tryon_jobs_one_running_idx ON tryon_jobs (organization_id, owner_id) WHERE status = 'running';
COMMENT ON TABLE tryon_jobs IS 'AI 试衣任务和结果，记录每次采用的图片素材和模型';
COMMENT ON COLUMN tryon_jobs.garment_id IS '本次采用的衣物素材标识';
COMMENT ON COLUMN tryon_jobs.status IS '任务状态，分为生成中、成功和失败';
COMMENT ON COLUMN tryon_jobs.expires_at IS '生成任务的最迟完成时间，用于恢复进程中断的任务';
