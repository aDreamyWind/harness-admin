import {
  CloudServerOutlined,
  CopyOutlined,
  DesktopOutlined,
  EditOutlined,
  EyeOutlined,
  PlusOutlined,
  SyncOutlined,
} from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Alert,
  Button,
  Card,
  Checkbox,
  Form,
  Input,
  Modal,
  Popconfirm,
  Radio,
  Select,
  Space,
  Table,
  Tag,
  Typography,
  message,
  type TableColumnsType,
} from 'antd'
import { useState } from 'react'

import { ApiError } from '../api/client'
import {
  changeOIDCClientStatus,
  createOIDCClient,
  listOIDCClients,
  revealOIDCClientSecret,
  rotateOIDCClientSecret,
  updateOIDCClient,
  type CreatedOIDCClient,
  type OIDCClient,
  type OIDCClientInput,
  type OIDCScope,
} from '../api/oidc'
import { useAuth } from '../auth/AuthContext'
import { SecretRevealModal } from '../components/SecretRevealModal'
import { StatusIndicator } from '../components/StatusIndicator'

const { Paragraph, Text } = Typography

const scopeOptions: Array<{ label: string; value: OIDCScope; description: string }> = [
  { label: '登录身份', value: 'openid', description: '必须，用于识别登录用户' },
  { label: '基础资料', value: 'profile', description: '用户名和显示名称' },
  { label: '邮箱', value: 'email', description: '用户邮箱地址' },
  { label: '调用模型', value: 'model.invoke', description: '代表用户访问模型网关' },
  { label: '调用 MCP', value: 'mcp.invoke', description: '代表用户访问已授权的企业 MCP 连接' },
  { label: '长期会话', value: 'offline_access', description: '允许使用刷新令牌' },
]
const scopeLabels = new Map(scopeOptions.map((item) => [item.value, item.label]))

export function ApplicationAccessPage() {
  const { hasPermission } = useAuth()
  const canWrite = hasPermission('oidc.client.write')
  const queryClient = useQueryClient()
  const [messageApi, messageContext] = message.useMessage()
  const [form] = Form.useForm<OIDCClientInput>()
  const [modalOpen, setModalOpen] = useState(false)
  const [editingClient, setEditingClient] = useState<OIDCClient | null>(null)
  const [createdClient, setCreatedClient] = useState<CreatedOIDCClient | null>(null)
  const [viewedClient, setViewedClient] = useState<OIDCClient | null>(null)
  const selectedApplicationType = Form.useWatch('applicationType', form)

  const clientsQuery = useQuery({ queryKey: ['oidc-clients'], queryFn: listOIDCClients })
  const clients = clientsQuery.data ?? []

  const saveClient = useMutation({
    mutationFn: async (values: OIDCClientInput) => {
      const input = {
        name: values.name.trim(),
        applicationType: values.applicationType,
        redirectUris: values.redirectUris ?? [],
        allowedScopes: values.allowedScopes ?? [],
      }
      const isCreate = !editingClient
      const result = editingClient ? await updateOIDCClient(editingClient.id, input) : await createOIDCClient(input)
      return { result, isCreate }
    },
    onSuccess: async ({ result, isCreate }) => {
      closeModal()
      if (isCreate) {
        setCreatedClient(result as CreatedOIDCClient)
      } else {
        messageApi.success('应用接入设置已更新')
      }
      await queryClient.invalidateQueries({ queryKey: ['oidc-clients'] })
    },
  })

  const clientStatus = useMutation({
    mutationFn: (client: OIDCClient) => changeOIDCClientStatus(client.id, client.status === 'active' ? 'disabled' : 'active'),
    onSuccess: async () => {
      messageApi.success('应用状态已更新')
      await queryClient.invalidateQueries({ queryKey: ['oidc-clients'] })
    },
    onError: (error) => messageApi.error(errorMessage(error, '应用状态更新失败')),
  })

  const rotateSecret = useMutation({
    mutationFn: rotateOIDCClientSecret,
    onSuccess: async (result) => {
      setCreatedClient(result)
      await queryClient.invalidateQueries({ queryKey: ['oidc-clients'] })
    },
    onError: (error) => messageApi.error(errorMessage(error, '客户端密钥轮换失败')),
  })

  function openCreate() {
    setEditingClient(null)
    saveClient.reset()
    form.setFieldsValue({ name: '', applicationType: 'desktop', redirectUris: ['http://127.0.0.1/oauth/callback'], allowedScopes: ['openid', 'profile', 'model.invoke', 'mcp.invoke', 'offline_access'] })
    setModalOpen(true)
  }

  function openEdit(client: OIDCClient) {
    setEditingClient(client)
    saveClient.reset()
    form.setFieldsValue({ name: client.name, applicationType: client.applicationType, redirectUris: client.redirectUris, allowedScopes: client.allowedScopes })
    setModalOpen(true)
  }

  function closeModal() {
    setModalOpen(false)
    setEditingClient(null)
    form.resetFields()
    saveClient.reset()
  }

  const columns: TableColumnsType<OIDCClient> = [
    {
      title: '应用', key: 'identity', width: 190,
      render: (_, client) => <strong className="application-access-cell application-access-name">{client.name}</strong>,
    },
    {
      title: '客户端 ID', dataIndex: 'clientId', key: 'clientId', width: 354,
      render: (value: string) => <div className="application-access-cell application-access-client-id">
        <Text code copyable title={value}>{value}</Text>
      </div>,
    },
    {
      title: '登录回调', dataIndex: 'redirectUris', key: 'redirectUris', width: 244,
      render: (values: string[]) => <div className="application-access-cell application-access-callback">
        {values.map((value) => <code key={value}>{value}</code>)}
      </div>,
    },
    {
      title: '授权范围', dataIndex: 'allowedScopes', key: 'allowedScopes', width: 234,
      render: (values: OIDCScope[]) => <div className="application-access-cell application-access-scopes">
        {values.map((value) => <Tag key={value}>{scopeLabels.get(value) ?? value}</Tag>)}
      </div>,
    },
    {
      title: '状态', key: 'status', width: 88,
      render: (_, client) => <div className="application-access-cell application-access-status">
        <StatusIndicator label={client.status === 'active' ? '已启用' : '已停用'} tone={client.status === 'active' ? 'success' : 'muted'} />
      </div>,
    },
    {
      title: '应用类型', key: 'applicationType', width: 104,
      render: (_, client) => <div className="application-access-cell application-access-type">
        {client.applicationType === 'desktop' ? '桌面应用' : '服务端应用'}
      </div>,
    },
    ...(canWrite ? [{
      title: '操作', key: 'actions', width: 352,
      render: (_: unknown, client: OIDCClient) => <Space className="application-access-cell application-access-actions" size={2} wrap>
        <Button type="link" size="small" icon={<EditOutlined />} onClick={() => openEdit(client)}>编辑</Button>
        {client.applicationType === 'web' ? <Button type="link" size="small" icon={<EyeOutlined />} onClick={() => setViewedClient(client)}>查看密钥</Button> : null}
        {client.applicationType === 'web' ? <Popconfirm title="轮换客户端密钥？" description="旧密钥将立即失效，请及时更新接入应用。" onConfirm={() => rotateSecret.mutate(client.id)}>
          <Button type="link" size="small" icon={<SyncOutlined />}>轮换密钥</Button>
        </Popconfirm> : null}
        <Popconfirm title={client.status === 'active' ? '停用这个应用？' : '重新启用这个应用？'} onConfirm={() => clientStatus.mutate(client)}>
          <Button type="link" size="small" danger={client.status === 'active'}>{client.status === 'active' ? '停用' : '启用'}</Button>
        </Popconfirm>
      </Space>,
    }] : []),
  ]

  return (
    <main>
      {messageContext}
      <section className="module-heading"><Text className="module-kicker">AI 资源管理 / 应用接入</Text></section>

      <Card className="model-resource-card" variant="borderless">
        <div className="model-resource-toolbar">
          <div><strong>接入应用</strong><span>桌面应用使用 PKCE 且无需密钥；服务端应用密钥加密保存并可按权限查看。</span></div>
          {canWrite ? <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>接入应用</Button> : null}
        </div>
        {clientsQuery.isError ? <Alert className="model-resource-error" type="error" showIcon message={errorMessage(clientsQuery.error, '接入应用加载失败')} action={<Button size="small" onClick={() => clientsQuery.refetch()}>重试</Button>} /> : null}
        <Table<OIDCClient> className="application-access-table" rowKey="id" columns={columns} dataSource={clients} loading={clientsQuery.isLoading}
          pagination={false} tableLayout="fixed" scroll={{ x: canWrite ? 1566 : 1214 }} locale={{ emptyText: '尚未接入统一登录应用' }} />
      </Card>

      <Modal title={editingClient ? '编辑接入应用' : '接入应用'} open={modalOpen} width={720} okText={editingClient ? '保存修改' : '创建应用'} cancelText="取消" confirmLoading={saveClient.isPending} onCancel={closeModal} onOk={() => form.submit()} forceRender>
        {saveClient.isError ? <Alert className="form-alert" type="error" showIcon message={errorMessage(saveClient.error, '应用接入设置保存失败')} /> : null}
        <Form<OIDCClientInput> form={form} layout="vertical" requiredMark={false} onFinish={(values) => saveClient.mutate(values)}>
          <Form.Item label="应用名称" name="name" rules={[{ required: true, whitespace: true, message: '请输入应用名称' }, { max: 120 }]}>
            <Input placeholder="例如：DeepSeek Harness" autoFocus />
          </Form.Item>
          <Form.Item label="应用类型" name="applicationType" rules={[{ required: true }]}>
            <Radio.Group className="application-type-options" disabled={Boolean(editingClient)}>
              <Radio.Button value="desktop"><DesktopOutlined /><span><strong>桌面应用</strong><small>适用于每位员工安装的 Harness</small></span></Radio.Button>
              <Radio.Button value="web"><CloudServerOutlined /><span><strong>服务端应用</strong><small>适用于能安全保存密钥的系统</small></span></Radio.Button>
            </Radio.Group>
          </Form.Item>
          {selectedApplicationType === 'desktop' ? <Alert className="application-type-hint" type="info" showIcon message="员工无需配置密钥" description="桌面端启动本地回调后，可将随机端口带入登录请求。管理端只需登记不带端口的回环地址。" /> : null}
          <Form.Item label="登录回调地址" name="redirectUris" extra={selectedApplicationType === 'desktop' ? '建议使用 http://127.0.0.1/oauth/callback；员工电脑上的实际回调可使用随机端口。' : '必须填写完整 HTTPS 地址，授权时需精确匹配。'} rules={[{ required: true, type: 'array', min: 1, message: '请至少填写一个登录回调地址' }]}>
            <Select mode="tags" tokenSeparators={['\n']} placeholder={selectedApplicationType === 'desktop' ? 'http://127.0.0.1/oauth/callback' : 'https://harness.example.com/oauth/callback'} />
          </Form.Item>
          <Form.Item
            label="授权范围"
            name="allowedScopes"
            rules={[{ validator: (_, values: OIDCScope[]) => values?.includes('openid') ? Promise.resolve() : Promise.reject(new Error('必须保留登录身份权限')) }]}
          >
            <Checkbox.Group className="application-scope-options">
              {scopeOptions.map((scope) => <Checkbox key={scope.value} value={scope.value} disabled={scope.value === 'openid'}><span><strong>{scope.label}</strong><small>{scope.description}</small></span></Checkbox>)}
            </Checkbox.Group>
          </Form.Item>
        </Form>
      </Modal>

      <Modal title={createdClient?.applicationType === 'desktop' ? '桌面应用已接入' : '保存客户端凭据'} open={Boolean(createdClient)} width={800} okText="完成" cancelButtonProps={{ style: { display: 'none' } }} closable={false} mask={{ closable: false }} onOk={() => { setCreatedClient(null); saveClient.reset(); rotateSecret.reset() }}>
        {createdClient?.applicationType === 'web' ? <Alert type="success" showIcon message="客户端密钥已加密保存，之后仍可在应用列表中查看" /> : <Alert type="success" showIcon message="无需为员工分发密钥" description="将客户端 ID 和 Harness Admin 地址预置到企业桌面端即可。" />}
        <div className="application-credential-box">
          <span><small>客户端 ID</small><Input aria-label="客户端 ID" readOnly value={createdClient?.clientId ?? ''} /></span>
          <Button icon={<CopyOutlined />} onClick={async () => { if (!createdClient) return; await navigator.clipboard.writeText(createdClient.clientId); messageApi.success('客户端 ID 已复制') }}>复制</Button>
        </div>
        {createdClient?.applicationType === 'web' ? <div className="application-credential-box">
          <span><small>客户端密钥</small><Input aria-label="客户端密钥" readOnly value={createdClient?.clientSecret ?? ''} /></span>
          <Button icon={<CopyOutlined />} onClick={async () => { if (!createdClient?.clientSecret) return; await navigator.clipboard.writeText(createdClient.clientSecret); messageApi.success('客户端密钥已复制') }}>复制</Button>
        </div> : null}
        <Paragraph type="secondary">{createdClient?.applicationType === 'desktop' ? '这些配置应由企业版 DeepSeek Harness 预置，不需要员工自己填写。' : '密钥只能保存在接入应用的服务端，不要放入浏览器或桌面客户端。'}</Paragraph>
      </Modal>

      {viewedClient && <SecretRevealModal key={viewedClient.id} title={`${viewedClient.name} · 查看密钥`} label="客户端密钥"
        requestSecret={() => revealOIDCClientSecret(viewedClient.id)} onClose={() => setViewedClient(null)}
        onCopy={() => messageApi.success('客户端密钥已复制')} />}
    </main>
  )
}

function errorMessage(error: unknown, fallback: string) {
  return error instanceof ApiError ? error.message : fallback
}
