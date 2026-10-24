import { AppstoreOutlined, ClockCircleOutlined, DesktopOutlined, LockOutlined, SafetyCertificateOutlined, UserOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Alert,
  Button,
  Card,
  Form,
  Input,
  Popconfirm,
  Space,
  Tag,
  Typography,
  message,
} from 'antd'
import { useEffect } from 'react'

import {
  changePassword,
  listSessions,
  revokeOtherSessions,
  revokeSession,
  type ChangePasswordInput,
  type UserSession,
} from '../api/auth'
import {
  listAuthorizedApplications,
  revokeAuthorizedApplication,
  type AuthorizedApplication,
  type OIDCScope,
} from '../api/oidc'
import { useAuth } from '../auth/AuthContext'
import { StatusIndicator } from '../components/StatusIndicator'

const { Paragraph, Text, Title } = Typography

interface PasswordForm extends ChangePasswordInput {
  confirmPassword: string
}

interface ProfileForm {
  displayName: string
  email: string
}

export function AccountSecurityPage() {
  const [form] = Form.useForm<PasswordForm>()
  const [profileForm] = Form.useForm<ProfileForm>()
  const [messageApi, messageContext] = message.useMessage()
  const queryClient = useQueryClient()
  const { user, updateProfile } = useAuth()

  useEffect(() => {
    if (user) profileForm.setFieldsValue({ displayName: user.displayName, email: user.email ?? '' })
  }, [profileForm, user])

  const sessionsQuery = useQuery({ queryKey: ['account-sessions'], queryFn: listSessions })
  const authorizedApplicationsQuery = useQuery({
    queryKey: ['authorized-applications'],
    queryFn: listAuthorizedApplications,
  })

  const profileMutation = useMutation({
    mutationFn: updateProfile,
    onSuccess: () => messageApi.success('个人资料已更新'),
    onError: (error) => messageApi.error(error instanceof Error ? error.message : '个人资料修改失败'),
  })

  const passwordMutation = useMutation({
    mutationFn: (values: PasswordForm) => changePassword({
      currentPassword: values.currentPassword,
      newPassword: values.newPassword,
    }),
    onSuccess: async (result) => {
      form.resetFields()
      messageApi.success(result.revokedSessions > 0
        ? `密码已修改，另外 ${result.revokedSessions} 个会话已退出`
        : '密码已修改')
      await queryClient.invalidateQueries({ queryKey: ['account-sessions'] })
    },
  })

  const revokeMutation = useMutation({
    mutationFn: revokeSession,
    onSuccess: async () => {
      messageApi.success('该设备已退出')
      await queryClient.invalidateQueries({ queryKey: ['account-sessions'] })
    },
    onError: (error) => messageApi.error(error instanceof Error ? error.message : '会话吊销失败'),
  })

  const revokeOthersMutation = useMutation({
    mutationFn: revokeOtherSessions,
    onSuccess: async (result) => {
      messageApi.success(result.revokedSessions > 0 ? `已退出 ${result.revokedSessions} 个其他会话` : '没有其他活跃会话')
      await queryClient.invalidateQueries({ queryKey: ['account-sessions'] })
    },
    onError: (error) => messageApi.error(error instanceof Error ? error.message : '退出其他设备失败'),
  })

  const revokeApplicationMutation = useMutation({
    mutationFn: revokeAuthorizedApplication,
    onSuccess: async () => {
      messageApi.success('应用授权已撤销')
      await queryClient.invalidateQueries({ queryKey: ['authorized-applications'] })
    },
    onError: (error) => messageApi.error(error instanceof Error ? error.message : '应用授权撤销失败'),
  })

  const sessions = sessionsQuery.data ?? []
  const otherSessionCount = sessions.filter((session) => !session.current).length

  return (
    <main>
      {messageContext}
      <section className="module-heading security-heading">
        <div>
          <Text className="module-kicker">个人账号 / 账号与安全</Text>
        </div>
        <div className="security-state">
          <SafetyCertificateOutlined />
          <span>
            <strong>当前会话受保护</strong>
            <small>服务端会话 · CSRF 校验</small>
          </span>
        </div>
      </section>

      <div className="security-layout">
        <div className="security-settings-column">
          <Card className="security-card profile-card" variant="borderless">
            <div className="security-section-title">
              <UserOutlined />
              <div>
                <Title level={3}>个人资料</Title>
                <Paragraph>登录用户名不可修改，姓名和邮箱会用于页面展示与审计。</Paragraph>
              </div>
            </div>
            <Form<ProfileForm>
              form={profileForm}
              layout="vertical"
              requiredMark={false}
              onFinish={(values) => profileMutation.mutate(values)}
            >
              <Form.Item label="登录用户名">
                <Input value={user?.username ?? ''} disabled />
              </Form.Item>
              <Form.Item label="显示名称" name="displayName" rules={[
                { required: true, whitespace: true, message: '请输入显示名称' },
                { max: 120, message: '显示名称不能超过 120 个字符' },
              ]}>
                <Input autoComplete="name" />
              </Form.Item>
              <Form.Item label="邮箱" name="email" extra="留空表示不设置邮箱。" rules={[
                { type: 'email', message: '请输入正确的邮箱地址' },
              ]}>
                <Input autoComplete="email" />
              </Form.Item>
              <Button type="primary" htmlType="submit" loading={profileMutation.isPending}>保存资料</Button>
            </Form>
          </Card>

          <Card className="security-card password-card" variant="borderless">
            <div className="security-section-title">
              <LockOutlined />
              <div>
                <Title level={3}>修改密码</Title>
                <Paragraph>修改后保留当前设备，其他设备会自动退出。</Paragraph>
              </div>
            </div>
            {passwordMutation.isError ? (
              <Alert className="form-alert" type="error" showIcon message={passwordMutation.error instanceof Error ? passwordMutation.error.message : '密码修改失败'} />
            ) : null}
            <Form<PasswordForm>
              form={form}
              layout="vertical"
              requiredMark={false}
              onFinish={(values) => passwordMutation.mutate(values)}
            >
              <Form.Item label="当前密码" name="currentPassword" rules={[{ required: true, message: '请输入当前密码' }]}>
                <Input.Password autoComplete="current-password" />
              </Form.Item>
              <Form.Item label="新密码" name="newPassword" extra="至少 3 位。" rules={[
                { required: true, message: '请输入新密码' },
                { min: 3, message: '密码不能少于 3 位' },
              ]}>
                <Input.Password autoComplete="new-password" />
              </Form.Item>
              <Form.Item
                label="确认新密码"
                name="confirmPassword"
                dependencies={['newPassword']}
                rules={[
                  { required: true, message: '请再次输入新密码' },
                  ({ getFieldValue }) => ({
                    validator(_, value) {
                      return !value || getFieldValue('newPassword') === value
                        ? Promise.resolve()
                        : Promise.reject(new Error('两次输入的新密码不一致'))
                    },
                  }),
                ]}
              >
                <Input.Password autoComplete="new-password" />
              </Form.Item>
              <Button type="primary" htmlType="submit" loading={passwordMutation.isPending}>修改密码</Button>
            </Form>
          </Card>
        </div>

        <div className="security-activity-column">
          <Card className="security-card sessions-card" variant="borderless">
          <div className="security-section-title sessions-title">
            <DesktopOutlined />
            <div>
              <Title level={3}>登录设备</Title>
              <Paragraph>仅显示尚未过期且未被吊销的会话。</Paragraph>
            </div>
            <Popconfirm
              title="退出其他设备？"
              description="当前设备会保留，其他会话将立即失效。"
              okText="退出其他设备"
              cancelText="取消"
              disabled={otherSessionCount === 0}
              onConfirm={() => revokeOthersMutation.mutate()}
            >
              <Button disabled={otherSessionCount === 0} loading={revokeOthersMutation.isPending}>退出其他设备</Button>
            </Popconfirm>
          </div>

          {sessionsQuery.isError ? (
            <Alert
              type="error"
              showIcon
              message="登录设备加载失败"
              description={sessionsQuery.error instanceof Error ? sessionsQuery.error.message : '请稍后重试'}
              action={<Button size="small" onClick={() => sessionsQuery.refetch()}>重新加载</Button>}
            />
          ) : sessionsQuery.isLoading ? (
            <div className="security-loading">正在读取登录设备</div>
          ) : sessions.length === 0 ? (
            <div className="security-empty">当前没有可管理的登录会话。</div>
          ) : (
            <div className="session-list">
              {sessions.map((session) => (
                <SessionRow
                  key={session.id}
                  session={session}
                  revoking={revokeMutation.isPending && revokeMutation.variables === session.id}
                  onRevoke={() => revokeMutation.mutate(session.id)}
                />
              ))}
            </div>
          )}
          </Card>

          <Card className="security-card authorized-applications-card" variant="borderless">
            <div className="security-section-title">
              <AppstoreOutlined />
              <div>
                <Title level={3}>已授权应用</Title>
                <Paragraph>这些应用可以使用你同意过的账号信息和业务权限。</Paragraph>
              </div>
            </div>
            {authorizedApplicationsQuery.isError ? (
              <Alert
                type="error"
                showIcon
                message="已授权应用加载失败"
                action={<Button size="small" onClick={() => authorizedApplicationsQuery.refetch()}>重新加载</Button>}
              />
            ) : authorizedApplicationsQuery.isLoading ? (
              <div className="security-loading">正在读取已授权应用</div>
            ) : authorizedApplicationsQuery.data?.length ? (
              <div className="authorized-application-list">
                {authorizedApplicationsQuery.data.map((application) => (
                  <AuthorizedApplicationRow
                    key={application.id}
                    application={application}
                    revoking={revokeApplicationMutation.isPending && revokeApplicationMutation.variables === application.id}
                    onRevoke={() => revokeApplicationMutation.mutate(application.id)}
                  />
                ))}
              </div>
            ) : (
              <div className="security-empty">你还没有授权任何应用。</div>
            )}
          </Card>
        </div>
      </div>
    </main>
  )
}

function AuthorizedApplicationRow({ application, revoking, onRevoke }: {
  application: AuthorizedApplication
  revoking: boolean
  onRevoke: () => void
}) {
  return (
    <article className="authorized-application-row">
      <div className="authorized-application-mark">{applicationInitials(application.clientName)}</div>
      <div className="authorized-application-main">
        <div className="authorized-application-name">
          <strong>{application.clientName}</strong>
          <Tag bordered={false}>{application.applicationType === 'desktop' ? '桌面应用' : 'Web 应用'}</Tag>
        </div>
        <div className="authorized-application-scopes">
          {application.scopes.map((scope) => <span key={scope}>{scopeLabel(scope)}</span>)}
        </div>
        <Text type="secondary">最近授权于 {formatDateTime(application.updatedAt)}</Text>
      </div>
      <Popconfirm
        title="撤销这个应用的授权？"
        description="刷新令牌会立即失效；已签发的访问令牌最长约一小时后失效。"
        okText="撤销授权"
        cancelText="取消"
        onConfirm={onRevoke}
      >
        <Button type="link" danger loading={revoking}>撤销</Button>
      </Popconfirm>
    </article>
  )
}

function scopeLabel(scope: OIDCScope): string {
  return ({
    openid: '身份确认',
    profile: '基本资料',
    email: '企业邮箱',
    'model.invoke': '模型调用',
    'mcp.invoke': 'MCP 调用',
    offline_access: '保持登录',
  } as Record<OIDCScope, string>)[scope] ?? scope
}

function applicationInitials(name: string): string {
  const words = name.trim().split(/\s+/).filter(Boolean)
  if (words.length > 1) return words.slice(0, 2).map((word) => word[0]).join('').toUpperCase()
  return name.trim().slice(0, 2).toUpperCase() || 'APP'
}

function SessionRow({ session, revoking, onRevoke }: { session: UserSession; revoking: boolean; onRevoke: () => void }) {
  const client = describeClient(session.userAgent)
  return (
    <article className={`session-row${session.current ? ' current' : ''}`}>
      <div className="session-device-icon"><DesktopOutlined /></div>
      <div className="session-main">
        <div className="session-name">
          <strong>{client}</strong>
          {session.current ? <StatusIndicator label="当前设备" tone="info" /> : null}
        </div>
        <Text className="session-user-agent">{session.userAgent || '未记录客户端信息'}</Text>
        <Space size={16} wrap className="session-meta">
          <span>{session.sourceIp || '未知 IP'}</span>
          <span><ClockCircleOutlined /> 最近活动 {formatDateTime(session.lastSeenAt)}</span>
          <span>登录于 {formatDateTime(session.createdAt)}</span>
        </Space>
      </div>
      {session.current ? (
        <Text type="secondary">请从右上角退出</Text>
      ) : (
        <Popconfirm title="退出这个设备？" description="该会话会立即失效。" okText="退出设备" cancelText="取消" onConfirm={onRevoke}>
          <Button type="link" danger loading={revoking}>退出</Button>
        </Popconfirm>
      )}
    </article>
  )
}

function describeClient(userAgent: string): string {
  const browser = /Edg\//.test(userAgent) ? 'Edge'
    : /Chrome\//.test(userAgent) ? 'Chrome'
      : /Firefox\//.test(userAgent) ? 'Firefox'
        : /Safari\//.test(userAgent) ? 'Safari' : '未知浏览器'
  const system = /Macintosh|Mac OS X/.test(userAgent) ? 'macOS'
    : /Windows/.test(userAgent) ? 'Windows'
      : /iPhone|iPad/.test(userAgent) ? 'iOS'
        : /Android/.test(userAgent) ? 'Android'
          : /Linux/.test(userAgent) ? 'Linux' : '未知设备'
  return `${browser} · ${system}`
}

function formatDateTime(value: string): string {
  return new Intl.DateTimeFormat('zh-CN', {
    month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false,
  }).format(new Date(value))
}
