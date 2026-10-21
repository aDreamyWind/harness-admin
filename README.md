# Harness Admin

Harness Admin 是面向企业 Harness 的控制平面。项目从零构建，不依赖任何管理后台脚手架的私有 Core 或作者服务。

第一阶段已收口，当前提供：

- Go HTTP 服务与优雅停机；
- PostgreSQL、Redis 基础设施连接；
- 内嵌数据库迁移；
- 存活、就绪和系统概览接口；
- React + Ant Design 管理端外壳；
- 用户、组织、角色、会话和审计的首版数据模型；
- Argon2id 密码、服务端会话、CSRF 校验与登录限流。
- 基于角色权限的接口授权、企业用户管理、角色权限配置与部门层级维护。

第二阶段已启动，当前新增模型接入底座：

- 企业级 OpenAI 兼容供应商连接，内置 OpenAI、DeepSeek、阿里云百炼、Kimi 与 Ollama 地址预设；
- API Key 加密存储、连接检测和出站地址安全校验；
- 企业模型目录、能力标记和启停管理；
- 模型资源权限与审计追踪；
- 企业网关调用密钥，服务端保存认证哈希和加密密文，具备管理权限时可再次查看；
- OpenAI 兼容聊天补全入口，按企业模型编码路由到上游模型；
- 不保存提示词和模型回答的调用结果、耗时与 Token 用量记录。
- 企业统一登录应用登记与 OIDC 授权码登录，客户端密钥、授权码和刷新令牌均不保存明文。
- OIDC 用户访问令牌可直接调用模型网关，并以用户身份记录 Token 用量。
- 部门与用户模型策略、共享及个人额度，以及按调用身份过滤的 OpenAI 兼容模型目录。

MCP 中台开始分阶段实现。“AI 资源管理 / MCP 管理”采用左侧服务列表、右侧服务详情的布局；右侧上方展示描述、连接和接入示例，下方展示工具列表及测试。支持远程 Streamable HTTP／SSE 服务登记、修改、启停、工具发现与管理员测试调用。服务描述支持最长 10000 字的 Markdown 编辑、预览与展示；长描述默认折叠，可展开阅读全文。接入无必填参数的服务后会自动发现工具，有必填参数的服务在生成连接后自动发现，失败时保留服务或连接并提示原因。工具测试按上游输入结构展示常见参数输入项，复杂参数可切换 JSON，结果将内容与元信息分开展示。接入服务时可手动填写或导入 MCP JSON 配置，只保存传输方式、地址及参数字段定义和说明，不要求参数值；生成每条连接时按定义填写独立参数值，并加密保存。已有服务级凭证和旧连接专用凭证保留兼容，不会自动挪用或删除。连接可面向 Harness 员工或开发者应用：员工连接按明确勾选的部门和用户授权，开发者连接默认使用 Bearer 密钥，也可由管理员主动选择无鉴权。连接入口密钥可再次查看。URL 参数可能进入上游日志，优先使用 Header。调用统计从每条连接的操作入口查看；Harness 调用另记录用户及调用时部门。GitHub／本地代码部署、Stdio 运行器、对外 SSE 与 Stdio 桥接尚未交付。完整目标和实施顺序见 [MCP 中台与网关方案](docs/enterprise-skills-mcp-plan.md)。

## 技术栈

- Go、Gin、pgx、go-redis、Goose
- PostgreSQL、Redis
- React、TypeScript、Vite、Ant Design、TanStack Query

## 本地启动

```bash
cp .env.example .env
make infra-up
BOOTSTRAP_PLATFORM_ADMIN_PASSWORD='请替换为至少3位的密码' make bootstrap-platform-admin
BOOTSTRAP_ADMIN_PASSWORD='请替换为至少3位的密码' make bootstrap-admin
make seed-test-organization
make dev
```

另开一个终端启动前端：

```bash
make web-install
make web-dev
```

访问：

- 管理端：http://localhost:5173
- 平台管理：http://localhost:5173/platform/login
- 后端：http://localhost:8080
- 存活检查：http://localhost:8080/healthz
- 就绪检查：http://localhost:8080/readyz

### 为什么使用 Make

`make` 不是 Go 的替代品，它只是把项目里较长、容易写错的常用命令取一个短名字。本项目的 `make dev` 实际执行的是：

```bash
go run ./cmd/server
```

很多小型 Go 项目把 `main.go` 放在仓库根目录，所以可以直接执行 `go run .`。本项目把不同的可执行程序分别放在 `cmd/` 下，目前包括 HTTP 服务 `cmd/server` 和初始化管理员 `cmd/bootstrap-admin`，因此需要明确运行哪一个。你可以完全不用 Make，直接执行对应的 Go 命令；使用 Make 只是为了统一入口，并自动读取本地 `.env`。

常用命令可以通过 `make help` 查看。

身份上只保留两个产品概念：“平台管理员”管理全部企业，“企业管理员”只管理所属企业。平台管理员使用独立账号、独立登录入口和独立会话；创建企业时，会在同一个数据库事务中同时创建该企业的首位企业管理员。

平台管理接口：

- `POST /api/v1/platform/auth/login`
- `GET /api/v1/platform/auth/me`
- `POST /api/v1/platform/auth/logout`
- `GET /api/v1/platform/organizations`
- `POST /api/v1/platform/organizations`
- `PUT /api/v1/platform/organizations/{organizationId}/status`

`make seed-test-organization` 仅允许在 `development` 或 `test` 环境执行，会为指定企业写入一套常见企业组织树，包括集团总部、总经理办公室、人力资源、财务、产品、研发、营销、法务等 17 个部门。该命令可重复执行：已有部门会按代码更新层级、名称、排序和启用状态，不会重复创建。默认使用 `default` 企业和 `admin` 操作人，可通过 `TEST_ORG_CODE`、`TEST_ORG_ACTOR_USERNAME` 配置。

数据库迁移已合并为版本 20 的当前结构基线：全新数据库只执行 `internal/database/migrations/00020_baseline.sql`，不会逐条重放开发期间的 1～20 号结构修改。历史 SQL 保存在 `internal/database/migrations/legacy/`；迁移版本处于 1～19 的已有数据库仍沿历史路径升级到 20，已到 20 的数据库不会重建表或清理数据。后续增量迁移从 21 开始。不要手动清空已有数据库的 `goose_db_version`，也不要将基线 SQL 直接执行到已有业务库。

MCP 当前新增 `00021_mcp_services.sql` 至 `00026_mcp_connection_legacy_credentials.sql` 六条增量迁移；全新数据库执行 20 号基线后会自动执行，已有数据库也按 Goose 版本增量升级。MCP 服务和连接的内部编码由系统生成；服务只定义上游实际需要的 Header、URL 参数或路径参数，连接填写并加密保存对应的值。26 号迁移使已有连接保留历史服务级值的继承行为，而新连接不再继承。每分钟请求上限可省略，默认 60 次。远程 MCP 指向私有网络或使用 HTTP 时，必须通过 `MCP_PRIVATE_HOSTS` 显式配置精确主机名；此白名单独立于模型供应商白名单。

身份接口：

- `GET /api/v1/auth/organizations`
- `POST /api/v1/auth/login`
- `GET /api/v1/auth/me`
- `POST /api/v1/auth/logout`
- `PUT /api/v1/auth/password`
- `PUT /api/v1/auth/profile`
- `GET /api/v1/auth/sessions`
- `DELETE /api/v1/auth/sessions/{sessionId}`
- `POST /api/v1/auth/sessions/revoke-others`

用户管理接口：

- `GET /api/v1/users`
- `POST /api/v1/users`
- `PUT /api/v1/users/{userId}`
- `PATCH /api/v1/users/{userId}/status`
- `PUT /api/v1/users/{userId}/password`
- `POST /api/v1/users/{userId}/unlock`
- `PUT /api/v1/users/{userId}/roles`
- `PATCH /api/v1/users/{userId}/department`

企业设置接口：

- `GET /api/v1/organization`
- `PUT /api/v1/organization`

部门管理接口：

- `GET /api/v1/departments`
- `POST /api/v1/departments`
- `PUT /api/v1/departments/{departmentId}`
- `PATCH /api/v1/departments/{departmentId}/status`

角色权限接口：

- `GET /api/v1/roles`
- `POST /api/v1/roles`
- `PUT /api/v1/roles/{roleId}`
- `GET /api/v1/permissions`

审计追踪接口：

- `GET /api/v1/audit-events`
- `GET /api/v1/audit-events/{eventId}`

应用接入管理接口：

- `GET /api/v1/oidc/clients`
- `POST /api/v1/oidc/clients`
- `PUT /api/v1/oidc/clients/{oidcClientId}`
- `PATCH /api/v1/oidc/clients/{oidcClientId}/status`
- `POST /api/v1/oidc/clients/{oidcClientId}/rotate-secret`

企业管理员可在“AI 资源管理 / 应用接入”登记 DeepSeek Harness 等独立系统，配置登录回调地址和 `openid`、`profile`、`email`、`model.invoke`、`mcp.invoke`、`offline_access` 授权范围。现有应用如需调用 MCP，应由管理员加入 `mcp.invoke`，员工重新授权后新令牌才包含这一范围。应用类型分为：

- 桌面应用：作为 OAuth 公开客户端，不生成客户端密钥，必须使用 PKCE S256。登记 `http://127.0.0.1/oauth/callback` 后，员工电脑上的实际回调可使用随机端口。
- 服务端应用：回调地址精确匹配，客户端密钥加密保存，具备管理权限时可再次查看。

企业桌面端应预置 `OIDC_ISSUER`、客户端 ID、回调路径和模型网关地址。员工不需要自己填写这些参数，只需在桌面端点击“企业账号登录”，并在系统浏览器中完成 Harness Admin 登录。

OIDC 协议端点：

- `GET /.well-known/openid-configuration`
- `GET /api/v1/oauth2/authorize`
- `GET /api/v1/oauth2/consent`
- `POST /api/v1/oauth2/consent`
- `POST /api/v1/oauth2/token`
- `GET /api/v1/oauth2/userinfo`
- `GET /api/v1/oauth2/jwks`
- `GET /api/v1/account/authorized-applications`
- `DELETE /api/v1/account/authorized-applications/{consentId}`

协议采用 Authorization Code + PKCE S256。用户首次使用应用或应用新增申请范围时，Harness Admin 会显示独立授权确认页；已经同意且范围未增加时直接继续。用户可在“账号与安全 / 已授权应用”中撤销授权，撤销后未兑换授权码和刷新令牌立即失效，已经签发的访问令牌在最长一小时内自然到期，下次登录需要重新确认。身份令牌和访问令牌使用 RS256 签名，刷新令牌每次使用后自动轮换。`OIDC_ISSUER` 用于配置外部系统可访问的签发者地址，默认是 `http://localhost:8080`；`CONSOLE_URL` 用于配置登录页和授权确认页地址，本地默认是 `http://localhost:5173`。生产环境必须为两者配置 HTTPS 地址，并通过 `OIDC_SIGNING_PRIVATE_KEY` 直接注入 PEM 格式 RSA 私钥，或使用 `OIDC_SIGNING_PRIVATE_KEY_FILE` 指向挂载的私钥文件。两种方式只能选择一种；都未配置时，开发环境会在启动时生成临时密钥，服务重启后旧令牌会失效。

本地开发建议生成一份不入库的固定密钥：

```bash
mkdir -p .secrets
openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 -out .secrets/oidc-signing-private-key.pem
chmod 600 .secrets/oidc-signing-private-key.pem
```

然后在 `.env` 中配置 `OIDC_SIGNING_PRIVATE_KEY_FILE=.secrets/oidc-signing-private-key.pem`。`.secrets/` 已被 Git 忽略；生产环境应由密钥管理系统生成或挂载独立密钥，不要复用本地开发密钥。

模型接入接口：

- `GET /api/v1/model-providers`
- `POST /api/v1/model-providers`
- `PUT /api/v1/model-providers/{providerId}`
- `PATCH /api/v1/model-providers/{providerId}/status`
- `POST /api/v1/model-providers/{providerId}/probe`
- `GET /api/v1/models`
- `POST /api/v1/models`
- `PUT /api/v1/models/{modelId}`
- `PATCH /api/v1/models/{modelId}/status`

模型网关接口：

- `GET /api/v1/model-gateway/api-keys`
- `POST /api/v1/model-gateway/api-keys`
- `PUT /api/v1/model-gateway/api-keys/{apiKeyId}`
- `PATCH /api/v1/model-gateway/api-keys/{apiKeyId}/revoke`
- `GET /api/v1/model-gateway/calls`
- `GET /api/v1/model-gateway/usage`
- `GET /v1/models`
- `POST /v1/chat/completions`

模型策略接口：

- `GET /api/v1/model-policies`
- `PUT /api/v1/model-policies/departments`
- `PUT /api/v1/model-policies/{subjectType}/{subjectId}`
- `DELETE /api/v1/model-policies/{subjectType}/{subjectId}`

管理员在“AI 资源管理 / 模型策略”中可按组织树一次选中多个部门，批量设置策略。勾选上级部门时，界面默认勾选其现有下级，管理员可再逐个取消；只有最终勾选的部门会各自保存策略，未来新增的下级不会自动纳入。每个部门的共享额度池独立，不会把多个选中部门合并成一个池。未设置策略时，OIDC 用户仍可使用全部已启用模型。用户只受所属部门的策略和自身策略约束，不受上级部门策略约束；用户策略可覆盖所属部门的模型范围。部门月度 Token 额度可选择由本部门成员共享，或让每名成员各自享有同样上限；部门和用户的每分钟请求上限都按每名用户独立计算。批量保存为单个事务，失败时不产生部分策略。月度 Token 采用软上限：已开始的并发请求允许完成，记账后若超过额度会在该条调用记录上标记，后续请求被拒绝。用量按上游返回的 `usage` 记录；未返回 `usage` 的调用明确显示“用量未知”，不计入已统计 Token，也不误报为零消耗。删除策略会清除该策略累计的月度用量。

“AI 资源管理 / 模型网关 / 用量统计”可按 UTC 月份查看部门、用户或模型维度的调用次数、已统计 Token、用量未知次数与超额次数。部门和用户视图只统计 OIDC 用户调用；模型视图还包含调用密钥。部门维度使用调用发生时记录的部门标识，历史未记录部门的调用单列为“未记录部门”。

“AI 资源管理 / 模型网关 / 站内预警”的“后台定时刷新”按企业保存，默认关闭。关闭时只在管理员打开站内预警页签、切换查询条件或手动点击“刷新”时计算当前 UTC 月预警；开启后服务端每分钟自动计算当前 UTC 月，页面不必保持打开。多实例由 PostgreSQL 锁协调同一轮计算；不依赖真实 Harness 联调，也不发送邮件等外部通知。部门共享额度、用户额度、调用密钥额度达到 80% 时提示，达到 100% 时标为紧急；部门按人独立额度按每位成员分别判断，绝不把部门合计用量与单人上限比较。一个模型当月成功调用至少 5 次未返回用量，且占成功调用至少 20% 时提示。预警按企业、月份和对象去重；管理员可标记知悉，升级到紧急或解除后重新出现时会重新变成待查看。额度用量仅包含上游明确报告的 Token，未报告的调用仍显示为未知。历史月份只读取当时保存的预警，不用后来变更的策略重新计算。

供应商连接可按需开启“请求流式 Token 用量”：网关仅在流式请求中补充 `stream_options.include_usage=true`，并保留其他流式选项。此项默认关闭，须先确认上游兼容；上游若拒绝该参数，网关不会自动重试，以免重复计费。即使开启，上游未返回 `usage` 时仍记为“用量未知”。

Harness 使用包含 `model.invoke` scope 的 OIDC 访问令牌请求 `GET /v1/models`，即可获得当前用户有权使用、且模型和供应商均已启用的模型编码。网关调用密钥也可使用此接口，按密钥自身模型范围过滤。

调用密钥可配置每分钟请求上限、按 UTC 自然月统计的 Token 额度和允许访问的模型编码范围。调用记录支持按调用密钥、模型编码、调用结果和发生时间筛选，并使用服务端分页。

无需真实 Harness 即可运行网关模拟上游测试：`go test ./internal/httpserver -run '^TestGatewayE2E' -count=1`。常规测试覆盖真实 HTTP 转发、SSE 首块实时送达、分块工具调用、多模态请求透传以及用量已知/未知记录。设置 `HARNESS_TEST_WRITE_DATABASE_URL` 为已完成迁移的可写测试库连接后，同一命令还会运行并发软额度集成测试；测试创建独立企业、供应商、模型和密钥，并在结束时清理这些数据。不要将该变量指向生产数据库。

调用方可以使用创建密钥时返回的完整调用密钥，或包含 `model.invoke` scope 的 OIDC 用户访问令牌访问 OpenAI 兼容入口，并把模型目录中的“模型编码”填入 `model`：

```bash
curl http://localhost:8080/v1/chat/completions \
  -H 'Authorization: Bearer 请替换为调用密钥' \
  -H 'Content-Type: application/json' \
  -d '{"model":"请替换为模型编码","messages":[{"role":"user","content":"你好"}]}'
```

供应商 API Key、OIDC 服务端应用客户端密钥和模型网关调用密钥使用 `MODEL_CREDENTIAL_MASTER_KEY` 加密保存；认证仍使用不可逆哈希。具备相应管理权限的用户可以再次查看密钥，每次查看都会记录审计事件。生产环境必须设置独立且稳定的 Base64 编码 32 字节密钥；更换该主密钥会导致历史密文无法解密。私有网络或 HTTP 模型服务必须通过 `MODEL_PROVIDER_PRIVATE_HOSTS` 显式配置精确主机名；连接检测不跟随重定向，也不会发送业务数据。

审计查询按当前登录企业隔离，需要 `audit.event.read` 权限，支持按关键字、动作、结果和发生时间筛选。无权限访问管理接口时也会写入一条 `denied` 审计事件。

接口契约见 [`api/openapi.yaml`](api/openapi.yaml)。

登录请求需要提供 `organizationCode`、`username` 和 `password`。登录成功后服务端写入仅限 `/api`、前端不可读取的 HttpOnly 会话 Cookie，同时写入同源前端可读取的 `harness_admin_csrf` Cookie。管理端会在退出及后续写接口中自动把该值放入 `X-CSRF-Token` 请求头；非浏览器客户端也可以使用登录响应中的 `csrfToken`。登录和当前用户响应同时返回由角色汇总得到的 `permissionCodes`，前端据此隐藏无权访问的菜单和管理操作，后端仍会对每次请求执行最终权限校验。

当前账号可在“账号与安全”中修改显示名称、邮箱和密码，并管理登录设备。自己修改密码会保留当前会话并吊销其他会话；管理员为其他用户重置密码会吊销该用户的全部会话。密码最少 3 位。

同一企业账号在 15 分钟内连续登录失败 5 次后会被锁定，并立即吊销该账号的全部现有会话。拥有 `iam.user.write` 权限的管理员可在用户目录中解锁账号；解锁会同时清除 Redis 中的登录失败计数，使用户可以立即重新登录。

## 验证

```bash
make test
make build
make web-build
```

需要核对合并基线时，可在具备 `CREATE DATABASE` 权限的专用测试 PostgreSQL 实例上设置 `HARNESS_TEST_MIGRATION_ADMIN_URL`，运行 `go test ./internal/database -run '^TestMigrateBaselineMatchesLegacy$' -count=1`。测试会自动创建并删除两个独立数据库，对比新库基线和历史迁移的最终结构、权限数据，并检查新库只执行一个迁移及管理员权限。不要把此变量指向生产实例。

## 第一阶段收口范围

第一阶段已经完成身份、组织、权限与审计底座，包括：

- 平台管理员独立登录、企业列表，以及创建企业和首位企业管理员；
- 企业登录、服务端会话、CSRF、登录限流、账号锁定和设备管理；
- 用户、部门、角色与权限管理，以及企业资料和个人资料维护；
- 企业级数据隔离、接口权限校验和审计追踪；
- PostgreSQL 迁移与中文注释、Redis 缓存、OpenAPI 契约及后端自动化测试；
- React 管理端、响应式布局、侧边栏折叠和统一的企业编码、部门层级展示。

第二阶段已形成“供应商连接 → 模型目录 → 调用身份 → OpenAI 兼容网关 → 调用记录”的闭环，并支持调用密钥、部门和用户的模型范围、请求限流和月度 Token 额度。OIDC 用户可通过 `/v1/models` 读取自身可用模型。当前路由保持一个模型编码对应一个上游模型，不包含权重路由、自动故障转移、Skills、审批和通用执行环境；MCP 的已交付范围及未完成项见上文。
