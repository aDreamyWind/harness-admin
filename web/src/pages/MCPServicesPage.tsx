import { ApiOutlined, CloudUploadOutlined, CopyOutlined, ImportOutlined, MinusCircleOutlined, PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Alert, Button, Card, Checkbox, Empty, Form, Input, Modal, Radio, Segmented, Select, Space, Table, Tag, Tooltip, Typography, message } from 'antd'
import { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'

import { ApiError } from '../api/client'
import {
  changeMCPServiceStatus, createMCPService, discoverMCPTools, listMCPServices, listMCPTools,
  updateMCPService,
  type MCPParameterDefinition, type MCPService, type MCPTool,
} from '../api/mcp'
import { listMCPConnections } from '../api/mcpConnections'
import { buildMCPDeployment, changeMCPDeployment, getMCPDeploymentCapability, listMCPDeploymentVersions,
  updateMCPDeploymentMetadata, type MCPDeploymentInput } from '../api/mcpDeployments'
import { useAuth } from '../auth/AuthContext'
import { MCPDeploymentDialog } from '../components/MCPDeploymentDialog'
import { MCPDeploymentPanel } from '../components/MCPDeploymentPanel'
import { MarkdownContent } from '../components/MarkdownContent'
import { MCPToolTestPanel } from '../components/MCPToolTestPanel'
import { MCPConnectionsPage } from './MCPConnectionsPage'

const { Text } = Typography

type ServiceForm = { name: string; description?: string; transport: MCPService['transport']; upstreamUrl: string; rows: MCPParameterDefinition[] }
type PendingDeployment = { serviceId: string; versionId: string }
const pendingDeploymentKey = 'harness-admin:pending-mcp-deployment'

function readPendingDeployment(key: string): PendingDeployment | null {
  try {
    const value = JSON.parse(sessionStorage.getItem(key) || 'null') as Partial<PendingDeployment> | null
    return typeof value?.serviceId === 'string' && typeof value.versionId === 'string'
      ? { serviceId: value.serviceId, versionId: value.versionId } : null
  } catch {
    return null
  }
}

export function MCPServicesPage() {
  const [searchParams] = useSearchParams()
  const { user, hasPermission } = useAuth()
  const pendingStorageKey = `${pendingDeploymentKey}:${user?.id ?? ''}`
  const canWrite = hasPermission('mcp.service.write')
  const canReadConnections = hasPermission('mcp.connection.read')
  const queryClient = useQueryClient()
  const [messageApi, contextHolder] = message.useMessage()
  const [serviceForm] = Form.useForm<ServiceForm>()
  const [editorMode, setEditorMode] = useState<'create' | 'edit' | null>(null)
  const [importOpen, setImportOpen] = useState(false)
  const [importText, setImportText] = useState('')
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [exampleId, setExampleId] = useState<string | null>(null)
  const [toolConnectionId, setToolConnectionId] = useState<string | null>(null)
  const [testing, setTesting] = useState<MCPTool | null>(null)
  const [descriptionExpanded, setDescriptionExpanded] = useState(false)
  const [descriptionMode, setDescriptionMode] = useState<'编辑' | '预览'>('编辑')
  const [deploymentOpen, setDeploymentOpen] = useState(false)
  const [pendingDeployment, setPendingDeployment] = useState<PendingDeployment | null>(() => readPendingDeployment(pendingStorageKey))
  const descriptionPreview = Form.useWatch('description', serviceForm) ?? ''

	const servicesQuery = useQuery({ queryKey: ['mcp-services'], queryFn: listMCPServices,
	  refetchInterval: (query) => query.state.data?.some((item) => item.deploymentStatus === 'building') ? 3000 : false })
  const deploymentCapability = useQuery({ queryKey: ['mcp-deployment-capability'], queryFn: getMCPDeploymentCapability, enabled: canWrite })
  const pendingVersions = useQuery({
    queryKey: ['mcp-deployment-versions', pendingDeployment?.serviceId],
    queryFn: () => listMCPDeploymentVersions(pendingDeployment!.serviceId),
    enabled: Boolean(pendingDeployment),
    refetchInterval: (query) => {
      const version = query.state.data?.find((item) => item.id === pendingDeployment?.versionId)
      return !version || version.buildStatus === 'building' ? 3000 : false
    },
  })
  const selected = servicesQuery.data?.find((item) => item.id === selectedId)
    ?? servicesQuery.data?.find((item) => item.id === searchParams.get('service'))
    ?? servicesQuery.data?.[0] ?? null
  const isManagedService = selected?.sourceType === 'github' || selected?.sourceType === 'gitee' || selected?.sourceType === 'upload'
  const toolsQuery = useQuery({
    queryKey: ['mcp-tools', selected?.id], queryFn: () => listMCPTools(selected!.id), enabled: Boolean(selected),
  })
  const connectionsQuery = useQuery({
    queryKey: ['mcp-connections'], queryFn: listMCPConnections, enabled: canReadConnections,
  })
  const connections = (connectionsQuery.data ?? []).filter((item) => item.serviceId === selected?.id)
  const example = connections.find((item) => item.id === exampleId) ?? connections[0]
  const toolConnection = connections.find((item) => item.id === toolConnectionId && item.status === 'active')
    ?? connections.find((item) => item.status === 'active')
  const toolsNeedConnection = Boolean(selected?.parameters.some((parameter) => parameter.required))

  const create = useMutation({
    mutationFn: (values: ServiceForm) => createMCPService(serviceInput(values)),
    onSuccess: async (item) => {
      setEditorMode(null)
      serviceForm.resetFields()
      setSelectedId(item.id)
      messageApi.success('MCP 服务已接入')
      await queryClient.invalidateQueries({ queryKey: ['mcp-services'] })
      if (item.parameters.some((parameter) => parameter.required)) {
        messageApi.info('此服务需要连接参数；生成连接后会自动获取工具')
      } else {
        try {
          const tools = await discoverMCPTools(item.id)
          await queryClient.invalidateQueries({ queryKey: ['mcp-tools', item.id] })
          messageApi.success(`已自动获取 ${tools.length} 个工具`)
        } catch (error) {
          messageApi.warning(`服务已接入，但自动获取工具失败：${errorMessage(error, '请检查上游配置')}`)
        }
      }
    },
    onError: (error) => messageApi.error(errorMessage(error, '接入 MCP 服务失败')),
  })
  async function refreshDeploymentTools(serviceId: string) {
    const tools = await discoverMCPTools(serviceId)
    await queryClient.invalidateQueries({ queryKey: ['mcp-tools', serviceId] })
    return tools.length
  }

  function trackDeployment(serviceId: string, versionId: string) {
    const pending = { serviceId, versionId }
    try { sessionStorage.setItem(pendingStorageKey, JSON.stringify(pending)) } catch { /* 内存状态仍可跟踪 */ }
    setPendingDeployment(pending)
  }

  const importDeployment = useMutation({
    mutationFn: (input: MCPDeploymentInput) => buildMCPDeployment(input),
    onSuccess: async (result) => {
      setDeploymentOpen(false)
      setSelectedId(result.service.id)
      trackDeployment(result.service.id, result.version.id)
      await queryClient.invalidateQueries({ queryKey: ['mcp-services'] })
	  messageApi.success('已提交后台构建，可在部署与运行中查看状态和日志')
    },
    onError: (error) => messageApi.error(errorMessage(error, '部署 MCP 失败')),
  })
  useEffect(() => {
    if (!pendingDeployment) return
    const version = pendingVersions.data?.find((item) => item.id === pendingDeployment.versionId)
    if (!version || version.buildStatus === 'building') return
    setPendingDeployment(null)
    try { sessionStorage.removeItem(pendingStorageKey) } catch { /* 内存状态已清除 */ }
    if (version.buildStatus === 'failed') {
      messageApi.warning('MCP 构建失败，请查看构建日志')
      return
    }
    void (async () => {
      try {
        await changeMCPDeployment(pendingDeployment.serviceId, 'start')
        await queryClient.invalidateQueries({ queryKey: ['mcp-services'] })
        await queryClient.invalidateQueries({ queryKey: ['mcp-deployment-runtime', pendingDeployment.serviceId] })
        const count = await refreshDeploymentTools(pendingDeployment.serviceId)
        messageApi.success(`MCP 已部署，发现 ${count} 个工具`)
      } catch (error) {
        messageApi.warning(`构建成功，但启动或发现工具失败：${errorMessage(error, '请检查运行状态')}`)
      }
    })()
  }, [pendingDeployment, pendingVersions.data, messageApi, queryClient, pendingStorageKey])
  const update = useMutation({
    mutationFn: (values: ServiceForm) => isManagedService
      ? updateMCPDeploymentMetadata(selected!.id, values.name)
      : updateMCPService(selected!.id, serviceInput(values)),
    onSuccess: async () => {
      setEditorMode(null)
      messageApi.success('MCP 服务已修改')
      await queryClient.invalidateQueries({ queryKey: ['mcp-services'] })
      await queryClient.invalidateQueries({ queryKey: ['mcp-tools', selected?.id] })
    },
    onError: (error) => messageApi.error(errorMessage(error, '修改 MCP 服务失败')),
  })
  const status = useMutation({
    mutationFn: (service: MCPService) => changeMCPServiceStatus(service.id, service.status === 'active' ? 'disabled' : 'active'),
    onSuccess: async () => {
      messageApi.success('服务状态已更新')
      await queryClient.invalidateQueries({ queryKey: ['mcp-services'] })
    },
    onError: (error) => messageApi.error(errorMessage(error, '更新服务状态失败')),
  })
  const discover = useMutation({
    mutationFn: () => discoverMCPTools(selected!.id, toolConnection?.id),
    onSuccess: async (tools) => {
      messageApi.success(`已发现 ${tools.length} 个工具`)
      await queryClient.invalidateQueries({ queryKey: ['mcp-tools', selected?.id] })
    },
    onError: (error) => messageApi.error(errorMessage(error, '发现工具失败')),
  })
  function selectService(service: MCPService) {
    setSelectedId(service.id)
    setExampleId(null)
    setToolConnectionId(null)
    setTesting(null)
    setDescriptionExpanded(false)
  }

  function openEditor(mode: 'create' | 'edit') {
    serviceForm.resetFields()
    setImportText('')
    setImportOpen(false)
    setDescriptionMode('编辑')
    if (mode === 'edit' && selected) serviceForm.setFieldsValue({
      name: selected.name, description: selected.description, transport: selected.transport,
      upstreamUrl: selected.upstreamUrl, rows: selected.parameters.map((row) => ({ ...row, description: row.description || row.label || '' })),
    })
    else serviceForm.setFieldsValue({ transport: 'streamable_http', rows: [] })
    setEditorMode(mode)
  }

  function importConfig() {
    try {
      const imported = parseMCPConfig(importText)
      serviceForm.setFieldsValue({
        transport: imported.transport,
        upstreamUrl: imported.upstreamUrl,
        rows: imported.parameters,
        name: serviceForm.getFieldValue('name') || imported.name,
      })
      setImportOpen(false)
      messageApi.success('配置已导入；参数值请在生成连接时填写')
    } catch (error) {
      messageApi.error(errorMessage(error, 'MCP 配置格式不正确'))
    }
  }

  async function copy(value: string) {
    try {
      await navigator.clipboard.writeText(value)
      messageApi.success('已复制')
    } catch {
      messageApi.error('复制失败，请手动选择文本')
    }
  }

  const exampleConfig = example ? JSON.stringify({
    mcpServers: {
      [selected?.name ?? 'MCP 服务']: {
        type: 'streamable_http', url: example.gatewayUrl,
        ...(example.authMode === 'bearer' ? { headers: { Authorization: 'Bearer <连接密钥>' } } : {}),
      },
    },
  }, null, 2) : ''

  return <div className="mcp-workspace">
    {contextHolder}
    <aside className="mcp-service-sidebar" aria-label="MCP 服务列表">
      <div className="mcp-sidebar-head">
        <strong>MCP 服务</strong>
        <Space size={4}>
          <Button type="text" size="small" icon={<ReloadOutlined />} aria-label="刷新服务" onClick={() => servicesQuery.refetch()} />
          {canWrite && <Tooltip title={deploymentCapability.data?.enabled ? '从 GitHub、Gitee 或代码包部署' : '服务器未开启源码部署（MCP_DEPLOY_ENABLED）'}>
            <Button type="text" size="small" icon={<CloudUploadOutlined />} aria-label="从代码部署 MCP"
              disabled={!deploymentCapability.data?.enabled} onClick={() => setDeploymentOpen(true)} />
          </Tooltip>}
          {canWrite && <Button type="text" size="small" icon={<PlusOutlined />} aria-label="接入远程服务" onClick={() => openEditor('create')} />}
        </Space>
      </div>
      <div className="mcp-service-list">
        {(servicesQuery.data ?? []).map((service) => <button key={service.id} type="button"
          className={`mcp-service-item${selected?.id === service.id ? ' is-active' : ''}`}
          onClick={() => selectService(service)}>
          <strong>{service.name}</strong>
          <span>{service.transport === 'sse' ? 'SSE' : service.transport === 'stdio' ? 'Stdio · 托管' : 'Streamable HTTP'} · {service.status === 'active' ? '已启用' : '已停用'}</span>
        </button>)}
        {!servicesQuery.isLoading && !servicesQuery.data?.length && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="还没有 MCP 服务" />}
      </div>
    </aside>

    <div className="mcp-service-main">
      {selected ? <>
        <div className="mcp-service-overview">
          <Card className="content-card" variant="borderless">
            <div className="mcp-section-head">
              <div><h2>{selected.name}</h2><Tag color={selected.status === 'active' ? 'green' : 'default'}>{selected.status === 'active' ? '已启用' : '已停用'}</Tag></div>
              {canWrite && <Space><Button onClick={() => openEditor('edit')}>修改服务</Button>
                <Button loading={status.isPending} onClick={() => status.mutate(selected)}>{selected.status === 'active' ? '停用服务' : '启用服务'}</Button></Space>}
            </div>
            {selected.description ? <div className="mcp-service-description">
              <div className={`mcp-description-shell${descriptionExpanded ? ' is-expanded' : ''}`}>
                <MarkdownContent value={selected.description} />
              </div>
              {selected.description.length > 400 && <Button type="link" className="mcp-description-toggle"
                onClick={() => setDescriptionExpanded((expanded) => !expanded)}>
                {descriptionExpanded ? '收起描述' : '展开完整描述'}
              </Button>}
            </div>
              : <p className="mcp-description">暂无服务描述</p>}
            {!isManagedService ? <>
              <div className="mcp-service-meta"><span>上游地址</span><Text copyable={{ text: selected.upstreamUrl }}>{selected.upstreamUrl}</Text></div>
              <div className="mcp-service-meta"><span>参数配置</span><Text>{selected.parameters.length ? `${selected.parameters.length} 项，生成连接时填写参数值` : '无需额外参数'}</Text></div>
            </> : <div className="mcp-service-meta"><span>服务来源</span><Text>{selected.sourceType === 'github' ? 'GitHub 仓库' : selected.sourceType === 'gitee' ? 'Gitee 仓库' : '本地代码包'} · Stdio</Text></div>}
          </Card>
          {canWrite && isManagedService && <MCPDeploymentPanel service={selected}
            onBuildSubmitted={(versionId) => trackDeployment(selected.id, versionId)}
            onReady={refreshDeploymentTools}
            onUpdated={async () => { await queryClient.invalidateQueries({ queryKey: ['mcp-services'] }) }} />}
          {canReadConnections && <MCPConnectionsPage serviceId={selected.id} />}
          {canReadConnections && <Card className="content-card" variant="borderless">
            <div className="mcp-section-head"><h2>接入示例</h2></div>
            {example ? <>
              <Select style={{ minWidth: 220, maxWidth: '100%', marginBottom: 12 }} value={example.id}
                onChange={setExampleId} options={connections.map((item) => ({ label: item.name, value: item.id }))} />
              {example.purpose === 'harness'
                ? <p className="mcp-description">员工登录 Harness 后自动获取已授权的连接，无需逐人配置。</p>
                : <p className="mcp-description">{example.authMode === 'none'
                  ? '此连接无入口鉴权，持有地址即可调用。请仅在受控环境使用。'
                  : '将连接地址和入口密钥配置到业务应用。下方是 Streamable HTTP 配置示例。'}</p>}
              {example.purpose === 'harness'
                ? <Input readOnly value={example.gatewayUrl} addonAfter={<CopyOutlined onClick={() => copy(example.gatewayUrl)} />} />
                : <div className="mcp-example"><pre>{exampleConfig}</pre><Button size="small" icon={<CopyOutlined />} onClick={() => copy(exampleConfig)}>复制</Button></div>}
            </> : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="生成连接后，这里会显示接入示例" />}
          </Card>}
        </div>
        <Card className="content-card mcp-tools-card" variant="borderless">
          <div className="mcp-section-head"><h2>工具列表</h2>
            {canWrite && <Button icon={<ReloadOutlined />} loading={discover.isPending}
              disabled={toolsNeedConnection && !toolConnection} onClick={() => discover.mutate()}>发现工具</Button>}
          </div>
          {connections.length > 0 && <div className="mcp-tool-connection">
            <Text type="secondary">工具发现和测试使用</Text>
            <Select value={toolConnection?.id} onChange={setToolConnectionId} style={{ minWidth: 220 }}
              options={connections.filter((item) => item.status === 'active').map((item) => ({ label: item.name, value: item.id }))}
              placeholder="选择连接" />
          </div>}
          {toolsNeedConnection && !toolConnection && <Alert type="info" showIcon style={{ marginBottom: 16 }}
            message="先生成连接并填写参数值，再发现或测试工具。" />}
          <Table<MCPTool> rowKey="name" size="small" loading={toolsQuery.isLoading} dataSource={toolsQuery.data ?? []}
            pagination={{ pageSize: 10 }} columns={[
              { title: '工具', dataIndex: 'name' },
              { title: '说明', dataIndex: 'description' },
              { title: '操作', render: (_, tool) => canWrite && <Button type="link" icon={<ApiOutlined />}
                disabled={toolsNeedConnection && !toolConnection} onClick={() => {
                setTesting(tool)
              }}>测试</Button> },
            ]} />
          {testing && <MCPToolTestPanel key={`${selected.id}:${testing.name}`} serviceId={selected.id}
            tool={testing} connectionId={toolConnection?.id} />}
        </Card>
      </> : <Card className="content-card" variant="borderless"><Empty description="从左侧选择 MCP 服务" /></Card>}
    </div>

    <Modal title={editorMode === 'edit' ? '修改 MCP 服务' : '接入 MCP 服务'} open={Boolean(editorMode)} width={840}
      onCancel={() => setEditorMode(null)} onOk={() => serviceForm.submit()}
      okText={editorMode === 'edit' ? '保存修改' : '接入服务'} confirmLoading={create.isPending || update.isPending} destroyOnHidden>
      <Form form={serviceForm} layout="vertical" onFinish={(values) => editorMode === 'edit' ? update.mutate(values) : create.mutate(values)}>
        <Form.Item name="name" label="服务名称" rules={[{ required: true, message: '请填写服务名称' }]}><Input maxLength={120} /></Form.Item>
        {(editorMode !== 'edit' || !isManagedService) && <><div className="mcp-description-editor-head"><strong>服务描述（Markdown）</strong>
          <Segmented size="small" value={descriptionMode} onChange={(value) => setDescriptionMode(value as typeof descriptionMode)} options={['编辑', '预览']} />
        </div>
        <Form.Item name="description" hidden={descriptionMode === '预览'}><Input.TextArea maxLength={10000}
          showCount autoSize={{ minRows: 7, maxRows: 14 }} placeholder="支持标题、列表、链接和代码块" /></Form.Item>
        {descriptionMode === '预览' && <div className="mcp-description-preview">
          {descriptionPreview ? <MarkdownContent value={descriptionPreview} /> : <Text type="secondary">暂无内容，切换到编辑填写服务描述。</Text>}
        </div>}</>}
        {editorMode === 'edit' && isManagedService && <Alert type="info" showIcon style={{ marginBottom: 16 }}
          message="托管服务的描述来自当前源码版本根目录 README.md；更新描述请构建新版本。" />}
        {editorMode === 'edit' && !isManagedService && connections.length > 0 && <Alert type="info" showIcon style={{ marginBottom: 16 }}
          message="修改地址或传输方式后需要重新发现工具；修改参数字段后，已有连接可能需要补充参数值。" />}
        {(editorMode !== 'edit' || !isManagedService) && <div className="mcp-config-block">
          <div className="mcp-config-head"><strong>服务配置</strong><Button type="link" icon={<ImportOutlined />}
            onClick={() => setImportOpen((open) => !open)}>{importOpen ? '收起导入' : '从 MCP 配置导入'}</Button></div>
          {importOpen && <div className="mcp-config-import">
            <Text type="secondary">粘贴包含 mcpServers 的 JSON。只导入传输方式、地址和参数字段，不保存其中的参数值。</Text>
            <Input.TextArea value={importText} onChange={(event) => setImportText(event.target.value)}
              autoSize={{ minRows: 5, maxRows: 10 }} placeholder={'{"mcpServers":{"my-server":{"type":"streamable_http","url":"https://example.com/mcp"}}}'} />
            <Button onClick={importConfig}>导入配置</Button>
          </div>}
          <Form.Item name="transport" label="传输方式" rules={[{ required: true }]}>
            <Radio.Group optionType="button" buttonStyle="solid" options={[
              { label: 'Streamable HTTP', value: 'streamable_http' }, { label: 'SSE', value: 'sse' },
            ]} />
          </Form.Item>
          <Form.Item name="upstreamUrl" label="MCP 地址" rules={[{ required: true, message: '请填写 MCP 地址' }]}>
            <Input placeholder="https://example.com/mcp" />
          </Form.Item>
          <div className="mcp-config-subhead"><strong>参数配置</strong><Text type="secondary">这里说明连接需要填写什么；具体值在生成连接时填写。</Text></div>
          <ParameterRows />
        </div>}
      </Form>
    </Modal>
    <MCPDeploymentDialog open={deploymentOpen} loading={importDeployment.isPending}
      onClose={() => setDeploymentOpen(false)} onSubmit={(input) => importDeployment.mutate(input)} />
  </div>
}

function ParameterRows() {
  return <Form.List name="rows">{(fields, { add, remove }) => <>
    {fields.map((field) => <div className="mcp-parameter-row" key={field.key}>
      <Form.Item name={[field.name, 'location']} label="位置" rules={[{ required: true }]}>
        <Select options={[{ label: '请求头', value: 'header' }, { label: 'URL 参数', value: 'query' }, { label: '路径参数', value: 'path' }]} />
      </Form.Item>
      <Form.Item name={[field.name, 'name']} label="字段名" rules={[{ required: true, message: '填写字段名' }]}>
        <Input />
      </Form.Item>
      <Form.Item name={[field.name, 'description']} label="说明（可选）"><Input maxLength={500} /></Form.Item>
      <Form.Item name={[field.name, 'required']} valuePropName="checked"><Checkbox>必填</Checkbox></Form.Item>
      <Button type="text" icon={<MinusCircleOutlined />} aria-label="移除参数" onClick={() => remove(field.name)} />
    </div>)}
    <Button type="dashed" icon={<PlusOutlined />} onClick={() => add({ location: 'header', name: '', description: '', required: true })}>添加参数</Button>
  </>}</Form.List>
}

function serviceInput(values: ServiceForm) {
  return {
    name: values.name, description: values.description, transport: values.transport, upstreamUrl: values.upstreamUrl,
    parameters: (values.rows ?? []).map(({ location, name, label, description, required }) => ({
      location, name: name.trim(), label, description: description?.trim(), required: Boolean(required),
    })),
  }
}

function parseMCPConfig(raw: string): Pick<ServiceForm, 'name' | 'transport' | 'upstreamUrl'> & { parameters: MCPParameterDefinition[] } {
  let parsed: unknown
  try {
    parsed = JSON.parse(raw)
  } catch {
    throw new Error('请粘贴有效的 JSON 配置')
  }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error('MCP 配置需要是 JSON 对象')
  const root = parsed as Record<string, unknown>
  const servers = root.mcpServers
  let name = ''
  let config: Record<string, unknown> = root
  if (servers !== undefined) {
    if (!servers || typeof servers !== 'object' || Array.isArray(servers)) throw new Error('mcpServers 格式不正确')
    const entries = Object.entries(servers)
    if (entries.length !== 1) throw new Error('一次只能导入一个 MCP 服务，请保留一个 mcpServers 条目')
    name = entries[0][0]
    if (!entries[0][1] || typeof entries[0][1] !== 'object' || Array.isArray(entries[0][1])) throw new Error('服务配置格式不正确')
    config = entries[0][1] as Record<string, unknown>
  }
  if (config.type !== 'streamable_http' && config.type !== 'sse') throw new Error('仅支持 Streamable HTTP 或 SSE 配置')
  if (typeof config.url !== 'string' || !config.url) throw new Error('配置中缺少 MCP 地址')
  let url: URL
  try {
    url = new URL(config.url)
  } catch {
    throw new Error('MCP 地址格式不正确')
  }
  if (url.protocol !== 'http:' && url.protocol !== 'https:') throw new Error('MCP 地址需要使用 HTTP 或 HTTPS')
  const parameters: MCPParameterDefinition[] = []
  const headers = config.headers
  if (headers !== undefined) {
    if (!headers || typeof headers !== 'object' || Array.isArray(headers)) throw new Error('headers 格式不正确')
    for (const key of Object.keys(headers)) parameters.push({ location: 'header', name: key, required: true })
  }
  for (const key of url.searchParams.keys()) {
    if (!parameters.some((item) => item.location === 'query' && item.name === key)) {
      parameters.push({ location: 'query', name: key, required: true })
    }
  }
  url.search = ''
  url.hash = ''
  return { name, transport: config.type, upstreamUrl: url.toString(), parameters }
}

function errorMessage(error: unknown, fallback: string) {
  return error instanceof ApiError || error instanceof Error ? error.message : fallback
}
