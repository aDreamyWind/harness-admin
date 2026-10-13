import {
  ApiOutlined,
  CopyOutlined,
  EditOutlined,
  KeyOutlined,
  PlusOutlined,
  SafetyCertificateOutlined,
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
  Select,
  Space,
  Table,
  Tag,
  Typography,
  message,
  type TableColumnsType,
} from 'antd'
import { useMemo, useState } from 'react'

import { ApiError } from '../api/client'
import {
  changeOIDCClientStatus,
  createOIDCClient,
  listOIDCClients,
  rotateOIDCClientSecret,
  updateOIDCClient,
  type CreatedOIDCClient,
  type OIDCClient,
  type OIDCClientInput,
  type OIDCScope,
} from '../api/oidc'
import { useAuth } from '../auth/AuthContext'
import { StatusIndicator } from '../components/StatusIndicator'

const { Paragraph, Text } = Typography

const scopeOptions: Array<{ label: string; value: OIDCScope; description: string }> = [
  { label: '登录身份', value: 'openid', description: '必须，用于识别登录用户' },
  { label: '基础资料', value: 'profile', description: '用户名和显示名称' },
  { label: '邮箱', value: 'email', description: '用户邮箱地址' },
  { label: '调用模型', value: 'model.invoke', description: '代表用户访问模型网关' },
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

  const clientsQuery = useQuery({ queryKey: ['oidc-clients'], queryFn: listOIDCClients })
  const clients = clientsQuery.data ?? []

  const saveClient = useMutation({
    mutationFn: (values: OIDCClientInput) => {
      const input = {
        name: values.name.trim(),
        redirectUris: values.redirectUris ?? [],
        allowedScopes: values.allowedScopes ?? [],
      }
      return editingClient ? updateOIDCClient(editingClient.id, input) : createOIDCClient(input)
    },
    onSuccess: async (result) => {
      closeModal()
      if ('clientSecret' in result && typeof result.clientSecret === 'string') {
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
    form.setFieldsValue({ name: '', redirectUris: [], allowedScopes: ['openid', 'profile', 'model.invoke'] })
    setModalOpen(true)
  }

  function openEdit(client: OIDCClient) {
    setEditingClient(client)
    saveClient.reset()
    form.setFieldsValue({ name: client.name, redirectUris: client.redirectUris, allowedScopes: client.allowedScopes })
    setModalOpen(true)
  }

  function closeModal() {
    setModalOpen(false)
    setEditingClient(null)
    form.resetFields()
    saveClient.reset()
  }

  const columns: TableColumnsType<OIDCClient> = useMemo(() => [
    {
      title: '应用', key: 'identity', width: 190,
      render: (_, client) => <div className="application-access-name"><ApiOutlined /><span><strong>{client.name}</strong><small>{client.clientSecretPrefix}••••••</small></span></div>,
    },
    { title: '客户端 ID', dataIndex: 'clientId', key: 'clientId', width: 220, render: (value: string) => <Text code copyable>{value}</Text> },
    {
      title: '登录回调', dataIndex: 'redirectUris', key: 'redirectUris', width: 230,
      render: (values: string[]) => <div className="application-access-callback"><Text ellipsis={{ tooltip: values[0] }}>{values[0]}</Text>{values.length > 1 ? <small>另有 {values.length - 1} 个地址</small> : null}</div>,
    },
    {
      title: '授权范围', dataIndex: 'allowedScopes', key: 'allowedScopes', width: 200,
      render: (values: OIDCScope[]) => <div className="application-access-scopes">{values.map((value) => <Tag key={value}>{scopeLabels.get(value) ?? value}</Tag>)}</div>,
    },
    { title: '状态', key: 'status', width: 90, render: (_, client) => <StatusIndicator label={client.status === 'active' ? '已启用' : '已停用'} tone={client.status === 'active' ? 'success' : 'muted'} /> },
    ...(canWrite ? [{
      title: '操作', key: 'actions', width: 190,
      render: (_: unknown, client: OIDCClient) => <Space size={2}>
        <Button type="link" size="small" icon={<EditOutlined />} onClick={() => openEdit(client)}>编辑</Button>
        <Popconfirm title="轮换客户端密钥？" description="旧密钥将立即失效，请及时更新接入应用。" onConfirm={() => rotateSecret.mutate(client.id)}>
          <Button type="link" size="small" icon={<SyncOutlined />}>轮换密钥</Button>
        </Popconfirm>
        <Popconfirm title={client.status === 'active' ? '停用这个应用？' : '重新启用这个应用？'} onConfirm={() => clientStatus.mutate(client)}>
          <Button type="link" size="small" danger={client.status === 'active'}>{client.status === 'active' ? '停用' : '启用'}</Button>
        </Popconfirm>
      </Space>,
    }] : []),
  ], [canWrite, clientStatus, rotateSecret])

  return (
    <main>
      {messageContext}
      <section className="module-heading"><Text className="module-kicker">身份与组织 / 应用接入</Text></section>

      <section className="application-access-protocol" aria-label="统一登录协议">
        <div><SafetyCertificateOutlined /><span><small>接入协议</small><strong>OpenID Connect</strong></span></div>
        <i />
        <div><KeyOutlined /><span><small>安全模式</small><strong>授权码 + PKCE</strong></span></div>
        <i />
        <div><ApiOutlined /><span><small>网关权限</small><strong>按用户校验</strong></span></div>
      </section>

      <Card className="model-resource-card" variant="borderless">
        <div className="model-resource-toolbar">
          <div><strong>接入应用</strong><span>回调地址精确匹配；客户端密钥只在创建或轮换后展示一次。</span></div>
          {canWrite ? <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>接入应用</Button> : null}
        </div>
        {clientsQuery.isError ? <Alert className="model-resource-error" type="error" showIcon message={errorMessage(clientsQuery.error, '接入应用加载失败')} action={<Button size="small" onClick={() => clientsQuery.refetch()}>重试</Button>} /> : null}
        <Table<OIDCClient> rowKey="id" columns={columns} dataSource={clients} loading={clientsQuery.isLoading} pagination={false} scroll={{ x: 1120 }} locale={{ emptyText: '尚未接入统一登录应用' }} />
      </Card>

      <Modal title={editingClient ? '编辑接入应用' : '接入应用'} open={modalOpen} width={720} okText={editingClient ? '保存修改' : '创建应用'} cancelText="取消" confirmLoading={saveClient.isPending} onCancel={closeModal} onOk={() => form.submit()} forceRender>
        {saveClient.isError ? <Alert className="form-alert" type="error" showIcon message={errorMessage(saveClient.error, '应用接入设置保存失败')} /> : null}
        <Form<OIDCClientInput> form={form} layout="vertical" requiredMark={false} onFinish={(values) => saveClient.mutate(values)}>
          <Form.Item label="应用名称" name="name" rules={[{ required: true, whitespace: true, message: '请输入应用名称' }, { max: 120 }]}>
            <Input placeholder="例如：DeepSeek Harness" autoFocus />
          </Form.Item>
          <Form.Item label="登录回调地址" name="redirectUris" extra="必须填写完整地址。生产环境仅允许 HTTPS，本地开发可使用 localhost 或回环地址。" rules={[{ required: true, type: 'array', min: 1, message: '请至少填写一个登录回调地址' }]}>
            <Select mode="tags" tokenSeparators={['\n']} placeholder="输入地址后按回车，例如 https://harness.example.com/oauth/callback" />
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

      <Modal title="保存客户端凭据" open={Boolean(createdClient)} okText="我已保存" cancelButtonProps={{ style: { display: 'none' } }} closable={false} mask={{ closable: false }} onOk={() => setCreatedClient(null)}>
        <Alert type="warning" showIcon message="关闭后无法再次查看完整客户端密钥" />
        <div className="application-credential-box">
          <span><small>客户端 ID</small><Text code>{createdClient?.clientId}</Text></span>
          <Button icon={<CopyOutlined />} onClick={async () => { if (!createdClient) return; await navigator.clipboard.writeText(createdClient.clientId); messageApi.success('客户端 ID 已复制') }}>复制</Button>
        </div>
        <div className="application-credential-box">
          <span><small>客户端密钥</small><Text code>{createdClient?.clientSecret}</Text></span>
          <Button icon={<CopyOutlined />} onClick={async () => { if (!createdClient) return; await navigator.clipboard.writeText(createdClient.clientSecret); messageApi.success('客户端密钥已复制') }}>复制</Button>
        </div>
        <Paragraph type="secondary">请保存到 DeepSeek Harness 的服务端密钥配置中，不要放入浏览器代码或提交到 Git。</Paragraph>
      </Modal>
    </main>
  )
}

function errorMessage(error: unknown, fallback: string) {
  return error instanceof ApiError ? error.message : fallback
}
