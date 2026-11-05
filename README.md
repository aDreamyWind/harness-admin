# Harness Admin

Harness Admin 是一个可自托管的企业 AI 管理后台。你可以在同一处接入模型、MCP 服务和企业 Skills，为员工及应用分配使用权限，并查看调用记录和用量。

[快速启动](#快速启动) · [界面预览](#界面预览) · [开发与部署](docs/development.md) · [API 契约](api/openapi.yaml)

## 主要功能

| 功能 | 可以做什么 |
| --- | --- |
| 企业与账号 | 管理企业、部门、员工和角色权限，查看审计记录。通过 OIDC 让桌面端和服务端应用使用企业账号登录。 |
| 模型目录与网关 | 接入模型供应商，以统一入口调用聊天、图片和视频模型。按员工、部门或调用密钥控制可用模型和额度，查看用量、预警及调用记录。 |
| MCP 管理 | 接入远程 MCP 服务，发现和测试工具，为员工或开发者分配连接。也可在隔离的 Docker 环境中托管受信任的 Node.js、Python Stdio 服务。 |
| 企业 Skills | 从本地压缩包或公开 GitHub 仓库导入 Skill，审阅文件、发布版本，并按部门或员工授权。 |
| AI 创作 | 在漫剧工作台管理剧本、角色和场景资产，拆分分镜并生成片段视频。在小说工作台规划大纲、逐章写作和审稿。 |

## 界面预览

以下截图使用演示数据。

![企业首页，展示资源概况、待处理事项和常用入口](docs/screenshots/dashboard.png)

<details>
<summary>查看模型目录</summary>

![模型目录，展示模型类型、调用入口、能力和启用状态](docs/screenshots/model-catalog.png)

</details>

## 快速启动

准备 Go 1.26、Node.js 22.12 或更新的 LTS 版本、pnpm、Make，以及已启动的 Docker 和 Docker Compose。在项目根目录执行：

```bash
cp .env.example .env
make infra-up
```

打开 `.env`，给 `BOOTSTRAP_ADMIN_PASSWORD` 设置一个本地开发密码。等待 `docker compose ps` 显示 PostgreSQL 和 Redis 均为 `healthy`，再初始化企业管理员并启动后端：

```bash
make bootstrap-admin
make dev
```

另开一个终端，在项目根目录启动前端：

```bash
make web-install
make web-dev
```

打开 [企业管理端](http://localhost:5173)，选择「默认企业」，使用账号 `admin` 和刚设置的密码登录。可以在「AI 资源管理 → 模型目录」登记供应商和模型，再到「模型网关」创建调用密钥。模型调用和 AI 创作功能需要配置相应的上游模型。

如果要管理多个企业，在 `.env` 中设置 `BOOTSTRAP_PLATFORM_ADMIN_PASSWORD`，执行 `make bootstrap-platform-admin`，然后用 `platform-admin` 登录[平台管理端](http://localhost:5173/platform/login)。初始化完成后，可从 `.env` 移除初始化密码。

示例配置用于本地开发。部署到服务器前，请配置独立的数据库凭据、凭据加密主密钥和 OIDC 签名密钥，并启用 HTTPS。详见[开发与部署指南](docs/development.md)。

## 文档

- [开发与部署](docs/development.md) · 代码结构、数据库迁移、接入配置和验证命令
- [OpenAPI 契约](api/openapi.yaml) · 接口、认证与数据结构
- [图片与视频模型网关](docs/model-media-gateway.md) · 媒体生成与任务查询
- [MCP 接入方案](docs/enterprise-skills-mcp-plan.md) · 服务来源、网关和权限
- [企业 Skills 接入](docs/enterprise-skill-management-plan.md) · 导入、发布与分发
- [漫剧工作流](docs/ai-drama-workflow.md) · 剧本解析、资产与分镜制作
