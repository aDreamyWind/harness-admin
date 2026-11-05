# 开发与部署指南

Docker 一键演示见[项目首页](../README.md)。接口参数、响应和认证要求以 [OpenAPI 契约](../api/openapi.yaml)为准。

## 本地开发

准备 Go 1.26、Node.js 22.12 或更新的 LTS 版本、pnpm、Make，以及已启动的 Docker 和 Docker Compose。在项目根目录执行：

```bash
cp .env.example .env
make infra-up
```

在 `.env` 中设置 `BOOTSTRAP_ADMIN_PASSWORD`。等待 `docker compose ps` 显示 PostgreSQL 和 Redis 均为 `healthy`，再初始化企业管理员并启动后端：

```bash
make bootstrap-admin
make dev
```

另开一个终端，在项目根目录执行 `make web-install` 和 `make web-dev`。访问[企业管理端](http://localhost:5173)，选择「默认企业」，使用 `admin` 和刚设置的密码登录。

如需平台管理员，在 `.env` 中设置 `BOOTSTRAP_PLATFORM_ADMIN_PASSWORD`，执行 `make bootstrap-platform-admin`，然后用 `platform-admin` 登录[平台管理端](http://localhost:5173/platform/login)。初始化完成后，可从 `.env` 移除两个初始化密码。

正式部署时使用独立的数据库凭据、凭据加密主密钥和 OIDC 签名密钥，并配置 HTTPS。

## 代码结构

项目采用 Go 模块化单体，管理端使用 React、TypeScript、Vite 和 Ant Design。具体开发约定见 [AGENTS.md](../AGENTS.md)。

| 路径 | 用途 |
| --- | --- |
| `cmd/` | HTTP 服务、管理员初始化和演示组织树命令 |
| `internal/` | 业务模块、HTTP 接入和基础设施实现 |
| `internal/database/migrations/` | PostgreSQL 迁移 |
| `web/` | 管理端源码 |
| `api/openapi.yaml` | HTTP API 契约 |

### 常用命令

`make dev` 加载本地 `.env` 并执行：

```bash
go run ./cmd/server
```

直接运行 `cmd/` 下的服务或初始化程序时，请自行提供环境变量。运行 `make help` 可查看其他命令。

`make seed-test-organization` 仅允许在 `development` 或 `test` 环境执行，会为指定企业写入一套常见企业组织树，包括集团总部、总经理办公室、人力资源、财务、产品、研发、营销、法务等 17 个部门。该命令可重复执行：已有部门会按代码更新层级、名称、排序和启用状态，不会重复创建。默认使用 `default` 企业和 `admin` 操作人，可通过 `TEST_ORG_CODE`、`TEST_ORG_ACTOR_USERNAME` 配置。

## 数据库迁移

新数据库从 `internal/database/migrations/00020_baseline.sql` 开始，再执行后续增量迁移。版本处于 1～19 的已有数据库沿 `internal/database/migrations/legacy/` 中的历史路径升级到 20。已有版本 20 及以上的数据库只执行尚未应用的增量迁移。保留已有数据库的 `goose_db_version`，通过迁移程序升级。

## 身份、组织与审计

身份上只保留两个产品概念：“平台管理员”管理全部企业，“企业管理员”只管理所属企业。平台管理员使用独立账号、独立登录入口和独立会话；创建企业时，会在同一个数据库事务中同时创建该企业的首位企业管理员。

登录请求需要提供 `organizationCode`、`username` 和 `password`。登录成功后服务端写入仅限 `/api`、前端不可读取的 HttpOnly 会话 Cookie，同时写入同源前端可读取的 `harness_admin_csrf` Cookie。管理端会在退出及后续写接口中自动把该值放入 `X-CSRF-Token` 请求头；非浏览器客户端也可以使用登录响应中的 `csrfToken`。登录和当前用户响应同时返回由角色汇总得到的 `permissionCodes`，前端据此隐藏无权访问的菜单和管理操作，后端仍会对每次请求执行最终权限校验。

当前账号可在“账号与安全”中修改显示名称、邮箱和密码，并管理登录设备。自己修改密码会保留当前会话并吊销其他会话；管理员为其他用户重置密码会吊销该用户的全部会话。密码最少 3 位。

同一企业账号在 15 分钟内连续登录失败 5 次后会被锁定，并立即吊销该账号的全部现有会话。拥有 `iam.user.write` 权限的管理员可在用户目录中解锁账号；解锁会同时清除 Redis 中的登录失败计数，使用户可以立即重新登录。

审计查询按当前登录企业隔离，需要 `audit.event.read` 权限，支持按关键字、动作、结果和发生时间筛选。无权限访问管理接口时也会写入一条 `denied` 审计事件。

## OIDC 应用接入

企业管理员可在“AI 资源管理 / 应用接入”登记 DeepSeek Harness 等独立系统，配置登录回调地址和 `openid`、`profile`、`email`、`model.invoke`、`mcp.invoke`、`offline_access` 授权范围。现有应用如需调用 MCP，应由管理员加入 `mcp.invoke`，员工重新授权后新令牌才包含这一范围。应用类型分为：

- 桌面应用：作为 OAuth 公开客户端，不生成客户端密钥，必须使用 PKCE S256。登记 `http://127.0.0.1/oauth/callback` 后，员工电脑上的实际回调可使用随机端口。
- 服务端应用：回调地址精确匹配，客户端密钥加密保存，具备管理权限时可再次查看。

企业桌面端应预置 `OIDC_ISSUER`、客户端 ID、回调路径和模型网关地址。员工不需要自己填写这些参数，只需在桌面端点击“企业账号登录”，并在系统浏览器中完成 Harness Admin 登录。

协议采用 Authorization Code + PKCE S256。用户首次使用应用或应用新增申请范围时，Harness Admin 会显示独立授权确认页；已经同意且范围未增加时直接继续。用户可在“账号与安全 / 已授权应用”中撤销授权，撤销后未兑换授权码和刷新令牌立即失效，已经签发的访问令牌在最长一小时内自然到期，下次登录需要重新确认。身份令牌和访问令牌使用 RS256 签名，刷新令牌每次使用后自动轮换。`OIDC_ISSUER` 用于配置外部系统可访问的签发者地址，默认是 `http://localhost:8080`；`CONSOLE_URL` 用于配置登录页和授权确认页地址，本地默认是 `http://localhost:5173`。生产环境必须为两者配置 HTTPS 地址，并通过 `OIDC_SIGNING_PRIVATE_KEY` 直接注入 PEM 格式 RSA 私钥，或使用 `OIDC_SIGNING_PRIVATE_KEY_FILE` 指向挂载的私钥文件。两种方式只能选择一种；都未配置时，开发环境会在启动时生成临时密钥，服务重启后旧令牌会失效。

本地开发建议生成一份不入库的固定密钥：

```bash
mkdir -p .secrets
openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 -out .secrets/oidc-signing-private-key.pem
chmod 600 .secrets/oidc-signing-private-key.pem
```

然后在 `.env` 中配置 `OIDC_SIGNING_PRIVATE_KEY_FILE=.secrets/oidc-signing-private-key.pem`。`.secrets/` 已被 Git 忽略；生产环境应由密钥管理系统生成或挂载独立密钥，不要复用本地开发密钥。

## 模型网关与策略

图片与视频调用方式、适用供应商及任务归属规则见[图片与视频模型网关](model-media-gateway.md)。

### 模型范围与额度

管理员在“AI 资源管理 / 模型策略”中可按组织树一次选中多个部门，批量设置策略。勾选上级部门时，界面默认勾选其现有下级，管理员可再逐个取消；只有最终勾选的部门会各自保存策略，未来新增的下级不会自动纳入。每个部门的共享额度池独立，不会把多个选中部门合并成一个池。未设置策略时，OIDC 用户仍可使用全部已启用模型。用户只受所属部门的策略和自身策略约束，不受上级部门策略约束；用户策略可覆盖所属部门的模型范围。部门月度 Token 额度可选择由本部门成员共享，或让每名成员各自享有同样上限；部门和用户的每分钟请求上限都按每名用户独立计算。批量保存为单个事务，失败时不产生部分策略。月度 Token 采用软上限：已开始的并发请求允许完成，记账后若超过额度会在该条调用记录上标记，后续请求被拒绝。用量按上游返回的 `usage` 记录；未返回 `usage` 的调用明确显示“用量未知”，不计入已统计 Token，也不误报为零消耗。删除策略会清除该策略累计的月度用量。

### 用量与预警

“AI 资源管理 / 模型网关 / 用量统计”可按 UTC 月份查看部门、用户或模型维度的调用次数、已统计 Token、用量未知次数与超额次数。部门和用户视图只统计 OIDC 用户调用；模型视图还包含调用密钥。部门维度使用调用发生时记录的部门标识，历史未记录部门的调用单列为“未记录部门”。

“AI 资源管理 / 模型网关 / 站内预警”的“后台定时刷新”按企业保存，默认关闭。关闭时只在管理员打开站内预警页签、切换查询条件或手动点击“刷新”时计算当前 UTC 月预警；开启后服务端每分钟自动计算当前 UTC 月，页面不必保持打开。多实例由 PostgreSQL 锁协调同一轮计算；不依赖真实 Harness 联调，也不发送邮件等外部通知。部门共享额度、用户额度、调用密钥额度达到 80% 时提示，达到 100% 时标为紧急；部门按人独立额度按每位成员分别判断，绝不把部门合计用量与单人上限比较。一个模型当月成功调用至少 5 次未返回用量，且占成功调用至少 20% 时提示。预警按企业、月份和对象去重；管理员可标记知悉，升级到紧急或解除后重新出现时会重新变成待查看。额度用量仅包含上游明确报告的 Token，未报告的调用仍显示为未知。历史月份只读取当时保存的预警，不用后来变更的策略重新计算。

### 网关接入

供应商连接可按需开启“请求流式 Token 用量”：网关仅在流式请求中补充 `stream_options.include_usage=true`，并保留其他流式选项。此项默认关闭，须先确认上游兼容；上游若拒绝该参数，网关不会自动重试，以免重复计费。即使开启，上游未返回 `usage` 时仍记为“用量未知”。

Harness 使用包含 `model.invoke` scope 的 OIDC 访问令牌请求 `GET /v1/models`，即可获得当前用户有权使用、且模型和供应商均已启用的模型编码。网关调用密钥也可使用此接口，按密钥自身模型范围过滤。

调用密钥可配置每分钟请求上限、按 UTC 自然月统计的 Token 额度和允许访问的模型编码范围。调用记录支持按调用密钥、模型编码、调用结果和发生时间筛选，并使用服务端分页。

调用方可以使用创建密钥时返回的完整调用密钥，或包含 `model.invoke` scope 的 OIDC 用户访问令牌访问 OpenAI 兼容入口，并把模型目录中的“模型编码”填入 `model`：

```bash
curl http://localhost:8080/v1/chat/completions \
  -H 'Authorization: Bearer 请替换为调用密钥' \
  -H 'Content-Type: application/json' \
  -d '{"model":"请替换为模型编码","messages":[{"role":"user","content":"你好"}]}'
```

供应商 API Key、OIDC 服务端应用客户端密钥和模型网关调用密钥使用 `MODEL_CREDENTIAL_MASTER_KEY` 加密保存；认证仍使用不可逆哈希。具备相应管理权限的用户可以再次查看密钥，每次查看都会记录审计事件。生产环境必须设置独立且稳定的 Base64 编码 32 字节密钥；更换该主密钥会导致历史密文无法解密。私有网络或 HTTP 模型服务必须通过 `MODEL_PROVIDER_PRIVATE_HOSTS` 显式配置精确主机名；连接检测不跟随重定向，也不会发送业务数据。

## MCP 与企业 Skills

在「AI 资源管理 → MCP 管理」中可以登记、修改、启停远程 Streamable HTTP 或 SSE 服务，并查看连接和接入示例、发现及测试工具。

远程服务描述支持最长 10000 字的 Markdown 编辑、预览与展示；托管服务直接展示最多 64 KiB 的 README.md，长描述默认折叠。接入无必填参数的服务后会自动发现工具，有必填参数的服务在生成连接后自动发现。工具测试按上游输入结构展示常见参数输入项，复杂参数可切换 JSON，结果将内容与元信息分开展示。接入服务时可手动填写或导入 MCP JSON 配置，只保存传输方式、地址及参数字段定义和说明；生成每条连接时按定义填写独立参数值，并加密保存。已有服务级凭证和旧连接专用凭证保留兼容。连接可面向 Harness 员工或开发者应用：员工连接按明确勾选的部门和用户授权，开发者连接默认使用 Bearer 密钥，也可由管理员主动选择无鉴权。连接入口密钥可再次查看。调用统计从每条连接的操作入口查看；Harness 调用另记录用户及调用时部门。

源码托管支持完整的公开 GitHub／Gitee 仓库 HTTPS 地址：版本可填分支、标签或提交号，也可留空使用默认分支，系统解析并记录固定提交号；也支持上传 `.tar.gz`。两类源码均读取包根目录 `README.md` 作为服务描述，缺失或无效时明确报错，成功构建及回退时描述跟随版本。系统生成固定 Dockerfile 构建 Node.js/Python Stdio MCP，记录版本与构建结果，提供启动、停止、重启、容器状态、日志和镜像回退；托管调用仍经现有 MCP 连接网关。源码部署默认关闭，首版运行容器断网且不注入连接级环境变量，只适用于无需外部网络与运行时密钥的 Stdio MCP；对外提供 Streamable HTTP 与旧版 HTTP+SSE 两种入口，支持工具、资源及提示词的目录和读取／调用；旧版 SSE 使用 `/mcp/{code}/sse` 建立事件流，并向 endpoint 事件返回的 `/mcp/{code}/messages?sessionid=…` 提交消息，GET 与每次 POST 均须携带相同用途的有效鉴权。上游 MCP 声明 `io.modelcontextprotocol/skills` 时，网关还提供 `skills/list`、`skills/get`，并通过 `resources/read` 读取 `skill://` 文件。企业自有 Skill 的 `.tar.gz` 导入、版本审阅、发布、授权与专用 MCP 分发已实现。Stdio 本地桥接及更广泛的托管能力尚未交付。完整目标和实施顺序见 [MCP 中台与网关方案](enterprise-skills-mcp-plan.md)。

Skills 管理可在“AI 资源管理 / Skills 管理”从本地 `.zip`、`.tar.gz` 文件或公开 GitHub 仓库导入、查看完整 frontmatter 与文件清单、预览文本文件、发布或停用不可变版本，并对当前部门和员工明确授权。GitHub 导入支持可选分支、标签、提交号和 Skill 子目录，保存实际固定提交号。导入不会运行包内脚本；同名重复导入形成新版本。获授权的员工使用带 `mcp.invoke` scope 的 OIDC 令牌访问 `/api/v1/skills/directory` 或企业专用 `/enterprise-skills/mcp`，后者提供 `skills/list`、`skills/get` 与 `resources/read`；每次读取重新检查发布状态和授权。Web／桌面 Host 的真实加载与批准流程仍需联调。具体边界见 [企业 Skills 接入设计](enterprise-skill-management-plan.md)。

远程 MCP 服务不会出现“部署与运行／构建新版本”；这些操作只属于从 GitHub／Gitee 仓库或本地代码包导入的 Stdio 托管服务。MCP 连接列表将“启用状态”和“连通状态”分开显示：打开页面或手动刷新时，对当前页连接分别进行一次上游 MCP 握手及工具列表检查，不执行具体工具，也不计入工具调用统计；异常或工具列表变化时需要检查参数、上游运行状态或重新发现工具。此检查不替代员工授权验证或具体工具调用测试。

### MCP 数据迁移与网络配置

MCP 的 21～28 号迁移包括 `00021_mcp_services.sql` 至 `00028_mcp_repository_sources.sql` 八条增量迁移；全新数据库执行 20 号基线后会自动执行，已有数据库也按 Goose 版本增量升级。MCP 服务和连接的内部编码由系统生成；远程服务只定义上游实际需要的 Header、URL 参数或路径参数，连接填写并加密保存对应的值。26 号迁移使已有连接保留历史服务级值的继承行为，而新连接不再继承。27 号迁移添加托管服务来源、容器状态和构建版本记录；28 号迁移补充 Gitee 来源与版本 README 描述。每分钟请求上限可省略，默认 60 次。远程 MCP 指向私有网络或使用 HTTP 时，必须通过 `MCP_PRIVATE_HOSTS` 显式配置精确主机名；此白名单独立于模型供应商白名单。

### 源码托管

源码托管部署须由运维先提供可用的 Docker CLI 与隔离的 Docker 守护进程，再设置 `MCP_DEPLOY_ENABLED=true`。不要把宿主机高权限 Docker Socket 直接挂进对外运行的 Admin 容器：控制 Docker 守护进程接近控制宿主机。生产环境 `MCP_NODE_IMAGE`、`MCP_PYTHON_IMAGE` 必须固定到镜像摘要。`MCP_BUILD_NETWORK=none` 是默认值；若构建时要从 npm/PyPI 安装依赖，可在独立受控构建环境中显式使用 `default`（BuildKit 构建网络），运行容器仍始终使用 `--network none`。Node.js 项目可留空运行入口：服务端读取根目录 `package.json` 的简单 `node 文件` 启动命令或 `main`，在隔离构建阶段安装开发依赖、断网执行 `npm run build --if-present`，然后验证入口是否生成、裁剪开发依赖。无 `package-lock.json` 时会生成锁文件并保存在构建镜像中，但重新构建不保证依赖版本一致；生产仓库宜提交锁文件。构建采用系统生成的 Dockerfile，不采用代码包提供的 Dockerfile；拒绝归档中的符号链接、硬链接、特殊文件、路径穿越及超限内容。容器以非 root 用户、只读文件系统、受限 CPU/内存/进程数运行。源码导入请求创建构建记录后立即返回 202；仓库下载和容器构建在后台执行，最多运行约十分钟，管理页每三秒刷新构建状态，构建日志保留最后 64 KiB；上传包本身不长期保存，回退依赖 Docker 中保留的历史镜像。管理页分别显示容器进程状态与 MCP 握手/工具发现健康结果；要确认具体工具调用仍需测试。执行第三方 `build` 脚本仍意味着执行第三方代码，只能在隔离 Docker 守护进程中部署可信仓库；首版不支持需要运行时出网、连接级环境变量、仓库私有凭据或自定义 Dockerfile 的 MCP，不能把本版本用于不受信任的任意第三方代码公共托管平台。

## 验证

```bash
make test
make build
make web-build
```

需要核对合并基线时，可在具备 `CREATE DATABASE` 权限的专用测试 PostgreSQL 实例上设置 `HARNESS_TEST_MIGRATION_ADMIN_URL`，运行 `go test ./internal/database -run '^TestMigrateBaselineMatchesLegacy$' -count=1`。测试会自动创建并删除两个独立数据库，对比新库基线和历史迁移的最终结构、权限数据，并检查新库只执行一个迁移及管理员权限。不要把此变量指向生产实例。

无需真实 Harness 即可运行网关模拟上游测试：`go test ./internal/httpserver -run '^TestGatewayE2E' -count=1`。常规测试覆盖真实 HTTP 转发、SSE 首块实时送达、分块工具调用、多模态请求透传以及用量已知/未知记录。设置 `HARNESS_TEST_WRITE_DATABASE_URL` 为已完成迁移的可写测试库连接后，同一命令还会运行并发软额度集成测试；测试创建独立企业、供应商、模型和密钥，并在结束时清理这些数据。不要将该变量指向生产数据库。
