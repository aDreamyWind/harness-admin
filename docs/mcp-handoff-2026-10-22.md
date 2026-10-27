# MCP 管理开发交接（2026-10-22）

本记录供新会话接续工作使用。先阅读根目录 `AGENTS.md`、`README.md`、`docs/enterprise-skills-mcp-plan.md`、`api/openapi.yaml` 与当前 Git 状态；以代码和实时环境为准，不把本记录当成永远有效的运行状态。项目保持 Go 模块化单体，前端使用 React、TypeScript、Ant Design。未经用户要求不要提交、清理或覆盖当前工作树。

## 2026-10-27 续验记录

- 本轮修复了旧版 SSE 入站网关拒绝 MCP Skills 扩展方法的问题：Go MCP SDK v1.6.1 的 `SSEServerTransport.ServeHTTP` 会在分发前用固定内置方法表拒绝 `skills/list` 等自定义方法。Admin 改用受原有逐请求 OIDC／连接授权、会话绑定、请求体大小和并发上限保护的 SSE 消息传输，交由 MCP Server 正常分发。隔离 SSE 上游经 Admin 网关向 Streamable HTTP 与旧版 SSE 客户端提供 `skills/list`、`skills/get` 和 Skill 文件；撤权后，两种已建立会话的资源读取均失败。相关测试及 `go test -race ./internal/httpserver -run '^TestMCPGatewayWith'` 通过。
- 官方 DSH SDK 工作树增加了 `skills/change` Host→Client 转发，Skill 菜单收到事件后清除各会话目录缓存，避免员工登录后仍需刷新页面才能看到企业 Skill；同时防止过期请求回写缓存。定向测试 40/40、完整 GUI 测试 10,715 项通过，产物构建与 Host／Client 汇总类型检查通过。真实 Desktop 现场刷新验收仍需在本机解锁、登录 Admin 后完成。
- 官方 SDK 的 Skill 相关 Web 回放 3 个文件／8 项通过。完整 Web 回放在未改动的聊天滚动、历史会话和预览启动用例出现失败，因此未将该全套判为通过；现有失败需要另行定位。Admin 本地 8080 已重启到本轮 SSE 修复，`/readyz` 显示数据库与 Redis 正常。临时 Skill 授权尚未创建或更改。
- 独立企业插件已接入 Host 的 MCP resources、prompts、上游 Skills 和 Admin 已发布企业 Skills。插件改动仍在 `/Users/hanxiaoxu/Documents/project202607/harness-enterprise-plugin` 工作树；发行包已安装到 Web 与 Desktop 测试 profile，官方 SDK 仓库未修改。Desktop 使用原有 8080 Admin 与 5173 控制台配置，员工重新登录后，技能菜单能找到并选中 `stock-technical-indicator`。首次登录后旧会话的技能菜单保留了登录前的空目录，刷新 Desktop 页面后显示正确；这是客户端目录缓存的待修体验问题。
- Desktop 真实模型调用在轨迹中显示 `read_mcp_resource` 以 `server=enterprise-skills`、`uri=skill://enterprise/stock-technical-indicator/SKILL.md` 成功读取文件，助手只返回其首个标题“股票技术指标分析”。这验证了员工 OIDC、企业 Skill 文件资源、Host 工具与 Desktop 模型调用链路。未通过模型执行 Skill 脚本。Admin 本地 `/readyz` 返回 200。
- 新增 HTTP 网关网络验收：Go SDK 实际 SSE 上游经 Admin 网关向 Streamable HTTP 和旧版 SSE 客户端提供 resources、prompts；撤销测试员工授权后，两种入口均阻断资源读取。另以实际 Streamable HTTP 上游验证 `skills/list`、`skills/get`、`resources/read` 经 Admin 透传及撤权阻断。`go test ./internal/httpserver ./internal/mcpclient -count=1` 通过。测试使用回环 `httptest` 上游和隔离的员工授权替身，不改本地业务数据库。
- Go MCP SDK v1.6.1 的旧版 SSE 服务端处理器仍会在接收 `skills/list` 这类自定义方法时返回 400；本轮使用独立的兼容 SSE 上游补足网关协议验收。实际 Desktop 的撤权后目录自动消失仍待现场验证；网关逐次授权和上述网络测试已覆盖调用阻断。

## 已交付与已验证

- “AI 资源管理 / MCP 管理”是单一二级菜单。左侧服务列表，右侧服务描述、部署与运行、连接、接入示例及工具列表。远程 Streamable HTTP／SSE 可接入；源码可从公开 GitHub／Gitee 完整 URL 或 `.tar.gz` 包导入，固定 Git 提交、读取根目录 `README.md` 作为描述，并在受限 Docker 容器内构建 Node.js／Python Stdio MCP。
- 支持工具发现、参数表单／JSON 测试，Harness 员工连接与开发者连接，连接独立上游参数、入口鉴权、员工部门／用户授权、使用统计。新建远程服务或连接时可自动发现工具。源码新建或构建新版本在页面跟踪到构建成功后会启动并发现工具；手动启动、重启、回退后也会重新发现并刷新列表。该自动流程目前由管理端触发，并非后端持久任务保证。
- 本机已用公开仓库 `https://github.com/worryzyy/howtocook-mcp.git` 的固定提交 `d702e22896b930146974dc3ab0eadf60cdcfeb04` 成功构建。2026-10-22 检查时，左侧 **Stdio · 托管** 的 `howtocook-mcp` 显示“运行中／可连接”，已发现 5 个工具；管理员测试 `mcp_howtocook_whatToEat`、`peopleCount=2` 成功返回菜品推荐。左侧另有同名 **Streamable HTTP** 远程服务，勿混淆。
- 2026-10-22 后续联调已补齐**托管服务的开发者 Bearer 连接**：通过真实 `/mcp/{code}` 网关，MCP SDK 客户端 `tools/list` 返回 5 个工具，`mcp_howtocook_whatToEat`（`peopleCount=2`）调用成功、结果非空，连接统计记录 1 次成功调用。无密钥和错误密钥均返回 401；轮换后旧密钥返回 401、新密钥可用；停用返回 409、重新启用恢复；另一条数秒有效的连接在到期前返回 200、到期后返回 409。管理页刷新后的该连接连通状态为“正常”。联调密钥未写入代码、日志或文档。
- 以上开发者验证覆盖“托管服务 → 开发者 Bearer 连接 → Admin 对外网关 → `tools/list`／`tools/call`”。员工连接的 Admin 侧验收及企业 Web Host 的真实 OAuth／工具调用结果见下文；桌面 Host 仍待实测。
- 已与“核查企业插件现有能力”会话对齐：独立企业插件原有 OIDC、模型网关和工具扩展点；员工 MCP 工具接入现已在该仓库实现。插件申请 `mcp.invoke`，旧授权须重新登录才能获得新增 scope；员工目录由 Host 使用令牌读取 `GET /api/v1/mcp/connections`，MCP 请求逐次取得当前令牌并在 401 后至多刷新重试一次。官方 MCP 客户端的静态 headers 不能直接用于轮换员工令牌；插件使用 MCP SDK 的逐请求鉴权能力，无需修改官方核心。连接 code 最长 80 字符，插件生成符合 32 字符限制的稳定 serverName。当前只对齐工具调用，MCP `resources/list` 仍未实现。
- 插件侧提醒：现有企业账号凭据以每个 Host 固定的 `default` 键保存，适用于每名员工独立 Host；多员工共用 Web Host 前必须先设计按请求隔离身份和凭据。Admin 当前接口没有阻断工具版接入的必改项；已将停用／过期／服务不可用时实际返回的 409 补充到 OpenAPI。插件 Host 端实际联调仍待实施。
- 员工网关回归测试发现并修复了授权错误被局部 `err` 遮蔽的问题：修复前，有效 OIDC 令牌持有者即使未获该连接授权，也可能绕过 403 继续调用工具。现已增加 HTTP 回归测试，覆盖 `mcp.invoke`、员工目录隔离、真实 MCP SDK 的 `tools/list`／`tools/call`、按用户和部门统计、撤权后阻断上游调用。`go test ./...` 通过；本地 8080 后端已于 2026-10-22 16:36（北京时间）重启到修复版，`/readyz` 返回 200。
- 用户明确批准后，已为本地现有 Web OIDC 客户端“Deepseek-Harness-网页版”增加 `mcp.invoke`，并创建只授权本地 `admin` 用户的 Harness 员工连接 `mcp_c_bb593338736048a79ce9ba0746ca430e`，到期时间为 2026-10-23 16:42:24（北京时间）。未授权 `han`，也未给任何部门授权，未写入密钥。使用本地签发的短期测试访问令牌对**运行中的 Admin 服务**验证：缺 scope 的目录和网关返回 401；`admin` 可见该连接，`han` 不可见且网关返回 403；MCP SDK `tools/list` 返回 5 个工具，`mcp_howtocook_whatToEat`（`peopleCount=2`）真实调用成功，统计按 `admin` 入账；撤销 `admin` 授权后目录立即隐藏且网关返回 403，随后仅恢复 `admin` 授权。该测试令牌验证了网关认证与授权链路，但未替代浏览器 OAuth 授权码、同意页或刷新令牌的端到端测试。临时联调程序执行后已删除。
- 独立企业插件仓库已实现员工 MCP 工具接入，保留原有企业登录与模型能力；首轮针对性测试 12/12、发行包构建 7/7 通过。汇总 `build:check` 在临时组装工作区受官方源码与构建类型混用及测试依赖缺失影响，最终日志没有插件路径类型错误。新版 enterprise bundle 已安装到备份后的 `enterprise-web` 测试 profile。
- 2026-10-23 企业 Web 真实联调：点击“更新 MCP 授权”最初停在 `about:blank`，因为页面先打开空白弹窗，再等待前端向 Host 发起的 `startSignIn` RPC 返回授权地址。Admin OIDC 发现和授权接口、Host 匿名请求均快速响应；Chrome 同时有 5 张 DSH 标签和 6 条到 Host 的 TCP 连接，浏览器中的请求长期待处理。保留 1 张 DSH 标签后连接降到 2 条，再点击便进入 Admin 同意页。用户亲自同意后，回调到达 Host，DSH 不再提示更新授权；Admin 数据库确认 `admin` 对 Web 应用的新同意包含 `mcp.invoke`。这证明多标签状态与卡住现象相关，但**不能仅凭 6 条 TCP 连接断定 Chrome 的普通 HTTP 同源连接池耗尽**：DSH 实时流使用 WebSocket，Chrome 对 WebSocket 与普通 HTTP 使用不同的连接限制；还需用浏览器 Network Timing 和 Host 入口日志确认具体排队位置。尚未完成多标签下的长期修复。
- 同日 10:21:29（北京时间），Admin 网关真实收到企业 Web Host 的 `mcp_howtocook_whatToEat` 调用，按 `admin` 用户记为成功、耗时 487 毫秒；这条员工连接的调用记录从昨天基线 1 次增至 2 次，证明真实 OAuth 后的工具调用链路已通。插件会话还在独立仓库加入授权成功页尝试自动关闭弹窗的体验修复，新包已离线更新至 profile；当时运行 Host 尚未重启，因此旧成功页仍需手动关闭。该体验修复定向测试 5/5、构建 7/7 通过。插件的撤权自动移除、刷新令牌与桌面 Host 尚未以真实员工会话验收。
- 2026-10-23 后续处理：独立企业插件为前端 `startSignIn` 请求加入默认 15 秒等待上限；超时后关闭空白弹窗、恢复按钮，并阻止迟到响应或旧备用链接干扰重试。定向测试 12/12、插件构建 7/7 通过，新包已离线安装到 `enterprise-web` profile。测试 Host 随后已按原命令及官方仓库 `.env` 重启，浏览器重新连接后企业账号显示“企业管理员”已登录；**超时场景尚未在浏览器中实测**。本地员工连接再次短暂撤销 `admin` 授权，随后恢复为仅 `admin`、无部门授权；Admin `/readyz` 返回 200。由于插件会话未返回撤权窗口内的可核对观察，不能据此宣称插件工具已随撤权移除或恢复。联调用临时程序已删除，Admin 工作树中仅保留本条文档更新。
- 2026-10-24 五标签复现明确了另一处 `about:blank` 根因：Chrome 的 `POST /api/enterpriseAccount/startSignIn` 约 0.24 秒返回 200；Host 账号 WebSocket 状态流先推送含授权地址的 `waiting-browser`，随后前端把较早的 RPC `initializing` 结果覆盖到界面，弹窗因此停留在空白页。这次请求没有排队。独立插件已改为在 RPC 发出后若状态流产生新事件，就不再用旧 RPC 响应覆盖状态；定向竞态测试和账号 UI 相关测试 9/9 通过，发行包构建 7/7 通过。更新 `enterprise-web` profile、重启 Host 后，在五张 DSH 标签下重新登录成功，界面显示企业管理员，不再停在空白弹窗。另用浏览器 CDP 将本标签的 `startSignIn` 请求延迟超过 15 秒，验证超时提示、空白弹窗关闭、按钮恢复；释放迟到响应后点击重试，最终再次登录为企业管理员。延迟规则已清除。
- 同日经用户批准，在本地 `howtocook-mcp` 托管服务下创建员工测试连接 `mcp_c_39a6528d38304fbf892a21cf67483c32`，仅授权 `admin`、无部门授权，到期时间 2026-10-25 14:05（北京时间），管理页连通状态正常。独立插件增加只含动作、连接 code 与工具名的 Host 注册日志，不记录令牌或工具参数；定向测试通过。真实 Web Host 首次挂载 5 个工具；撤销 `admin` 授权并刷新账号状态后移除 5 个，空授权复登没有重新挂载；恢复仅 `admin` 授权并复登后重新挂载 5 个。当前插件通过账号状态变化或下一次模型步骤刷新目录，Admin 变更本身不会主动推送到 Host，因此不能把“撤权后立即从已登录 Host 工具表消失”视为已交付。网关逐次授权仍阻断撤权后的实际调用。
- 桌面 Host 现场检查：最初本机没有已安装的 DSH 应用、Electron 二进制、Desktop 开发运行时或独立 Desktop profile。官方源码已有 Desktop 主程序与 Host 的构建产物，独立插件有公开客户端、随机回环端口 PKCE 和原生账号适配，不能把 Web profile 或 Web 客户端密钥直接当作 Desktop 验收。2026-10-24 经用户批准，已在本地 Admin 创建启用的 Desktop 公开 OIDC 客户端“Deepseek-Harness-桌面版本地联调”，客户端 ID 为 `harness_client_OEU2ASKosmc1XTTQ_1s2mwP7`，回调 `http://127.0.0.1/oauth/callback`，范围包括 `openid`、`profile`、`model.invoke`、`mcp.invoke`、`offline_access`，无客户端密钥。独立插件会话在官方仓库的 `.desktop-build/development/home` 准备 Desktop 运行时和独立 profile，安装企业 bundle 并启动 `Harness Dev.app`。系统浏览器打开 Admin 同意页，随后回调到随机本机端口；Desktop 进入工作区，模型选择显示 `deepseek-v4-flash`。`/private/tmp/dsh-desktop-enterprise-20261024.log`（权限 0600）记录 Host ready 和该员工连接 5 个 MCP 工具挂载，无 fatal；日志不含令牌和工具参数。随后用户在 Desktop 新会话手动发送 `whatToEat` 请求并提供轨迹截图：企业 MCP 工具状态为“已完成”，参数包含 `peopleCount: 1`，结果返回菜品数据，助手据此给出午餐建议。这补齐了 Desktop 真实工具调用验收；Desktop 的撤权移除与恢复挂载尚未实测。
- 2026-10-22 执行 `go test ./...`、`npm run build --prefix web` 与 `git diff --check` 均通过。前端构建有现存的大 chunk 警告，但构建成功；本次没有为自动发现流程做独立浏览器自动化测试。

## 建议实施顺序

1. **完成剩余企业插件 Host 验收。** 开发者 Bearer、员工 OIDC 网关、企业 Web OAuth 与真实工具调用、五标签状态竞态修复、浏览器 15 秒超时／重试、账号刷新后的撤权移除／恢复挂载，以及 Desktop 独立 OIDC 登录、5 个工具挂载和用户手动触发的真实工具调用均已完成。剩余项目是真实令牌 401 刷新、无需账号刷新时的撤权感知策略，以及 Desktop 的撤权／恢复验证。具体契约见 OpenAPI；网关不是管理员“测试工具”接口。
2. **收紧部署生命周期。** 当前后台构建存在 Go 进程内，管理端在页面跟踪构建结果后触发自动启动和工具发现；如果页面、浏览器或后端在关键阶段退出，不能保证自动续跑。应设计后端可恢复的构建／发布状态机和明确的“构建完成、已发布、已运行、工具已同步”状态，并覆盖重启、失败重试、版本回退及旧镜像清理测试。
3. **扩展托管 MCP 的适用范围前先做安全设计。** 当前运行容器始终断网，不支持连接级环境变量或业务 API 凭据，因此只适合无需外网与运行时密钥的 Stdio MCP。要支持需要外部业务 API 的 MCP，需设计受控出口、每连接隔离的凭据注入与运行实例，不能把员工 OIDC 令牌直接透传给任意上游。构建阶段本地 `.env` 使用 `MCP_BUILD_NETWORK=default` 以从 npm 安装依赖；正式部署须使用隔离 Docker 守护进程、固定基础镜像摘要、受控构建出口和可信仓库，不能把当前本机 Docker Desktop 试验等同于生产隔离。
4. **按真实客户端需求补协议与能力。** 无状态 Streamable HTTP 保持原入口，旧版 HTTP+SSE 新增 `/mcp/{code}/sse` 与会话消息端点；网关已透传 resources、prompts，以及上游声明的 MCP Skills 扩展。当前仅有本地回环协议测试，仍需真实客户端和上游联调。企业自有 Skill 的 `.tar.gz` 导入、版本审阅、发布、授权与专用 MCP 分发已有代码及隔离数据库测试；真实 Host 加载与批准尚未验收。Stdio↔HTTP 本地桥仍未交付。

## 开发现场与注意事项

- 2026-10-22 后续联调开始时，`main` 工作树为干净状态；原先“大量未提交改动”的记录已过时。本次修改了 OpenAPI、本交接文档、MCP 方案文档及员工网关授权代码和回归测试。新会话仍应先检查 Git 状态，**不要**清理、重置或覆盖现有改动；用户此前明确说过不需要代为 Git 提交。
- 本地开发库有一条名为“托管网关联调验证”的开发者 Bearer 连接，2026-10-22 16:54（北京时间）到期，另有一条已过期的短时验证连接。两者只用于本地联调；若需要继续测试，应创建新连接并自行保管密钥，勿将密钥写入仓库。
- 本地开发库还有上述“托管 MCP 员工联调”连接，仅授权 `admin`，于 2026-10-23 16:42:24（北京时间）到期。过期后需要继续联调时，按明确授权范围创建新的短期连接。
- 2026-10-24 新建的“MCP 多标签授权验收”连接仅授权 `admin`，于 2026-10-25 14:05（北京时间）到期；Web 撤权／恢复测试后已恢复原批准范围。测试结束或到期后应按需要停用，勿扩大到 `han` 或任何部门。
- 数据库迁移在本机后端启动时显示当前 Goose 版本为 28；新环境应按自动迁移验证，绝不要手工重放迁移或清空版本表。`README.md` 已更新为 21～28 号增量迁移说明。
- `.env` 与密钥不入库。源码部署默认关闭；本地要运行须确认 Docker、PostgreSQL、Redis 和 `MCP_DEPLOY_ENABLED` 等配置。构建网络可选 `none` 或 BuildKit 的 `default`，不可使用 `bridge` 作为 `docker build --network` 值；运行容器依旧 `--network none`。重新进入会话先检查前后端进程、`/readyz`、容器和页面状态，不要假设本机服务会一直运行。
- `README.md` 记录部署与本地启动，`docs/enterprise-skills-mcp-plan.md` 记录目标和未交付范围，`api/openapi.yaml` 是接口契约。计划文档中的后续兼容项不是已交付功能。
