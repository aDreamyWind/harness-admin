-- +goose Up
ALTER TABLE drama_projects
    ADD COLUMN protagonist text NOT NULL DEFAULT '',
    ADD COLUMN genre text NOT NULL DEFAULT '',
    ADD COLUMN synopsis text NOT NULL DEFAULT '',
    ADD COLUMN background text NOT NULL DEFAULT '',
    ADD COLUMN setting text NOT NULL DEFAULT '';
COMMENT ON COLUMN drama_projects.protagonist IS '概览中的主角姓名或稳定称谓';
COMMENT ON COLUMN drama_projects.genre IS '概览中的故事类型';
COMMENT ON COLUMN drama_projects.synopsis IS '概览中的故事梗概';
COMMENT ON COLUMN drama_projects.background IS '概览中的故事背景';
COMMENT ON COLUMN drama_projects.setting IS '概览中的故事设定';
UPDATE drama_projects SET
    style = CASE WHEN NULLIF(overview->>'videoStyle', '') IS NOT NULL AND length(overview->>'videoStyle') <= 80 THEN overview->>'videoStyle' ELSE style END,
    aspect_ratio = CASE WHEN overview->>'aspectRatio' IN ('9:16', '16:9') THEN overview->>'aspectRatio' ELSE aspect_ratio END,
    protagonist = COALESCE(overview->>'protagonist', ''),
    genre = COALESCE(overview->>'genre', ''),
    synopsis = COALESCE(overview->>'synopsis', ''),
    background = COALESCE(overview->>'background', ''),
    setting = COALESCE(overview->>'setting', '');
ALTER TABLE drama_projects DROP COLUMN overview;

-- +goose Down
ALTER TABLE drama_projects ADD COLUMN overview jsonb NOT NULL DEFAULT '{}'::jsonb;
COMMENT ON COLUMN drama_projects.overview IS '兼容旧版本的项目概览字段';
UPDATE drama_projects SET overview = CASE
    WHEN protagonist = '' AND genre = '' AND synopsis = '' AND background = '' AND setting = '' THEN '{}'::jsonb
    ELSE jsonb_build_object('videoStyle', style, 'aspectRatio', aspect_ratio, 'protagonist', protagonist,
        'genre', genre, 'synopsis', synopsis, 'background', background, 'setting', setting)
END;
ALTER TABLE drama_projects
    DROP COLUMN protagonist,
    DROP COLUMN genre,
    DROP COLUMN synopsis,
    DROP COLUMN background,
    DROP COLUMN setting;
