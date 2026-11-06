-- +goose Up
ALTER TABLE tryon_assets ADD COLUMN deleted_at timestamptz;

COMMENT ON TABLE tryon_assets IS 'AI 试衣素材库，按组织和用户隔离，已删除且被试穿记录引用的图片保留为历史快照';
COMMENT ON COLUMN tryon_assets.deleted_at IS '素材从素材库删除的时间，仅对已有试穿记录引用的素材保留，空值表示可继续试穿';

-- +goose Down
ALTER TABLE tryon_assets DROP COLUMN deleted_at;
COMMENT ON TABLE tryon_assets IS 'AI 试衣素材库，按组织和用户隔离，素材图像固定供历史任务引用';
