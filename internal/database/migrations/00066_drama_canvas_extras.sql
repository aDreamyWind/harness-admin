-- +goose Up
ALTER TABLE drama_canvas_boards
    ADD COLUMN extras jsonb CHECK (extras IS NULL OR jsonb_typeof(extras) = 'object');

COMMENT ON TABLE drama_canvas_boards IS '漫剧项目自由画布的视口、素材节点和手工连线';
COMMENT ON COLUMN drama_canvas_boards.extras IS '画布素材节点、手工连线和隐藏形象。空值表示未保存，允许首次导入当前浏览器的旧画布。';

-- +goose Down
ALTER TABLE drama_canvas_boards DROP COLUMN extras;
COMMENT ON TABLE drama_canvas_boards IS '漫剧项目自由画布及视口';
