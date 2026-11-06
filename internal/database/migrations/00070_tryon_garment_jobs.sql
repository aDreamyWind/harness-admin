-- +goose Up
CREATE TABLE tryon_garment_batches (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    owner_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    source_mime varchar(20) NOT NULL CHECK (source_mime IN ('image/png', 'image/jpeg', 'image/webp')),
    source_data bytea NOT NULL CHECK (octet_length(source_data) BETWEEN 1 AND 6291456),
    created_at timestamptz NOT NULL DEFAULT NOW(),
    UNIQUE (organization_id, owner_id, id)
);

CREATE TABLE tryon_garment_jobs (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL,
    owner_id uuid NOT NULL,
    batch_id uuid NOT NULL,
    name varchar(80) NOT NULL,
    category varchar(20) NOT NULL,
    target_box double precision[] CHECK (target_box IS NULL OR cardinality(target_box) = 4),
    model_code varchar(120) NOT NULL,
    status varchar(16) NOT NULL CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'saved')),
    error_message text NOT NULL DEFAULT '',
    image_mime varchar(20) CHECK (image_mime IN ('image/png', 'image/jpeg')),
    image_data bytea CHECK (octet_length(image_data) BETWEEN 1 AND 6291456),
    lease_expires_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    FOREIGN KEY (organization_id, owner_id, batch_id)
        REFERENCES tryon_garment_batches (organization_id, owner_id, id) ON DELETE CASCADE,
    CHECK ((status = 'running') = (lease_expires_at IS NOT NULL)),
    CHECK ((status = 'failed') = (error_message <> '')),
    CHECK ((image_mime IS NULL) = (image_data IS NULL)),
    CHECK ((status = 'succeeded') = (image_data IS NOT NULL))
);

CREATE INDEX tryon_garment_jobs_owner_idx ON tryon_garment_jobs (organization_id, owner_id, created_at DESC, id DESC);
CREATE INDEX tryon_garment_jobs_batch_idx ON tryon_garment_jobs (batch_id);
CREATE INDEX tryon_garment_jobs_queued_idx ON tryon_garment_jobs (updated_at, id) WHERE status = 'queued';
CREATE INDEX tryon_garment_jobs_lease_idx ON tryon_garment_jobs (lease_expires_at) WHERE status = 'running';
CREATE INDEX tryon_garment_jobs_prune_idx ON tryon_garment_jobs (updated_at) WHERE status IN ('succeeded', 'failed', 'saved');

COMMENT ON TABLE tryon_garment_batches IS '衣物后台生成批次，同一批的多个目标共用一份参考原图';
COMMENT ON COLUMN tryon_garment_batches.id IS '衣物生成批次唯一标识';
COMMENT ON COLUMN tryon_garment_batches.organization_id IS '批次所属组织标识';
COMMENT ON COLUMN tryon_garment_batches.owner_id IS '批次发起用户标识';
COMMENT ON COLUMN tryon_garment_batches.source_mime IS '参考原图媒体类型';
COMMENT ON COLUMN tryon_garment_batches.source_data IS '同批任务共用的参考原图字节，供生成和显式重试使用';
COMMENT ON COLUMN tryon_garment_batches.created_at IS '批次提交时间';
COMMENT ON TABLE tryon_garment_jobs IS '衣物后台生成任务，每件衣物拥有独立状态和待确认结果';
COMMENT ON COLUMN tryon_garment_jobs.id IS '单件生成任务标识，确认入库时沿用为素材标识';
COMMENT ON COLUMN tryon_garment_jobs.organization_id IS '任务所属组织标识';
COMMENT ON COLUMN tryon_garment_jobs.owner_id IS '任务发起用户标识';
COMMENT ON COLUMN tryon_garment_jobs.batch_id IS '提供共享参考原图的批次标识';
COMMENT ON COLUMN tryon_garment_jobs.name IS '提交时选定的目标衣物名称';
COMMENT ON COLUMN tryon_garment_jobs.category IS '提交时选定的目标衣物分类';
COMMENT ON COLUMN tryon_garment_jobs.target_box IS '可选的目标框，按左、上、右、下记录归一化坐标';
COMMENT ON COLUMN tryon_garment_jobs.model_code IS '生成这件衣物使用的图片模型目录编码';
COMMENT ON COLUMN tryon_garment_jobs.status IS '任务状态，分为排队、生成中、待确认、失败和已入库';
COMMENT ON COLUMN tryon_garment_jobs.error_message IS '失败时可向提交者显示的原因';
COMMENT ON COLUMN tryon_garment_jobs.image_mime IS '待确认生成图的媒体类型，入库后清空';
COMMENT ON COLUMN tryon_garment_jobs.image_data IS '待确认生成图的字节，入库后转存到试衣素材库';
COMMENT ON COLUMN tryon_garment_jobs.lease_expires_at IS '本次领取的到期时间，用于失效处理并拒绝迟到的旧结果';
COMMENT ON COLUMN tryon_garment_jobs.created_at IS '任务首次提交时间';
COMMENT ON COLUMN tryon_garment_jobs.updated_at IS '最近一次状态更新时间，也用于重试排队和到期清理';

-- +goose Down
DROP TABLE tryon_garment_jobs;
DROP TABLE tryon_garment_batches;
