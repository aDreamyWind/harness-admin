-- +goose Up
ALTER TABLE drama_assets
    ADD COLUMN appearance_description text NOT NULL DEFAULT '',
    ADD COLUMN voice_description text NOT NULL DEFAULT '';
COMMENT ON COLUMN drama_assets.appearance_description IS '角色可用于形象创作的外貌与服饰描述；非角色资产留空';
COMMENT ON COLUMN drama_assets.voice_description IS '角色可用于配音创作的音色与说话特征描述；非角色资产留空';

-- +goose Down
ALTER TABLE drama_assets DROP COLUMN voice_description, DROP COLUMN appearance_description;
