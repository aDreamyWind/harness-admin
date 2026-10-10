import { EditOutlined, PlusOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Alert,
  Button,
  Card,
  Form,
  Input,
  InputNumber,
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
  changeDepartmentStatus,
  createDepartment,
  listDepartments,
  updateDepartment,
  type Department,
  type DepartmentInput,
} from '../api/departments'

const { Paragraph, Text, Title } = Typography

interface DepartmentNode extends Department {
  children?: DepartmentNode[]
}

export function DepartmentsPage() {
  const queryClient = useQueryClient()
  const [form] = Form.useForm<DepartmentInput>()
  const [messageApi, messageContext] = message.useMessage()
  const [modalOpen, setModalOpen] = useState(false)
  const [editingDepartment, setEditingDepartment] = useState<Department | null>(null)

  const departmentsQuery = useQuery({ queryKey: ['departments'], queryFn: listDepartments })
  const treeData = useMemo(() => buildDepartmentTree(departmentsQuery.data ?? []), [departmentsQuery.data])
  const parentOptions = useMemo(
    () => buildParentOptions(departmentsQuery.data ?? [], editingDepartment?.id),
    [departmentsQuery.data, editingDepartment?.id],
  )

  const saveMutation = useMutation({
    mutationFn: (values: DepartmentInput) => editingDepartment
      ? updateDepartment(editingDepartment.id, values)
      : createDepartment(values),
    onSuccess: async () => {
      messageApi.success(editingDepartment ? '部门已更新' : '部门已创建')
      closeModal()
      await queryClient.invalidateQueries({ queryKey: ['departments'] })
    },
  })

  const statusMutation = useMutation({
    mutationFn: ({ id, status }: { id: string; status: Department['status'] }) => changeDepartmentStatus(id, status),
    onSuccess: async (_, variables) => {
      messageApi.success(variables.status === 'active' ? '部门已启用' : '部门已停用')
      await queryClient.invalidateQueries({ queryKey: ['departments'] })
    },
    onError: (error) => messageApi.error(error instanceof Error ? error.message : '部门状态修改失败'),
  })

  function openCreate(parentId?: string) {
    setEditingDepartment(null)
    form.setFieldsValue({ parentId, code: '', name: '', sortOrder: 0 })
    saveMutation.reset()
    setModalOpen(true)
  }

  function openEdit(department: Department) {
    setEditingDepartment(department)
    form.setFieldsValue({
      parentId: department.parentId,
      code: department.code,
      name: department.name,
      sortOrder: department.sortOrder,
    })
    saveMutation.reset()
    setModalOpen(true)
  }

  function closeModal() {
    setModalOpen(false)
    setEditingDepartment(null)
    form.resetFields()
    saveMutation.reset()
  }

  const columns: TableColumnsType<DepartmentNode> = [
    {
      title: '部门',
      dataIndex: 'name',
      key: 'name',
      width: 280,
      render: (_, department) => (
        <div className="department-identity-cell">
          <strong>{department.name}</strong>
          <small>{department.code}</small>
        </div>
      ),
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 100,
      render: (status: Department['status']) => status === 'active'
        ? <Tag color="success">启用</Tag>
        : <Tag>停用</Tag>,
    },
    { title: '直属用户', dataIndex: 'userCount', key: 'userCount', width: 100, align: 'right' },
    { title: '排序', dataIndex: 'sortOrder', key: 'sortOrder', width: 90, align: 'right' },
    {
      title: '操作',
      key: 'action',
      fixed: 'right',
      width: 205,
      render: (_, department) => {
        const nextStatus = department.status === 'active' ? 'disabled' : 'active'
        return (
          <Space size={0}>
            <Button type="link" size="small" icon={<EditOutlined />} onClick={() => openEdit(department)}>编辑</Button>
            <Button type="link" size="small" disabled={department.status !== 'active'} onClick={() => openCreate(department.id)}>新增下级</Button>
            <Popconfirm
              title={nextStatus === 'disabled' ? '停用这个部门？' : '启用这个部门？'}
              description={nextStatus === 'disabled' ? '停用前需先处理启用中的下级部门和用户。' : '启用后可重新分配用户。'}
              okText={nextStatus === 'disabled' ? '停用' : '启用'}
              cancelText="取消"
              onConfirm={() => statusMutation.mutate({ id: department.id, status: nextStatus })}
            >
              <Button type="link" size="small">{nextStatus === 'disabled' ? '停用' : '启用'}</Button>
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
          <Text className="module-kicker">身份与组织 / 部门</Text>
          <Title level={1}>部门结构</Title>
          <Paragraph>维护当前企业的部门层级与用户归属。</Paragraph>
        </div>
        <Button type="primary" icon={<PlusOutlined />} onClick={() => openCreate()}>新建部门</Button>
      </section>

      <Card className="directory-card" variant="borderless">
        <div className="directory-toolbar">
          <Text>组织层级</Text>
          <span className="directory-total">共 {departmentsQuery.data?.length ?? 0} 个部门</span>
        </div>
        {departmentsQuery.isError ? (
          <Alert
            type="error"
            showIcon
            message="部门数据加载失败"
            description={departmentsQuery.error instanceof Error ? departmentsQuery.error.message : '请稍后重试'}
            action={<Button size="small" onClick={() => departmentsQuery.refetch()}>重新加载</Button>}
          />
        ) : (
          <Table<DepartmentNode>
            key={(departmentsQuery.data ?? []).map((department) => department.id).join(',')}
            rowKey="id"
            columns={columns}
            dataSource={treeData}
            loading={departmentsQuery.isLoading}
            pagination={false}
            expandable={{ defaultExpandAllRows: true, indentSize: 24 }}
            scroll={{ x: 820 }}
            locale={{ emptyText: '当前企业还没有部门，可以先建立一级部门' }}
          />
        )}
      </Card>

      <Modal
        title={editingDepartment ? '编辑部门' : '新建部门'}
        open={modalOpen}
        okText={editingDepartment ? '保存修改' : '创建部门'}
        cancelText="取消"
        confirmLoading={saveMutation.isPending}
        onCancel={closeModal}
        onOk={() => form.submit()}
        forceRender
      >
        {saveMutation.isError ? (
          <Alert className="form-alert" type="error" showIcon message={saveMutation.error instanceof ApiError ? saveMutation.error.message : '部门保存失败'} />
        ) : null}
        <Form<DepartmentInput> form={form} layout="vertical" requiredMark={false} onFinish={(values) => saveMutation.mutate(values)}>
          <Form.Item label="部门名称" name="name" rules={[{ required: true, whitespace: true, message: '请输入部门名称' }, { max: 160 }]}>
            <Input placeholder="例如：研发中心" />
          </Form.Item>
          <Form.Item
            label="部门代码"
            name="code"
            extra={editingDepartment ? '部门创建后代码不可修改。' : '以小写字母开头，可使用数字、点、下划线和连字符。'}
            rules={editingDepartment ? [] : [
              { required: true, message: '请输入部门代码' },
              { pattern: /^[a-z][a-z0-9._-]{1,63}$/, message: '请输入至少 2 位的合法部门代码' },
            ]}
          >
            <Input disabled={Boolean(editingDepartment)} placeholder="例如：engineering" />
          </Form.Item>
          <Form.Item label="上级部门" name="parentId">
            <Select allowClear placeholder="不选择则为一级部门" options={parentOptions} showSearch optionFilterProp="label" />
          </Form.Item>
          <Form.Item label="排序值" name="sortOrder" extra="同级部门按数值从小到大排列。" rules={[{ required: true, message: '请输入排序值' }]}>
            <InputNumber min={0} max={999999} precision={0} className="sort-order-input" />
          </Form.Item>
        </Form>
      </Modal>
    </main>
  )
}

function buildDepartmentTree(departments: Department[]): DepartmentNode[] {
  const nodes = new Map(departments.map((department) => [department.id, { ...department } as DepartmentNode]))
  const roots: DepartmentNode[] = []
  departments.forEach((department) => {
    const node = nodes.get(department.id)!
    const parent = department.parentId ? nodes.get(department.parentId) : undefined
    if (parent) {
      parent.children = [...(parent.children ?? []), node]
    } else {
      roots.push(node)
    }
  })
  return roots
}

function buildParentOptions(departments: Department[], editingID?: string) {
  const children = new Map<string, string[]>()
  departments.forEach((department) => {
    if (department.parentId) children.set(department.parentId, [...(children.get(department.parentId) ?? []), department.id])
  })
  const excluded = new Set<string>()
  const visit = (id: string) => {
    excluded.add(id)
    ;(children.get(id) ?? []).forEach(visit)
  }
  if (editingID) visit(editingID)
  return departments
    .filter((department) => department.status === 'active' && !excluded.has(department.id))
    .map((department) => ({ value: department.id, label: `${department.name} · ${department.code}` }))
}
