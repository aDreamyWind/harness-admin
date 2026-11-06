import { LogoutOutlined, PlusOutlined } from '@ant-design/icons'
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
  Typography,
  message,
  type TableColumnsType,
} from 'antd'
import { useState } from 'react'

import {
  createPlatformOrganization,
  listPlatformOrganizations,
  updatePlatformOrganizationStatus,
  type CreatePlatformOrganizationInput,
  type PlatformOrganization,
} from '../api/platform'
import { formatDateTime } from '../utils/dateTime'
import { usePlatformAuth } from '../auth/PlatformAuthContext'
import { StatusIndicator } from '../components/StatusIndicator'

const { Text } = Typography

interface OrganizationForm extends CreatePlatformOrganizationInput {
  confirmPassword: string
}

export function PlatformOrganizationsPage() {
  const { admin, logout } = usePlatformAuth()
  const [form] = Form.useForm<OrganizationForm>()
  const [modalOpen, setModalOpen] = useState(false)
  const [messageApi, contextHolder] = message.useMessage()
  const queryClient = useQueryClient()
  const organizationsQuery = useQuery({ queryKey: ['platform-organizations'], queryFn: listPlatformOrganizations })

  const createMutation = useMutation({
    mutationFn: (values: OrganizationForm) => createPlatformOrganization(values),
    onSuccess: async (organization) => {
      messageApi.success(`企业“${organization.name}”已创建`)
      setModalOpen(false)
      form.resetFields()
      await queryClient.invalidateQueries({ queryKey: ['platform-organizations'] })
    },
  })

  const statusMutation = useMutation({
    mutationFn: ({ id, status }: { id: string; status: PlatformOrganization['status'] }) => updatePlatformOrganizationStatus(id, status),
    onSuccess: async (organization) => {
      messageApi.success(organization.status === 'active' ? '企业已启用' : '企业已停用')
      await queryClient.invalidateQueries({ queryKey: ['platform-organizations'] })
    },
    onError: (error) => messageApi.error(error instanceof Error ? error.message : '企业状态修改失败'),
  })

  const columns: TableColumnsType<PlatformOrganization> = [
    { title: '企业', dataIndex: 'name', key: 'name', width: 180, render: (value) => <strong>{value}</strong> },
    { title: '企业编码', dataIndex: 'code', key: 'code', width: 120 },
    { title: '企业管理员账号', dataIndex: 'adminUsername', key: 'adminUsername', width: 150, render: (value) => value || <Text type="secondary">未设置</Text> },
    { title: '用户数', dataIndex: 'userCount', key: 'userCount', width: 70 },
    {
      title: '状态', dataIndex: 'status', key: 'status', width: 80,
      render: (status: PlatformOrganization['status']) => <StatusIndicator label={status === 'active' ? '启用' : '停用'} tone={status === 'active' ? 'success' : 'muted'} />,
    },
    { title: '创建时间', dataIndex: 'createdAt', key: 'createdAt', width: 170, responsive: ['lg'], render: formatDateTime },
    {
      title: '操作', key: 'action', width: 80, fixed: 'right',
      render: (_, organization) => {
        const next = organization.status === 'active' ? 'disabled' : 'active'
        return (
          <Popconfirm
            title={next === 'disabled' ? '停用这个企业？' : '启用这个企业？'}
            description={next === 'disabled' ? '停用后，该企业所有用户将无法登录。' : '启用后，该企业用户可以重新登录。'}
            okText={next === 'disabled' ? '停用' : '启用'}
            cancelText="取消"
            onConfirm={() => statusMutation.mutate({ id: organization.id, status: next })}
          >
            <Button type="link" size="small">{next === 'disabled' ? '停用' : '启用'}</Button>
          </Popconfirm>
        )
      },
    },
  ]

  return (
    <main className="platform-page">
      {contextHolder}
      <header className="platform-topbar">
        <div className="platform-brand"><span>PA</span><strong>平台管理</strong></div>
        <Space>
          <Text>{admin?.displayName}</Text>
          <Button icon={<LogoutOutlined />} onClick={() => logout()}>退出</Button>
        </Space>
      </header>
      <section className="platform-content">
        <div className="module-heading">
          <div>
            <Text className="module-kicker">平台管理 / 企业</Text>
          </div>
          <Button type="primary" icon={<PlusOutlined />} onClick={() => { createMutation.reset(); setModalOpen(true) }}>新建企业</Button>
        </div>
        <Card className="directory-card" variant="borderless">
          <div className="directory-toolbar">
            <Text>全部企业</Text>
            <span className="directory-total">共 {organizationsQuery.data?.length ?? 0} 个企业</span>
          </div>
          {organizationsQuery.isError ? (
            <Alert type="error" showIcon message="企业列表加载失败" description={organizationsQuery.error instanceof Error ? organizationsQuery.error.message : '请稍后重试'} />
          ) : (
            <Table rowKey="id" columns={columns} dataSource={organizationsQuery.data ?? []} loading={organizationsQuery.isLoading} pagination={false} scroll={{ x: 680 }} locale={{ emptyText: '还没有企业，可以先创建一个企业' }} />
          )}
        </Card>
      </section>

      <Modal
        title="新建企业"
        open={modalOpen}
        okText="创建企业"
        cancelText="取消"
        width={680}
        confirmLoading={createMutation.isPending}
        onCancel={() => { setModalOpen(false); form.resetFields() }}
        onOk={() => form.submit()}
        forceRender
      >
        {createMutation.isError ? <Alert className="form-alert" type="error" showIcon message={createMutation.error instanceof Error ? createMutation.error.message : '企业创建失败'} /> : null}
        <Form<OrganizationForm> form={form} layout="vertical" requiredMark={false} onFinish={(values) => createMutation.mutate(values)}>
          <div className="platform-form-section"><strong>企业资料</strong><span>企业编码创建后不可修改。</span></div>
          <div className="platform-form-grid">
            <Form.Item label="企业名称" name="name" rules={[{ required: true, whitespace: true, message: '请输入企业名称' }, { max: 160 }]}>
              <Input placeholder="例如：示例科技有限公司" />
            </Form.Item>
            <Form.Item label="企业编码" name="code" rules={[
              { required: true, message: '请输入企业编码' },
              { pattern: /^[a-z][a-z0-9._-]{1,63}$/, message: '以小写字母开头，至少 2 位' },
            ]}>
              <Input placeholder="例如：example" />
            </Form.Item>
          </div>
          <div className="platform-form-section"><strong>首位企业管理员</strong><span>创建后可登录企业控制台继续配置组织。</span></div>
          <div className="platform-form-grid">
            <Form.Item label="管理员账号" name="adminUsername" rules={[{ required: true, whitespace: true, message: '请输入管理员账号' }, { max: 80 }]}>
              <Input placeholder="例如：admin" />
            </Form.Item>
            <Form.Item label="管理员姓名" name="adminDisplayName" rules={[{ required: true, whitespace: true, message: '请输入管理员姓名' }, { max: 120 }]}>
              <Input placeholder="例如：企业管理员" />
            </Form.Item>
          </div>
          <Form.Item label="管理员邮箱" name="adminEmail" rules={[{ type: 'email', message: '请输入正确的邮箱地址' }]}>
            <Input placeholder="可选" />
          </Form.Item>
          <div className="platform-form-grid">
            <Form.Item label="初始密码" name="adminPassword" rules={[{ required: true, message: '请输入初始密码' }, { min: 3, message: '密码不能少于 3 位' }]}>
              <Input.Password autoComplete="new-password" />
            </Form.Item>
            <Form.Item label="确认密码" name="confirmPassword" dependencies={['adminPassword']} rules={[
              { required: true, message: '请再次输入密码' },
              ({ getFieldValue }) => ({ validator(_, value) { return !value || getFieldValue('adminPassword') === value ? Promise.resolve() : Promise.reject(new Error('两次输入的密码不一致')) } }),
            ]}>
              <Input.Password autoComplete="new-password" />
            </Form.Item>
          </div>
        </Form>
      </Modal>
    </main>
  )
}
