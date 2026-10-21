import { ArrowLeftOutlined, ReloadOutlined } from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { Alert, Button, Card, Empty, Table, Tag, Typography } from 'antd'
import { useNavigate, useParams } from 'react-router-dom'

import { listMCPConnections, listMCPUsage, type MCPUsage } from '../api/mcpConnections'

const { Text } = Typography

export function MCPConnectionUsagePage() {
  const navigate = useNavigate()
  const { connectionId } = useParams<{ connectionId: string }>()
  const connectionsQuery = useQuery({ queryKey: ['mcp-connections'], queryFn: listMCPConnections })
  const usageQuery = useQuery({ queryKey: ['mcp-usage'], queryFn: listMCPUsage })
  const connection = connectionsQuery.data?.find((item) => item.id === connectionId)
  const usage = (usageQuery.data ?? []).filter((item) => item.connectionId === connectionId)
  const calls = usage.reduce((sum, item) => sum + item.calls, 0)
  const failures = usage.reduce((sum, item) => sum + item.failures, 0)

  return <Card className="content-card mcp-usage-page" variant="borderless">
    <div className="mcp-usage-heading">
      <div className="mcp-usage-heading-text">
        <Button type="link" className="mcp-usage-back" icon={<ArrowLeftOutlined />} onClick={() => navigate(connection
          ? `/ai-resources/mcp?service=${encodeURIComponent(connection.serviceId)}` : '/ai-resources/mcp')}>MCP 管理</Button>
        <h2>{connection ? connection.name : '连接调用统计'}</h2>
        <Text type="secondary">调用统计</Text>
      </div>
      <Button icon={<ReloadOutlined />} onClick={() => usageQuery.refetch()}>刷新数据</Button>
    </div>
    {!connectionsQuery.isLoading && !connection ? <Alert type="warning" message="连接不存在或无权查看" /> : <>
      {connection && <div className="mcp-usage-context">
        <span>所属服务：{connection.serviceName}</span>
        <span>用途：{connection.purpose === 'harness' ? 'Harness 员工' : '开发者／应用'}</span>
        <Tag color={connection.status === 'active' ? 'green' : 'default'}>{connection.status === 'active' ? '启用' : '停用'}</Tag>
      </div>}
      <div className="mcp-usage-totals">
        <div><Text type="secondary">调用次数</Text><strong>{calls}</strong></div>
        <div><Text type="secondary">失败次数</Text><strong>{failures}</strong></div>
      </div>
      <Table<MCPUsage> rowKey={(item) => `${item.connectionId}:${item.toolName}:${item.userId ?? ''}:${item.departmentId ?? ''}`}
        size="small" loading={usageQuery.isLoading} dataSource={usage} pagination={{ pageSize: 10 }}
        locale={{ emptyText: <Empty description="这条连接还没有调用记录" /> }}
        columns={[
          { title: '工具', dataIndex: 'toolName' },
          ...(connection?.purpose === 'harness' ? [
            { title: '员工', dataIndex: 'userName' as const, render: (value: string | undefined) => value || '—' },
            { title: '部门', dataIndex: 'departmentName' as const, render: (value: string | undefined) => value || '—' },
          ] : []),
          { title: '调用次数', dataIndex: 'calls' },
          { title: '失败次数', dataIndex: 'failures' },
          { title: '平均耗时', dataIndex: 'averageMs', render: (value: number) => `${value} ms` },
        ]} />
    </>}
  </Card>
}
