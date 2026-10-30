-- +goose Up
CREATE TABLE drama_canvas_boards (
 project_id uuid PRIMARY KEY REFERENCES drama_projects(id) ON DELETE CASCADE,
 viewport_x double precision NOT NULL DEFAULT 0,
 viewport_y double precision NOT NULL DEFAULT 0,
 viewport_zoom double precision NOT NULL DEFAULT 0.6 CHECK(viewport_zoom BETWEEN 0.05 AND 2),
 revision integer NOT NULL DEFAULT 1,
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE drama_asset_forms (
 id uuid PRIMARY KEY,
 asset_id uuid NOT NULL REFERENCES drama_assets(id) ON DELETE CASCADE,
 parent_id uuid,
 name text NOT NULL,
 description text NOT NULL DEFAULT '',
 gender text NOT NULL DEFAULT 'unspecified' CHECK(gender IN ('unspecified','male','female','other')),
 style text NOT NULL DEFAULT '',
 aspect_ratio text NOT NULL CHECK(aspect_ratio IN ('9:16','16:9','1:1')),
 position_x double precision NOT NULL DEFAULT 0,
 position_y double precision NOT NULL DEFAULT 0,
 revision integer NOT NULL DEFAULT 1,
 current_image_id uuid,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(asset_id,id),
 FOREIGN KEY(asset_id,parent_id) REFERENCES drama_asset_forms(asset_id,id),
 CHECK(parent_id IS NULL OR parent_id<>id)
);
CREATE UNIQUE INDEX drama_asset_default_form ON drama_asset_forms(asset_id) WHERE parent_id IS NULL;
CREATE TABLE drama_image_jobs (
 id uuid PRIMARY KEY,
 form_id uuid NOT NULL REFERENCES drama_asset_forms(id) ON DELETE CASCADE,
 model_code text NOT NULL,
 form_revision integer NOT NULL,
 prompt text NOT NULL,
 aspect_ratio text NOT NULL,
 reference_id uuid REFERENCES drama_image_jobs(id),
 status text NOT NULL CHECK(status IN ('queued','running','succeeded','failed')),
 error_message text NOT NULL DEFAULT '',
 storage_key text NOT NULL DEFAULT '',
 mime_type text NOT NULL DEFAULT '',
 byte_size integer NOT NULL DEFAULT 0,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE drama_asset_forms ADD CONSTRAINT drama_form_current_image FOREIGN KEY(current_image_id) REFERENCES drama_image_jobs(id);
CREATE UNIQUE INDEX drama_image_one_active ON drama_image_jobs(form_id) WHERE status IN ('queued','running');
CREATE INDEX drama_image_history ON drama_image_jobs(form_id,created_at DESC,id);
COMMENT ON TABLE drama_canvas_boards IS '漫剧项目自由画布及视口';
COMMENT ON COLUMN drama_canvas_boards.project_id IS '所属项目主键';
COMMENT ON COLUMN drama_canvas_boards.viewport_x IS '视口横向位移';
COMMENT ON COLUMN drama_canvas_boards.viewport_y IS '视口纵向位移';
COMMENT ON COLUMN drama_canvas_boards.viewport_zoom IS '视口缩放比例';
COMMENT ON COLUMN drama_canvas_boards.revision IS '布局并发控制版本';
COMMENT ON COLUMN drama_canvas_boards.updated_at IS '布局更新时间';
COMMENT ON TABLE drama_asset_forms IS '资产的默认及派生形象，同一资产的形象共享资产关联';
COMMENT ON COLUMN drama_asset_forms.id IS '形象主键，默认形象与资产主键相同';
COMMENT ON COLUMN drama_asset_forms.asset_id IS '项目资产主键';
COMMENT ON COLUMN drama_asset_forms.parent_id IS '同资产的来源形象主键，空值表示默认形象';
COMMENT ON COLUMN drama_asset_forms.name IS '形象名称';
COMMENT ON COLUMN drama_asset_forms.description IS '用于图片生成的形象描述';
COMMENT ON COLUMN drama_asset_forms.gender IS '性别标识';
COMMENT ON COLUMN drama_asset_forms.style IS '画面风格';
COMMENT ON COLUMN drama_asset_forms.aspect_ratio IS '图片画面比例';
COMMENT ON COLUMN drama_asset_forms.position_x IS '画布横向位置';
COMMENT ON COLUMN drama_asset_forms.position_y IS '画布纵向位置';
COMMENT ON COLUMN drama_asset_forms.revision IS '形象内容并发控制版本';
COMMENT ON COLUMN drama_asset_forms.current_image_id IS '当前成功图片的任务主键，失败不覆盖';
COMMENT ON COLUMN drama_asset_forms.created_at IS '形象创建时间';
COMMENT ON COLUMN drama_asset_forms.updated_at IS '形象内容更新时间';
COMMENT ON TABLE drama_image_jobs IS '漫剧图片生成任务与历史图片元数据，图片文件独立保存';
COMMENT ON COLUMN drama_image_jobs.id IS '任务及历史图片主键';
COMMENT ON COLUMN drama_image_jobs.form_id IS '对应资产形象主键';
COMMENT ON COLUMN drama_image_jobs.model_code IS '实际使用的图片模型目录编码';
COMMENT ON COLUMN drama_image_jobs.form_revision IS '生成时形象内容版本';
COMMENT ON COLUMN drama_image_jobs.prompt IS '本次实际生成提示词快照';
COMMENT ON COLUMN drama_image_jobs.aspect_ratio IS '本次图片比例快照';
COMMENT ON COLUMN drama_image_jobs.reference_id IS '派生形象使用的原始图片任务主键';
COMMENT ON COLUMN drama_image_jobs.status IS '任务状态：排队、进行中、成功、失败';
COMMENT ON COLUMN drama_image_jobs.error_message IS '失败原因';
COMMENT ON COLUMN drama_image_jobs.storage_key IS '本地媒体存储相对键';
COMMENT ON COLUMN drama_image_jobs.mime_type IS '图片媒体类型';
COMMENT ON COLUMN drama_image_jobs.byte_size IS '图片文件字节数';
COMMENT ON COLUMN drama_image_jobs.created_at IS '任务创建时间';
COMMENT ON COLUMN drama_image_jobs.updated_at IS '任务最后更新时间';

-- +goose Down
ALTER TABLE drama_asset_forms DROP CONSTRAINT drama_form_current_image;
DROP TABLE drama_image_jobs;
DROP TABLE drama_asset_forms;
DROP TABLE drama_canvas_boards;
