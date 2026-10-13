import { EditOutlined, PlusOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Alert,
  Button,
  Card,
  Checkbox,
  Form,
  Input,
  Modal,
  Table,
  Tag,
  Typography,
  message,
  type TableColumnsType,
} from 'antd'
import { useMemo, useState } from 'react'

import { ApiError } from '../api/client'
import {
  createRole,
  listPermissions,
  listRoles,
  updateRole,
  type Permission,
  type Role,
  type RoleInput,
} from '../api/roles'
import { useAuth } from '../auth/AuthContext'

const { Text } = Typography

export function RolesPage() {
  const { hasPermission } = useAuth()
  const canWrite = hasPermission('iam.role.write')
  const queryClient = useQueryClient()
  const [form] = Form.useForm<RoleInput>()
  const [messageApi, messageContext] = message.useMessage()
  const [modalOpen, setModalOpen] = useState(false)
  const [editingRole, setEditingRole] = useState<Role | null>(null)

  const rolesQuery = useQuery({ queryKey: ['roles'], queryFn: listRoles })
  const permissionsQuery = useQuery({ queryKey: ['permissions'], queryFn: listPermissions })
  const permissionMap = useMemo(
    () => new Map((permissionsQuery.data ?? []).map((permission) => [permission.code, permission])),
    [permissionsQuery.data],
  )
  const permissionGroups = useMemo(() => groupPermissions(permissionsQuery.data ?? []), [permissionsQuery.data])

  const saveMutation = useMutation({
    mutationFn: (values: RoleInput) => editingRole ? updateRole(editingRole.id, values) : createRole(values),
    onSuccess: async () => {
      messageApi.success(editingRole ? '角色已更新' : '角色已创建')
      closeModal()
      await queryClient.invalidateQueries({ queryKey: ['roles'] })
    },
  })

  function openCreate() {
    setEditingRole(null)
    form.setFieldsValue({ code: '', name: '', description: '', permissionCodes: [] })
    saveMutation.reset()
    setModalOpen(true)
  }

  function openEdit(role: Role) {
    setEditingRole(role)
    form.setFieldsValue({
      code: role.code,
      name: role.name,
      description: role.description,
      permissionCodes: role.permissionCodes,
    })
    saveMutation.reset()
    setModalOpen(true)
  }

  function closeModal() {
    setModalOpen(false)
    setEditingRole(null)
    form.resetFields()
    saveMutation.reset()
  }

  const columns: TableColumnsType<Role> = [
    {
      title: '角色',
      key: 'role',
      width: 250,
      render: (_, role) => (
        <div className="role-identity-cell">
          <span>
            <strong>{role.name}</strong>
          </span>
          {role.builtIn ? <Tag>内置</Tag> : null}
        </div>
      ),
    },
    {
      title: '角色编码',
      dataIndex: 'code',
      key: 'code',
      width: 180,
    },
    {
      title: '说明',
      dataIndex: 'description',
      key: 'description',
      width: 260,
      render: (description: string) => description || <Text type="secondary">未填写</Text>,
    },
    {
      title: '权限',
      dataIndex: 'permissionCodes',
      key: 'permissions',
      render: (codes: string[]) => codes.length ? (
        <div className="permission-tags">
          {codes.map((code) => <Tag key={code}>{permissionMap.get(code)?.name ?? code}</Tag>)}
        </div>
      ) : <Text type="secondary">未配置</Text>,
    },
    {
      title: '用户数',
      dataIndex: 'userCount',
      key: 'userCount',
      width: 90,
      align: 'right',
    },
  ]

  if (canWrite) columns.push({
      title: '操作',
      key: 'action',
      width: 90,
      fixed: 'right',
      render: (_, role) => (
        <Button type="link" size="small" icon={<EditOutlined />} disabled={role.builtIn} onClick={() => openEdit(role)}>
          编辑
        </Button>
      ),
    })

  return (
    <main>
      {messageContext}
      <section className="module-heading">
        <div>
          <Text className="module-kicker">身份与组织 / 角色与权限</Text>
        </div>
        {canWrite
          ? <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>新建角色</Button>
          : <Tag>只读</Tag>}
      </section>

      <Card className="directory-card" variant="borderless">
        <div className="directory-toolbar">
          <Text>当前企业角色</Text>
          <span className="directory-total">共 {rolesQuery.data?.length ?? 0} 个角色</span>
        </div>
        {rolesQuery.isError || permissionsQuery.isError ? (
          <Alert
            type="error"
            showIcon
            message="角色权限数据加载失败"
            description={(rolesQuery.error ?? permissionsQuery.error) instanceof Error ? (rolesQuery.error ?? permissionsQuery.error)?.message : '请稍后重试'}
            action={<Button size="small" onClick={() => { rolesQuery.refetch(); permissionsQuery.refetch() }}>重新加载</Button>}
          />
        ) : (
          <Table<Role>
            rowKey="id"
            columns={columns}
            dataSource={rolesQuery.data ?? []}
            loading={rolesQuery.isLoading || permissionsQuery.isLoading}
            pagination={false}
            scroll={{ x: 980 }}
            locale={{ emptyText: '当前企业还没有角色' }}
          />
        )}
      </Card>

      <Modal
        title={editingRole ? '编辑角色' : '新建角色'}
        open={modalOpen}
        width={680}
        okText={editingRole ? '保存修改' : '创建角色'}
        cancelText="取消"
        confirmLoading={saveMutation.isPending}
        onCancel={closeModal}
        onOk={() => form.submit()}
        forceRender
      >
        {saveMutation.isError ? (
          <Alert className="form-alert" type="error" showIcon message={saveMutation.error instanceof ApiError ? saveMutation.error.message : '角色保存失败'} />
        ) : null}
        <Form<RoleInput>
          form={form}
          layout="vertical"
          requiredMark={false}
          onFinish={(values) => saveMutation.mutate(values)}
        >
          <div className="role-form-grid">
            <Form.Item label="角色名称" name="name" rules={[{ required: true, whitespace: true, message: '请输入角色名称' }, { max: 120 }]}>
              <Input placeholder="例如：只读审计员" />
            </Form.Item>
            <Form.Item
              label="角色代码"
              name="code"
              extra={editingRole ? '角色创建后代码不可修改。' : '以小写字母开头，可使用数字、点、下划线和连字符。'}
              rules={editingRole ? [] : [
                { required: true, message: '请输入角色代码' },
                { pattern: /^[a-z][a-z0-9._-]{1,79}$/, message: '请输入至少 2 位的合法角色代码' },
              ]}
            >
              <Input disabled={Boolean(editingRole)} placeholder="例如：audit_viewer" />
            </Form.Item>
          </div>
          <Form.Item label="角色说明" name="description" rules={[{ max: 500, message: '角色说明不能超过 500 字' }]}>
            <Input.TextArea rows={2} placeholder="说明该角色适合哪些用户" />
          </Form.Item>
          <Form.Item label="系统权限" name="permissionCodes">
            <Checkbox.Group className="permission-matrix">
              {permissionGroups.map(([resource, permissions]) => (
                <div className="permission-group" key={resource}>
                  <div className="permission-group-title">{resourceLabels[resource] ?? resource}</div>
                  <div className="permission-group-options">
                    {permissions.map((permission) => (
                      <Checkbox key={permission.code} value={permission.code}>
                        <span>{permission.name}</span>
                        <small>{permission.code}</small>
                      </Checkbox>
                    ))}
                  </div>
                </div>
              ))}
            </Checkbox.Group>
          </Form.Item>
        </Form>
      </Modal>
    </main>
  )
}

const resourceLabels: Record<string, string> = {
  user: '用户',
  department: '部门',
  role: '角色',
  audit_event: '审计事件',
  organization: '企业',
  model_provider: '模型供应商',
  model_catalog: '模型目录',
}

function groupPermissions(permissions: Permission[]): Array<[string, Permission[]]> {
  const groups = new Map<string, Permission[]>()
  permissions.forEach((permission) => {
    groups.set(permission.resource, [...(groups.get(permission.resource) ?? []), permission])
  })
  return Array.from(groups.entries())
}
