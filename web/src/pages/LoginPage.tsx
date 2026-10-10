import { LockOutlined } from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { Alert, Button, Form, Input, Select, Typography } from 'antd'
import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'

import { ApiError, listLoginOrganizations, type LoginInput } from '../api/auth'
import { useAuth } from '../auth/AuthContext'

const { Paragraph, Text, Title } = Typography

export function LoginPage() {
  const { login, startupError } = useAuth()
  const [form] = Form.useForm<LoginInput>()
  const [submitting, setSubmitting] = useState(false)
  const [errorMessage, setErrorMessage] = useState(startupError)
  const organizationsQuery = useQuery({ queryKey: ['login-organizations'], queryFn: listLoginOrganizations })

  useEffect(() => {
    const firstOrganization = organizationsQuery.data?.[0]
    if (firstOrganization && !form.getFieldValue('organizationCode')) {
      form.setFieldValue('organizationCode', firstOrganization.code)
    }
  }, [form, organizationsQuery.data])

  async function handleSubmit(values: LoginInput) {
    setSubmitting(true)
    setErrorMessage('')
    try {
      await login(values)
    } catch (error) {
      setErrorMessage(error instanceof ApiError ? error.message : '登录失败，请稍后重试')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <main className="login-shell">
      <section className="login-panel" aria-labelledby="login-title">
        <header className="login-brand">
          <span className="login-brand-mark" aria-hidden="true">
            HA
          </span>
          <span>
            <strong>Harness Admin</strong>
          </span>
        </header>

        <div className="login-heading">
          <Text className="login-kicker">企业访问入口</Text>
          <Title id="login-title" level={1}>
            登录管理控制台
          </Title>
          <Paragraph>选择所属企业，并使用账号密码继续。</Paragraph>
        </div>

        {errorMessage ? (
          <Alert className="login-alert" type="error" showIcon message={errorMessage} />
        ) : null}

        {organizationsQuery.isError ? (
          <Alert className="login-alert" type="error" showIcon message="企业列表加载失败，请稍后刷新页面" />
        ) : null}

        <Form<LoginInput>
          form={form}
          layout="vertical"
          requiredMark={false}
          onFinish={handleSubmit}
          autoComplete="on"
        >
          <Form.Item
            label="企业"
            name="organizationCode"
            rules={[{ required: true, message: '请选择企业' }]}
          >
            <Select
              size="large"
              loading={organizationsQuery.isLoading}
              placeholder="请选择企业"
              options={(organizationsQuery.data ?? []).map((organization) => ({
                value: organization.code,
                label: organization.name,
              }))}
            />
          </Form.Item>
          <Form.Item
            label="用户名"
            name="username"
            rules={[{ required: true, whitespace: true, message: '请输入用户名' }]}
          >
            <Input size="large" autoComplete="username" autoFocus placeholder="请输入用户名" />
          </Form.Item>
          <Form.Item
            label="密码"
            name="password"
            rules={[{ required: true, message: '请输入密码' }]}
          >
            <Input.Password size="large" autoComplete="current-password" placeholder="请输入密码" />
          </Form.Item>
          <Button
            className="login-submit"
            type="primary"
            htmlType="submit"
            size="large"
            loading={submitting}
            disabled={organizationsQuery.isError || !organizationsQuery.data?.length}
            block
          >
            登录
          </Button>
        </Form>

        <footer className="login-footnote">
          <span><LockOutlined aria-hidden="true" /> 登录会话由服务端管理。</span>
          <Link to="/platform/login">平台管理员入口</Link>
        </footer>
      </section>
    </main>
  )
}
