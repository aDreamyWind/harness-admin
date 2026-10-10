import { LockOutlined, SaveOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Alert, Button, Card, Col, Form, Input, Row, Skeleton, Tag, Typography, message } from 'antd'
import { useEffect } from 'react'

import {
  getOrganization,
  updateOrganization,
  type Organization,
  type UpdateOrganizationInput,
} from '../api/organization'
import { useAuth } from '../auth/AuthContext'
import { StatusIndicator } from '../components/StatusIndicator'

const { Paragraph, Text, Title } = Typography

export function OrganizationSettingsPage() {
  const { hasPermission, refreshUser } = useAuth()
  const canWrite = hasPermission('iam.organization.write')
  const [form] = Form.useForm<UpdateOrganizationInput>()
  const [messageApi, messageContext] = message.useMessage()
  const queryClient = useQueryClient()
  const organizationQuery = useQuery({ queryKey: ['organization'], queryFn: getOrganization })

  useEffect(() => {
    if (organizationQuery.data) {
      form.setFieldsValue({ name: organizationQuery.data.name })
    }
  }, [form, organizationQuery.data])

  const updateMutation = useMutation({
    mutationFn: updateOrganization,
    onSuccess: async (organization) => {
      queryClient.setQueryData<Organization>(['organization'], organization)
      form.setFieldsValue({ name: organization.name })
      try {
        await refreshUser()
        messageApi.success('企业名称已更新')
      } catch {
        messageApi.warning('企业名称已保存，顶部名称将在刷新页面后更新')
      }
    },
    onError: (error) => messageApi.error(error instanceof Error ? error.message : '企业名称修改失败'),
  })

  const organization = organizationQuery.data

  return (
    <main>
      {messageContext}
      <section className="module-heading organization-heading">
        <div>
          <Text className="module-kicker">系统设置 / 企业资料</Text>
          <Title level={1}>企业设置</Title>
          <Paragraph>维护当前企业在控制面中展示的名称和企业编码。</Paragraph>
        </div>
        {!canWrite ? <Tag>只读</Tag> : null}
      </section>

      {organizationQuery.isError ? (
        <Alert
          type="error"
          showIcon
          message="企业资料加载失败"
          description={organizationQuery.error instanceof Error ? organizationQuery.error.message : '请稍后重试'}
          action={<Button size="small" onClick={() => organizationQuery.refetch()}>重新加载</Button>}
        />
      ) : organizationQuery.isLoading || !organization ? (
        <Card className="content-card" variant="borderless"><Skeleton active paragraph={{ rows: 5 }} /></Card>
      ) : (
        <Row gutter={[18, 18]}>
          <Col xs={24} xl={9}>
            <Card className="organization-identity-card" variant="borderless">
              <div className="organization-card-label">
                <span>当前企业</span>
                <StatusIndicator
                  label={organization.status === 'active' ? '正常' : '已停用'}
                  tone={organization.status === 'active' ? 'success' : 'muted'}
                />
              </div>
              <div className="organization-code">{organization.name}</div>
              <div className="organization-lock-note">
                <LockOutlined />
                <span>企业编码用于登录和数据隔离，创建后不可修改。</span>
              </div>
              <dl className="organization-metadata">
                <div><dt>企业编码</dt><dd>{organization.code}</dd></div>
                <div><dt>企业 ID</dt><dd>{organization.id}</dd></div>
                <div><dt>创建时间</dt><dd>{formatDateTime(organization.createdAt)}</dd></div>
                <div><dt>最后更新</dt><dd>{formatDateTime(organization.updatedAt)}</dd></div>
              </dl>
            </Card>
          </Col>

          <Col xs={24} xl={15}>
            <Card className="content-card organization-name-card" variant="borderless">
              <div className="organization-form-heading">
                <div>
                  <Title level={3}>企业名称</Title>
                  <Paragraph>该名称会出现在账号菜单和后续企业级配置中。</Paragraph>
                </div>
              </div>
              <Form<UpdateOrganizationInput>
                form={form}
                layout="vertical"
                requiredMark={false}
                disabled={!canWrite}
                onFinish={(values) => updateMutation.mutate(values)}
              >
                <Form.Item label="企业名称" name="name" rules={[
                  { required: true, whitespace: true, message: '请输入企业名称' },
                  { max: 160, message: '企业名称不能超过 160 个字符' },
                ]}>
                  <Input size="large" autoComplete="organization" placeholder="例如：星河科技有限公司" />
                </Form.Item>
                {canWrite ? (
                  <div className="organization-form-actions">
                    <Text type="secondary">只会修改展示名称，不影响登录和已有数据。</Text>
                    <Button type="primary" icon={<SaveOutlined />} loading={updateMutation.isPending} onClick={() => form.submit()}>
                      保存更改
                    </Button>
                  </div>
                ) : null}
              </Form>
            </Card>
          </Col>
        </Row>
      )}
    </main>
  )
}

function formatDateTime(value: string): string {
  return new Intl.DateTimeFormat('zh-CN', {
    year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false,
  }).format(new Date(value))
}
