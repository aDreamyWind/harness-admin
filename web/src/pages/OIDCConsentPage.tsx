import {
  ArrowRightOutlined,
  CheckOutlined,
  CloseOutlined,
  LockOutlined,
  SafetyCertificateOutlined,
} from '@ant-design/icons'
import { useMutation, useQuery } from '@tanstack/react-query'
import { Alert, Button, Spin, Typography, message } from 'antd'
import { useMemo } from 'react'

import { decideOIDCAuthorization, getOIDCAuthorizationRequest, type OIDCScope } from '../api/oidc'
import { useAuth } from '../auth/AuthContext'

const { Text, Title } = Typography

const scopeContent: Record<OIDCScope, { title: string; detail: string }> = {
  openid: { title: '确认你的身份', detail: '识别当前登录账号。' },
  profile: { title: '读取基本资料', detail: '读取姓名和登录用户名。' },
  email: { title: '读取企业邮箱', detail: '读取账号绑定的企业邮箱。' },
  'model.invoke': { title: '调用企业模型', detail: '使用分配给你的模型和额度发起调用。' },
  offline_access: { title: '保持登录状态', detail: '在当前登录过期后继续换取新的访问凭证。' },
}

export function OIDCConsentPage() {
  const { user } = useAuth()
  const [messageApi, messageContext] = message.useMessage()
  const search = useMemo(() => window.location.search, [])
  const requestQuery = useQuery({
    queryKey: ['oidc-consent', search],
    queryFn: () => getOIDCAuthorizationRequest(search),
    retry: false,
  })
  const decisionMutation = useMutation({
    mutationFn: (approved: boolean) => decideOIDCAuthorization(search, approved),
    onSuccess: ({ redirectUri }) => window.location.assign(redirectUri),
    onError: (error) => messageApi.error(error instanceof Error ? error.message : '授权决定提交失败'),
  })

  const authorization = requestQuery.data
  const isApproving = decisionMutation.isPending && decisionMutation.variables === true
  const isDenying = decisionMutation.isPending && decisionMutation.variables === false

  return (
    <main className="consent-shell">
      {messageContext}
      <section className="consent-panel" aria-labelledby="consent-title">
        <header className="consent-brand">
          <span className="brand-mark" aria-hidden="true">HA</span>
          <strong>Harness Admin</strong>
          <span className="consent-security"><LockOutlined /> 安全授权</span>
        </header>

        {requestQuery.isLoading ? (
          <div className="consent-loading"><Spin /><span>正在核对应用信息</span></div>
        ) : requestQuery.isError || !authorization ? (
          <Alert
            type="error"
            showIcon
            message="无法继续授权"
            description={requestQuery.error instanceof Error ? requestQuery.error.message : '授权请求不完整或已经失效。'}
          />
        ) : (
          <>
            <div className="consent-handoff" aria-label={`${authorization.clientName} 请求 Harness Admin 授权`}>
              <div className="consent-party application">
                <span className="consent-party-mark">{applicationInitials(authorization.clientName)}</span>
                <span><strong>{authorization.clientName}</strong><small>{authorization.redirectHost}</small></span>
              </div>
              <ArrowRightOutlined className="consent-handoff-arrow" />
              <div className="consent-party account">
                <SafetyCertificateOutlined className="consent-party-mark" />
                <span><strong>{user?.displayName}</strong><small>{user?.organization}</small></span>
              </div>
            </div>

            <div className="consent-heading">
              <Text>应用授权</Text>
              <Title id="consent-title" level={1}>允许此应用使用你的企业账号？</Title>
            </div>

            <div className="consent-permissions">
              <span className="consent-section-label">申请的权限</span>
              {authorization.scopes.map((scope) => {
                const content = scopeContent[scope] ?? { title: scope, detail: '使用此权限范围。' }
                return (
                  <div className="consent-permission" key={scope}>
                    <CheckOutlined />
                    <span><strong>{content.title}</strong><small>{content.detail}</small></span>
                  </div>
                )
              })}
            </div>

            <p className="consent-note">
              同意后可在“账号与安全”中撤销。Harness Admin 不会把你的密码交给此应用。
            </p>

            <div className="consent-actions">
              <Button
                size="large"
                icon={<CloseOutlined />}
                loading={isDenying}
                disabled={decisionMutation.isPending && !isDenying}
                onClick={() => decisionMutation.mutate(false)}
              >
                拒绝
              </Button>
              <Button
                type="primary"
                size="large"
                icon={<CheckOutlined />}
                loading={isApproving}
                disabled={decisionMutation.isPending && !isApproving}
                onClick={() => decisionMutation.mutate(true)}
              >
                同意并继续
              </Button>
            </div>
          </>
        )}
      </section>
    </main>
  )
}

function applicationInitials(name: string): string {
  const words = name.trim().split(/\s+/).filter(Boolean)
  if (words.length > 1) return words.slice(0, 2).map((word) => word[0]).join('').toUpperCase()
  return name.trim().slice(0, 2).toUpperCase() || 'APP'
}
