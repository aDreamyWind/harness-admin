import {
  ApartmentOutlined,
  AppstoreOutlined,
  AuditOutlined,
  DashboardOutlined,
  LogoutOutlined,
  LockOutlined,
  MenuOutlined,
  NodeIndexOutlined,
  SafetyCertificateOutlined,
  SettingOutlined,
  TeamOutlined,
  UserOutlined,
} from '@ant-design/icons'
import { Avatar, Button, Card, Drawer, Dropdown, Layout, Menu, Spin, Tag, message, type MenuProps } from 'antd'
import { useState, type ReactNode } from 'react'
import { Navigate, Route, Routes, useLocation, useNavigate } from 'react-router-dom'

import { useAuth } from './auth/AuthContext'
import { DashboardPage } from './pages/DashboardPage'
import { AuditPage } from './pages/AuditPage'
import { AccountSecurityPage } from './pages/AccountSecurityPage'
import { DepartmentsPage } from './pages/DepartmentsPage'
import { LoginPage } from './pages/LoginPage'
import { RolesPage } from './pages/RolesPage'
import { UsersPage } from './pages/UsersPage'

const { Header, Sider, Content } = Layout

const menuRoutes: Record<string, string> = {
  overview: '/',
  users: '/identity/users',
  departments: '/identity/departments',
  roles: '/identity/roles',
  audit: '/audit',
}

function Navigation({ items, selectedKey, onNavigate }: { items: MenuProps['items']; selectedKey: string; onNavigate: (path: string) => void }) {
  return (
    <>
      <div className="brand">
        <div className="brand-mark" aria-hidden="true">
          HA
        </div>
        <div>
          <strong>Harness Admin</strong>
          <span>Agent Control Plane</span>
        </div>
      </div>
      <Menu
        theme="dark"
        mode="inline"
        selectedKeys={[selectedKey]}
        defaultOpenKeys={['identity']}
        items={items}
        onClick={({ key }) => {
          const path = menuRoutes[key]
          if (path) onNavigate(path)
        }}
      />
      <div className="sider-footer">
        <span className="environment-dot" />
        <div>
          <strong>本地环境</strong>
          <span>Foundation · 0.1</span>
        </div>
      </div>
    </>
  )
}

export function App() {
  const { user, isLoading, logout, hasPermission } = useAuth()
  const navigate = useNavigate()
  const location = useLocation()
  const [mobileOpen, setMobileOpen] = useState(false)
  const [loggingOut, setLoggingOut] = useState(false)
  const [messageApi, messageContext] = message.useMessage()
  const selectedMenuKey = location.pathname.startsWith('/identity/users')
    ? 'users'
    : location.pathname.startsWith('/identity/departments')
      ? 'departments'
      : location.pathname.startsWith('/identity/roles')
        ? 'roles'
        : location.pathname.startsWith('/audit')
          ? 'audit'
          : location.pathname.startsWith('/account/security') ? 'account-security' : 'overview'

  function navigateFromMenu(path: string) {
    navigate(path)
    setMobileOpen(false)
  }

  if (isLoading) {
    return (
      <div className="auth-loading" role="status" aria-label="正在检查登录状态">
        <Spin size="large" />
        <span>正在检查登录状态</span>
      </div>
    )
  }

  if (!user) {
    return <LoginPage />
  }

  const menuItems = buildMenuItems(hasPermission)

  const accountItems: MenuProps['items'] = [
    {
      key: 'identity',
      disabled: true,
      label: (
        <div className="account-summary">
          <strong>{user.displayName}</strong>
          <span>{user.organization}</span>
        </div>
      ),
    },
    { type: 'divider' },
    { key: 'security', icon: <LockOutlined />, label: '密码与会话' },
    { key: 'logout', icon: <LogoutOutlined />, label: '退出登录', danger: true },
  ]

  async function handleAccountAction(info: { key: string }) {
    if (info.key === 'security') {
      navigate('/account/security')
      return
    }
    if (info.key !== 'logout' || loggingOut) return
    setLoggingOut(true)
    try {
      await logout()
    } catch (error) {
      messageApi.error(error instanceof Error ? error.message : '退出失败，请稍后重试')
    } finally {
      setLoggingOut(false)
    }
  }

  return (
    <>
      {messageContext}
      <Layout className="app-layout">
        <Sider width={248} className="desktop-sider">
          <Navigation items={menuItems} selectedKey={selectedMenuKey} onNavigate={navigateFromMenu} />
        </Sider>
        <Drawer
          placement="left"
          size={280}
          open={mobileOpen}
          onClose={() => setMobileOpen(false)}
          styles={{ body: { padding: 0, background: '#111b2d' } }}
        >
          <Navigation items={menuItems} selectedKey={selectedMenuKey} onNavigate={navigateFromMenu} />
        </Drawer>
        <Layout>
          <Header className="topbar">
            <Button
              className="mobile-menu"
              type="text"
              icon={<MenuOutlined />}
              onClick={() => setMobileOpen(true)}
              aria-label="打开导航"
            />
            <div className="breadcrumb-label">
              <ApartmentOutlined />
              企业 Agent 基础设施
            </div>
            <Tag className="phase-tag">Phase 0 · 建设中</Tag>
            <Dropdown menu={{ items: accountItems, onClick: handleAccountAction }} trigger={['click']}>
              <Button className="account-trigger" type="text" loading={loggingOut}>
                <Avatar size={28} icon={<UserOutlined />} />
                <span>{user.displayName}</span>
              </Button>
            </Dropdown>
          </Header>
          <Content className="main-content">
            <Routes>
              <Route path="/" element={<DashboardPage />} />
              <Route path="/identity/users" element={<PermissionPage permission="iam.user.read"><UsersPage /></PermissionPage>} />
              <Route path="/identity/departments" element={<PermissionPage permission="iam.department.read"><DepartmentsPage /></PermissionPage>} />
              <Route path="/identity/roles" element={<PermissionPage permission="iam.role.read"><RolesPage /></PermissionPage>} />
              <Route path="/audit" element={<PermissionPage permission="audit.event.read"><AuditPage /></PermissionPage>} />
              <Route path="/account/security" element={<AccountSecurityPage />} />
              <Route path="*" element={<Navigate to="/" replace />} />
            </Routes>
          </Content>
        </Layout>
      </Layout>
    </>
  )
}

function buildMenuItems(hasPermission: (permissionCode: string) => boolean): MenuProps['items'] {
  const identityItems = [
    hasPermission('iam.user.read') ? { key: 'users', label: '用户' } : null,
    hasPermission('iam.department.read') ? { key: 'departments', label: '部门' } : null,
    hasPermission('iam.role.read') ? { key: 'roles', label: '角色与权限' } : null,
  ].filter(Boolean) as NonNullable<MenuProps['items']>

  return [
    { key: 'overview', icon: <DashboardOutlined />, label: '控制面总览' },
    identityItems.length ? {
      key: 'identity', icon: <TeamOutlined />, label: '身份与组织', children: identityItems,
    } : null,
    { key: 'models', icon: <AppstoreOutlined />, label: '模型目录', disabled: true },
    { key: 'gateway', icon: <NodeIndexOutlined />, label: '模型网关', disabled: true },
    { key: 'policy', icon: <SafetyCertificateOutlined />, label: '策略与审批', disabled: true },
    hasPermission('audit.event.read') ? { key: 'audit', icon: <AuditOutlined />, label: '审计追踪' } : null,
    { key: 'settings', icon: <SettingOutlined />, label: '系统设置', disabled: true },
  ].filter(Boolean) as NonNullable<MenuProps['items']>
}

function PermissionPage({ permission, children }: { permission: string; children: ReactNode }) {
  const { hasPermission } = useAuth()
  const navigate = useNavigate()
  if (hasPermission(permission)) return children
  return (
    <main>
      <Card className="content-card permission-card" variant="borderless">
        <div className="permission-denied">
          <span className="permission-code">403</span>
          <div>
            <h1>没有访问权限</h1>
            <p>当前账号没有查看这个页面的权限。若工作需要，请联系管理员调整角色。</p>
            <Button type="primary" onClick={() => navigate('/')}>返回控制面总览</Button>
          </div>
        </div>
      </Card>
    </main>
  )
}
