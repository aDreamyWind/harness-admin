import {
  CheckCircleOutlined,
  CopyOutlined,
  EditOutlined,
  EyeOutlined,
  KeyOutlined,
  PlusOutlined,
  ReloadOutlined,
  SearchOutlined,
  SwapOutlined,
} from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Alert,
  Button,
  Card,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Select,
  Space,
  Table,
  Tabs,
  Tag,
  Typography,
  message,
  type TableColumnsType,
} from 'antd'
import { useMemo, useState, type ReactNode } from 'react'

import { ApiError } from '../api/client'
import {
  acknowledgeGatewayAlert,
  createGatewayAPIKey,
  listGatewayAPIKeys,
  listGatewayAlerts,
  listGatewayCalls,
  listGatewayUsage,
  revokeGatewayAPIKey,
  revealGatewayAPIKeySecret,
  updateGatewayAPIKey,
  type CreatedGatewayAPIKey,
  type GatewayAPIKey,
  type GatewayAlert,
  type GatewayAlertQuery,
  type GatewayCall,
  type GatewayCallQuery,
  type GatewayUsageGroup,
  type GatewayUsageQuery,
} from '../api/modelGateway'
import { listModelPolicies, type ModelPolicy } from '../api/modelPolicy'
import { useAuth } from '../auth/AuthContext'
import { StatusIndicator } from '../components/StatusIndicator'

const { Paragraph, Text } = Typography

interface APIKeyFormValue {
  name: string
  expiresAt?: string
  requestsPerMinute: number
  monthlyTokenLimit: number
  allowedModelCodes: string[]
}

interface CallFilterValue {
  apiKeyId?: string
  modelCode?: string
  result?: GatewayCall['result']
  startedAt?: string
  endedAt?: string
}

export function ModelGatewayPage() {
  const { hasPermission } = useAuth()
  const canWrite = hasPermission('model.gateway.write')
  const canReadPolicies = hasPermission('model.policy.read')
  const queryClient = useQueryClient()
  const [messageApi, messageContext] = message.useMessage()
  const [form] = Form.useForm<APIKeyFormValue>()
  const [filterForm] = Form.useForm<CallFilterValue>()
  const [keyModalOpen, setKeyModalOpen] = useState(false)
  const [editingKey, setEditingKey] = useState<GatewayAPIKey | null>(null)
  const [createdKey, setCreatedKey] = useState<CreatedGatewayAPIKey | null>(null)
  const [revealedKey, setRevealedKey] = useState<{ name: string; secret: string } | null>(null)
  const [callQuery, setCallQuery] = useState<GatewayCallQuery>({ page: 1, pageSize: 20 })
  const [activeTab, setActiveTab] = useState('keys')
  const [usageQuery, setUsageQuery] = useState<GatewayUsageQuery>({
    month: new Date().toISOString().slice(0, 7), groupBy: 'department', page: 1, pageSize: 20,
  })
  const [alertQuery, setAlertQuery] = useState<GatewayAlertQuery>({
    month: new Date().toISOString().slice(0, 7), status: 'active', page: 1, pageSize: 20,
  })

  const keysQuery = useQuery({ queryKey: ['model-gateway-api-keys'], queryFn: listGatewayAPIKeys })
  const callsQuery = useQuery({ queryKey: ['model-gateway-calls', callQuery], queryFn: () => listGatewayCalls(callQuery), refetchInterval: 15000 })
  const usageResult = useQuery({ queryKey: ['model-gateway-usage', usageQuery], queryFn: () => listGatewayUsage(usageQuery), enabled: activeTab === 'usage' })
  const policiesResult = useQuery({ queryKey: ['model-policies'], queryFn: listModelPolicies, enabled: canReadPolicies && activeTab === 'usage' })
  const alertsResult = useQuery({ queryKey: ['model-gateway-alerts', alertQuery], queryFn: () => listGatewayAlerts(alertQuery), refetchInterval: 60000 })
  const keys = keysQuery.data ?? []
  const callsPage = callsQuery.data ?? { items: [], total: 0, page: callQuery.page, pageSize: callQuery.pageSize }
  const usagePage = usageResult.data
  const alertsPage = alertsResult.data
  const currentUTCMonth = new Date().toISOString().slice(0, 7)
  const policyBySubject = new Map((policiesResult.data ?? []).map((policy) => [`${policy.subjectType}:${policy.subjectId}`, policy]))

  const saveKey = useMutation({
    mutationFn: (value: APIKeyFormValue) => {
      const input = {
        name: value.name.trim(),
        expiresAt: value.expiresAt ? new Date(value.expiresAt).toISOString() : undefined,
        requestsPerMinute: value.requestsPerMinute,
        monthlyTokenLimit: value.monthlyTokenLimit,
        allowedModelCodes: value.allowedModelCodes ?? [],
      }
      return editingKey ? updateGatewayAPIKey(editingKey.id, input) : createGatewayAPIKey(input)
    },
    onSuccess: async (result) => {
      setKeyModalOpen(false)
      form.resetFields()
      if ('secret' in result && typeof result.secret === 'string') setCreatedKey(result as CreatedGatewayAPIKey)
      else messageApi.success('调用密钥设置已更新')
      setEditingKey(null)
      await queryClient.invalidateQueries({ queryKey: ['model-gateway-api-keys'] })
    },
  })
  const revokeKey = useMutation({
    mutationFn: revokeGatewayAPIKey,
    onSuccess: async () => {
      messageApi.success('调用密钥已吊销')
      await queryClient.invalidateQueries({ queryKey: ['model-gateway-api-keys'] })
    },
    onError: (error) => messageApi.error(errorMessage(error, '调用密钥吊销失败')),
  })
  const revealKey = useMutation({
    mutationFn: async (key: GatewayAPIKey) => ({ key, result: await revealGatewayAPIKeySecret(key.id) }),
    onSuccess: ({ key, result }) => setRevealedKey({ name: key.name, secret: result.secret }),
    onError: (error) => messageApi.error(errorMessage(error, '调用密钥查看失败')),
  })
  const acknowledgeAlert = useMutation({
    mutationFn: acknowledgeGatewayAlert,
    onSuccess: async () => {
      messageApi.success('预警已标记为知悉')
      await queryClient.invalidateQueries({ queryKey: ['model-gateway-alerts'] })
    },
    onError: (error) => messageApi.error(errorMessage(error, '确认预警失败')),
  })

  const openCreateKey = () => {
    setEditingKey(null)
    saveKey.reset()
    form.setFieldsValue({
      name: '',
      expiresAt: undefined,
      requestsPerMinute: 60,
      monthlyTokenLimit: 0,
      allowedModelCodes: [],
    })
    setKeyModalOpen(true)
  }

  const openEditKey = (item: GatewayAPIKey) => {
    setEditingKey(item)
    saveKey.reset()
    form.setFieldsValue({
      name: item.name,
      expiresAt: item.expiresAt ? localDateTime(new Date(item.expiresAt)) : undefined,
      requestsPerMinute: item.requestsPerMinute,
      monthlyTokenLimit: item.monthlyTokenLimit,
      allowedModelCodes: item.allowedModelCodes,
    })
    setKeyModalOpen(true)
  }

  const closeKeyModal = () => {
    setKeyModalOpen(false)
    setEditingKey(null)
    form.resetFields()
    saveKey.reset()
  }

  const applyCallFilters = (value: CallFilterValue) => {
    setCallQuery((current) => ({
      page: 1,
      pageSize: current.pageSize,
      apiKeyId: value.apiKeyId,
      modelCode: value.modelCode?.trim() || undefined,
      result: value.result,
      startedAt: value.startedAt ? new Date(value.startedAt).toISOString() : undefined,
      endedAt: value.endedAt ? new Date(value.endedAt).toISOString() : undefined,
    }))
  }

  const keyColumns: TableColumnsType<GatewayAPIKey> = useMemo(() => [
    {
      title: '调用密钥', key: 'identity', width: 230,
      render: (_, item) => <div className="gateway-key-name"><KeyOutlined /><span><strong>{item.name}</strong><small>{item.keyPrefix}••••••••</small></span></div>,
    },
    { title: '状态', key: 'status', width: 110, render: (_, item) => <StatusIndicator label={keyStatus(item)} tone={keyTone(item)} /> },
    {
      title: '调用限制', key: 'limits', width: 205,
      render: (_, item) => <div className="gateway-limit-cell">
        <span>{item.requestsPerMinute > 0 ? `${item.requestsPerMinute} 次 / 分钟` : '不限每分钟请求'}</span>
        <small>{item.monthlyTokenLimit > 0 ? `本月 ${item.monthlyTokenUsed.toLocaleString()} / ${item.monthlyTokenLimit.toLocaleString()} Token` : '不限月度 Token'}</small>
        {item.monthlyTokenLimit > 0 && item.monthlyTokenUsed >= item.monthlyTokenLimit
          ? <Tag color="red">已达上限</Tag>
          : item.monthlyTokenLimit > 0 && item.monthlyTokenUsed >= item.monthlyTokenLimit - Math.floor(item.monthlyTokenLimit / 5)
            ? <Tag color="orange">接近上限</Tag> : null}
      </div>,
    },
    {
      title: '可用模型', key: 'allowedModels', width: 220,
      render: (_, item) => item.allowedModelCodes.length > 0
        ? <div className="gateway-model-scope">{item.allowedModelCodes.slice(0, 2).map((code) => <Tag key={code}>{code}</Tag>)}{item.allowedModelCodes.length > 2 ? <Text type="secondary">+{item.allowedModelCodes.length - 2}</Text> : null}</div>
        : <Text type="secondary">全部模型</Text>,
    },
    { title: '有效期', key: 'expiresAt', width: 180, render: (_, item) => item.expiresAt ? formatTime(item.expiresAt) : '永不过期' },
    { title: '最近调用', key: 'lastUsedAt', width: 180, render: (_, item) => item.lastUsedAt ? formatTime(item.lastUsedAt) : '尚未使用' },
    { title: '创建时间', dataIndex: 'createdAt', key: 'createdAt', width: 180, render: formatTime },
    ...(canWrite ? [{
      title: '操作', key: 'actions', width: 200,
      render: (_: unknown, item: GatewayAPIKey) => <Space size={4}>
        <Button type="link" size="small" icon={<EyeOutlined />} loading={revealKey.isPending && revealKey.variables?.id === item.id} onClick={() => revealKey.mutate(item)}>查看</Button>
        {item.status === 'active' ? <>
          <Button type="link" size="small" icon={<EditOutlined />} onClick={() => openEditKey(item)}>编辑</Button>
          <Popconfirm title="吊销这个调用密钥？" description="吊销后立即失效，且无法恢复。" onConfirm={() => revokeKey.mutate(item.id)}>
            <Button type="link" danger size="small">吊销</Button>
          </Popconfirm>
        </> : null}
      </Space>,
    }] : []),
  ], [canWrite, revokeKey, revealKey.isPending, revealKey.variables])

  const callColumns: TableColumnsType<GatewayCall> = useMemo(() => [
    { title: '发生时间', dataIndex: 'createdAt', key: 'createdAt', width: 178, render: formatTime },
    {
      title: '请求流向', key: 'flow', width: 300,
      render: (_, item) => <div className="gateway-call-flow"><strong>{item.modelCode}</strong><SwapOutlined /><span>{item.upstreamModelId}</span>{item.streaming ? <Tag>流式</Tag> : null}</div>,
    },
    { title: '调用身份', key: 'caller', width: 170, render: (_, item) => item.authType === 'user' ? <span>{item.userName || item.userId}<Tag color="blue">用户</Tag></span> : item.apiKeyName },
    { title: '结果', key: 'result', width: 105, render: (_, item) => <StatusIndicator label={item.result === 'success' ? '成功' : '失败'} tone={item.result === 'success' ? 'success' : 'danger'} /> },
    { title: 'HTTP', dataIndex: 'httpStatus', key: 'httpStatus', width: 80 },
    { title: '耗时', dataIndex: 'latencyMs', key: 'latencyMs', width: 100, render: (value: number) => `${value} ms` },
    { title: 'Token', key: 'totalTokens', width: 120, render: (_, item) => item.usageReported ? item.totalTokens.toLocaleString() : <Tag color="orange">用量未知</Tag> },
    { title: '额度', key: 'quota', width: 100, render: (_, item) => item.quotaExceeded ? <Tag color="red">本次超额</Tag> : '—' },
    { title: '请求 ID', dataIndex: 'requestId', key: 'requestId', width: 210, render: (value: string) => <Text code copyable>{value}</Text> },
  ], [])

  const usageColumns: TableColumnsType<GatewayUsageGroup> = [
    { title: usageQuery.groupBy === 'department' ? '部门' : usageQuery.groupBy === 'user' ? '用户' : '模型编码', key: 'name', render: (_, item) => <span>{item.name}{item.id && usageQuery.groupBy !== 'model' ? <Text type="secondary" style={{ marginLeft: 8 }}>{item.id.slice(0, 8)}</Text> : null}</span> },
    { title: '调用次数', dataIndex: 'callCount', key: 'callCount', render: (value: number) => value.toLocaleString() },
    { title: '已统计 Token', dataIndex: 'knownTokens', key: 'knownTokens', render: (value: number) => value.toLocaleString() },
    { title: '用量未知', dataIndex: 'unknownUsageCalls', key: 'unknownUsageCalls', render: (value: number) => value > 0 ? <Tag color="orange">{value} 次</Tag> : '—' },
    { title: '超额调用', dataIndex: 'overQuotaCalls', key: 'overQuotaCalls', render: (value: number) => value > 0 ? <Tag color="red">{value} 次</Tag> : '—' },
    ...(canReadPolicies && usageQuery.month === currentUTCMonth && usageQuery.groupBy !== 'model' ? [{
      title: '策略额度', key: 'quota', width: 230,
      render: (_: unknown, item: GatewayUsageGroup) => renderPolicyQuota(policyBySubject.get(`${usageQuery.groupBy}:${item.id}`)),
    }] : []),
  ]

  const alertColumns: TableColumnsType<GatewayAlert> = [
    { title: '级别', key: 'severity', width: 105, render: (_, item) => <Tag color={item.severity === 'critical' ? 'red' : 'orange'}>{item.severity === 'critical' ? '已达上限' : '需关注'}</Tag> },
    { title: '对象', dataIndex: 'subjectName', key: 'subjectName', width: 200 },
    { title: '预警内容', key: 'detail', render: (_, item) => item.kind === 'quota'
      ? <span>已统计 {item.usedTokens.toLocaleString()} / {item.tokenLimit.toLocaleString()} Token（{Math.floor(item.usedTokens / item.tokenLimit * 100)}%）</span>
      : <span>成功调用 {item.callCount.toLocaleString()} 次，其中 {item.unknownUsageCalls.toLocaleString()} 次用量未知</span> },
    { title: '首次发现', dataIndex: 'firstDetectedAt', key: 'firstDetectedAt', width: 170, render: formatTime },
    { title: '处理状态', key: 'acknowledgedAt', width: 100, render: (_, item) => item.resolvedAt ? <Tag>已解除</Tag> : item.acknowledgedAt ? <Tag color="blue">已知悉</Tag> : <Tag color="gold">待查看</Tag> },
    ...(canWrite ? [{ title: '操作', key: 'actions', width: 90, render: (_: unknown, item: GatewayAlert) => !item.resolvedAt && !item.acknowledgedAt
      ? <Button type="link" size="small" loading={acknowledgeAlert.isPending && acknowledgeAlert.variables === item.id} onClick={() => acknowledgeAlert.mutate(item.id)}>标记知悉</Button>
      : null }] : []),
  ]

  const tabs = [
    {
      key: 'keys', label: `调用密钥 ${keys.length}`,
      children: <GatewayResource
        title="调用密钥"
        hint="调用密钥加密保存，具备管理权限的用户可以再次查看。"
        action={canWrite ? <Button type="primary" icon={<PlusOutlined />} onClick={openCreateKey}>创建调用密钥</Button> : undefined}
        error={keysQuery.isError ? errorMessage(keysQuery.error, '调用密钥加载失败') : ''}
        retry={() => keysQuery.refetch()}
      >
        <Table<GatewayAPIKey> rowKey="id" columns={keyColumns} dataSource={keys} loading={keysQuery.isLoading} pagination={false} scroll={{ x: 1380 }} locale={{ emptyText: '尚未创建调用密钥' }} />
      </GatewayResource>,
    },
    {
      key: 'usage', label: '用量统计',
      children: <GatewayResource
        title="用量统计"
        hint="按 UTC 自然月统计；部门和用户仅统计登录用户调用，模型包含调用密钥。未知用量不计入 Token 合计。"
        action={<Button icon={<ReloadOutlined />} loading={usageResult.isFetching} onClick={() => usageResult.refetch()}>刷新</Button>}
        error={usageResult.isError ? errorMessage(usageResult.error, '用量统计加载失败') : ''}
        retry={() => usageResult.refetch()}
      >
        <div className="gateway-usage-controls">
          <Input type="month" aria-label="统计月份" value={usageQuery.month} onChange={(event) => {
            if (event.target.value) setUsageQuery((current) => ({ ...current, month: event.target.value, page: 1 }))
          }} />
          <Select aria-label="统计维度" value={usageQuery.groupBy} onChange={(groupBy) => setUsageQuery((current) => ({ ...current, groupBy, page: 1 }))}
            options={[{ value: 'department', label: '按部门' }, { value: 'user', label: '按用户' }, { value: 'model', label: '按模型' }]} />
          <Text type="secondary">{usagePage ? `调用 ${usagePage.callCount.toLocaleString()} 次 · 已统计 ${usagePage.knownTokens.toLocaleString()} Token · 用量未知 ${usagePage.unknownUsageCalls.toLocaleString()} 次 · 超额 ${usagePage.overQuotaCalls.toLocaleString()} 次` : '正在加载统计'}</Text>
        </div>
        {usagePage && usagePage.unknownUsageCalls > 0 ? <Alert type="warning" showIcon style={{ marginBottom: 12 }} message="部分调用未返回 Token 用量，额度使用率只反映已统计部分；请检查对应供应商是否支持流式用量返回。" /> : null}
        <Table<GatewayUsageGroup> rowKey={(item) => item.id || 'unknown'} columns={usageColumns} dataSource={usagePage?.items ?? []}
          loading={usageResult.isLoading} pagination={{ current: usageQuery.page, pageSize: usageQuery.pageSize,
            total: usagePage?.total ?? 0, showSizeChanger: true, showTotal: (total) => `共 ${total} 项`,
            onChange: (page, pageSize) => setUsageQuery((current) => ({ ...current, page, pageSize })) }}
          scroll={{ x: 780 }} locale={{ emptyText: '此月份没有调用记录' }} />
      </GatewayResource>,
    },
    {
      key: 'alerts', label: <span>站内预警{alertsPage?.activeCount ? <Tag color="orange" style={{ marginLeft: 6, marginRight: 0 }}>{alertsPage.activeCount}</Tag> : null}</span>,
      children: <GatewayResource
        title="站内预警"
        hint="额度达到 80% 提醒、达到 100% 标为紧急；模型成功调用中，至少 5 次且至少 20% 用量未知时提醒。按月份和对象去重，严重程度升级后需重新确认。"
        action={<Button icon={<ReloadOutlined />} loading={alertsResult.isFetching} onClick={() => alertsResult.refetch()}>刷新</Button>}
        error={alertsResult.isError ? errorMessage(alertsResult.error, '站内预警加载失败') : ''}
        retry={() => alertsResult.refetch()}
      >
        <div className="gateway-usage-controls">
          <Input type="month" aria-label="预警月份" value={alertQuery.month} onChange={(event) => {
            if (event.target.value) setAlertQuery((current) => ({ ...current, month: event.target.value, page: 1 }))
          }} />
          <Select aria-label="预警状态" value={alertQuery.status} onChange={(status) => setAlertQuery((current) => ({ ...current, status, page: 1 }))}
            options={[{ value: 'active', label: '当前预警' }, { value: 'all', label: '含已解除' }]} />
          <Text type="secondary">当前有效 {alertsPage?.activeCount ?? 0} 条 · 已确认的预警仍保留记录</Text>
        </div>
        <Table<GatewayAlert> rowKey="id" columns={alertColumns} dataSource={alertsPage?.items ?? []}
          loading={alertsResult.isLoading} pagination={{ current: alertQuery.page, pageSize: alertQuery.pageSize,
            total: alertsPage?.total ?? 0, showSizeChanger: true, showTotal: (total) => `共 ${total} 条`,
            onChange: (page, pageSize) => setAlertQuery((current) => ({ ...current, page, pageSize })) }}
          scroll={{ x: 950 }} locale={{ emptyText: '此月份没有符合条件的预警' }} />
      </GatewayResource>,
    },
    {
      key: 'calls', label: `调用记录 ${callsPage.total}`,
      children: <GatewayResource
        title="最近调用"
        hint="只记录路由、结果和用量，不保存提示词与模型回答。"
        action={<Button icon={<ReloadOutlined />} loading={callsQuery.isFetching} onClick={() => callsQuery.refetch()}>刷新</Button>}
        error={callsQuery.isError ? errorMessage(callsQuery.error, '调用记录加载失败') : ''}
        retry={() => callsQuery.refetch()}
      >
        <Form form={filterForm} className="gateway-call-filters" layout="inline" onFinish={applyCallFilters}>
          <Form.Item name="apiKeyId">
            <Select allowClear placeholder="全部调用密钥" options={keys.map((item) => ({ label: item.name, value: item.id }))} />
          </Form.Item>
          <Form.Item name="modelCode">
            <Input allowClear placeholder="模型编码" />
          </Form.Item>
          <Form.Item name="result">
            <Select allowClear placeholder="全部结果" options={[{ label: '成功', value: 'success' }, { label: '失败', value: 'failed' }]} />
          </Form.Item>
          <Form.Item name="startedAt"><Input type="datetime-local" aria-label="开始时间" /></Form.Item>
          <Form.Item name="endedAt"><Input type="datetime-local" aria-label="结束时间" /></Form.Item>
          <Form.Item>
            <Space>
              <Button type="primary" htmlType="submit" icon={<SearchOutlined />}>查询</Button>
              <Button onClick={() => { filterForm.resetFields(); setCallQuery({ page: 1, pageSize: callQuery.pageSize }) }}>重置</Button>
            </Space>
          </Form.Item>
        </Form>
        <Table<GatewayCall>
          rowKey="id"
          columns={callColumns}
          dataSource={callsPage.items}
          loading={callsQuery.isLoading}
          pagination={{
            current: callsPage.page,
            pageSize: callsPage.pageSize,
            total: callsPage.total,
            showSizeChanger: true,
            showTotal: (total) => `共 ${total} 条`,
            onChange: (page, pageSize) => setCallQuery((current) => ({ ...current, page, pageSize })),
          }}
          scroll={{ x: 1250 }}
          locale={{ emptyText: '尚无模型调用记录' }}
        />
      </GatewayResource>,
    },
  ]

  return (
    <main>
      {messageContext}
      <section className="module-heading"><Text className="module-kicker">AI 资源管理 / 模型网关</Text></section>

      <div className="gateway-endpoint-strip">
        <div><span>OpenAI 兼容地址</span><Text code copyable>{`${window.location.origin}/v1`}</Text></div>
        <div><span>模型参数</span><strong>填写模型目录中的模型编码</strong></div>
        <CheckCircleOutlined />
      </div>

      <Tabs className="model-resource-tabs" activeKey={activeTab} onChange={setActiveTab} items={tabs} />

      <Modal
        title={editingKey ? '编辑调用密钥' : '创建调用密钥'}
        open={keyModalOpen}
        okText={editingKey ? '保存' : '创建'}
        cancelText="取消"
        confirmLoading={saveKey.isPending}
        onCancel={closeKeyModal}
        onOk={() => form.submit()}
        destroyOnHidden
      >
        {saveKey.isError ? <Alert type="error" showIcon message={errorMessage(saveKey.error, editingKey ? '调用密钥保存失败' : '调用密钥创建失败')} /> : null}
        <Form form={form} layout="vertical" onFinish={(value) => saveKey.mutate(value)} requiredMark={false}>
          <Form.Item name="name" label="密钥名称" rules={[{ required: true, whitespace: true, message: '请输入密钥名称' }, { max: 120, message: '最多 120 个字符' }]}>
            <Input placeholder="例如：生产环境后端" autoFocus />
          </Form.Item>
          <Form.Item name="expiresAt" label="过期时间（选填）" extra="留空表示永不过期。">
            <Input type="datetime-local" min={localDateTime(new Date(Date.now() + 60_000))} />
          </Form.Item>
          <div className="gateway-limit-grid">
            <Form.Item name="requestsPerMinute" label="每分钟请求上限" extra="0 表示不限制。" rules={[{ required: true, message: '请填写请求上限' }]}>
              <InputNumber min={0} max={100000} precision={0} />
            </Form.Item>
            <Form.Item name="monthlyTokenLimit" label="每月 Token 上限" extra="按 UTC 自然月统计，0 表示不限制。" rules={[{ required: true, message: '请填写 Token 上限' }]}>
              <InputNumber min={0} max={9_000_000_000_000_000} precision={0} />
            </Form.Item>
          </div>
          <Form.Item name="allowedModelCodes" label="可用模型编码（选填）" extra="留空可调用全部已启用模型；输入编码后回车添加。">
            <Select mode="tags" tokenSeparators={[',']} placeholder="例如：gpt-4o-mini" />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title="保存调用密钥"
        open={Boolean(createdKey)}
        okText="完成"
        cancelButtonProps={{ style: { display: 'none' } }}
        closable={false}
        mask={{ closable: false }}
        onOk={() => { setCreatedKey(null); saveKey.reset() }}
      >
        <Alert type="success" showIcon message="调用密钥已加密保存，之后仍可在列表中查看" />
        <div className="gateway-secret-box">
          <Text code>{createdKey?.secret}</Text>
          <Button icon={<CopyOutlined />} onClick={async () => {
            if (!createdKey) return
            await navigator.clipboard.writeText(createdKey.secret)
            messageApi.success('调用密钥已复制')
          }}>复制</Button>
        </div>
        <Paragraph type="secondary">请保存到服务端密钥管理工具，不要写入前端代码或提交到 Git。</Paragraph>
      </Modal>

      <Modal title={`${revealedKey?.name ?? ''} · 调用密钥`} open={Boolean(revealedKey)} footer={null} onCancel={() => { setRevealedKey(null); revealKey.reset() }} destroyOnHidden>
        <Alert type="info" showIcon message="本次查看已记录到审计日志" />
        <div className="gateway-secret-box">
          <Text code>{revealedKey?.secret}</Text>
          <Button icon={<CopyOutlined />} onClick={async () => { if (!revealedKey) return; await navigator.clipboard.writeText(revealedKey.secret); messageApi.success('调用密钥已复制') }}>复制</Button>
        </div>
      </Modal>
    </main>
  )
}

function GatewayResource({ title, hint, action, error, retry, children }: {
  title: string
  hint: string
  action?: ReactNode
  error: string
  retry: () => void
  children: ReactNode
}) {
  return (
    <Card className="model-resource-card" variant="borderless">
      <div className="model-resource-toolbar">
        <div><strong>{title}</strong><span>{hint}</span></div>
        {action}
      </div>
      {error ? <Alert className="model-resource-error" type="error" showIcon message={error} action={<Button size="small" onClick={retry}>重试</Button>} /> : null}
      {children}
    </Card>
  )
}

function keyStatus(item: GatewayAPIKey) {
  if (item.status === 'revoked') return '已吊销'
  if (item.expiresAt && new Date(item.expiresAt).getTime() <= Date.now()) return '已过期'
  return '可用'
}

function keyTone(item: GatewayAPIKey): 'success' | 'muted' | 'danger' {
  return keyStatus(item) === '可用' ? 'success' : 'muted'
}

function renderPolicyQuota(policy?: ModelPolicy) {
  if (!policy || policy.monthlyTokenLimit <= 0) return <Text type="secondary">未设置额度</Text>
  if (policy.subjectType === 'department' && policy.monthlyTokenMode === 'per_user') {
    return <Text type="secondary">每人 {policy.monthlyTokenLimit.toLocaleString()}；预警按人判断</Text>
  }
  const used = policy.monthlyTokenUsed
  const limit = policy.monthlyTokenLimit
  const ratio = Math.floor(used / limit * 100)
  return <Space size={4}>
    <span>{used.toLocaleString()} / {limit.toLocaleString()}（{ratio}%）</span>
    {used >= limit ? <Tag color="red">已达上限</Tag> : used >= limit - Math.floor(limit / 5) ? <Tag color="orange">接近上限</Tag> : null}
  </Space>
}

function formatTime(value: string) {
  return new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short', hour12: false }).format(new Date(value))
}

function localDateTime(value: Date) {
  const offset = value.getTimezoneOffset() * 60_000
  return new Date(value.getTime() - offset).toISOString().slice(0, 16)
}

function errorMessage(error: unknown, fallback: string) {
  return error instanceof ApiError ? error.message : fallback
}
