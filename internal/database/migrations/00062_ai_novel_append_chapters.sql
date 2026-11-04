-- +goose Up
ALTER TABLE novel_projects DROP CONSTRAINT novel_projects_chapter_count_check;
ALTER TABLE novel_projects ADD CONSTRAINT novel_projects_chapter_count_check CHECK (chapter_count BETWEEN 1 AND 500);
COMMENT ON TABLE novel_projects IS '按组织和作者隔离的 AI 小说作品';
COMMENT ON COLUMN novel_projects.chapter_count IS '作品计划章节总数，包含作者分批追加的章节';

-- +goose Down
ALTER TABLE novel_projects DROP CONSTRAINT novel_projects_chapter_count_check;
ALTER TABLE novel_projects ADD CONSTRAINT novel_projects_chapter_count_check CHECK (chapter_count BETWEEN 1 AND 30);
COMMENT ON TABLE novel_projects IS '按组织和作者隔离的 AI 小说作品';
COMMENT ON COLUMN novel_projects.chapter_count IS '作品计划章节总数';
