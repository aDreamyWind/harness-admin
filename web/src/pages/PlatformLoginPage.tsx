import { LockOutlined } from '@ant-design/icons'
import { Alert, Button, Form, Input, Typography } from 'antd'
import { useState } from 'react'
import { Link } from 'react-router-dom'

import { ApiError } from '../api/client'
import type { PlatformLoginInput } from '../api/platform'
import { usePlatformAuth } from '../auth/PlatformAuthContext'

const { Paragraph, Text, Title } = Typography

export function PlatformLoginPage() {
  const { login, startupError } = usePlatformAuth()
  const [submitting, setSubmitting] = useState(false)
  const [errorMessage, setErrorMessage] = useState(startupError)

  async function handleSubmit(values: PlatformLoginInput) {
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
    <main className="login-shell platform-login-shell">
      <section className="login-panel" aria-labelledby="platform-login-title">
        <header className="login-brand">
          <span className="login-brand-mark platform-brand-mark" aria-hidden="true">PA</span>
          <span><strong>Harness Admin</strong></span>
        </header>
        <div className="login-heading">
          <Text className="login-kicker">平台管理入口</Text>
          <Title id="platform-login-title" level={1}>登录平台管理</Title>
          <Paragraph>管理企业及其首位企业管理员。</Paragraph>
        </div>
        {errorMessage ? <Alert className="login-alert" type="error" showIcon message={errorMessage} /> : null}
        <Form<PlatformLoginInput> layout="vertical" requiredMark={false} onFinish={handleSubmit} autoComplete="on">
          <Form.Item label="用户名" name="username" rules={[{ required: true, whitespace: true, message: '请输入用户名' }]}>
            <Input size="large" autoComplete="username" autoFocus placeholder="请输入平台管理员用户名" />
          </Form.Item>
          <Form.Item label="密码" name="password" rules={[{ required: true, message: '请输入密码' }]}>
            <Input.Password size="large" autoComplete="current-password" placeholder="请输入密码" />
          </Form.Item>
          <Button className="login-submit" type="primary" htmlType="submit" size="large" loading={submitting} block>登录</Button>
        </Form>
        <footer className="login-footnote platform-login-footnote">
          <span><LockOutlined aria-hidden="true" /> 平台账号与企业账号相互隔离。</span>
          <Link to="/">返回企业登录</Link>
        </footer>
      </section>
    </main>
  )
}
