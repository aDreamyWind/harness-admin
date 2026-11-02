# 模型调用原始日志

`model_gateway_calls` 保存模型、调用身份、状态、耗时和 Token 用量等索引信息；`model_call_logs` 以 `call_id` 一对一关联，保存实际发给供应商的请求正文、收到的原始响应正文、入口、操作及失败详情。漫剧解析、网关聊天、图片、视频生成和视频任务查询均写入。日志与用量记录在同一个数据库事务提交。

请求和响应以 `bytea` 原样保存，不记录 `Authorization` 等认证请求头。无响应的超时或网络失败，响应正文为空，`error_message` 保存错误。网关响应超过 32 MiB 时只保存前 32 MiB，并将 `response_truncated` 置为 `true`；现有非流式接口本身也不接受超过此限制的响应。请求正文可能包含用户原文或图片数据，排查时应按企业权限访问数据库，不要把正文写入普通应用日志。

排查某次调用时，可通过现有调用记录的 `request_id` 查询：

```sql
SELECT c.created_at, c.request_id, c.model_code, c.result, c.http_status,
       l.source, l.operation, l.error_message, l.response_truncated,
       convert_from(l.request_body, 'UTF8') AS request_text,
       convert_from(l.response_body, 'UTF8') AS response_text
FROM model_gateway_calls c
JOIN model_call_logs l ON l.call_id = c.id
WHERE c.organization_id = $1 AND c.request_id = $2;
```

历史调用在该表建立前没有原始正文，无法追补。


迁移52为漫剧文字调用增加明确字段：`workflow_stage`、`reasoning_policy`、`requested_at`、`headers_at`、`first_reasoning_at`、`first_content_at`、`finished_at`和`reasoning_tokens`。时间与用量未观测时为空；供应商明确报告0才记录0。其他入口尚未观测的记录和旧记录不返回`timing`，不影响原始请求/响应保存。

`GET /api/v1/model-gateway/calls`返回可选的`timing`，沿用企业权限隔离，前端调用记录可展开查看。`latencyMs`统计本次请求至响应读取完成（或失败），不含后续调用日志事务保存；检查和业务保存用时记在对应漫剧阶段事件。首次正文时间包含供应商等待和思考，不能直接当作网络耗时。思考Token是否缺失与思考是否关闭是两个不同概念，诊断时应同时查看策略及供应商报告值。
