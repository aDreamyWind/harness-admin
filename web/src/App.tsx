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
import { Avatar, Button, Drawer, Dropdown, Layout, Menu, Spin, Tag, message, type MenuProps } from 'antd'
import { useState } from 'react'
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

const menuItems = [
  { key: 'overview', icon: <DashboardOutlined />, label: '控制面总览' },
  {
    key: 'identity',
    icon: <TeamOutlined />,
    label: '身份与组织',
    children: [
      { key: 'users', label: '用户' },
      { key: 'departments', label: '部门' },
      { key: 'roles', label: '角色与权限' },
    ],
  },
  { key: 'models', icon: <AppstoreOutlined />, label: '模型目录', disabled: true },
  { key: 'gateway', icon: <NodeIndexOutlined />, label: '模型网关', disabled: true },
  { key: 'policy', icon: <SafetyCertificateOutlined />, label: '策略与审批', disabled: true },
  { key: 'audit', icon: <AuditOutlined />, label: '审计追踪' },
  { key: 'settings', icon: <SettingOutlined />, label: '系统设置', disabled: true },
]

const menuRoutes: Record<string, string> = {
  overview: '/',
  users: '/identity/users',
  departments: '/identity/departments',
  roles: '/identity/roles',
  audit: '/audit',
}

function Navigation({ selectedKey, onNavigate }: { selectedKey: string; onNavigate: (path: string) => void }) {
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
        items={menuItems}
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
  const { user, isLoading, logout } = useAuth()
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
          <Navigation selectedKey={selectedMenuKey} onNavigate={navigateFromMenu} />
        </Sider>
        <Drawer
          placement="left"
          size={280}
          open={mobileOpen}
          onClose={() => setMobileOpen(false)}
          styles={{ body: { padding: 0, background: '#111b2d' } }}
        >
          <Navigation selectedKey={selectedMenuKey} onNavigate={navigateFromMenu} />
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
              <Route path="/identity/users" element={<UsersPage />} />
              <Route path="/identity/departments" element={<DepartmentsPage />} />
              <Route path="/identity/roles" element={<RolesPage />} />
              <Route path="/audit" element={<AuditPage />} />
              <Route path="/account/security" element={<AccountSecurityPage />} />
              <Route path="*" element={<Navigate to="/" replace />} />
            </Routes>
          </Content>
        </Layout>
      </Layout>
    </>
  )
}
