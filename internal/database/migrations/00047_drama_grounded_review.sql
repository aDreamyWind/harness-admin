-- +goose Up
ALTER TABLE drama_batches ADD COLUMN analysis_run_id uuid NOT NULL DEFAULT gen_random_uuid();
COMMENT ON COLUMN drama_batches.analysis_run_id IS '解析运行标识；重新解析创建新值，继续解析保留，用于隔离历史审校问题';
ALTER TABLE drama_batch_steps ADD COLUMN input_hash varchar(64) NOT NULL DEFAULT '';
COMMENT ON COLUMN drama_batch_steps.input_hash IS '工作流版本、模型及完整输入的 SHA256；输入改变时不得复用旧检查点';
ALTER TABLE drama_batch_steps DROP CONSTRAINT drama_batch_steps_stage_check;
ALTER TABLE drama_batch_steps ADD CONSTRAINT drama_batch_steps_stage_check CHECK(stage IN ('art','story','context','plan','name','director','review','repair'));
COMMENT ON COLUMN drama_batch_steps.stage IS '工作阶段：美术、原文证据、本批上下文、分集规划、命名、导演、审校、局部修正';
CREATE TABLE drama_review_issues (
 id uuid PRIMARY KEY,
 batch_id uuid NOT NULL REFERENCES drama_batches(id) ON DELETE CASCADE,
 run_id uuid NOT NULL,
 episode_number integer NOT NULL CHECK(episode_number>0),
 context_hash varchar(64) NOT NULL,
 issue_key varchar(64) NOT NULL,
 category varchar(24) NOT NULL CHECK(category IN ('fact','continuity','causality','style')),
 severity varchar(16) NOT NULL CHECK(severity IN ('blocking','suggestion')),
 message text NOT NULL,
 source_quote text NOT NULL DEFAULT '',
 script_quote text NOT NULL DEFAULT '',
 source_start integer NOT NULL CHECK(source_start>=0),
 source_end integer NOT NULL CHECK(source_end>=source_start),
 status varchar(16) NOT NULL CHECK(status IN ('open','resolved')),
 created_at timestamptz NOT NULL DEFAULT NOW(),
 updated_at timestamptz NOT NULL DEFAULT NOW(),
 UNIQUE(run_id,episode_number,context_hash,issue_key)
);
CREATE INDEX drama_review_issues_batch_run_idx ON drama_review_issues(batch_id,run_id,episode_number);
COMMENT ON TABLE drama_review_issues IS '逐集累计审校问题与已解决约束；历史运行保留，继续解析沿用';
COMMENT ON COLUMN drama_review_issues.id IS '审校问题稳定主键，复核时逐项引用';
COMMENT ON COLUMN drama_review_issues.batch_id IS '所属剧本批次';
COMMENT ON COLUMN drama_review_issues.run_id IS '所属解析运行标识';
COMMENT ON COLUMN drama_review_issues.episode_number IS '当前运行中的集序号';
COMMENT ON COLUMN drama_review_issues.context_hash IS '本集原文及生成约束的指纹，防止跨规划误用';
COMMENT ON COLUMN drama_review_issues.issue_key IS '问题依据指纹，用于重复审校幂等写入';
COMMENT ON COLUMN drama_review_issues.category IS '问题类别：事实、衔接、关键因果或表达';
COMMENT ON COLUMN drama_review_issues.severity IS '严重级别：阻断错误或非阻断建议；表达问题只能为建议';
COMMENT ON COLUMN drama_review_issues.message IS '具体问题及修正要求';
COMMENT ON COLUMN drama_review_issues.source_quote IS '直接从当前集原文引用的依据';
COMMENT ON COLUMN drama_review_issues.script_quote IS '当前剧本中的问题文字，遗漏问题可为空';
COMMENT ON COLUMN drama_review_issues.source_start IS '原文依据起点，按完整原文字符计数，从零开始';
COMMENT ON COLUMN drama_review_issues.source_end IS '原文依据终点，不包含此位置';
COMMENT ON COLUMN drama_review_issues.status IS '问题当前状态；已解决问题仍需后续回归核对';
COMMENT ON COLUMN drama_review_issues.created_at IS '首次发现时间';
COMMENT ON COLUMN drama_review_issues.updated_at IS '最近一次复核时间';
-- +goose Down
DROP TABLE drama_review_issues;
DELETE FROM drama_batch_steps WHERE stage IN ('context','repair');
ALTER TABLE drama_batch_steps DROP CONSTRAINT drama_batch_steps_stage_check;
ALTER TABLE drama_batch_steps ADD CONSTRAINT drama_batch_steps_stage_check CHECK(stage IN ('art','story','plan','name','director','review'));
COMMENT ON COLUMN drama_batch_steps.stage IS '工作阶段：美术、剧情整理、分集规划、命名、导演或审校';
ALTER TABLE drama_batch_steps DROP COLUMN input_hash;
ALTER TABLE drama_batches DROP COLUMN analysis_run_id;
