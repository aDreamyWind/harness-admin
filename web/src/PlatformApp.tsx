import { Spin } from 'antd'

import { usePlatformAuth } from './auth/PlatformAuthContext'
import { PlatformLoginPage } from './pages/PlatformLoginPage'
import { PlatformOrganizationsPage } from './pages/PlatformOrganizationsPage'

export function PlatformApp() {
  const { admin, isLoading } = usePlatformAuth()
  if (isLoading) {
    return <div className="auth-loading" role="status"><Spin size="large" /><span>正在检查平台登录状态</span></div>
  }
  return admin ? <PlatformOrganizationsPage /> : <PlatformLoginPage />
}
