-- +goose Up
ALTER TABLE drama_storyboard_segments
    ADD COLUMN lineage_id uuid NOT NULL DEFAULT gen_random_uuid(),
    ADD COLUMN segment_version integer NOT NULL DEFAULT 1 CHECK (segment_version > 0);
COMMENT ON COLUMN drama_storyboard_segments.lineage_id IS '跨分镜快照稳定的片段标识，关联片段版本及视频历史';
COMMENT ON COLUMN drama_storyboard_segments.segment_version IS '片段自身的版本号，仅片段内容更新时递增';

-- 旧数据没有片段标识。只在连续人工保存的快照中回填原文范围唯一匹配的片段。
-- +goose StatementBegin
DO $$
DECLARE snapshot record;
BEGIN
    FOR snapshot IN
        SELECT cur.id, prev.id AS previous_id
        FROM drama_storyboard_runs cur
        JOIN drama_storyboard_runs prev ON prev.project_id = cur.project_id
            AND prev.source_episode_id = cur.source_episode_id AND prev.version = cur.version - 1
        WHERE cur.phase = '人工编辑' AND cur.script_text = prev.script_text
        ORDER BY cur.version
    LOOP
        UPDATE drama_storyboard_segments target
        SET lineage_id = prior.lineage_id, segment_version = prior.segment_version + 1
        FROM drama_storyboard_segments prior
        WHERE target.run_id = snapshot.id AND prior.run_id = snapshot.previous_id
            AND target.start_line = prior.start_line AND target.end_line = prior.end_line
            AND 1 = (SELECT count(*) FROM drama_storyboard_segments s
                WHERE s.run_id = snapshot.id AND s.start_line = target.start_line AND s.end_line = target.end_line)
            AND 1 = (SELECT count(*) FROM drama_storyboard_segments s
                WHERE s.run_id = snapshot.previous_id AND s.start_line = prior.start_line AND s.end_line = prior.end_line);
    END LOOP;
END $$;
-- +goose StatementEnd
CREATE UNIQUE INDEX drama_storyboard_segment_lineage_snapshot ON drama_storyboard_segments(run_id, lineage_id);
CREATE INDEX drama_storyboard_segment_lineage_versions ON drama_storyboard_segments(lineage_id, segment_version);

-- +goose Down
DROP INDEX drama_storyboard_segment_lineage_versions;
DROP INDEX drama_storyboard_segment_lineage_snapshot;
ALTER TABLE drama_storyboard_segments DROP COLUMN segment_version, DROP COLUMN lineage_id;
