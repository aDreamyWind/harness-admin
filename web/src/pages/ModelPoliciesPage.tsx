import { DeleteOutlined, EditOutlined, PlusOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Alert, Button, Card, Form, InputNumber, Modal, Popconfirm, Select, Space, Table, Tabs, Tag, Typography, message, type TableColumnsType } from 'antd'
import { useState } from 'react'

import { listDepartments } from '../api/departments'
import { listModels } from '../api/models'
import { deleteModelPolicy, listModelPolicies, saveDepartmentModelPolicies, saveModelPolicy, type ModelPolicy, type ModelScopeMode, type MonthlyTokenMode, type PolicySubjectType } from '../api/modelPolicy'
import { listUsers } from '../api/users'
import { useAuth } from '../auth/AuthContext'
import { DepartmentSelect } from '../components/DepartmentSelect'
import { ModelPolicyDepartmentTree } from '../components/ModelPolicyDepartmentTree'

const { Text } = Typography

type PolicyForm = {
  subjectId?: string
  scopeMode: ModelScopeMode
  allowedModelCodes: string[]
  requestsPerMinute: number
  monthlyTokenLimit: number
  monthlyTokenMode: MonthlyTokenMode
}

const scopeOptions = [
  { value: 'inherit', label: '继承默认范围' },
  { value: 'all', label: '全部已启用模型' },
  { value: 'selected', label: '指定模型' },
  { value: 'none', label: '禁止调用模型' },
]

export function ModelPoliciesPage() {
  const { hasPermission } = useAuth()
  const canWrite = hasPermission('model.policy.write')
  const queryClient = useQueryClient()
  const [messageApi, messageContext] = message.useMessage()
  const [subjectType, setSubjectType] = useState<PolicySubjectType>('department')
  const [editing, setEditing] = useState<ModelPolicy | null>(null)
  const [modalOpen, setModalOpen] = useState(false)
  const [userSearch, setUserSearch] = useState('')
  const [selectedDepartmentIDs, setSelectedDepartmentIDs] = useState<string[]>([])
  const [form] = Form.useForm<PolicyForm>()
  const scopeMode = Form.useWatch('scopeMode', form)

  const policiesQuery = useQuery({ queryKey: ['model-policies'], queryFn: listModelPolicies })
  const departmentsQuery = useQuery({ queryKey: ['departments'], queryFn: listDepartments })
  const modelsQuery = useQuery({ queryKey: ['models'], queryFn: listModels })
  const usersQuery = useQuery({ queryKey: ['policy-users', userSearch], queryFn: () => listUsers({ page: 1, pageSize: 50, search: userSearch }) })

  const save = useMutation({
    mutationFn: async (values: PolicyForm) => {
      const input = {
        scopeMode: values.scopeMode,
        allowedModelCodes: values.scopeMode === 'selected' ? values.allowedModelCodes ?? [] : [],
        requestsPerMinute: values.requestsPerMinute,
        monthlyTokenLimit: values.monthlyTokenLimit,
        monthlyTokenMode: subjectType === 'department' ? values.monthlyTokenMode : 'shared' as const,
      }
      if (subjectType === 'department' && !editing) {
        await saveDepartmentModelPolicies(selectedDepartmentIDs, input)
        return
      }
      if (!values.subjectId) throw new Error('请选择策略对象')
      await saveModelPolicy(subjectType, values.subjectId, input)
    },
    onSuccess: async () => {
      messageApi.success('模型策略已保存')
      setModalOpen(false)
      setEditing(null)
      await queryClient.invalidateQueries({ queryKey: ['model-policies'] })
    },
    onError: (error) => messageApi.error(error instanceof Error ? error.message : '保存失败'),
  })
  const remove = useMutation({
    mutationFn: (policy: ModelPolicy) => deleteModelPolicy(policy.subjectType, policy.subjectId),
    onSuccess: async () => {
      messageApi.success('策略已删除')
      await queryClient.invalidateQueries({ queryKey: ['model-policies'] })
    },
    onError: (error) => messageApi.error(error instanceof Error ? error.message : '删除失败'),
  })

  function openCreate() {
    setEditing(null)
    setSelectedDepartmentIDs([])
    form.setFieldsValue({ subjectId: undefined, scopeMode: subjectType === 'department' ? 'all' : 'inherit', allowedModelCodes: [], requestsPerMinute: 0, monthlyTokenLimit: 0, monthlyTokenMode: 'shared' })
    setModalOpen(true)
  }

  function openEdit(policy: ModelPolicy) {
    setSubjectType(policy.subjectType)
    setEditing(policy)
    form.setFieldsValue({
      subjectId: policy.subjectId, scopeMode: policy.subjectType === 'department' && policy.scopeMode === 'inherit' ? 'all' : policy.scopeMode, allowedModelCodes: policy.allowedModelCodes ?? [],
      requestsPerMinute: policy.requestsPerMinute, monthlyTokenLimit: policy.monthlyTokenLimit,
      monthlyTokenMode: policy.monthlyTokenMode,
    })
    setModalOpen(true)
  }

  const policies = policiesQuery.data ?? []
  const columns: TableColumnsType<ModelPolicy> = [
    { title: subjectType === 'department' ? '部门' : '用户', dataIndex: 'subjectName', key: 'subjectName', render: (name: string, item) => name || item.subjectId },
    { title: '可用模型', key: 'scope', render: (_, item) => item.scopeMode === 'selected'
      ? <Space size={4} wrap>{item.allowedModelCodes?.map((code) => <Tag key={code}>{code}</Tag>)}</Space>
      : <Text>{scopeLabel(item.scopeMode, item.subjectType)}</Text> },
    { title: '每人每分钟上限', dataIndex: 'requestsPerMinute', key: 'rpm', render: (value: number) => value > 0 ? `${value} 次` : '不限' },
    { title: '本月 Token', key: 'tokens', render: (_, item) => item.monthlyTokenLimit > 0
      ? item.subjectType === 'department' && item.monthlyTokenMode === 'per_user'
        ? <span>每人上限 {item.monthlyTokenLimit.toLocaleString()} · 部门合计 {item.monthlyTokenUsed.toLocaleString()}<Text type="secondary">（按人预警见模型网关）</Text></span>
        : <Space size={4}><span>{item.monthlyTokenUsed.toLocaleString()} / {item.monthlyTokenLimit.toLocaleString()}</span>{quotaTag(item.monthlyTokenUsed, item.monthlyTokenLimit)}</Space>
      : `${item.monthlyTokenUsed.toLocaleString()} / 不限` },
    ...(canWrite ? [{ title: '操作', key: 'actions', width: 180, render: (_: unknown, item: ModelPolicy) => <Space>
      <Button type="link" size="small" icon={<EditOutlined />} onClick={() => openEdit(item)}>编辑</Button>
      <Popconfirm title="删除此策略？" description="删除后该部门恢复默认规则，并清除这条策略的本月累计用量。" onConfirm={() => remove.mutate(item)}>
        <Button type="link" size="small" danger icon={<DeleteOutlined />}>删除</Button>
      </Popconfirm>
    </Space> }] : []),
  ]

  const userOptions = (usersQuery.data?.items ?? []).map((item) => ({ value: item.id, label: `${item.displayName}（${item.username}）` }))
  if (editing?.subjectType === 'user' && !userOptions.some((item) => item.value === editing.subjectId)) {
    userOptions.unshift({ value: editing.subjectId, label: editing.subjectName })
  }

  return <main>
    {messageContext}
    <section className="module-heading"><Text className="module-kicker">AI 资源管理 / 模型策略</Text></section>
    <Card className="content-card" variant="borderless">
      <Alert type="info" showIcon message="部门策略只对选中的部门生效。勾选上级时会同时勾选现有下级，也可以单独取消；RPM 按人，Token 可选部门共享或按人。额度只统计上游已返回的 Token 用量。" />
      {policiesQuery.isError ? <Alert type="error" showIcon message={policiesQuery.error instanceof Error ? policiesQuery.error.message : '策略加载失败'} /> : null}
      <Tabs activeKey={subjectType} onChange={(key) => setSubjectType(key as PolicySubjectType)} items={([
        ['department', '部门策略'], ['user', '用户策略'],
      ] as const).map(([type, label]) => ({
        key: type, label: `${label} ${policies.filter((item) => item.subjectType === type).length}`,
        children: <>
          {canWrite ? <div style={{ marginBottom: 16 }}><Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>设置{label}</Button></div> : null}
          <Table<ModelPolicy> rowKey="subjectId" columns={columns} dataSource={policies.filter((item) => item.subjectType === type)} loading={policiesQuery.isLoading} pagination={{ pageSize: 10 }} locale={{ emptyText: `尚未设置${label}` }} scroll={{ x: 800 }} />
        </>,
      }))} />
    </Card>
    <Modal title={editing ? '编辑模型策略' : '设置模型策略'} open={modalOpen} width={680} onCancel={() => { setModalOpen(false); setEditing(null) }} onOk={() => {
      if (subjectType === 'department' && !editing && selectedDepartmentIDs.length === 0) {
        messageApi.error('请至少勾选一个部门')
        return
      }
      form.submit()
    }} okText="保存" confirmLoading={save.isPending} destroyOnHidden>
      <Form form={form} layout="vertical" onFinish={(values) => save.mutate(values)} requiredMark={false}>
        {subjectType === 'department' && !editing
          ? <Form.Item label="适用部门" extra="勾选上级会带上现有下级，可逐个取消。只更新最终勾选的部门；未勾选部门的已有策略不变。">
            <ModelPolicyDepartmentTree departments={departmentsQuery.data ?? []} selectedIds={selectedDepartmentIDs} onChange={setSelectedDepartmentIDs} />
          </Form.Item>
          : <Form.Item name="subjectId" label={subjectType === 'department' ? '部门' : '用户'} rules={[{ required: true, message: '请选择对象' }]}>
            {subjectType === 'department'
              ? <DepartmentSelect departments={departmentsQuery.data ?? []} disabled loading={departmentsQuery.isLoading} placeholder="请选择部门" />
              : <Select disabled={Boolean(editing)} showSearch onSearch={setUserSearch} options={userOptions} placeholder="请选择用户" />}
          </Form.Item>}
        <Form.Item name="scopeMode" label="可用模型范围" rules={[{ required: true }]}>
          <Select options={subjectType === 'department' ? scopeOptions.filter((item) => item.value !== 'inherit') : scopeOptions} />
        </Form.Item>
        {scopeMode === 'selected' ? <Form.Item name="allowedModelCodes" label="指定模型" rules={[{ required: true, type: 'array', min: 1, message: '请至少选择一个模型' }]}>
          <Select mode="multiple" options={(modelsQuery.data ?? []).map((item) => ({ value: item.code, label: `${item.name}（${item.code}）` }))} placeholder="选择模型" />
        </Form.Item> : null}
        <Space align="start" size={16}>
          <Form.Item name="requestsPerMinute" label="每人每分钟请求上限" extra="0 表示不限；不与部门其他成员共享" rules={[{ required: true }]}><InputNumber min={0} max={100000} precision={0} /></Form.Item>
          <Form.Item name="monthlyTokenLimit" label="每月 Token 上限" extra="按 UTC 月份统计，0 表示不限" rules={[{ required: true }]}><InputNumber min={0} precision={0} /></Form.Item>
        </Space>
        {subjectType === 'department' ? <Form.Item name="monthlyTokenMode" label="Token 额度计算方式" rules={[{ required: true }]}>
          <Select options={[{ value: 'shared', label: '本部门成员共享' }, { value: 'per_user', label: '本部门每位成员独立' }]} />
        </Form.Item> : null}
      </Form>
    </Modal>
  </main>
}

function scopeLabel(mode: ModelScopeMode, subjectType: PolicySubjectType) {
  if (mode === 'all') return '全部已启用模型'
  if (mode === 'none') return '禁止调用模型'
  return subjectType === 'department' ? '全部已启用模型' : '继承所属部门规则'
}

function quotaTag(used: number, limit: number) {
  if (limit <= 0) return null
  if (used >= limit) return <Tag color="red">已达上限</Tag>
  if (used >= limit - Math.floor(limit / 5)) return <Tag color="orange">接近上限</Tag>
  return null
}
