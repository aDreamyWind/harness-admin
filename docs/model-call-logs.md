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
