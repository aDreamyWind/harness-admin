import { Typography } from 'antd'
import { MCPServicesPage } from './MCPServicesPage'

export function MCPManagementPage() {
  return <main>
    <section className="module-heading"><Typography.Text className="module-kicker">AI 资源管理 / MCP 管理</Typography.Text></section>
    <MCPServicesPage />
  </main>
}
