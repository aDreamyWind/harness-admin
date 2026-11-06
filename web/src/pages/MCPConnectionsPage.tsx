import { CopyOutlined, EyeOutlined, PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import { useMutation, useQueries, useQuery, useQueryClient } from '@tanstack/react-query'
import { Alert, Button, Card, DatePicker, Form, Input, InputNumber, Modal, Select, Space, Table, Tag, Tooltip, Typography, message } from 'antd'
import type { Dayjs } from 'dayjs'
import { useEffect, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'

import { ApiError } from '../api/client'
import { listDepartments } from '../api/departments'
import {
  changeMCPConnectionStatus, checkMCPConnectionHealth, createMCPConnection, listMCPConnections,
  revealMCPConnectionSecret, revealMCPUpstreamCredentials, replaceMCPConnectionGrants,
  updateMCPUpstreamCredentials,
  rotateMCPConnectionSecret, type CreatedMCPConnection, type MCPConnection, type MCPConnectionInput,
} from '../api/mcpConnections'
import { discoverMCPTools, listMCPServices, type MCPParameterDefinition, type MCPParameterValue } from '../api/mcp'
import { listUsers } from '../api/users'
import { useAuth } from '../auth/AuthContext'
import { ModelPolicyDepartmentTree } from '../components/ModelPolicyDepartmentTree'
import { dateTimeFormat } from '../utils/dateTime'

const { Text } = Typography

type ConnectionForm = Omit<MCPConnectionInput, 'expiresAt' | 'upstreamParameters'> & {
  expiresAt?: Dayjs
  parameterValues?: Record<string, string>
}

const parameterKey = (item: Pick<MCPParameterDefinition, 'location' | 'name'>) =>
  `${item.location}:${item.location === 'header' ? item.name.toLowerCase() : item.name}`
const parameterValues = (definitions: MCPParameterDefinition[], values?: Record<string, string>): MCPParameterValue[] =>
  definitions.filter((item) => values?.[parameterKey(item)])
    .map((item) => ({ location: item.location, name: item.name, value: values![parameterKey(item)] }))

export function MCPConnectionsPage({ serviceId }: { serviceId?: string } = {}) {
  const { hasPermission } = useAuth()
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const canWrite = hasPermission('mcp.connection.write')
  const queryClient = useQueryClient()
  const [messageApi, contextHolder] = message.useMessage()
  const [form] = Form.useForm<ConnectionForm>()
  const [parameterForm] = Form.useForm<{ parameterValues: Record<string, string> }>()
  const [grantForm] = Form.useForm<{ userIds: string[] }>()
  const [modalOpen, setModalOpen] = useState(false)
  const [created, setCreated] = useState<CreatedMCPConnection | null>(null)
  const [revealed, setRevealed] = useState<{ name: string; secret: string } | null>(null)
  const [rotateTarget, setRotateTarget] = useState<MCPConnection | null>(null)
  const [parameterTarget, setParameterTarget] = useState<MCPConnection | null>(null)
  const [preservedParameters, setPreservedParameters] = useState<MCPParameterValue[]>([])
  const [selectedDepartments, setSelectedDepartments] = useState<string[]>([])
  const [grantTarget, setGrantTarget] = useState<MCPConnection | null>(null)
  const [grantDepartments, setGrantDepartments] = useState<string[]>([])
  const [userSearch, setUserSearch] = useState('')
  const [page, setPage] = useState(1)
  const purpose = Form.useWatch('purpose', form)
  const authMode = Form.useWatch('authMode', form)
  const selectedServiceId = Form.useWatch('serviceId', form)

  const connectionsQuery = useQuery({ queryKey: ['mcp-connections'], queryFn: listMCPConnections })
  const visibleConnections = (connectionsQuery.data ?? []).filter((item) => !serviceId || item.serviceId === serviceId)
  const currentPage = Math.min(page, Math.max(1, Math.ceil(visibleConnections.length / 10)))
  const pageConnections = visibleConnections.slice((currentPage - 1) * 10, currentPage * 10)
  const healthQueries = useQueries({ queries: pageConnections.map((item) => ({
    queryKey: ['mcp-connection-health', item.id], queryFn: () => checkMCPConnectionHealth(item.id),
    retry: false, refetchOnWindowFocus: false, refetchOnReconnect: false, refetchOnMount: 'always' as const,
  })) })
  const healthByID = new Map(pageConnections.map((item, index) => [item.id, healthQueries[index]]))
  useEffect(() => setPage(1), [serviceId])
  const servicesQuery = useQuery({ queryKey: ['mcp-services'], queryFn: listMCPServices })
  const departmentsQuery = useQuery({ queryKey: ['departments'], queryFn: listDepartments, enabled: canWrite && hasPermission('iam.department.read') })
  const usersQuery = useQuery({
    queryKey: ['mcp-grant-users', userSearch],
    queryFn: () => listUsers({ search: userSearch, status: 'active', page: 1, pageSize: 50 }),
    enabled: canWrite && (purpose === 'harness' || Boolean(grantTarget)) && hasPermission('iam.user.read'),
  })
  const selectedService = servicesQuery.data?.find((item) => item.id === selectedServiceId)
  const targetService = servicesQuery.data?.find((item) => item.id === parameterTarget?.serviceId)

  const create = useMutation({
    mutationFn: (values: ConnectionForm) => {
      const { parameterValues: configured, ...connectionValues } = values
      return createMCPConnection({
        ...connectionValues,
        upstreamParameters: parameterValues(selectedService?.parameters ?? [], configured),
        expiresAt: values.expiresAt?.startOf('minute').toISOString(),
        departmentIds: values.purpose === 'harness' ? selectedDepartments : [],
        userIds: values.purpose === 'harness' ? (values.userIds ?? []) : [],
      })
    },
    onSuccess: async (result) => {
      setModalOpen(false)
      setCreated(result)
      setSelectedDepartments([])
      form.resetFields()
      messageApi.success('MCP 连接已生成')
      await queryClient.invalidateQueries({ queryKey: ['mcp-connections'] })
      await queryClient.invalidateQueries({ queryKey: ['mcp-connection-health', result.id] })
      try {
        const tools = await discoverMCPTools(result.serviceId, result.id)
        await queryClient.invalidateQueries({ queryKey: ['mcp-tools', result.serviceId] })
        await queryClient.invalidateQueries({ queryKey: ['mcp-connection-health', result.id] })
        messageApi.success(`已自动获取 ${tools.length} 个工具`)
      } catch (error) {
        messageApi.warning(`连接已生成，但自动获取工具失败：${errorMessage(error, '请检查连接参数')}`)
      }
    },
    onError: (error) => messageApi.error(errorMessage(error, '生成 MCP 连接失败')),
  })
  const status = useMutation({
    mutationFn: (connection: MCPConnection) => changeMCPConnectionStatus(connection.id, connection.status === 'active' ? 'disabled' : 'active'),
    onSuccess: async () => {
      messageApi.success('连接状态已更新')
      await queryClient.invalidateQueries({ queryKey: ['mcp-connections'] })
      await queryClient.invalidateQueries({ queryKey: ['mcp-connection-health'] })
    },
    onError: (error) => messageApi.error(errorMessage(error, '更新连接状态失败')),
  })
  const saveGrants = useMutation({
    mutationFn: (values: { userIds: string[] }) => replaceMCPConnectionGrants(grantTarget!.id, grantDepartments, values.userIds ?? []),
    onSuccess: async () => {
      setGrantTarget(null)
      messageApi.success('员工授权已更新')
      await queryClient.invalidateQueries({ queryKey: ['mcp-connections'] })
    },
    onError: (error) => messageApi.error(errorMessage(error, '更新员工授权失败')),
  })
  const reveal = useMutation({
    mutationFn: async (connection: MCPConnection) => ({ connection, value: await revealMCPConnectionSecret(connection.id) }),
    onSuccess: ({ connection, value }) => setRevealed({ name: connection.name, secret: value.secret }),
    onError: (error) => messageApi.error(errorMessage(error, '查看入口密钥失败')),
  })
  const rotate = useMutation({
    mutationFn: (connection: MCPConnection) => rotateMCPConnectionSecret(connection.id),
    onSuccess: async (value) => {
      setRevealed({ name: rotateTarget?.name ?? 'MCP 连接', secret: value.secret })
      setRotateTarget(null)
      messageApi.success('入口密钥已轮换，旧密钥立即失效')
      await queryClient.invalidateQueries({ queryKey: ['mcp-connections'] })
    },
    onError: (error) => messageApi.error(errorMessage(error, '轮换密钥失败')),
  })
  const saveParameters = useMutation({
    mutationFn: (values: { parameterValues: Record<string, string> }) =>
      updateMCPUpstreamCredentials(parameterTarget!.id, {
        parameters: [...preservedParameters, ...parameterValues(targetService?.parameters ?? [], values.parameterValues)],
      }),
    onSuccess: async () => {
      const connectionId = parameterTarget!.id
      setParameterTarget(null)
      setPreservedParameters([])
      messageApi.success('连接参数已更新')
      await queryClient.invalidateQueries({ queryKey: ['mcp-connections'] })
      await queryClient.invalidateQueries({ queryKey: ['mcp-connection-health', connectionId] })
    },
    onError: (error) => messageApi.error(errorMessage(error, '更新连接参数失败')),
  })
  function openCreate() {
    setSelectedDepartments([])
    const chosenServiceId = serviceId ?? searchParams.get('service') ?? undefined
    form.resetFields()
    form.setFieldsValue({
      serviceId: chosenServiceId,
      name: '', purpose: 'developer', authMode: 'bearer',
      applicationName: '', requestsPerMinute: undefined, departmentIds: [], userIds: [], parameterValues: {},
    })
    setModalOpen(true)
  }

  function openGrants(connection: MCPConnection) {
    setGrantTarget(connection)
    setGrantDepartments(connection.departmentIds)
    grantForm.setFieldsValue({ userIds: connection.userIds })
  }

  async function openParameters(connection: MCPConnection) {
    try {
      const existing = await revealMCPUpstreamCredentials(connection.id)
      const all = new Map<string, MCPParameterValue>()
      for (const item of existing.parameters ?? []) all.set(parameterKey(item), item)
      if (existing.authorization) {
        const item = { location: 'header' as const, name: 'Authorization', value: existing.authorization }
        all.set(parameterKey(item), item)
      }
      if (existing.headerName && existing.headerValue) {
        const item = { location: 'header' as const, name: existing.headerName, value: existing.headerValue }
        all.set(parameterKey(item), item)
      }
      if (existing.queryName && existing.queryValue) {
        const item = { location: 'query' as const, name: existing.queryName, value: existing.queryValue }
        all.set(parameterKey(item), item)
      }
      const definitions = servicesQuery.data?.find((service) => service.id === connection.serviceId)?.parameters ?? []
      const values: Record<string, string> = {}
      for (const item of all.values()) values[parameterKey(item)] = item.value
      setPreservedParameters([...all.values()].filter((item) => !definitions.some((definition) => parameterKey(definition) === parameterKey(item))))
      parameterForm.setFieldsValue({ parameterValues: values })
      setParameterTarget(connection)
    } catch (error) {
      messageApi.error(errorMessage(error, '读取连接参数失败'))
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

  return <div>
    {contextHolder}
    <Card className="content-card" variant="borderless">
      <div className="page-toolbar">
        <h2>MCP 连接</h2>
        <Space>
          <Button icon={<ReloadOutlined />} onClick={() => {
            connectionsQuery.refetch()
            queryClient.invalidateQueries({ queryKey: ['mcp-connection-health'] })
          }}>刷新</Button>
          {canWrite && <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}
            disabled={Boolean(serviceId && servicesQuery.data?.find((item) => item.id === serviceId)?.status !== 'active')}>生成连接</Button>}
        </Space>
      </div>
      <Table<MCPConnection> rowKey="id" loading={connectionsQuery.isLoading} dataSource={visibleConnections}
        pagination={{ pageSize: 10, current: currentPage, onChange: setPage }} scroll={{ x: 1080 }} columns={[
          { title: '连接', dataIndex: 'name', render: (value: string) => <strong>{value}</strong> },
          ...(!serviceId ? [{ title: 'MCP 服务', dataIndex: 'serviceName' as const }] : []),
          { title: '用途', dataIndex: 'purpose', render: (value: string) => value === 'harness' ? 'Harness 员工' : '开发者／应用' },
          { title: '入口鉴权', dataIndex: 'authMode', render: (value: string) => value === 'oidc' ? '员工登录令牌' : value === 'bearer' ? '应用 Bearer 密钥' : '无鉴权' },
          { title: '状态', dataIndex: 'status', render: (value: string) => <Tag color={value === 'active' ? 'green' : 'default'}>{value === 'active' ? '启用' : '停用'}</Tag> },
          { title: '连通状态', width: 110, render: (_, item) => {
            const check = healthByID.get(item.id)
            const health = check?.data
            const label = health?.status === 'healthy' ? '正常' : health?.status === 'inactive' ? '未启用'
              : health?.status === 'unavailable' ? '异常' : check?.isError ? '检查失败' : '检查中'
            const color = health?.status === 'healthy' ? 'green' : health?.status === 'unavailable' || check?.isError ? 'red' : 'default'
            return <Tooltip title={health?.message ?? (check?.isError ? '检查请求失败，请点击刷新重试' : '正在检查 MCP 握手和工具列表')}>
              <Tag color={color}>{label}</Tag>
            </Tooltip>
          } },
          { title: '地址', render: (_, item) => <Button type="link" icon={<CopyOutlined />} onClick={() => copy(item.gatewayUrl)}>复制地址</Button> },
          { title: '操作', render: (_, item) => <Space>
            <Button type="link" onClick={() => navigate(`/ai-resources/mcp-connections/${item.id}/usage`)}>调用统计</Button>
            {canWrite && <>
              {item.purpose === 'harness' && <Button type="link" onClick={() => openGrants(item)}>授权</Button>}
              {servicesQuery.data?.find((service) => service.id === item.serviceId)?.parameters.length
                ? <Button type="link" onClick={() => openParameters(item)}>修改参数</Button> : null}
              {item.authMode === 'bearer' && <Button type="link" icon={<EyeOutlined />} loading={reveal.isPending} onClick={() => reveal.mutate(item)}>查看密钥</Button>}
              {item.authMode === 'bearer' && <Button type="link" onClick={() => setRotateTarget(item)}>轮换密钥</Button>}
              <Button type="link" loading={status.isPending} onClick={() => status.mutate(item)}>{item.status === 'active' ? '停用' : '启用'}</Button>
            </>}
          </Space> },
        ]} />
    </Card>

    <Modal title="生成 MCP 连接" open={modalOpen} width={740} onCancel={() => setModalOpen(false)}
      onOk={() => form.submit()} okText="生成连接" confirmLoading={create.isPending} destroyOnHidden>
      <Form form={form} layout="vertical" autoComplete="off" onFinish={(values) => create.mutate(values)}>
        <Form.Item name="serviceId" label="MCP 服务" rules={[{ required: true, message: '请选择已登记的 MCP 服务' }]}>
          <Select showSearch optionFilterProp="label" disabled={Boolean(serviceId)} onChange={() => form.setFieldValue('parameterValues', {})}
            options={(servicesQuery.data ?? []).filter((item) => item.status === 'active').map((item) => ({ label: item.name, value: item.id }))} />
        </Form.Item>
        <Form.Item name="name" label="连接名称" rules={[{ required: true, message: '请填写连接名称' }]}>
          <Input maxLength={120} placeholder="例如：研发助手" autoComplete="off" />
        </Form.Item>
        {selectedService?.parameters.length ? <div className="mcp-connection-parameters">
          <strong>参数值</strong>
          <Text type="secondary">这些值只用于当前连接，不影响同一服务的其他连接。</Text>
          <ParameterValueFields definitions={selectedService.parameters} />
        </div> : null}
        <Form.Item name="purpose" label="连接用途" rules={[{ required: true }]}>
          <Select onChange={(value: MCPConnection['purpose']) => form.setFieldValue('authMode', value === 'harness' ? 'oidc' : 'bearer')}
            options={[{ label: '给 Harness 员工使用', value: 'harness' }, { label: '给开发者／业务应用使用', value: 'developer' }]} />
        </Form.Item>
        {purpose === 'developer' ? <>
          <Form.Item name="applicationName" label="归属应用／项目（建议填写，可共用连接）"><Input maxLength={120} /></Form.Item>
          <Form.Item name="authMode" label="入口鉴权" rules={[{ required: true }]}>
            <Select options={[{ label: 'Bearer 密钥（默认）', value: 'bearer' }, { label: '无鉴权（需主动选择）', value: 'none' }]} />
          </Form.Item>
          {authMode === 'none' && <Alert type="warning" showIcon style={{ marginBottom: 16 }}
            message="无鉴权连接无法识别具体调用者；获得地址的人都可在有效期内调用。建议设置到期时间和较低的请求上限。" />}
        </> : <>
          <Form.Item name="authMode" hidden><Input /></Form.Item>
          <Form.Item label="授权部门">
            <ModelPolicyDepartmentTree departments={departmentsQuery.data ?? []} selectedIds={selectedDepartments} onChange={setSelectedDepartments} />
          </Form.Item>
          <Form.Item name="userIds" label="单独授权用户">
            <Select mode="multiple" showSearch filterOption={false} onSearch={setUserSearch}
              options={(usersQuery.data?.items ?? []).map((user) => ({ label: `${user.displayName}（${user.username}）`, value: user.id }))} />
          </Form.Item>
          <Text type="secondary">只授权明确勾选的部门和用户；新建下级部门不会自动加入。</Text>
        </>}
        <Space align="start">
          <Form.Item name="expiresAt" label="到期时间（可选）"><DatePicker showTime={{ format: 'HH:mm' }} format={dateTimeFormat} /></Form.Item>
          <Form.Item name="requestsPerMinute" label={`${purpose === 'harness' ? '每位员工每分钟请求上限' : '此连接每分钟请求上限'}（可选）`}
            extra="不填时默认 60 次／分钟。">
            <InputNumber min={1} max={100000} />
          </Form.Item>
        </Space>
      </Form>
    </Modal>

    <Modal title={`修改连接参数：${parameterTarget?.name ?? ''}`} open={Boolean(parameterTarget)} width={640}
      onCancel={() => { setParameterTarget(null); setPreservedParameters([]) }} onOk={() => parameterForm.submit()}
      okText="保存参数" confirmLoading={saveParameters.isPending} destroyOnHidden>
      <Form form={parameterForm} layout="vertical" autoComplete="off" onFinish={(values) => saveParameters.mutate(values)}>
        {preservedParameters.length > 0 && <Alert type="info" showIcon style={{ marginBottom: 16 }}
          message="此连接另有旧版参数；保存时会原样保留。" />}
        <ParameterValueFields definitions={targetService?.parameters ?? []} />
      </Form>
    </Modal>

    <Modal title={`员工授权：${grantTarget?.name ?? ''}`} open={Boolean(grantTarget)}
      onCancel={() => setGrantTarget(null)} onOk={() => grantForm.submit()}
      okText="保存授权" confirmLoading={saveGrants.isPending} width={680} destroyOnHidden>
      <Form form={grantForm} layout="vertical" onFinish={(values) => saveGrants.mutate(values)}>
        <Form.Item label="授权部门">
          <ModelPolicyDepartmentTree departments={departmentsQuery.data ?? []} selectedIds={grantDepartments} onChange={setGrantDepartments} />
        </Form.Item>
        <Form.Item name="userIds" label="单独授权用户">
          <Select mode="multiple" showSearch filterOption={false} onSearch={setUserSearch}
            options={(usersQuery.data?.items ?? []).map((user) => ({ label: `${user.displayName}（${user.username}）`, value: user.id }))} />
        </Form.Item>
        <Text type="secondary">保存最终明确勾选的部门与用户；也可以全部取消以撤销此连接的员工授权。</Text>
      </Form>
    </Modal>

    <Modal title="连接已生成" open={Boolean(created)} onCancel={() => setCreated(null)} footer={<Button onClick={() => setCreated(null)}>完成</Button>}>
      {created && <Space direction="vertical" style={{ width: '100%' }}>
        <Text>网关地址</Text><Input readOnly value={created.gatewayUrl} addonAfter={<CopyOutlined onClick={() => copy(created.gatewayUrl)} />} />
        <Text>入口鉴权：{created.authMode === 'oidc' ? '员工 OIDC 登录令牌' : created.authMode === 'bearer' ? '应用 Bearer 密钥' : '无鉴权'}</Text>
        {created.secret && <><Text>应用密钥（具备管理权限时可以再次查看）</Text>
          <Input.Password readOnly value={created.secret} addonAfter={<CopyOutlined onClick={() => copy(created.secret!)} />} /></>}
      </Space>}
    </Modal>
    <Modal title={`查看入口密钥：${revealed?.name ?? ''}`} open={Boolean(revealed)} onCancel={() => setRevealed(null)}
      footer={<Button onClick={() => setRevealed(null)}>关闭</Button>}>
      {revealed && <Input.Password readOnly value={revealed.secret} addonAfter={<CopyOutlined onClick={() => copy(revealed.secret)} />} />}
    </Modal>
    <Modal title={`轮换入口密钥：${rotateTarget?.name ?? ''}`} open={Boolean(rotateTarget)}
      onCancel={() => setRotateTarget(null)} onOk={() => rotateTarget && rotate.mutate(rotateTarget)}
      okText="确认轮换" confirmLoading={rotate.isPending}>
      <Alert type="warning" showIcon message="轮换后旧密钥立即失效。使用这条连接的所有应用都需要改为新密钥。" />
    </Modal>
  </div>
}

function ParameterValueFields({ definitions }: { definitions: MCPParameterDefinition[] }) {
  return <div className="mcp-parameter-values">
    {definitions.map((item) => <Form.Item key={parameterKey(item)} name={['parameterValues', parameterKey(item)]}
      label={`${item.name}（${item.location === 'header' ? '请求头' : item.location === 'query' ? 'URL 参数' : '路径参数'}）`}
      extra={item.description || item.label || undefined}
      rules={item.required ? [{ required: true, whitespace: true, message: `请填写 ${item.name}` }] : undefined}>
      <Input.Password autoComplete="new-password" placeholder={item.required ? '必填' : '可选'} />
    </Form.Item>)}
  </div>
}

function errorMessage(error: unknown, fallback: string) {
  return error instanceof ApiError || error instanceof Error ? error.message : fallback
}
