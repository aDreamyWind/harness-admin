# AI 漫剧数据模型待办

- 项目概览使用 `drama_projects` 的明确字段保存：`style`、`aspect_ratio`、`protagonist`、`genre`、`synopsis`、`background`、`setting`。接口继续使用 `overview` 对象传输，由仓储层组装；AI 重新解析成功后整体覆盖概览字段，失败时保留上次结果。
- 后续开发角色、场景、道具和剧本／视频分集时，将当前 `characters`、`scenes`、`props`、`episodes` JSONB 列迁移为有项目外键、稳定 ID、顺序及必要状态字段的独立表。具体字段、关联关系、迁移与 API 在对应功能实现时细化，届时不要继续向这些 JSONB 列追加业务数据。
