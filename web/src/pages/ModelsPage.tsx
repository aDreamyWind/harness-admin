import {
  ApiOutlined,
  CheckCircleOutlined,
  CloudServerOutlined,
  EditOutlined,
  EyeOutlined,
  PlusOutlined,
  ReloadOutlined,
} from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Alert,
  Button,
  Card,
  Checkbox,
  Collapse,
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
  changeModelProviderStatus,
  changeModelStatus,
  createModel,
  createModelProvider,
  listModelProviders,
  listModels,
  probeModelProvider,
  revealModelProviderCredential,
  updateModel,
  updateModelProvider,
  type ModelCatalogEntry,
  type ModelInput,
  type ModelProvider,
  type ProviderKind,
  type ProviderInput,
} from '../api/models'
import { useAuth } from '../auth/AuthContext'
import { StatusIndicator } from '../components/StatusIndicator'

const { Text } = Typography
const providerPresets: Array<{ value: ProviderKind; label: string; baseUrl: string }> = [
  { value: 'openai_compatible', label: 'OpenAI / OpenAI 兼容', baseUrl: 'https://api.openai.com/v1' },
  { value: 'deepseek', label: 'DeepSeek', baseUrl: 'https://api.deepseek.com' },
  { value: 'aliyun_bailian', label: '阿里云百炼', baseUrl: 'https://dashscope.aliyuncs.com/compatible-mode/v1' },
  { value: 'kimi', label: 'Kimi', baseUrl: 'https://api.moonshot.cn/v1' },
  { value: 'ollama', label: 'Ollama', baseUrl: 'http://localhost:11434/v1' },
]
const providerPresetByKind = new Map(providerPresets.map((item) => [item.value, item]))
const capabilityOptions = [
  { label: '文本', value: 'text' },
  { label: '视觉', value: 'vision' },
  { label: '工具调用', value: 'tools' },
  { label: '流式响应', value: 'streaming' },
  { label: 'JSON 输出', value: 'json' },
]
const capabilityLabels = new Map(capabilityOptions.map((item) => [item.value, item.label]))

export function ModelsPage() {
  const { hasPermission } = useAuth()
  const canReadProviders = hasPermission('model.provider.read')
  const canWriteProviders = hasPermission('model.provider.write')
  const canReadModels = hasPermission('model.catalog.read')
  const canWriteModels = hasPermission('model.catalog.write')
  const queryClient = useQueryClient()
  const [messageApi, messageContext] = message.useMessage()
  const [providerForm] = Form.useForm<ProviderInput>()
  const [modelForm] = Form.useForm<ModelInput>()
  const [providerModalOpen, setProviderModalOpen] = useState(false)
  const [modelModalOpen, setModelModalOpen] = useState(false)
  const [editingProvider, setEditingProvider] = useState<ModelProvider | null>(null)
  const [editingModel, setEditingModel] = useState<ModelCatalogEntry | null>(null)
  const [revealedProviderSecret, setRevealedProviderSecret] = useState<{ name: string; secret: string } | null>(null)

  const providersQuery = useQuery({ queryKey: ['model-providers'], queryFn: listModelProviders, enabled: canReadProviders })
  const modelsQuery = useQuery({ queryKey: ['model-catalog'], queryFn: listModels, enabled: canReadModels })
  const providers = providersQuery.data ?? []
  const models = modelsQuery.data ?? []

  const saveProvider = useMutation({
    mutationFn: (values: ProviderInput) => {
      const payload = { ...values, apiKey: values.apiKey?.trim() || undefined }
      return editingProvider ? updateModelProvider(editingProvider.id, payload) : createModelProvider(payload)
    },
    onSuccess: async () => {
      messageApi.success(editingProvider ? '供应商连接已更新' : '供应商连接已创建')
      closeProviderModal()
      await queryClient.invalidateQueries({ queryKey: ['model-providers'] })
    },
  })
  const providerStatus = useMutation({
    mutationFn: (provider: ModelProvider) => changeModelProviderStatus(provider.id, provider.status === 'active' ? 'disabled' : 'active'),
    onSuccess: async () => { messageApi.success('供应商连接状态已更新'); await queryClient.invalidateQueries({ queryKey: ['model-providers'] }) },
    onError: (error) => messageApi.error(errorMessage(error, '供应商连接状态更新失败')),
  })
  const probeProvider = useMutation({
    mutationFn: probeModelProvider,
    onSuccess: async (result) => {
      messageApi.success(`连接正常，响应耗时 ${result.latencyMs} ms`)
      await queryClient.invalidateQueries({ queryKey: ['model-providers'] })
    },
    onError: (error) => messageApi.error(errorMessage(error, '连接检测失败')),
  })
  const revealProviderSecret = useMutation({
    mutationFn: async (provider: ModelProvider) => ({ provider, result: await revealModelProviderCredential(provider.id) }),
    onSuccess: ({ provider, result }) => setRevealedProviderSecret({ name: provider.name, secret: result.secret }),
    onError: (error) => messageApi.error(errorMessage(error, 'API Key 查看失败')),
  })

  const saveModel = useMutation({
    mutationFn: (values: ModelInput) => editingModel ? updateModel(editingModel.id, values) : createModel(values),
    onSuccess: async () => {
      messageApi.success(editingModel ? '模型已更新' : '模型已登记')
      closeModelModal()
      await queryClient.invalidateQueries({ queryKey: ['model-catalog'] })
    },
  })
  const modelStatus = useMutation({
    mutationFn: (model: ModelCatalogEntry) => changeModelStatus(model.id, model.status === 'active' ? 'disabled' : 'active'),
    onSuccess: async () => { messageApi.success('模型状态已更新'); await queryClient.invalidateQueries({ queryKey: ['model-catalog'] }) },
    onError: (error) => messageApi.error(errorMessage(error, '模型状态更新失败')),
  })

  const activeProviders = providers.filter((item) => item.status === 'active').length
  const healthyProviders = providers.filter((item) => item.lastProbeStatus === 'success').length
  const activeModels = models.filter((item) => item.status === 'active').length

  function openProviderCreate() {
    setEditingProvider(null)
    providerForm.setFieldsValue({ code: '', name: '', providerKind: 'openai_compatible', baseUrl: providerPresetByKind.get('openai_compatible')?.baseUrl, apiKey: '', requestTimeoutMs: 15000 })
    saveProvider.reset()
    setProviderModalOpen(true)
  }

  function openProviderEdit(provider: ModelProvider) {
    setEditingProvider(provider)
    providerForm.setFieldsValue({ code: provider.code, name: provider.name, providerKind: provider.providerKind, baseUrl: provider.baseUrl, apiKey: '', requestTimeoutMs: provider.requestTimeoutMs })
    saveProvider.reset()
    setProviderModalOpen(true)
  }

  function closeProviderModal() {
    setProviderModalOpen(false)
    setEditingProvider(null)
    providerForm.resetFields()
    saveProvider.reset()
  }

  function openModelCreate() {
    setEditingModel(null)
    modelForm.setFieldsValue({ code: '', providerConnectionId: providers.find((item) => item.status === 'active')?.id ?? '', upstreamModelId: '', name: '', modelType: 'chat', capabilities: ['text', 'streaming'], contextWindowTokens: 0, maxOutputTokens: 0 })
    saveModel.reset()
    setModelModalOpen(true)
  }

  function openModelEdit(model: ModelCatalogEntry) {
    setEditingModel(model)
    modelForm.setFieldsValue({ code: model.code, providerConnectionId: model.providerConnectionId, upstreamModelId: model.upstreamModelId, name: model.name, modelType: model.modelType, capabilities: model.capabilities, contextWindowTokens: model.contextWindowTokens, maxOutputTokens: model.maxOutputTokens })
    saveModel.reset()
    setModelModalOpen(true)
  }

  function closeModelModal() {
    setModelModalOpen(false)
    setEditingModel(null)
    modelForm.resetFields()
    saveModel.reset()
  }

  function handleSaveModel(values: ModelInput) {
    const upstreamModelId = values.upstreamModelId.trim()
    const providerCode = providers.find((provider) => provider.id === values.providerConnectionId)?.code ?? 'model'
    saveModel.mutate({
      ...values,
      upstreamModelId,
      name: values.name?.trim() || upstreamModelId,
      code: editingModel ? editingModel.code : values.code?.trim() || generateModelCode(providerCode, upstreamModelId),
    })
  }

  const providerColumns: TableColumnsType<ModelProvider> = useMemo(() => [
    {
      title: '连接', key: 'identity', width: 210,
      render: (_, provider) => <div className="model-resource-name"><CloudServerOutlined /><span><strong>{provider.name}</strong><small>{provider.code}</small></span></div>,
    },
    { title: '供应商协议', dataIndex: 'providerKind', key: 'providerKind', width: 170, render: (value: ProviderKind) => providerPresetByKind.get(value)?.label ?? value },
    { title: 'API 地址', dataIndex: 'baseUrl', key: 'baseUrl', render: (value: string) => <Text code>{value}</Text> },
    { title: '状态', key: 'status', width: 110, render: (_, provider) => <StatusIndicator label={provider.status === 'active' ? '已启用' : '已停用'} tone={provider.status === 'active' ? 'success' : 'muted'} /> },
    {
      title: '最近检测', key: 'probe', width: 150,
      render: (_, provider) => <div className="probe-state"><StatusIndicator label={probeLabel(provider.lastProbeStatus)} tone={probeTone(provider.lastProbeStatus)} />{provider.lastProbeAt ? <small>{formatTime(provider.lastProbeAt)}</small> : null}</div>,
    },
    ...(canWriteProviders ? [{
      title: '操作', key: 'actions', width: 290,
      render: (_: unknown, provider: ModelProvider) => <Space size={4}>
        <Button type="link" size="small" icon={<CheckCircleOutlined />} loading={probeProvider.isPending && probeProvider.variables === provider.id} disabled={provider.status !== 'active'} onClick={() => probeProvider.mutate(provider.id)}>检测</Button>
        <Button type="link" size="small" icon={<EyeOutlined />} loading={revealProviderSecret.isPending && revealProviderSecret.variables?.id === provider.id} onClick={() => revealProviderSecret.mutate(provider)}>查看密钥</Button>
        <Button type="link" size="small" icon={<EditOutlined />} onClick={() => openProviderEdit(provider)}>编辑</Button>
        <Popconfirm title={provider.status === 'active' ? '停用这个供应商连接？' : '重新启用这个供应商连接？'} onConfirm={() => providerStatus.mutate(provider)}>
          <Button type="link" size="small" danger={provider.status === 'active'}>{provider.status === 'active' ? '停用' : '启用'}</Button>
        </Popconfirm>
      </Space>,
    }] : []),
  ], [canWriteProviders, probeProvider.isPending, probeProvider.variables, revealProviderSecret.isPending, revealProviderSecret.variables])

  const modelColumns: TableColumnsType<ModelCatalogEntry> = useMemo(() => [
    {
      title: '模型', key: 'identity', width: 210,
      render: (_, model) => <div className="model-resource-name model-entry-name"><ApiOutlined /><span><strong>{model.name}</strong><small>{model.code}</small></span></div>,
    },
    { title: '供应商', dataIndex: 'providerName', key: 'providerName', width: 160 },
    { title: '上游模型标识', dataIndex: 'upstreamModelId', key: 'upstreamModelId', render: (value: string) => <Text code>{value}</Text> },
    {
      title: '能力', dataIndex: 'capabilities', key: 'capabilities', width: 250,
      render: (values: string[]) => <div className="model-capabilities">{values.map((value) => <Tag key={value}>{capabilityLabels.get(value) ?? value}</Tag>)}</div>,
    },
    { title: '状态', key: 'status', width: 100, render: (_, model) => <StatusIndicator label={model.status === 'active' ? '已启用' : '已停用'} tone={model.status === 'active' ? 'success' : 'muted'} /> },
    ...(canWriteModels ? [{
      title: '操作', key: 'actions', width: 150,
      render: (_: unknown, model: ModelCatalogEntry) => <Space size={4}>
        <Button type="link" size="small" icon={<EditOutlined />} onClick={() => openModelEdit(model)}>编辑</Button>
        <Popconfirm title={model.status === 'active' ? '停用这个模型？' : '重新启用这个模型？'} onConfirm={() => modelStatus.mutate(model)}>
          <Button type="link" size="small" danger={model.status === 'active'}>{model.status === 'active' ? '停用' : '启用'}</Button>
        </Popconfirm>
      </Space>,
    }] : []),
  ], [canWriteModels])

  const tabs = [
    canReadProviders ? {
      key: 'providers', label: `供应商连接 ${providers.length}`,
      children: <ResourceTable
        title="上游连接" description="连接凭据加密保存，具备管理权限的用户可以查看。"
        action={canWriteProviders ? <Button type="primary" icon={<PlusOutlined />} onClick={openProviderCreate}>接入供应商</Button> : undefined}
        error={providersQuery.isError ? errorMessage(providersQuery.error, '供应商连接加载失败') : ''}
        retry={() => providersQuery.refetch()}
      ><Table<ModelProvider> rowKey="id" columns={providerColumns} dataSource={providers} loading={providersQuery.isLoading} pagination={false} scroll={{ x: 980 }} locale={{ emptyText: '尚未接入模型供应商' }} /></ResourceTable>,
    } : null,
    canReadModels ? {
      key: 'models', label: `模型 ${models.length}`,
      children: <ResourceTable
        title="企业模型目录" description="稳定模型编码供后续网关和策略引用，上游模型标识可随连接调整。"
        action={canWriteModels ? <Button type="primary" icon={<PlusOutlined />} disabled={!activeProviders} onClick={openModelCreate}>登记模型</Button> : undefined}
        error={modelsQuery.isError ? errorMessage(modelsQuery.error, '模型目录加载失败') : ''}
        retry={() => modelsQuery.refetch()}
      ><Table<ModelCatalogEntry> rowKey="id" columns={modelColumns} dataSource={models} loading={modelsQuery.isLoading} pagination={false} scroll={{ x: 1050 }} locale={{ emptyText: providers.length ? '尚未登记模型' : '请先接入供应商，再登记模型' }} /></ResourceTable>,
    } : null,
  ].filter(Boolean) as Array<{ key: string; label: string; children: ReactNode }>

  return (
    <main>
      {messageContext}
      <section className="module-heading">
        <div>
          <Text className="module-kicker">AI 资源管理 / 模型目录</Text>
        </div>
      </section>

      <section className="model-health-rail" aria-label="模型资源概览">
        <div><span>供应商连接</span><strong>{activeProviders}<small> / {providers.length}</small></strong><em>已启用</em></div>
        <div><span>最近检测正常</span><strong>{healthyProviders}<small> / {providers.length}</small></strong><em>可连接</em></div>
        <div><span>目录模型</span><strong>{activeModels}<small> / {models.length}</small></strong><em>已启用</em></div>
        <div className="model-health-flow"><CloudServerOutlined /><i /><ApiOutlined /><span>连接 → 目录 → 网关</span></div>
      </section>

      <Tabs className="model-resource-tabs" items={tabs} />

      <Modal title={`${revealedProviderSecret?.name ?? ''} · API Key`} open={Boolean(revealedProviderSecret)} footer={null} onCancel={() => { setRevealedProviderSecret(null); revealProviderSecret.reset() }} destroyOnHidden>
        <Alert type="info" showIcon message="本次查看已记录到审计日志" />
        <div className="gateway-secret-box">
          <Text code>{revealedProviderSecret?.secret}</Text>
          <Button onClick={async () => { if (!revealedProviderSecret) return; await navigator.clipboard.writeText(revealedProviderSecret.secret); messageApi.success('API Key 已复制') }}>复制</Button>
        </div>
      </Modal>

      <Modal title={editingProvider ? '编辑供应商连接' : '接入供应商'} open={providerModalOpen} width={680} okText={editingProvider ? '保存修改' : '创建连接'} cancelText="取消" confirmLoading={saveProvider.isPending} onCancel={closeProviderModal} onOk={() => providerForm.submit()} forceRender>
        {saveProvider.isError ? <Alert className="form-alert" type="error" showIcon message={errorMessage(saveProvider.error, '供应商连接保存失败')} /> : null}
        <Form<ProviderInput> form={providerForm} layout="vertical" requiredMark={false} onFinish={(values) => saveProvider.mutate(values)}>
          <div className="model-form-grid">
            <Form.Item label="连接名称" name="name" rules={[{ required: true, whitespace: true, message: '请输入连接名称' }, { max: 120 }]}><Input placeholder="例如：生产模型服务" /></Form.Item>
            <Form.Item label="连接编码" name="code" extra="企业内唯一，创建后不可修改。" rules={editingProvider ? [] : [{ required: true, message: '请输入连接编码' }, { pattern: /^[a-z][a-z0-9._-]{1,79}$/, message: '请输入至少 2 位的小写编码' }]}><Input disabled={Boolean(editingProvider)} placeholder="例如：production_ai" /></Form.Item>
          </div>
          <Form.Item label="供应商协议" name="providerKind" extra="选择后自动填入推荐地址，API 基础地址仍可修改。">
            <Select
              options={providerPresets.map(({ value, label }) => ({ value, label }))}
              onChange={(value: ProviderKind) => providerForm.setFieldValue('baseUrl', providerPresetByKind.get(value)?.baseUrl ?? '')}
            />
          </Form.Item>
          <Form.Item label="API 基础地址" name="baseUrl" extra="填写到版本路径，例如 https://models.example.com/v1。" rules={[{ required: true, type: 'url', message: '请输入完整的 HTTP(S) 地址' }]}><Input placeholder="https://models.example.com/v1" /></Form.Item>
          <div className="model-form-grid">
            <Form.Item label={editingProvider ? '替换 API Key' : 'API Key'} name="apiKey" extra={editingProvider ? '留空表示继续使用当前凭据。' : '密钥将加密保存。'} rules={editingProvider ? [] : [{ required: true, message: '请输入 API Key' }]}><Input.Password autoComplete="new-password" placeholder={editingProvider ? '不修改请留空' : '输入供应商访问密钥'} /></Form.Item>
            <Form.Item label="请求超时（毫秒）" name="requestTimeoutMs" rules={[{ required: true }]}><InputNumber min={1000} max={120000} step={1000} style={{ width: '100%' }} /></Form.Item>
          </div>
        </Form>
      </Modal>

      <Modal title={editingModel ? '编辑模型' : '登记模型'} open={modelModalOpen} width={720} okText={editingModel ? '保存修改' : '登记模型'} cancelText="取消" confirmLoading={saveModel.isPending} onCancel={closeModelModal} onOk={() => modelForm.submit()} forceRender>
        {saveModel.isError ? <Alert className="form-alert" type="error" showIcon message={errorMessage(saveModel.error, '模型保存失败')} /> : null}
        <Form<ModelInput> form={modelForm} layout="vertical" requiredMark={false} onFinish={handleSaveModel}>
          <div className="model-form-grid">
            <Form.Item label="供应商连接" name="providerConnectionId" rules={[{ required: true, message: '请选择供应商连接' }]}><Select options={providers.map((provider) => ({ value: provider.id, label: provider.name, disabled: provider.status !== 'active' }))} /></Form.Item>
            <Form.Item label="上游模型标识" name="upstreamModelId" extra="供应商实际使用的模型 ID。" rules={[{ required: true, whitespace: true, message: '请输入上游模型标识' }, { max: 200 }]}><Input placeholder="例如：deepseek-chat" /></Form.Item>
          </div>
          <Form.Item label="模型名称（选填）" name="name" extra="留空时使用上游模型标识。" rules={[{ max: 120 }]}><Input placeholder="例如：DeepSeek 通用对话模型" /></Form.Item>
          <Form.Item name="modelType" hidden><Input /></Form.Item>
          <Form.Item label="模型能力" name="capabilities"><Checkbox.Group options={capabilityOptions} /></Form.Item>
          <Collapse
            className="model-advanced-settings"
            ghost
            items={[{
              key: 'advanced',
              label: '高级设置',
              children: <>
                <Form.Item label="模型编码" name="code" extra={editingModel ? '模型编码创建后不可修改。' : '留空时根据供应商连接和上游模型标识自动生成。'} rules={editingModel ? [] : [{ pattern: /^[a-z][a-z0-9._-]{1,79}$/, message: '请输入至少 2 位的小写编码' }]}><Input disabled={Boolean(editingModel)} placeholder="例如：deepseek_main_deepseek-chat" /></Form.Item>
                <div className="model-form-grid">
                  <Form.Item label="上下文窗口 Token" name="contextWindowTokens" extra="未知时填写 0。"><InputNumber min={0} style={{ width: '100%' }} /></Form.Item>
                  <Form.Item label="最大输出 Token" name="maxOutputTokens" extra="未知时填写 0。"><InputNumber min={0} style={{ width: '100%' }} /></Form.Item>
                </div>
              </>,
            }]}
          />
        </Form>
      </Modal>
    </main>
  )
}

function ResourceTable({ title, description, action, error, retry, children }: { title: string; description: string; action?: ReactNode; error: string; retry: () => void; children: ReactNode }) {
  return <Card className="directory-card model-resource-card" variant="borderless">
    <div className="model-resource-toolbar"><div><strong>{title}</strong><span>{description}</span></div>{action}</div>
    {error ? <Alert className="model-resource-error" type="error" showIcon message={error} action={<Button size="small" icon={<ReloadOutlined />} onClick={retry}>重新加载</Button>} /> : children}
  </Card>
}

function probeLabel(status: ModelProvider['lastProbeStatus']) {
  return status === 'success' ? '连接正常' : status === 'failed' ? '连接失败' : '尚未检测'
}

function probeTone(status: ModelProvider['lastProbeStatus']): 'success' | 'danger' | 'muted' {
  return status === 'success' ? 'success' : status === 'failed' ? 'danger' : 'muted'
}

function formatTime(value: string) {
  return new Intl.DateTimeFormat('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }).format(new Date(value))
}

function errorMessage(error: unknown, fallback: string) {
  return error instanceof ApiError || error instanceof Error ? error.message : fallback
}

function generateModelCode(providerCode: string, upstreamModelId: string) {
  const normalized = `${providerCode}_${upstreamModelId}`
    .toLowerCase()
    .replace(/[^a-z0-9._-]+/g, '_')
    .replace(/^[^a-z]+/, '')
    .replace(/[._-]+$/, '')
  return (normalized || 'model').slice(0, 80).replace(/[._-]+$/, '') || 'model'
}
