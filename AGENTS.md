# Harness Admin 开发约定

- 所有面向用户的文案、项目文档和协作回复使用简体中文。
- 项目采用模块化单体，除非已有明确的独立部署需求，否则不要拆分微服务。
- 业务模块不得直接依赖 Gin、PostgreSQL、Redis 等基础设施实现；依赖应通过小接口进入。
- 禁止引入全局 Runtime、Service Locator，以及要求业务对象继承的 BaseController/BaseService。
- SQL、迁移和权限判断必须显式可追踪，禁止吞掉错误。
- 生成代码必须提交到仓库，拉取后应可直接编译。
- 新增接口时同步补充测试和 OpenAPI 契约。
- 前端使用 React、TypeScript、Ant Design；业务页面优先使用组合，不创建二次封装的“大而全组件框架”。

