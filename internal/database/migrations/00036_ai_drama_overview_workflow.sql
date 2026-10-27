-- +goose Up
-- 旧版自动解析在概览完成后仍继续调用资产和分集模型；已得到完整概览的失败项目转为可进入下一步。
UPDATE drama_projects
SET status = 'ready', phase = 'overview_complete', error_message = '', updated_at = NOW()
WHERE status = 'failed'
  AND overview ?& ARRAY['videoStyle', 'aspectRatio', 'protagonist', 'genre', 'synopsis', 'background', 'setting']
  AND COALESCE(overview->>'synopsis', '') <> '';

-- +goose Down
-- 已修复的用户项目状态不回退。
SELECT 1;
