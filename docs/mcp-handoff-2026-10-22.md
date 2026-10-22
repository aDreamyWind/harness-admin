# MCP 管理开发交接（2026-10-22）

本记录供新会话接续工作使用。先阅读根目录 `AGENTS.md`、`README.md`、`docs/enterprise-skills-mcp-plan.md`、`api/openapi.yaml` 与当前 Git 状态；以代码和实时环境为准，不把本记录当成永远有效的运行状态。项目保持 Go 模块化单体，前端使用 React、TypeScript、Ant Design。未经用户要求不要提交、清理或覆盖当前工作树。

## 已交付与已验证

- “AI 资源管理 / MCP 管理”是单一二级菜单。左侧服务列表，右侧服务描述、部署与运行、连接、接入示例及工具列表。远程 Streamable HTTP／SSE 可接入；源码可从公开 GitHub／Gitee 完整 URL 或 `.tar.gz` 包导入，固定 Git 提交、读取根目录 `README.md` 作为描述，并在受限 Docker 容器内构建 Node.js／Python Stdio MCP。
- 支持工具发现、参数表单／JSON 测试，Harness 员工连接与开发者连接，连接独立上游参数、入口鉴权、员工部门／用户授权、使用统计。新建远程服务或连接时可自动发现工具。源码新建或构建新版本在页面跟踪到构建成功后会启动并发现工具；手动启动、重启、回退后也会重新发现并刷新列表。该自动流程目前由管理端触发，并非后端持久任务保证。
- 本机已用公开仓库 `https://github.com/worryzyy/howtocook-mcp.git` 的固定提交 `d702e22896b930146974dc3ab0eadf60cdcfeb04` 成功构建。2026-10-22 检查时，左侧 **Stdio · 托管** 的 `howtocook-mcp` 显示“运行中／可连接”，已发现 5 个工具；管理员测试 `mcp_howtocook_whatToEat`、`peopleCount=2` 成功返回菜品推荐。左侧另有同名 **Streamable HTTP** 远程服务，勿混淆。
- 这次验证只覆盖“托管服务启动 → 握手 → 发现工具 → 管理员直接测试”。托管服务尚未生成 MCP 连接，因此**没有验收**通过 Admin 对外网关由 Harness 或开发者应用发起的 `tools/list`、`tools/call`。
- 2026-10-22 执行 `go test ./...`、`npm run build --prefix web` 与 `git diff --check` 均通过。前端构建有现存的大 chunk 警告，但构建成功；本次没有为自动发现流程做独立浏览器自动化测试。

## 建议实施顺序

1. **先闭环真实连接调用。** 为托管 `howtocook-mcp` 生成一条开发者 Bearer 连接（不要把完整密钥写入代码、日志或文档），通过对外 `/mcp/{code}` 做 `tools/list` 和一个只读 `tools/call`；核对连接健康、统计、过期／停用／密钥轮换。之后再建 Harness 员工连接，验证 `mcp.invoke` scope、部门／用户授权、撤权及 Web／桌面资源目录。具体契约见 OpenAPI；网关不是管理员“测试工具”接口。连接用途和授权对象应先与用户确认。
2. **收紧部署生命周期。** 当前后台构建存在 Go 进程内，管理端在页面跟踪构建结果后触发自动启动和工具发现；如果页面、浏览器或后端在关键阶段退出，不能保证自动续跑。应设计后端可恢复的构建／发布状态机和明确的“构建完成、已发布、已运行、工具已同步”状态，并覆盖重启、失败重试、版本回退及旧镜像清理测试。
3. **扩展托管 MCP 的适用范围前先做安全设计。** 当前运行容器始终断网，不支持连接级环境变量或业务 API 凭据，因此只适合无需外网与运行时密钥的 Stdio MCP。要支持需要外部业务 API 的 MCP，需设计受控出口、每连接隔离的凭据注入与运行实例，不能把员工 OIDC 令牌直接透传给任意上游。构建阶段本地 `.env` 使用 `MCP_BUILD_NETWORK=default` 以从 npm 安装依赖；正式部署须使用隔离 Docker 守护进程、固定基础镜像摘要、受控构建出口和可信仓库，不能把当前本机 Docker Desktop 试验等同于生产隔离。
4. **按真实客户端需求补协议与能力。** 对外现为无状态 Streamable HTTP，`GET /mcp/{code}` 返回 405；SSE 对外兼容和 Stdio↔HTTP 本地桥尚未交付。MCP resources、prompts 等扩展也不能宣称已支持。先拿客户端的确切协议需求与实际请求，再决定先做哪一项；Skills 是另一个独立主题。

## 开发现场与注意事项

- 当前分支为 `main`，工作树有大量未提交的已修改和未跟踪文件，包括 `internal/mcpdeploy/`、`internal/mcpruntime/`、`internal/mcpsource/`、迁移 `00027`／`00028`、HTTP 接口及前端部署组件。新会话**不要**清理、重置或把未跟踪文件当作可丢弃的构建产物；用户此前明确说过不需要代为 Git 提交。
- 数据库迁移在本机后端启动时显示当前 Goose 版本为 28；新环境应按自动迁移验证，绝不要手工重放迁移或清空版本表。`README.md` 已更新为 21～28 号增量迁移说明。
- `.env` 与密钥不入库。源码部署默认关闭；本地要运行须确认 Docker、PostgreSQL、Redis 和 `MCP_DEPLOY_ENABLED` 等配置。构建网络可选 `none` 或 BuildKit 的 `default`，不可使用 `bridge` 作为 `docker build --network` 值；运行容器依旧 `--network none`。重新进入会话先检查前后端进程、`/readyz`、容器和页面状态，不要假设本机服务会一直运行。
- `README.md` 记录部署与本地启动，`docs/enterprise-skills-mcp-plan.md` 记录目标和未交付范围，`api/openapi.yaml` 是接口契约。计划文档中的后续兼容项不是已交付功能。
