import { EditOutlined, KeyOutlined, PlusOutlined, SearchOutlined, UnlockOutlined, UserOutlined } from '@ant-design/icons'
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
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
import { useState } from 'react'

import { ApiError } from '../api/client'
import { assignUserDepartment, listDepartments } from '../api/departments'
import { assignUserRoles, listRoles } from '../api/roles'
import {
  changeUserStatus,
  createUser,
  listUsers,
  resetUserPassword,
  unlockUser,
  updateUser,
  type CreateUserInput,
  type ManagedUser,
  type UpdateUserInput,
} from '../api/users'
import { useAuth } from '../auth/AuthContext'
import { DepartmentSelect } from '../components/DepartmentSelect'
import { StatusIndicator } from '../components/StatusIndicator'

const { Paragraph, Text } = Typography

const statusLabels: Record<ManagedUser['status'], { text: string; tone: 'success' | 'warning' | 'muted' }> = {
  active: { text: '正常', tone: 'success' },
  locked: { text: '已锁定', tone: 'warning' },
  disabled: { text: '已停用', tone: 'muted' },
}

export function UsersPage() {
  const { user: currentUser, hasPermission, refreshUser } = useAuth()
  const canWrite = hasPermission('iam.user.write')
  const canReadRoles = hasPermission('iam.role.read')
  const canReadDepartments = hasPermission('iam.department.read')
  const queryClient = useQueryClient()
  const [form] = Form.useForm<CreateUserInput>()
  const [editForm] = Form.useForm<UpdateUserInput>()
  const [passwordForm] = Form.useForm<{ password: string; confirmPassword: string }>()
  const [messageApi, messageContext] = message.useMessage()
  const [createOpen, setCreateOpen] = useState(false)
  const [searchInput, setSearchInput] = useState('')
  const [search, setSearch] = useState('')
  const [status, setStatus] = useState('')
  const [page, setPage] = useState(1)
  const [editingUser, setEditingUser] = useState<ManagedUser | null>(null)
  const [roleUser, setRoleUser] = useState<ManagedUser | null>(null)
  const [selectedRoleIDs, setSelectedRoleIDs] = useState<string[]>([])
  const [departmentUser, setDepartmentUser] = useState<ManagedUser | null>(null)
  const [selectedDepartmentID, setSelectedDepartmentID] = useState<string | undefined>()
  const [passwordUser, setPasswordUser] = useState<ManagedUser | null>(null)
  const pageSize = 20

  const usersQuery = useQuery({
    queryKey: ['users', { search, status, page, pageSize }],
    queryFn: () => listUsers({ search, status, page, pageSize }),
    placeholderData: keepPreviousData,
  })

  const rolesQuery = useQuery({ queryKey: ['roles'], queryFn: listRoles, enabled: canWrite && canReadRoles })
  const departmentsQuery = useQuery({ queryKey: ['departments'], queryFn: listDepartments, enabled: canWrite && canReadDepartments })

  const createMutation = useMutation({
    mutationFn: createUser,
    onSuccess: async () => {
      messageApi.success('用户已创建')
      setCreateOpen(false)
      form.resetFields()
      await queryClient.invalidateQueries({ queryKey: ['users'] })
    },
  })

  const statusMutation = useMutation({
    mutationFn: ({ id, nextStatus }: { id: string; nextStatus: 'active' | 'disabled' }) =>
      changeUserStatus(id, nextStatus),
    onSuccess: async (_, variables) => {
      messageApi.success(variables.nextStatus === 'active' ? '用户已启用' : '用户已停用')
      await queryClient.invalidateQueries({ queryKey: ['users'] })
    },
    onError: (error) => messageApi.error(error instanceof Error ? error.message : '状态修改失败'),
  })

  const updateMutation = useMutation({
    mutationFn: ({ userId, input }: { userId: string; input: UpdateUserInput }) => updateUser(userId, input),
    onSuccess: async (updatedUser) => {
      messageApi.success('用户资料已更新')
      setEditingUser(null)
      editForm.resetFields()
      await queryClient.invalidateQueries({ queryKey: ['users'] })
      if (updatedUser.id === currentUser?.id) {
        await refreshUser()
      }
    },
    onError: (error) => messageApi.error(error instanceof Error ? error.message : '用户资料修改失败'),
  })

  const assignRolesMutation = useMutation({
    mutationFn: ({ userId, roleIds }: { userId: string; roleIds: string[] }) => assignUserRoles(userId, roleIds),
    onSuccess: async () => {
      messageApi.success('用户角色已更新')
      setRoleUser(null)
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['users'] }),
        queryClient.invalidateQueries({ queryKey: ['roles'] }),
      ])
    },
    onError: (error) => messageApi.error(error instanceof Error ? error.message : '角色分配失败'),
  })

  const assignDepartmentMutation = useMutation({
    mutationFn: ({ userId, departmentId }: { userId: string; departmentId?: string }) => assignUserDepartment(userId, departmentId),
    onSuccess: async () => {
      messageApi.success('用户部门已更新')
      setDepartmentUser(null)
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['users'] }),
        queryClient.invalidateQueries({ queryKey: ['departments'] }),
      ])
    },
    onError: (error) => messageApi.error(error instanceof Error ? error.message : '部门调整失败'),
  })

  const resetPasswordMutation = useMutation({
    mutationFn: ({ userId, password }: { userId: string; password: string }) => resetUserPassword(userId, password),
    onSuccess: () => {
      messageApi.success('密码已重置，该用户的登录会话已全部退出')
      setPasswordUser(null)
      passwordForm.resetFields()
    },
    onError: (error) => messageApi.error(error instanceof Error ? error.message : '密码重置失败'),
  })

  const unlockMutation = useMutation({
    mutationFn: unlockUser,
    onSuccess: async () => {
      messageApi.success('用户已解锁，可以重新登录')
      await queryClient.invalidateQueries({ queryKey: ['users'] })
    },
    onError: (error) => messageApi.error(error instanceof Error ? error.message : '用户解锁失败'),
  })

  const columns: TableColumnsType<ManagedUser> = [
    {
      title: '用户',
      key: 'user',
      width: 190,
      render: (_, user) => (
        <div className="user-identity-cell">
          <span className="user-table-avatar"><UserOutlined /></span>
          <span>
            <strong>{user.displayName}</strong>
          </span>
        </div>
      ),
    },
    {
      title: '账号',
      dataIndex: 'username',
      key: 'username',
      width: 140,
    },
    {
      title: '邮箱',
      dataIndex: 'email',
      key: 'email',
      width: 220,
      render: (email?: string) => email || <Text type="secondary">未设置</Text>,
    },
    {
      title: '部门',
      dataIndex: 'departmentName',
      key: 'department',
      width: 150,
      render: (name?: string) => name || <Text type="secondary">未分配</Text>,
    },
    {
      title: '角色',
      dataIndex: 'roleNames',
      key: 'roles',
      width: 180,
      render: (roles: string[]) =>
        roles.length ? roles.map((role) => <Tag key={role}>{role}</Tag>) : <Text type="secondary">未分配</Text>,
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 100,
      render: (value: ManagedUser['status']) => <StatusIndicator label={statusLabels[value].text} tone={statusLabels[value].tone} />,
    },
    {
      title: '最后登录',
      dataIndex: 'lastLoginAt',
      key: 'lastLoginAt',
      width: 170,
      render: (value?: string) => value ? formatDateTime(value) : <Text type="secondary">尚未登录</Text>,
    },
  ]

  if (canWrite) columns.push({
      title: '操作',
      key: 'action',
      width: 285,
      fixed: 'right',
      render: (_, user) => {
        const nextStatus = user.status === 'active' ? 'disabled' : 'active'
        const isSelf = user.id === currentUser?.id
        return (
          <Space size={0}>
            <Button
              type="link"
              size="small"
              icon={<EditOutlined />}
              onClick={() => {
                setEditingUser(user)
                editForm.setFieldsValue({ displayName: user.displayName, email: user.email ?? '' })
                updateMutation.reset()
              }}
            >
              资料
            </Button>
            {canReadRoles ? (
              <Button
                type="link"
                size="small"
                disabled={isSelf}
                onClick={() => { setRoleUser(user); setSelectedRoleIDs(user.roleIds); assignRolesMutation.reset() }}
              >
                角色
              </Button>
            ) : null}
            {canReadDepartments ? (
              <Button
                type="link"
                size="small"
                onClick={() => {
                  setDepartmentUser(user)
                  setSelectedDepartmentID(user.departmentId || undefined)
                  assignDepartmentMutation.reset()
                }}
              >
                部门
              </Button>
            ) : null}
            <Button
              type="link"
              size="small"
              icon={<KeyOutlined />}
              disabled={isSelf}
              onClick={() => {
                setPasswordUser(user)
                passwordForm.resetFields()
                resetPasswordMutation.reset()
              }}
            >
              密码
            </Button>
            {user.status === 'locked' ? (
              <Popconfirm
                title="解锁这个用户？"
                description="解锁后，该用户可以立即重新登录。"
                okText="解锁"
                cancelText="取消"
                onConfirm={() => unlockMutation.mutate(user.id)}
              >
                <Button type="link" size="small" icon={<UnlockOutlined />} loading={unlockMutation.isPending}>
                  解锁
                </Button>
              </Popconfirm>
            ) : (
              <Popconfirm
                title={nextStatus === 'disabled' ? '停用这个用户？' : '启用这个用户？'}
                description={nextStatus === 'disabled' ? '停用后，该用户的现有会话会立即失效。' : '启用后，该用户可以重新登录。'}
                okText={nextStatus === 'disabled' ? '停用' : '启用'}
                cancelText="取消"
                disabled={isSelf}
                onConfirm={() => statusMutation.mutate({ id: user.id, nextStatus })}
              >
                <Button type="link" size="small" disabled={isSelf}>
                  {nextStatus === 'disabled' ? '停用' : '启用'}
                </Button>
              </Popconfirm>
            )}
          </Space>
        )
      },
    })

  return (
    <main>
      {messageContext}
      <section className="module-heading">
        <div>
          <Text className="module-kicker">身份与组织 / 用户</Text>
        </div>
        {canWrite ? (
          <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreateOpen(true)}>
            新建用户
          </Button>
        ) : <Tag>只读</Tag>}
      </section>

      <Card className="directory-card" variant="borderless">
        <div className="directory-toolbar">
          <Input
            className="user-search"
            prefix={<SearchOutlined />}
            allowClear
            value={searchInput}
            placeholder="搜索用户名、姓名或邮箱"
            onChange={(event) => setSearchInput(event.target.value)}
            onPressEnter={() => { setSearch(searchInput.trim()); setPage(1) }}
            onClear={() => { setSearch(''); setPage(1) }}
          />
          <Select
            value={status}
            className="status-filter"
            onChange={(value) => { setStatus(value); setPage(1) }}
            options={[
              { value: '', label: '全部状态' },
              { value: 'active', label: '正常' },
              { value: 'locked', label: '已锁定' },
              { value: 'disabled', label: '已停用' },
            ]}
          />
          <Button onClick={() => { setSearch(searchInput.trim()); setPage(1) }}>查询</Button>
          <span className="directory-total">共 {usersQuery.data?.total ?? 0} 个用户</span>
        </div>

        {usersQuery.isError ? (
          <Alert
            type="error"
            showIcon
            message="用户列表加载失败"
            description={usersQuery.error instanceof Error ? usersQuery.error.message : '请稍后重试'}
            action={<Button size="small" onClick={() => usersQuery.refetch()}>重新加载</Button>}
          />
        ) : (
          <Table<ManagedUser>
            rowKey="id"
            columns={columns}
            dataSource={usersQuery.data?.items ?? []}
            loading={usersQuery.isLoading}
            scroll={{ x: 1500 }}
            pagination={{
              current: page,
              pageSize,
              total: usersQuery.data?.total ?? 0,
              showSizeChanger: false,
              hideOnSinglePage: true,
              onChange: setPage,
            }}
            locale={{ emptyText: '当前条件下没有用户' }}
          />
        )}
      </Card>

      <Modal
        title="新建用户"
        open={createOpen}
        okText="创建用户"
        cancelText="取消"
        confirmLoading={createMutation.isPending}
        onCancel={() => { setCreateOpen(false); createMutation.reset() }}
        onOk={() => form.submit()}
        destroyOnHidden
      >
        {createMutation.isError ? (
          <Alert
            className="form-alert"
            type="error"
            showIcon
            message={createMutation.error instanceof ApiError ? createMutation.error.message : '创建用户失败'}
          />
        ) : null}
        <Form<CreateUserInput> form={form} layout="vertical" requiredMark={false} onFinish={(values) => createMutation.mutate(values)}>
          <Form.Item label="用户名" name="username" rules={[
            { required: true, whitespace: true, message: '请输入用户名' },
            { min: 3, max: 80, message: '用户名长度应为 3 到 80 位' },
            { pattern: /^\S+$/, message: '用户名不能包含空格' },
          ]}>
            <Input autoComplete="off" placeholder="例如：zhangsan" />
          </Form.Item>
          <Form.Item label="显示名称" name="displayName" rules={[{ required: true, whitespace: true, message: '请输入显示名称' }]}>
            <Input autoComplete="off" placeholder="例如：张三" />
          </Form.Item>
          <Form.Item label="邮箱" name="email" rules={[{ type: 'email', message: '请输入正确的邮箱地址' }]}>
            <Input autoComplete="off" placeholder="可选" />
          </Form.Item>
          {canReadDepartments ? (
            <Form.Item label="所属部门" name="departmentId">
              <DepartmentSelect
                departments={departmentsQuery.data ?? []}
                placeholder="可选"
              />
            </Form.Item>
          ) : null}
          <Form.Item label="初始密码" name="password" extra="至少 3 位。创建后请通过安全方式交给用户。" rules={[
            { required: true, message: '请输入初始密码' },
            { min: 3, message: '密码不能少于 3 位' },
          ]}>
            <Input.Password autoComplete="new-password" placeholder="至少 3 位" />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title={`编辑资料${editingUser ? ` · ${editingUser.displayName}` : ''}`}
        open={Boolean(editingUser)}
        okText="保存资料"
        cancelText="取消"
        confirmLoading={updateMutation.isPending}
        onCancel={() => { setEditingUser(null); editForm.resetFields(); updateMutation.reset() }}
        onOk={() => editForm.submit()}
        destroyOnHidden
      >
        <Form<UpdateUserInput>
          form={editForm}
          layout="vertical"
          requiredMark={false}
          onFinish={(values) => editingUser && updateMutation.mutate({ userId: editingUser.id, input: values })}
        >
          <Form.Item label="登录用户名" extra="用户名用于登录和审计，创建后不可修改。">
            <Input value={editingUser?.username} disabled />
          </Form.Item>
          <Form.Item label="显示名称" name="displayName" rules={[
            { required: true, whitespace: true, message: '请输入显示名称' },
            { max: 120, message: '显示名称不能超过 120 个字符' },
          ]}>
            <Input autoComplete="off" placeholder="例如：张三" />
          </Form.Item>
          <Form.Item label="邮箱" name="email" extra="留空表示清除邮箱。" rules={[
            { type: 'email', message: '请输入正确的邮箱地址' },
          ]}>
            <Input autoComplete="off" placeholder="可选" />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title={`重置密码${passwordUser ? ` · ${passwordUser.displayName}` : ''}`}
        open={Boolean(passwordUser)}
        okText="重置密码"
        cancelText="取消"
        confirmLoading={resetPasswordMutation.isPending}
        onCancel={() => { setPasswordUser(null); passwordForm.resetFields(); resetPasswordMutation.reset() }}
        onOk={() => passwordForm.submit()}
        destroyOnHidden
      >
        <Alert
          className="form-alert"
          type="warning"
          showIcon
          message="重置后，该用户在所有设备上的登录都会立即失效。"
        />
        <Form<{ password: string; confirmPassword: string }>
          form={passwordForm}
          layout="vertical"
          requiredMark={false}
          onFinish={(values) => passwordUser && resetPasswordMutation.mutate({ userId: passwordUser.id, password: values.password })}
        >
          <Form.Item label="新密码" name="password" rules={[
            { required: true, message: '请输入新密码' },
            { min: 3, message: '密码不能少于 3 位' },
          ]}>
            <Input.Password autoComplete="new-password" placeholder="至少 3 位" />
          </Form.Item>
          <Form.Item
            label="确认新密码"
            name="confirmPassword"
            dependencies={['password']}
            rules={[
              { required: true, message: '请再次输入新密码' },
              ({ getFieldValue }) => ({
                validator(_, value) {
                  return !value || getFieldValue('password') === value
                    ? Promise.resolve()
                    : Promise.reject(new Error('两次输入的新密码不一致'))
                },
              }),
            ]}
          >
            <Input.Password autoComplete="new-password" placeholder="再次输入" />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title={`分配角色${roleUser ? ` · ${roleUser.displayName}` : ''}`}
        open={Boolean(roleUser)}
        okText="保存角色"
        cancelText="取消"
        confirmLoading={assignRolesMutation.isPending}
        onCancel={() => { setRoleUser(null); assignRolesMutation.reset() }}
        onOk={() => roleUser && assignRolesMutation.mutate({ userId: roleUser.id, roleIds: selectedRoleIDs })}
        destroyOnHidden
      >
        <Paragraph type="secondary">角色变更会立即影响该用户后续请求的权限。</Paragraph>
        {assignRolesMutation.isError ? (
          <Alert className="form-alert" type="error" showIcon message={assignRolesMutation.error instanceof Error ? assignRolesMutation.error.message : '角色分配失败'} />
        ) : null}
        <Checkbox.Group
          className="user-role-options"
          value={selectedRoleIDs}
          onChange={(values) => setSelectedRoleIDs(values as string[])}
        >
          {(rolesQuery.data ?? []).map((role) => (
            <Checkbox key={role.id} value={role.id}>
              <span>{role.name}</span>
              <small>{role.code}</small>
            </Checkbox>
          ))}
        </Checkbox.Group>
        {!rolesQuery.isLoading && !rolesQuery.data?.length ? <Text type="secondary">当前企业还没有可分配角色。</Text> : null}
      </Modal>

      <Modal
        title={`调整部门${departmentUser ? ` · ${departmentUser.displayName}` : ''}`}
        open={Boolean(departmentUser)}
        okText="保存部门"
        cancelText="取消"
        confirmLoading={assignDepartmentMutation.isPending}
        onCancel={() => { setDepartmentUser(null); assignDepartmentMutation.reset() }}
        onOk={() => departmentUser && assignDepartmentMutation.mutate({ userId: departmentUser.id, departmentId: selectedDepartmentID })}
        destroyOnHidden
      >
        <Paragraph type="secondary">不选择部门时，用户将保留为未分配状态。</Paragraph>
        {assignDepartmentMutation.isError ? (
          <Alert className="form-alert" type="error" showIcon message={assignDepartmentMutation.error instanceof Error ? assignDepartmentMutation.error.message : '部门调整失败'} />
        ) : null}
        <DepartmentSelect
          className="user-department-select"
          departments={departmentsQuery.data ?? []}
          placeholder="选择部门"
          value={selectedDepartmentID}
          onChange={setSelectedDepartmentID}
        />
      </Modal>
    </main>
  )
}

function formatDateTime(value: string): string {
  return new Intl.DateTimeFormat('zh-CN', {
    year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false,
  }).format(new Date(value))
}
