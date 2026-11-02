-- +goose Up
CREATE UNIQUE INDEX drama_storyboard_shot_segment_key ON drama_storyboard_shots(id,segment_id);
CREATE TABLE drama_storyboard_text_mentions (
 id bigserial PRIMARY KEY,
 segment_id uuid NOT NULL REFERENCES drama_storyboard_segments(id) ON DELETE CASCADE,
 shot_id uuid,
 field text NOT NULL CHECK(field IN ('scene','purpose','description','sound','utterance','speaker')),
 utterance_number integer NOT NULL DEFAULT 0,
 start_offset integer NOT NULL CHECK(start_offset>=0),
 end_offset integer NOT NULL CHECK(end_offset>start_offset),
 form_id uuid NOT NULL REFERENCES drama_asset_forms(id),
 FOREIGN KEY(shot_id,segment_id) REFERENCES drama_storyboard_shots(id,segment_id) ON DELETE CASCADE,
 FOREIGN KEY(segment_id,form_id) REFERENCES drama_storyboard_bindings(segment_id,form_id) ON DELETE CASCADE,
 CHECK((field IN ('scene','purpose') AND shot_id IS NULL AND utterance_number=0) OR (field IN ('description','sound') AND shot_id IS NOT NULL AND utterance_number=0) OR (field IN ('utterance','speaker') AND shot_id IS NOT NULL AND utterance_number>0))
);
CREATE INDEX drama_storyboard_text_mentions_segment ON drama_storyboard_text_mentions(segment_id);
COMMENT ON TABLE drama_storyboard_text_mentions IS '分镜文字中的真实资产形象引用，随历史版本保存，不把资产名称冒充关联';
COMMENT ON COLUMN drama_storyboard_text_mentions.id IS '文字引用主键';
COMMENT ON COLUMN drama_storyboard_text_mentions.segment_id IS '所属片段，与形象绑定共同约束真实引用范围';
COMMENT ON COLUMN drama_storyboard_text_mentions.shot_id IS '所属镜头，场次与剧情职责引用时为空';
COMMENT ON COLUMN drama_storyboard_text_mentions.field IS '引用所在文字字段：场次、剧情职责、画面、音效、台词或说话者';
COMMENT ON COLUMN drama_storyboard_text_mentions.utterance_number IS '声音条目顺序，从一开始；非声音字段为零';
COMMENT ON COLUMN drama_storyboard_text_mentions.start_offset IS '引用开始的Unicode字符索引，从零开始且包含该位置';
COMMENT ON COLUMN drama_storyboard_text_mentions.end_offset IS '引用结束的Unicode字符索引，不包含该位置';
COMMENT ON COLUMN drama_storyboard_text_mentions.form_id IS '真实资产形象主键，同名的不同形象仍可明确区分';
-- +goose Down
DROP TABLE drama_storyboard_text_mentions;
DROP INDEX drama_storyboard_shot_segment_key;
