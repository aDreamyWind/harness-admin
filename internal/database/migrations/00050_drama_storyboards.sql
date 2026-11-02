-- +goose Up
CREATE TABLE drama_storyboard_runs (
 id uuid PRIMARY KEY,
 episode_id uuid REFERENCES drama_script_episodes(id) ON DELETE SET NULL,
 source_episode_id uuid NOT NULL,
 project_id uuid NOT NULL REFERENCES drama_projects(id) ON DELETE CASCADE,
 version integer NOT NULL CHECK(version>0),
 revision integer NOT NULL DEFAULT 1,
 status text NOT NULL CHECK(status IN ('queued','running','failed','ready','confirmed')),
 phase text NOT NULL DEFAULT 'planning',
 message text NOT NULL DEFAULT '',
 error_message text NOT NULL DEFAULT '',
 script_text text NOT NULL,
 source_hash text NOT NULL,
 model_code text NOT NULL,
 style_id text NOT NULL DEFAULT '',
 aspect_ratio text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(source_episode_id,version)
);
COMMENT ON TABLE drama_storyboard_runs IS '单集分镜运行与历史版本';
COMMENT ON COLUMN drama_storyboard_runs.id IS '运行主键';
COMMENT ON COLUMN drama_storyboard_runs.episode_id IS '当前关联剧本，重新拆集删除后置空';
COMMENT ON COLUMN drama_storyboard_runs.source_episode_id IS '创建时的剧本主键，历史追溯不随删除改变';
COMMENT ON COLUMN drama_storyboard_runs.project_id IS '所属项目';
COMMENT ON COLUMN drama_storyboard_runs.version IS '集内递增版本';
COMMENT ON COLUMN drama_storyboard_runs.revision IS '并发编辑版本';
COMMENT ON COLUMN drama_storyboard_runs.status IS '排队、进行中、失败、就绪、已确认';
COMMENT ON COLUMN drama_storyboard_runs.phase IS '当前处理阶段';
COMMENT ON COLUMN drama_storyboard_runs.message IS '真实进度说明';
COMMENT ON COLUMN drama_storyboard_runs.error_message IS '失败原因';
COMMENT ON COLUMN drama_storyboard_runs.script_text IS '本次使用的完整单集剧本快照';
COMMENT ON COLUMN drama_storyboard_runs.source_hash IS '剧本及关联资产摘要';
COMMENT ON COLUMN drama_storyboard_runs.model_code IS '文本模型编码快照';
COMMENT ON COLUMN drama_storyboard_runs.style_id IS '画面风格主键快照';
COMMENT ON COLUMN drama_storyboard_runs.aspect_ratio IS '画面比例快照';
COMMENT ON COLUMN drama_storyboard_runs.created_at IS '版本创建时间';
COMMENT ON COLUMN drama_storyboard_runs.updated_at IS '最新活动时间';
CREATE TABLE drama_storyboard_segments (
 id uuid PRIMARY KEY,
 run_id uuid NOT NULL REFERENCES drama_storyboard_runs(id) ON DELETE CASCADE,
 number integer NOT NULL CHECK(number>0),
 title text NOT NULL,
 scene text NOT NULL DEFAULT '',
 purpose text NOT NULL DEFAULT '',
 start_line integer NOT NULL CHECK(start_line>0),
 end_line integer NOT NULL CHECK(end_line>=start_line),
 start_state text NOT NULL DEFAULT '',
 end_state text NOT NULL DEFAULT '',
 status text NOT NULL CHECK(status IN ('pending','ready')),
 notes text NOT NULL DEFAULT '',
 UNIQUE(run_id,number)
);
COMMENT ON TABLE drama_storyboard_segments IS '分镜版本内的视频制作片段';
COMMENT ON COLUMN drama_storyboard_segments.id IS '片段主键';
COMMENT ON COLUMN drama_storyboard_segments.run_id IS '所属版本';
COMMENT ON COLUMN drama_storyboard_segments.number IS '片段顺序';
COMMENT ON COLUMN drama_storyboard_segments.title IS '片段名称';
COMMENT ON COLUMN drama_storyboard_segments.scene IS '场次及地点说明';
COMMENT ON COLUMN drama_storyboard_segments.purpose IS '剧情职责和情绪节拍';
COMMENT ON COLUMN drama_storyboard_segments.start_line IS '剧本起始行编号';
COMMENT ON COLUMN drama_storyboard_segments.end_line IS '剧本结束行编号';
COMMENT ON COLUMN drama_storyboard_segments.start_state IS '片段开始的人物和空间状态';
COMMENT ON COLUMN drama_storyboard_segments.end_state IS '片段结束状态及下段衔接';
COMMENT ON COLUMN drama_storyboard_segments.status IS '待编写或检查通过';
COMMENT ON COLUMN drama_storyboard_segments.notes IS '审阅建议及待素材说明';
CREATE TABLE drama_storyboard_shots (
 id uuid PRIMARY KEY,
 segment_id uuid NOT NULL REFERENCES drama_storyboard_segments(id) ON DELETE CASCADE,
 number integer NOT NULL CHECK(number>0),
 description text NOT NULL,
 shot_size text NOT NULL,
 camera text NOT NULL,
 sound text NOT NULL DEFAULT '',
 duration double precision NOT NULL CHECK(duration>0 AND duration<=60),
 UNIQUE(segment_id,number)
);
COMMENT ON TABLE drama_storyboard_shots IS '片段内可编辑的具体镜头';
COMMENT ON COLUMN drama_storyboard_shots.id IS '镜头主键';
COMMENT ON COLUMN drama_storyboard_shots.segment_id IS '所属片段';
COMMENT ON COLUMN drama_storyboard_shots.number IS '片段内顺序';
COMMENT ON COLUMN drama_storyboard_shots.description IS '可见画面与动作';
COMMENT ON COLUMN drama_storyboard_shots.shot_size IS '景别';
COMMENT ON COLUMN drama_storyboard_shots.camera IS '运镜';
COMMENT ON COLUMN drama_storyboard_shots.sound IS '环境音和动作音';
COMMENT ON COLUMN drama_storyboard_shots.duration IS '规划时长秒数，执行前按实际模型校验';
CREATE TABLE drama_storyboard_shot_sources (
 shot_id uuid NOT NULL REFERENCES drama_storyboard_shots(id) ON DELETE CASCADE,
 line_number integer NOT NULL CHECK(line_number>0),
 PRIMARY KEY(shot_id,line_number)
);
COMMENT ON TABLE drama_storyboard_shot_sources IS '镜头到单集剧本行的真实引用';
COMMENT ON COLUMN drama_storyboard_shot_sources.shot_id IS '镜头主键';
COMMENT ON COLUMN drama_storyboard_shot_sources.line_number IS '快照剧本非空行编号';
CREATE TABLE drama_storyboard_utterances (
 id uuid PRIMARY KEY,
 shot_id uuid NOT NULL REFERENCES drama_storyboard_shots(id) ON DELETE CASCADE,
 number integer NOT NULL,
 kind text NOT NULL CHECK(kind IN ('dialogue','os','vo')),
 speaker text NOT NULL DEFAULT '',
 text text NOT NULL,
 source_line integer NOT NULL CHECK(source_line>0)
);
COMMENT ON TABLE drama_storyboard_utterances IS '镜头内按顺序保存的台词和旁白';
COMMENT ON COLUMN drama_storyboard_utterances.id IS '声音条目主键';
COMMENT ON COLUMN drama_storyboard_utterances.shot_id IS '所属镜头';
COMMENT ON COLUMN drama_storyboard_utterances.number IS '发声顺序';
COMMENT ON COLUMN drama_storyboard_utterances.kind IS '对白、内心独白或画外音';
COMMENT ON COLUMN drama_storyboard_utterances.speaker IS '说话者';
COMMENT ON COLUMN drama_storyboard_utterances.text IS '实际台词，区别于画面说明';
COMMENT ON COLUMN drama_storyboard_utterances.source_line IS '台词来源行编号';
CREATE TABLE drama_storyboard_bindings (
 segment_id uuid NOT NULL REFERENCES drama_storyboard_segments(id) ON DELETE CASCADE,
 form_id uuid NOT NULL REFERENCES drama_asset_forms(id),
 image_id uuid REFERENCES drama_image_jobs(id),
 PRIMARY KEY(segment_id,form_id)
);
COMMENT ON TABLE drama_storyboard_bindings IS '片段引用的真实资产形象和采用图片版本';
COMMENT ON COLUMN drama_storyboard_bindings.segment_id IS '片段主键';
COMMENT ON COLUMN drama_storyboard_bindings.form_id IS '真实资产形象主键';
COMMENT ON COLUMN drama_storyboard_bindings.image_id IS '拆分时采用的图片版本，为空表示待素材';
CREATE TABLE drama_storyboard_events (
 id bigserial PRIMARY KEY,
 run_id uuid NOT NULL REFERENCES drama_storyboard_runs(id) ON DELETE CASCADE,
 message text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
COMMENT ON TABLE drama_storyboard_events IS '单集分镜真实进度与恢复事件';
COMMENT ON COLUMN drama_storyboard_events.id IS '递增事件序号';
COMMENT ON COLUMN drama_storyboard_events.run_id IS '所属运行';
COMMENT ON COLUMN drama_storyboard_events.message IS '阶段开始、接收、保存、重试或失败信息';
COMMENT ON COLUMN drama_storyboard_events.created_at IS '事件时间';
CREATE UNIQUE INDEX drama_storyboard_one_active ON drama_storyboard_runs(source_episode_id) WHERE status IN ('queued','running');
CREATE INDEX drama_storyboard_history ON drama_storyboard_runs(project_id,source_episode_id,version DESC);
CREATE INDEX drama_storyboard_event_order ON drama_storyboard_events(run_id,id);
-- +goose Down
DROP TABLE drama_storyboard_events;
DROP TABLE drama_storyboard_bindings;
DROP TABLE drama_storyboard_utterances;
DROP TABLE drama_storyboard_shot_sources;
DROP TABLE drama_storyboard_shots;
DROP TABLE drama_storyboard_segments;
DROP TABLE drama_storyboard_runs;
