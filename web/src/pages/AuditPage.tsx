import { EyeOutlined, SearchOutlined } from '@ant-design/icons'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import {
  Alert,
  Button,
  Card,
  Descriptions,
  Drawer,
  Input,
  Select,
  Space,
  Table,
  Typography,
  type TableColumnsType,
} from 'antd'
import { useState } from 'react'

import { getAuditEvent, listAuditEvents, type AuditEvent } from '../api/audit'
import { StatusIndicator } from '../components/StatusIndicator'

const { Text } = Typography

const actionLabels: Record<string, string> = {
  'auth.login': '登录',
  'auth.account.lock': '登录失败锁定账号',
  'auth.logout': '退出登录',
  'auth.password.change': '修改密码',
  'auth.profile.update': '修改个人资料',
  'auth.session.revoke': '退出指定设备',
  'auth.session.revoke_others': '退出其他设备',
  'authorization.permission.denied': '权限拒绝',
  'identity.bootstrap_admin': '初始化管理员',
  'identity.organization.update': '修改企业资料',
  'identity.user.create': '创建用户',
  'identity.user.update': '修改用户资料',
  'identity.user.status_change': '修改用户状态',
  'identity.user.password_reset': '重置用户密码',
  'identity.user.unlock': '管理员解锁用户',
  'identity.user.roles_change': '调整用户角色',
  'identity.user.department_change': '调整用户部门',
  'identity.role.create': '创建角色',
  'identity.role.update': '修改角色',
  'identity.department.create': '创建部门',
  'identity.department.update': '修改部门',
  'identity.department.status_change': '修改部门状态',
  'oidc.consent.grant': '同意应用授权',
  'oidc.consent.deny': '拒绝应用授权',
  'oidc.consent.revoke': '撤销应用授权',
  'model.policy.save': '保存模型策略',
  'model.policy.batch_save': '批量保存部门模型策略',
  'model.policy.delete': '删除模型策略',
  'test_data.organization_tree.seed': '生成测试组织',
}

const actionOptions = Object.entries(actionLabels).map(([value, label]) => ({ value, label }))

const resultLabels: Record<AuditEvent['result'], { text: string; tone: 'success' | 'warning' | 'danger' }> = {
  success: { text: '成功', tone: 'success' },
  denied: { text: '已拒绝', tone: 'warning' },
  failed: { text: '失败', tone: 'danger' },
}

export function AuditPage() {
  const [searchInput, setSearchInput] = useState('')
  const [search, setSearch] = useState('')
  const [action, setAction] = useState('')
  const [result, setResult] = useState('')
  const [startedDate, setStartedDate] = useState('')
  const [endedDate, setEndedDate] = useState('')
  const [page, setPage] = useState(1)
  const [selectedID, setSelectedID] = useState<string | null>(null)
  const pageSize = 20

  const eventsQuery = useQuery({
    queryKey: ['audit-events', { search, action, result, startedDate, endedDate, page, pageSize }],
    queryFn: () => listAuditEvents({
      search,
      action,
      result,
      startedAt: dateBoundary(startedDate, false),
      endedAt: dateBoundary(endedDate, true),
      page,
      pageSize,
    }),
    placeholderData: keepPreviousData,
  })

  const detailQuery = useQuery({
    queryKey: ['audit-event', selectedID],
    queryFn: () => getAuditEvent(selectedID!),
    enabled: Boolean(selectedID),
  })

  function applySearch() {
    setSearch(searchInput.trim())
    setPage(1)
  }

  function clearFilters() {
    setSearchInput('')
    setSearch('')
    setAction('')
    setResult('')
    setStartedDate('')
    setEndedDate('')
    setPage(1)
  }

  const columns: TableColumnsType<AuditEvent> = [
    {
      title: '发生时间',
      dataIndex: 'createdAt',
      key: 'createdAt',
      width: 176,
      render: (value: string) => <span className="audit-time">{formatDateTime(value)}</span>,
    },
    {
      title: '事件',
      dataIndex: 'action',
      key: 'action',
      width: 230,
      render: (value: string) => (
        <div className="audit-action-cell">
          <strong>{actionLabels[value] ?? value}</strong>
          <small>{value}</small>
        </div>
      ),
    },
    {
      title: '操作人',
      key: 'actor',
      width: 170,
      render: (_, event) => event.actorDisplayName ? (
        <div className="audit-actor-cell">
          <span>{event.actorDisplayName}</span>
          <small>@{event.actorUsername}</small>
        </div>
      ) : <Text type="secondary">系统或匿名请求</Text>,
    },
    {
      title: '操作对象',
      key: 'resource',
      width: 210,
      render: (_, event) => (
        <div className="audit-resource-cell">
          <span>{event.resourceType}</span>
          <small>{event.resourceId || '—'}</small>
        </div>
      ),
    },
    {
      title: '结果',
      dataIndex: 'result',
      key: 'result',
      width: 92,
      render: (value: AuditEvent['result']) => <StatusIndicator label={resultLabels[value].text} tone={resultLabels[value].tone} />,
    },
    {
      title: '来源 IP',
      dataIndex: 'sourceIp',
      key: 'sourceIp',
      width: 140,
      render: (value?: string) => value || <Text type="secondary">未记录</Text>,
    },
    {
      title: '操作',
      key: 'operation',
      width: 84,
      fixed: 'right',
      render: (_, event) => (
        <Button type="link" size="small" icon={<EyeOutlined />} onClick={() => setSelectedID(event.id)}>
          详情
        </Button>
      ),
    },
  ]

  const detail = detailQuery.data

  return (
    <main>
      <section className="module-heading">
        <div>
          <Text className="module-kicker">安全与审计 / 操作记录</Text>
        </div>
        <div className="audit-total-block">
          <span>当前条件</span>
          <strong>{eventsQuery.data?.total ?? 0}</strong>
          <small>条事件</small>
        </div>
      </section>

      <Card className="directory-card audit-ledger" variant="borderless">
        <div className="directory-toolbar audit-toolbar">
          <Input
            className="audit-search"
            prefix={<SearchOutlined />}
            allowClear
            value={searchInput}
            placeholder="搜索操作人、动作、对象或请求 ID"
            onChange={(event) => setSearchInput(event.target.value)}
            onPressEnter={applySearch}
            onClear={() => { setSearch(''); setPage(1) }}
          />
          <Select
            value={action}
            className="audit-action-filter"
            options={[{ value: '', label: '全部事件' }, ...actionOptions]}
            onChange={(value) => { setAction(value); setPage(1) }}
          />
          <Select
            value={result}
            className="status-filter"
            options={[
              { value: '', label: '全部结果' },
              { value: 'success', label: '成功' },
              { value: 'denied', label: '已拒绝' },
              { value: 'failed', label: '失败' },
            ]}
            onChange={(value) => { setResult(value); setPage(1) }}
          />
          <div className="audit-date-range" aria-label="发生日期范围">
            <Input type="date" value={startedDate} max={endedDate || undefined} onChange={(event) => { setStartedDate(event.target.value); setPage(1) }} />
            <span>至</span>
            <Input type="date" value={endedDate} min={startedDate || undefined} onChange={(event) => { setEndedDate(event.target.value); setPage(1) }} />
          </div>
          <Button onClick={applySearch}>查询</Button>
          <Button type="text" onClick={clearFilters}>清空</Button>
        </div>

        {eventsQuery.isError ? (
          <Alert
            type="error"
            showIcon
            message="审计事件加载失败"
            description={eventsQuery.error instanceof Error ? eventsQuery.error.message : '请稍后重试'}
            action={<Button size="small" onClick={() => eventsQuery.refetch()}>重新加载</Button>}
          />
        ) : (
          <Table<AuditEvent>
            rowKey="id"
            columns={columns}
            dataSource={eventsQuery.data?.items ?? []}
            loading={eventsQuery.isLoading}
            onRow={(event) => ({ onDoubleClick: () => setSelectedID(event.id) })}
            scroll={{ x: 1180 }}
            pagination={{
              current: page,
              pageSize,
              total: eventsQuery.data?.total ?? 0,
              showSizeChanger: false,
              hideOnSinglePage: true,
              onChange: setPage,
            }}
            locale={{ emptyText: '当前条件下没有审计事件' }}
          />
        )}
      </Card>

      <Drawer
        title="审计事件详情"
        size={560}
        open={Boolean(selectedID)}
        loading={detailQuery.isLoading}
        onClose={() => setSelectedID(null)}
      >
        {detailQuery.isError ? (
          <Alert type="error" showIcon message="事件详情加载失败" description={detailQuery.error instanceof Error ? detailQuery.error.message : '请稍后重试'} />
        ) : detail ? (
          <Space direction="vertical" size={22} className="audit-detail-stack">
            <div className={`audit-detail-result audit-detail-result-${detail.result}`}>
              <StatusIndicator label={resultLabels[detail.result].text} tone={resultLabels[detail.result].tone} />
              <div>
                <strong>{actionLabels[detail.action] ?? detail.action}</strong>
                <small>{detail.action}</small>
              </div>
            </div>
            <Descriptions column={1} size="small" bordered>
              <Descriptions.Item label="事件编号">{detail.id}</Descriptions.Item>
              <Descriptions.Item label="发生时间">{formatDateTime(detail.createdAt)}</Descriptions.Item>
              <Descriptions.Item label="操作人">{detail.actorDisplayName ? `${detail.actorDisplayName} (@${detail.actorUsername})` : '系统或匿名请求'}</Descriptions.Item>
              <Descriptions.Item label="操作对象">{detail.resourceType} / {detail.resourceId || '—'}</Descriptions.Item>
              <Descriptions.Item label="来源 IP">{detail.sourceIp || '未记录'}</Descriptions.Item>
              <Descriptions.Item label="请求 ID"><Text copyable={Boolean(detail.requestId)}>{detail.requestId || '未记录'}</Text></Descriptions.Item>
            </Descriptions>
            <section className="audit-detail-data">
              <Text className="audit-detail-label">事件数据</Text>
              <pre>{JSON.stringify(detail.detail ?? {}, null, 2)}</pre>
            </section>
          </Space>
        ) : null}
      </Drawer>
    </main>
  )
}

function dateBoundary(value: string, endOfDay: boolean): string | undefined {
  if (!value) return undefined
  const suffix = endOfDay ? 'T23:59:59.999' : 'T00:00:00.000'
  return new Date(`${value}${suffix}`).toISOString()
}

function formatDateTime(value: string): string {
  return new Intl.DateTimeFormat('zh-CN', {
    year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit',
    hour12: false,
  }).format(new Date(value))
}
