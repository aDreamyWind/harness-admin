import { LockOutlined } from '@ant-design/icons'
import { Alert, Button, Form, Input, Typography } from 'antd'
import { useState } from 'react'

import { ApiError, type LoginInput } from '../api/auth'
import { useAuth } from '../auth/AuthContext'

const { Paragraph, Text, Title } = Typography

export function LoginPage() {
  const { login, startupError } = useAuth()
  const [submitting, setSubmitting] = useState(false)
  const [errorMessage, setErrorMessage] = useState(startupError)

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
            <small>CONTROL PLANE</small>
          </span>
        </header>

        <div className="login-heading">
          <Text className="login-kicker">企业访问入口</Text>
          <Title id="login-title" level={1}>
            登录管理控制台
          </Title>
          <Paragraph>使用所属企业代码和管理员账号继续。</Paragraph>
        </div>

        {errorMessage ? (
          <Alert className="login-alert" type="error" showIcon message={errorMessage} />
        ) : null}

        <Form<LoginInput>
          layout="vertical"
          requiredMark={false}
          initialValues={{ organizationCode: 'default' }}
          onFinish={handleSubmit}
          autoComplete="on"
        >
          <Form.Item
            label="企业代码"
            name="organizationCode"
            rules={[{ required: true, whitespace: true, message: '请输入企业代码' }]}
          >
            <Input size="large" autoComplete="organization" autoFocus placeholder="例如：default" />
          </Form.Item>
          <Form.Item
            label="用户名"
            name="username"
            rules={[{ required: true, whitespace: true, message: '请输入用户名' }]}
          >
            <Input size="large" autoComplete="username" placeholder="请输入用户名" />
          </Form.Item>
          <Form.Item
            label="密码"
            name="password"
            rules={[{ required: true, message: '请输入密码' }]}
          >
            <Input.Password size="large" autoComplete="current-password" placeholder="请输入密码" />
          </Form.Item>
          <Button className="login-submit" type="primary" htmlType="submit" size="large" loading={submitting} block>
            登录
          </Button>
        </Form>

        <footer className="login-footnote">
          <LockOutlined aria-hidden="true" />
          登录会话由服务端管理，凭据不会保存在浏览器存储中。
        </footer>
      </section>
    </main>
  )
}
