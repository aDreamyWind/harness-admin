# Harness Admin

Harness Admin 是面向企业 Harness 的控制平面。项目从零构建，不依赖任何管理后台脚手架的私有 Core 或作者服务。

当前阶段提供：

- Go HTTP 服务与优雅停机；
- PostgreSQL、Redis 基础设施连接；
- 内嵌数据库迁移；
- 存活、就绪和系统概览接口；
- React + Ant Design 管理端外壳；
- 用户、组织、角色、会话和审计的首版数据模型；
- Argon2id 密码、服务端会话、CSRF 校验与登录限流。
- 基于角色权限的接口授权、企业用户管理、角色权限配置与部门层级维护。

## 技术栈

- Go、Gin、pgx、go-redis、Goose
- PostgreSQL、Redis
- React、TypeScript、Vite、Ant Design、TanStack Query

## 本地启动

```bash
cp .env.example .env
make infra-up
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

`make seed-test-organization` 仅允许在 `development` 或 `test` 环境执行，会为指定企业写入一套常见企业组织树，包括集团总部、总经理办公室、人力资源、财务、产品、研发、营销、法务等 17 个部门。该命令可重复执行：已有部门会按代码更新层级、名称、排序和启用状态，不会重复创建。默认使用 `default` 企业和 `admin` 操作人，可通过 `TEST_ORG_CODE`、`TEST_ORG_ACTOR_USERNAME` 配置。

身份接口：

- `POST /api/v1/auth/login`
- `GET /api/v1/auth/me`
- `POST /api/v1/auth/logout`

用户管理接口：

- `GET /api/v1/users`
- `POST /api/v1/users`
- `PATCH /api/v1/users/{userId}/status`
- `PUT /api/v1/users/{userId}/roles`
- `PATCH /api/v1/users/{userId}/department`

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

审计查询按当前登录企业隔离，需要 `audit.event.read` 权限，支持按关键字、动作、结果和发生时间筛选。无权限访问管理接口时也会写入一条 `denied` 审计事件。

接口契约见 [`api/openapi.yaml`](api/openapi.yaml)。

登录请求需要提供 `organizationCode`、`username` 和 `password`。登录成功后服务端写入仅限 `/api`、前端不可读取的 HttpOnly 会话 Cookie，同时写入同源前端可读取的 `harness_admin_csrf` Cookie。管理端会在退出及后续写接口中自动把该值放入 `X-CSRF-Token` 请求头；非浏览器客户端也可以使用登录响应中的 `csrfToken`。

## 验证

```bash
make test
make build
make web-build
```

## 当前边界

第一阶段先完成身份、组织、权限与审计底座。模型网关、额度、Skills、MCP、审批和执行环境会在底座稳定后分阶段加入。
