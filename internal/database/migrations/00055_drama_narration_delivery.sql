-- +goose Up
ALTER TABLE drama_storyboard_utterances
 ADD COLUMN source_text text NOT NULL DEFAULT '',
 ADD COLUMN delivery text NOT NULL DEFAULT 'spoken' CHECK(delivery IN ('spoken','visual')),
 ADD COLUMN adaptation_reason text NOT NULL DEFAULT '';
ALTER TABLE drama_storyboard_utterances ADD CONSTRAINT drama_storyboard_voice_delivery CHECK((delivery='spoken' AND text<>'') OR (delivery='visual' AND kind='vo' AND text='' AND source_text<>''));
COMMENT ON TABLE drama_storyboard_utterances IS '镜头内按顺序保存的实际口播、旁白原句及转画面的事实依据';
COMMENT ON COLUMN drama_storyboard_utterances.text IS '实际口播文字；旁白转画面时为空，不计入声音时长';
COMMENT ON COLUMN drama_storyboard_utterances.source_text IS '精简或转画面前的原旁白，由程序从已校验稿保留，角色对白和心声不能借此改写';
COMMENT ON COLUMN drama_storyboard_utterances.delivery IS '声音表达方式：实际口播或原旁白改由画面表达';
COMMENT ON COLUMN drama_storyboard_utterances.adaptation_reason IS '旁白精简或转画面的说明，需经原句和实际画面复核';
-- +goose Down
ALTER TABLE drama_storyboard_utterances DROP CONSTRAINT drama_storyboard_voice_delivery;
UPDATE drama_storyboard_utterances SET text=source_text WHERE source_text<>'';
ALTER TABLE drama_storyboard_utterances DROP COLUMN source_text,DROP COLUMN delivery,DROP COLUMN adaptation_reason;
