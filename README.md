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
- 企业网关调用密钥，完整密钥只在创建时返回一次，服务端仅保存哈希；
- OpenAI 兼容聊天补全入口，按企业模型编码路由到上游模型；
- 不保存提示词和模型回答的调用结果、耗时与 Token 用量记录。

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
- `POST /v1/chat/completions`

调用密钥可配置每分钟请求上限、按 UTC 自然月统计的 Token 额度和允许访问的模型编码范围。调用记录支持按调用密钥、模型编码、调用结果和发生时间筛选，并使用服务端分页。

调用方使用创建密钥时返回的完整调用密钥访问 OpenAI 兼容入口，并把模型目录中的“模型编码”填入 `model`：

```bash
curl http://localhost:8080/v1/chat/completions \
  -H 'Authorization: Bearer 请替换为调用密钥' \
  -H 'Content-Type: application/json' \
  -d '{"model":"请替换为模型编码","messages":[{"role":"user","content":"你好"}]}'
```

供应商 API Key 使用 `MODEL_CREDENTIAL_MASTER_KEY` 加密保存。生产环境必须设置独立的 Base64 编码 32 字节密钥。私有网络或 HTTP 模型服务必须通过 `MODEL_PROVIDER_PRIVATE_HOSTS` 显式配置精确主机名；连接检测不跟随重定向，也不会发送业务数据。

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

## 第一阶段收口范围

第一阶段已经完成身份、组织、权限与审计底座，包括：

- 平台管理员独立登录、企业列表，以及创建企业和首位企业管理员；
- 企业登录、服务端会话、CSRF、登录限流、账号锁定和设备管理；
- 用户、部门、角色与权限管理，以及企业资料和个人资料维护；
- 企业级数据隔离、接口权限校验和审计追踪；
- PostgreSQL 迁移与中文注释、Redis 缓存、OpenAPI 契约及后端自动化测试；
- React 管理端、响应式布局、侧边栏折叠和统一的企业编码、部门层级展示。

第二阶段已形成“供应商连接 → 模型目录 → 调用密钥 → OpenAI 兼容网关 → 调用记录”的闭环，并已支持密钥级请求限流、月度 Token 额度和模型访问范围。当前路由保持一个模型编码对应一个上游模型，不包含权重路由、自动故障转移、Skills、MCP、审批和执行环境；这些能力继续按独立模块推进，避免与已经稳定的身份权限底座耦合。
