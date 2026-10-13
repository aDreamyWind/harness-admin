import {
  CheckCircleOutlined,
  CopyOutlined,
  KeyOutlined,
  PlusOutlined,
  ReloadOutlined,
  SwapOutlined,
} from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Alert,
  Button,
  Card,
  Form,
  Input,
  Modal,
  Popconfirm,
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
  type CreatedGatewayAPIKey,
  type GatewayAPIKey,
  type GatewayCall,
} from '../api/modelGateway'
import { useAuth } from '../auth/AuthContext'
import { StatusIndicator } from '../components/StatusIndicator'

const { Paragraph, Text } = Typography

interface APIKeyFormValue {
  name: string
  expiresAt?: string
}

export function ModelGatewayPage() {
  const { hasPermission } = useAuth()
  const canWrite = hasPermission('model.gateway.write')
  const queryClient = useQueryClient()
  const [messageApi, messageContext] = message.useMessage()
  const [form] = Form.useForm<APIKeyFormValue>()
  const [createOpen, setCreateOpen] = useState(false)
  const [createdKey, setCreatedKey] = useState<CreatedGatewayAPIKey | null>(null)

  const keysQuery = useQuery({ queryKey: ['model-gateway-api-keys'], queryFn: listGatewayAPIKeys })
  const callsQuery = useQuery({ queryKey: ['model-gateway-calls'], queryFn: listGatewayCalls, refetchInterval: 15000 })
  const keys = keysQuery.data ?? []
  const calls = callsQuery.data ?? []

  const createKey = useMutation({
    mutationFn: (value: APIKeyFormValue) => createGatewayAPIKey({
      name: value.name.trim(),
      expiresAt: value.expiresAt ? new Date(value.expiresAt).toISOString() : undefined,
    }),
    onSuccess: async (result) => {
      setCreateOpen(false)
      form.resetFields()
      setCreatedKey(result)
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

  const keyColumns: TableColumnsType<GatewayAPIKey> = useMemo(() => [
    {
      title: '调用密钥', key: 'identity', width: 230,
      render: (_, item) => <div className="gateway-key-name"><KeyOutlined /><span><strong>{item.name}</strong><small>{item.keyPrefix}••••••••</small></span></div>,
    },
    { title: '状态', key: 'status', width: 110, render: (_, item) => <StatusIndicator label={keyStatus(item)} tone={keyTone(item)} /> },
    { title: '有效期', key: 'expiresAt', width: 180, render: (_, item) => item.expiresAt ? formatTime(item.expiresAt) : '永不过期' },
    { title: '最近调用', key: 'lastUsedAt', width: 180, render: (_, item) => item.lastUsedAt ? formatTime(item.lastUsedAt) : '尚未使用' },
    { title: '创建时间', dataIndex: 'createdAt', key: 'createdAt', width: 180, render: formatTime },
    ...(canWrite ? [{
      title: '操作', key: 'actions', width: 100,
      render: (_: unknown, item: GatewayAPIKey) => item.status === 'active' ? (
        <Popconfirm title="吊销这个调用密钥？" description="吊销后立即失效，且无法恢复。" onConfirm={() => revokeKey.mutate(item.id)}>
          <Button type="link" danger size="small">吊销</Button>
        </Popconfirm>
      ) : <Text type="secondary">—</Text>,
    }] : []),
  ], [canWrite, revokeKey])

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
        action={canWrite ? <Button type="primary" icon={<PlusOutlined />} onClick={() => { createKey.reset(); setCreateOpen(true) }}>创建调用密钥</Button> : undefined}
        error={keysQuery.isError ? errorMessage(keysQuery.error, '调用密钥加载失败') : ''}
        retry={() => keysQuery.refetch()}
      >
        <Table<GatewayAPIKey> rowKey="id" columns={keyColumns} dataSource={keys} loading={keysQuery.isLoading} pagination={false} scroll={{ x: 1000 }} locale={{ emptyText: '尚未创建调用密钥' }} />
      </GatewayResource>,
    },
    {
      key: 'calls', label: `调用记录 ${calls.length}`,
      children: <GatewayResource
        title="最近调用"
        hint="只记录路由、结果和用量，不保存提示词与模型回答。"
        action={<Button icon={<ReloadOutlined />} loading={callsQuery.isFetching} onClick={() => callsQuery.refetch()}>刷新</Button>}
        error={callsQuery.isError ? errorMessage(callsQuery.error, '调用记录加载失败') : ''}
        retry={() => callsQuery.refetch()}
      >
        <Table<GatewayCall> rowKey="id" columns={callColumns} dataSource={calls} loading={callsQuery.isLoading} pagination={{ pageSize: 20, hideOnSinglePage: true }} scroll={{ x: 1250 }} locale={{ emptyText: '尚无模型调用记录' }} />
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
        title="创建调用密钥"
        open={createOpen}
        okText="创建"
        cancelText="取消"
        confirmLoading={createKey.isPending}
        onCancel={() => { setCreateOpen(false); form.resetFields(); createKey.reset() }}
        onOk={() => form.submit()}
        destroyOnHidden
      >
        {createKey.isError ? <Alert type="error" showIcon message={errorMessage(createKey.error, '调用密钥创建失败')} /> : null}
        <Form form={form} layout="vertical" onFinish={(value) => createKey.mutate(value)} requiredMark={false}>
          <Form.Item name="name" label="密钥名称" rules={[{ required: true, whitespace: true, message: '请输入密钥名称' }, { max: 120, message: '最多 120 个字符' }]}>
            <Input placeholder="例如：生产环境后端" autoFocus />
          </Form.Item>
          <Form.Item name="expiresAt" label="过期时间（选填）" extra="留空表示永不过期。">
            <Input type="datetime-local" min={localDateTime(new Date(Date.now() + 60_000))} />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title="保存调用密钥"
        open={Boolean(createdKey)}
        okText="我已保存"
        cancelButtonProps={{ style: { display: 'none' } }}
        closable={false}
        maskClosable={false}
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
