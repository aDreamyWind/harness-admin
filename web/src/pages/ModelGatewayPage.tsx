import {
  CheckCircleOutlined,
  CopyOutlined,
  EditOutlined,
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
  createGatewayAPIKey,
  listGatewayAPIKeys,
  listGatewayCalls,
  revokeGatewayAPIKey,
  updateGatewayAPIKey,
  type CreatedGatewayAPIKey,
  type GatewayAPIKey,
  type GatewayCall,
  type GatewayCallQuery,
} from '../api/modelGateway'
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
  const queryClient = useQueryClient()
  const [messageApi, messageContext] = message.useMessage()
  const [form] = Form.useForm<APIKeyFormValue>()
  const [filterForm] = Form.useForm<CallFilterValue>()
  const [keyModalOpen, setKeyModalOpen] = useState(false)
  const [editingKey, setEditingKey] = useState<GatewayAPIKey | null>(null)
  const [createdKey, setCreatedKey] = useState<CreatedGatewayAPIKey | null>(null)
  const [callQuery, setCallQuery] = useState<GatewayCallQuery>({ page: 1, pageSize: 20 })

  const keysQuery = useQuery({ queryKey: ['model-gateway-api-keys'], queryFn: listGatewayAPIKeys })
  const callsQuery = useQuery({ queryKey: ['model-gateway-calls', callQuery], queryFn: () => listGatewayCalls(callQuery), refetchInterval: 15000 })
  const keys = keysQuery.data ?? []
  const callsPage = callsQuery.data ?? { items: [], total: 0, page: callQuery.page, pageSize: callQuery.pageSize }

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
      title: '操作', key: 'actions', width: 140,
      render: (_: unknown, item: GatewayAPIKey) => item.status === 'active' ? (
        <Space size={4}>
          <Button type="link" size="small" icon={<EditOutlined />} onClick={() => openEditKey(item)}>编辑</Button>
          <Popconfirm title="吊销这个调用密钥？" description="吊销后立即失效，且无法恢复。" onConfirm={() => revokeKey.mutate(item.id)}>
            <Button type="link" danger size="small">吊销</Button>
          </Popconfirm>
        </Space>
      ) : <Text type="secondary">—</Text>,
    }] : []),
  ], [canWrite, revokeKey, saveKey])

  const callColumns: TableColumnsType<GatewayCall> = useMemo(() => [
    { title: '发生时间', dataIndex: 'createdAt', key: 'createdAt', width: 178, render: formatTime },
    {
      title: '请求流向', key: 'flow', width: 300,
      render: (_, item) => <div className="gateway-call-flow"><strong>{item.modelCode}</strong><SwapOutlined /><span>{item.upstreamModelId}</span>{item.streaming ? <Tag>流式</Tag> : null}</div>,
    },
    { title: '调用密钥', dataIndex: 'apiKeyName', key: 'apiKeyName', width: 150 },
    { title: '结果', key: 'result', width: 105, render: (_, item) => <StatusIndicator label={item.result === 'success' ? '成功' : '失败'} tone={item.result === 'success' ? 'success' : 'danger'} /> },
    { title: 'HTTP', dataIndex: 'httpStatus', key: 'httpStatus', width: 80 },
    { title: '耗时', dataIndex: 'latencyMs', key: 'latencyMs', width: 100, render: (value: number) => `${value} ms` },
    { title: 'Token', dataIndex: 'totalTokens', key: 'totalTokens', width: 100, render: (value: number) => value || '—' },
    { title: '请求 ID', dataIndex: 'requestId', key: 'requestId', width: 210, render: (value: string) => <Text code copyable>{value}</Text> },
  ], [])

  const tabs = [
    {
      key: 'keys', label: `调用密钥 ${keys.length}`,
      children: <GatewayResource
        title="调用密钥"
        hint="完整密钥只在创建时展示一次。"
        action={canWrite ? <Button type="primary" icon={<PlusOutlined />} onClick={openCreateKey}>创建调用密钥</Button> : undefined}
        error={keysQuery.isError ? errorMessage(keysQuery.error, '调用密钥加载失败') : ''}
        retry={() => keysQuery.refetch()}
      >
        <Table<GatewayAPIKey> rowKey="id" columns={keyColumns} dataSource={keys} loading={keysQuery.isLoading} pagination={false} scroll={{ x: 1380 }} locale={{ emptyText: '尚未创建调用密钥' }} />
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

      <Tabs className="model-resource-tabs" items={tabs} />

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
        okText="我已保存"
        cancelButtonProps={{ style: { display: 'none' } }}
        closable={false}
        mask={{ closable: false }}
        onOk={() => setCreatedKey(null)}
      >
        <Alert type="warning" showIcon message="关闭后无法再次查看完整密钥" />
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
