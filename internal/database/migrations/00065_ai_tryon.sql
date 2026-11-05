-- +goose Up
CREATE TABLE tryon_assets (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    owner_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind varchar(20) NOT NULL CHECK (kind IN ('person_2d', 'person_real', 'garment', 'pose')),
    name varchar(80) NOT NULL,
    category varchar(20) NOT NULL DEFAULT '',
    image_mime varchar(20) NOT NULL CHECK (image_mime IN ('image/png', 'image/jpeg', 'image/webp')),
    image_data bytea NOT NULL CHECK (octet_length(image_data) BETWEEN 1 AND 6291456),
    created_at timestamptz NOT NULL DEFAULT NOW(),
    UNIQUE (organization_id, owner_id, id),
    CHECK ((kind = 'garment') = (category <> ''))
);
CREATE INDEX tryon_assets_owner_idx ON tryon_assets (organization_id, owner_id, created_at DESC, id DESC);

CREATE TABLE tryon_jobs (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    owner_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    person_id uuid NOT NULL,
    garment_id uuid NOT NULL,
    pose_id uuid,
    model_code varchar(120) NOT NULL,
    status varchar(16) NOT NULL CHECK (status IN ('running', 'succeeded', 'failed')),
    error_message text NOT NULL DEFAULT '',
    image_mime varchar(20) CHECK (image_mime IN ('image/png', 'image/jpeg', 'image/webp')),
    image_data bytea CHECK (octet_length(image_data) BETWEEN 1 AND 20971520),
    created_at timestamptz NOT NULL DEFAULT NOW(),
    expires_at timestamptz NOT NULL,
    FOREIGN KEY (organization_id, owner_id, person_id) REFERENCES tryon_assets (organization_id, owner_id, id),
    FOREIGN KEY (organization_id, owner_id, garment_id) REFERENCES tryon_assets (organization_id, owner_id, id),
    FOREIGN KEY (organization_id, owner_id, pose_id) REFERENCES tryon_assets (organization_id, owner_id, id),
    CHECK ((status = 'succeeded') = (image_mime IS NOT NULL AND image_data IS NOT NULL)),
    CHECK ((status = 'failed') = (error_message <> ''))
);
CREATE INDEX tryon_jobs_owner_idx ON tryon_jobs (organization_id, owner_id, created_at DESC, id DESC);
CREATE UNIQUE INDEX tryon_jobs_one_running_idx ON tryon_jobs (organization_id, owner_id) WHERE status = 'running';

COMMENT ON TABLE tryon_assets IS 'AI 试衣素材库，按组织和用户隔离，素材图像固定供历史任务引用';
COMMENT ON COLUMN tryon_assets.id IS '素材唯一标识';
COMMENT ON COLUMN tryon_assets.organization_id IS '素材所属组织标识';
COMMENT ON COLUMN tryon_assets.owner_id IS '素材所有者标识';
COMMENT ON COLUMN tryon_assets.kind IS '素材类型，分为二维插画人物、写实人物、衣物和姿势';
COMMENT ON COLUMN tryon_assets.name IS '用户填写的素材名称';
COMMENT ON COLUMN tryon_assets.category IS '衣物分类，人物和姿势留空';
COMMENT ON COLUMN tryon_assets.image_mime IS '素材图片媒体类型';
COMMENT ON COLUMN tryon_assets.image_data IS '最终参与生成的素材图片字节，衣物可为裁剪结果';
COMMENT ON COLUMN tryon_assets.created_at IS '素材入库时间';
COMMENT ON TABLE tryon_jobs IS 'AI 试衣任务和结果，记录每次采用的图片素材和模型';
COMMENT ON COLUMN tryon_jobs.id IS '试穿任务唯一标识';
COMMENT ON COLUMN tryon_jobs.organization_id IS '任务所属组织标识';
COMMENT ON COLUMN tryon_jobs.owner_id IS '任务发起用户标识';
COMMENT ON COLUMN tryon_jobs.person_id IS '本次采用的人物素材标识';
COMMENT ON COLUMN tryon_jobs.garment_id IS '本次采用的衣物素材标识';
COMMENT ON COLUMN tryon_jobs.pose_id IS '可选姿势素材标识，空值表示保留人物原姿势';
COMMENT ON COLUMN tryon_jobs.model_code IS '发起生成时选择的模型目录编码';
COMMENT ON COLUMN tryon_jobs.status IS '任务状态，分为生成中、成功和失败';
COMMENT ON COLUMN tryon_jobs.error_message IS '失败时显示给用户的错误说明';
COMMENT ON COLUMN tryon_jobs.image_mime IS '成功结果的图片媒体类型';
COMMENT ON COLUMN tryon_jobs.image_data IS '成功试穿结果的图片字节';
COMMENT ON COLUMN tryon_jobs.created_at IS '任务创建时间';
COMMENT ON COLUMN tryon_jobs.expires_at IS '生成任务的最迟完成时间，用于恢复进程中断的任务';

-- +goose Down
DROP TABLE tryon_jobs;
DROP TABLE tryon_assets;
