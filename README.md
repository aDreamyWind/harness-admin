# Harness Admin

Harness Admin 是面向企业 Harness 的控制平面。项目从零构建，不依赖任何管理后台脚手架的私有 Core 或作者服务。

当前阶段提供：

- Go HTTP 服务与优雅停机；
- PostgreSQL、Redis 基础设施连接；
- 内嵌数据库迁移；
- 存活、就绪和系统概览接口；
- React + Ant Design 管理端外壳；
- 用户、组织、角色、会话和审计的首版数据模型。

## 技术栈

- Go、Gin、pgx、go-redis、Goose
- PostgreSQL、Redis
- React、TypeScript、Vite、Ant Design、TanStack Query

## 本地启动

```bash
cp .env.example .env
make infra-up
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

## 验证

```bash
make test
make build
make web-build
```

## 当前边界

第一阶段先完成身份、组织、权限与审计底座。模型网关、额度、Skills、MCP、审批和执行环境会在底座稳定后分阶段加入。

