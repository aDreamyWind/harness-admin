-- +goose Up
ALTER TABLE drama_storyboard_runs ADD COLUMN pacing_target integer NOT NULL DEFAULT 0 CHECK(pacing_target>=0);
ALTER TABLE drama_storyboard_runs DROP CONSTRAINT drama_storyboard_runs_status_check;
ALTER TABLE drama_storyboard_runs ADD CONSTRAINT drama_storyboard_runs_status_check CHECK(status IN ('queued','running','failed','ready','confirmed','partial'));
COMMENT ON TABLE drama_storyboard_runs IS '单集分镜独立版本、来源快照及仅优化指定片段的运行信息';
COMMENT ON COLUMN drama_storyboard_runs.pacing_target IS '只优化的片段编号，零表示普通整集拆分或继续；其他未完成片段保持原样';
COMMENT ON COLUMN drama_storyboard_runs.status IS '排队、进行中、失败、就绪、已确认或所选片段处理完但整集尚未完成';
-- +goose Down
UPDATE drama_storyboard_runs SET status='failed' WHERE status='partial';
ALTER TABLE drama_storyboard_runs DROP CONSTRAINT drama_storyboard_runs_status_check;
ALTER TABLE drama_storyboard_runs ADD CONSTRAINT drama_storyboard_runs_status_check CHECK(status IN ('queued','running','failed','ready','confirmed'));
ALTER TABLE drama_storyboard_runs DROP COLUMN pacing_target;
