# 图片与视频模型网关

模型目录项和供应商连接都必须启用。调用身份可以使用网关密钥，或带 `model.invoke` scope 的 OIDC 用户令牌；模型范围、每分钟请求限制、调用记录与对话入口相同。调用体的 `model` 使用企业模型编码，网关会换成供应商侧模型 ID。

## 图片生成

`POST /v1/images/generations` 接受 OpenAI 兼容图片生成 JSON 请求，必填 `model` 和 `prompt`。当前支持 OpenAI 兼容与阿里云百炼类型的供应商连接。对于百炼旧公共域名 `dashscope.aliyuncs.com` 和 `dashscope-intl.aliyuncs.com`，网关会转换到 DashScope 原生同步图片接口，并把图片地址转换回 `data[].url`；业务空间专属域名使用其 OpenAI 兼容图片接口。`image` 参考图、`size`、`n` 等百炼参数在旧公共域名下会按原生协议转换。其他供应商需支持其连接地址下的 `/images/generations`。例如：

```json
{"model":"企业图片模型编码","prompt":"山间日出","size":"1024x1024","n":1}
```

图片响应包含 `data[].url`。百炼图片结果 URL 有时效，业务系统应在有效期内自行保存结果。目录中启用图片模型只代表网关允许路由，实际调用仍取决于供应商密钥、地域及模型授权。
同步图片生成可能耗时较长；网关最长等待 10 分钟，调用方也应设置足够长的请求超时时间。

## 视频生成

`POST /v1/videos/generations` 当前支持阿里云百炼类型的供应商连接。请求体采用百炼异步视频接口的 `input` 和 `parameters` 结构，例如：

```json
{"model":"企业视频模型编码","input":{"prompt":"海浪拍打礁石"},"parameters":{"resolution":"720P","duration":5}}
```

网关会调用供应商的 `/api/v1/services/aigc/video-generation/video-synthesis`，并在响应的 `output.task_id` 中返回**网关任务 ID**。用 `GET /v1/videos/tasks/{taskId}` 查询任务状态和结果。只有创建该任务的同一调用密钥或同一 OIDC 用户可以查询；不同身份看到 404。视频结果 URL 由供应商提供，也可能限时有效。视频任务提交会保存归属映射并写入审计事件。

图片、视频调用通常不返回 Token 数量，因此不计入月度 Token 上限；每分钟请求限制仍适用。网关会在调用记录中标明模型类型，未知 Token 用量预警仅针对对话调用。当前没有图片或视频费用额度管理，也不代存生成结果。

联调供应商拒绝时，网关保留上游状态码；百炼即使对非流式请求以 `event:error` 返回错误，网关也会转换为包含 `code`、`message` 和 `request_id` 的 JSON，并在调用记录中保存错误码。百炼密钥、模型和连接地址须属于同一地域；出现 403 时，应根据这些字段核对 API Key 的自定义模型范围、IP 白名单及业务空间授权。

供应商协议依据：[百炼图片生成接口](https://help.aliyun.com/zh/model-studio/qwen-image-generation-and-editing-api-reference)、[百炼视频生成接口](https://help.aliyun.com/zh/model-studio/happyhorse-text-to-video-api-reference)。
