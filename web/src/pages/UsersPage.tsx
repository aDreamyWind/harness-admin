import { PlusOutlined, SearchOutlined, UserOutlined } from '@ant-design/icons'
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
  type CreateUserInput,
  type ManagedUser,
} from '../api/users'
import { useAuth } from '../auth/AuthContext'

const { Paragraph, Text, Title } = Typography

const statusLabels: Record<ManagedUser['status'], { text: string; color: string }> = {
  active: { text: '正常', color: 'success' },
  locked: { text: '已锁定', color: 'warning' },
  disabled: { text: '已停用', color: 'default' },
}

export function UsersPage() {
  const { user: currentUser } = useAuth()
  const queryClient = useQueryClient()
  const [form] = Form.useForm<CreateUserInput>()
  const [messageApi, messageContext] = message.useMessage()
  const [createOpen, setCreateOpen] = useState(false)
  const [searchInput, setSearchInput] = useState('')
  const [search, setSearch] = useState('')
  const [status, setStatus] = useState('')
  const [page, setPage] = useState(1)
  const [roleUser, setRoleUser] = useState<ManagedUser | null>(null)
  const [selectedRoleIDs, setSelectedRoleIDs] = useState<string[]>([])
  const [departmentUser, setDepartmentUser] = useState<ManagedUser | null>(null)
  const [selectedDepartmentID, setSelectedDepartmentID] = useState<string | undefined>()
  const pageSize = 20

  const usersQuery = useQuery({
    queryKey: ['users', { search, status, page, pageSize }],
    queryFn: () => listUsers({ search, status, page, pageSize }),
    placeholderData: keepPreviousData,
  })

  const rolesQuery = useQuery({ queryKey: ['roles'], queryFn: listRoles })
  const departmentsQuery = useQuery({ queryKey: ['departments'], queryFn: listDepartments })

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

  const columns: TableColumnsType<ManagedUser> = [
    {
      title: '用户',
      key: 'user',
      width: 250,
      render: (_, user) => (
        <div className="user-identity-cell">
          <span className="user-table-avatar"><UserOutlined /></span>
          <span>
            <strong>{user.displayName}</strong>
            <small>@{user.username}</small>
          </span>
        </div>
      ),
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
      width: 205,
      render: (value: ManagedUser['status']) => <Tag color={statusLabels[value].color}>{statusLabels[value].text}</Tag>,
    },
    {
      title: '最后登录',
      dataIndex: 'lastLoginAt',
      key: 'lastLoginAt',
      width: 170,
      render: (value?: string) => value ? formatDateTime(value) : <Text type="secondary">尚未登录</Text>,
    },
    {
      title: '操作',
      key: 'action',
      fixed: 'right',
      width: 100,
      render: (_, user) => {
        const nextStatus = user.status === 'active' ? 'disabled' : 'active'
        const isSelf = user.id === currentUser?.id
        return (
          <Space size={0}>
            <Button
              type="link"
              size="small"
              disabled={isSelf}
              onClick={() => { setRoleUser(user); setSelectedRoleIDs(user.roleIds); assignRolesMutation.reset() }}
            >
              角色
            </Button>
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
            <Popconfirm
              title={nextStatus === 'disabled' ? '停用这个用户？' : '启用这个用户？'}
              description={nextStatus === 'disabled' ? '停用后，该用户的现有会话会立即失效。' : '启用后，该用户可以重新登录。'}
              okText={nextStatus === 'disabled' ? '停用' : '启用'}
              cancelText="取消"
              disabled={isSelf}
              onConfirm={() => statusMutation.mutate({ id: user.id, nextStatus })}
            >
              <Button type="link" size="small" disabled={isSelf || user.status === 'locked'}>
                {nextStatus === 'disabled' ? '停用' : '启用'}
              </Button>
            </Popconfirm>
          </Space>
        )
      },
    },
  ]

  return (
    <main>
      {messageContext}
      <section className="module-heading">
        <div>
          <Text className="module-kicker">身份与组织 / 用户</Text>
          <Title level={1}>用户目录</Title>
          <Paragraph>管理当前企业内可以登录控制面的账号。</Paragraph>
        </div>
        <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreateOpen(true)}>
          新建用户
        </Button>
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
            scroll={{ x: 1240 }}
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
          <Form.Item label="所属部门" name="departmentId">
            <Select
              allowClear
              showSearch
              optionFilterProp="label"
              placeholder="可选"
              options={(departmentsQuery.data ?? [])
                .filter((department) => department.status === 'active')
                .map((department) => ({ value: department.id, label: `${department.name} · ${department.code}` }))}
            />
          </Form.Item>
          <Form.Item label="初始密码" name="password" extra="至少 3 位。创建后请通过安全方式交给用户。" rules={[
            { required: true, message: '请输入初始密码' },
            { min: 3, message: '密码不能少于 3 位' },
          ]}>
            <Input.Password autoComplete="new-password" placeholder="至少 3 位" />
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
        <Select
          className="user-department-select"
          allowClear
          showSearch
          optionFilterProp="label"
          placeholder="选择部门"
          value={selectedDepartmentID}
          onChange={setSelectedDepartmentID}
          options={(departmentsQuery.data ?? [])
            .filter((department) => department.status === 'active')
            .map((department) => ({ value: department.id, label: `${department.name} · ${department.code}` }))}
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
