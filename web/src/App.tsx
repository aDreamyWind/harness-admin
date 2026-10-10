import {
  ApartmentOutlined,
  AppstoreOutlined,
  AuditOutlined,
  DashboardOutlined,
  MenuOutlined,
  NodeIndexOutlined,
  SafetyCertificateOutlined,
  SettingOutlined,
  TeamOutlined,
} from '@ant-design/icons'
import { Button, Drawer, Layout, Menu, Tag } from 'antd'
import { useState } from 'react'

import { DashboardPage } from './pages/DashboardPage'

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
  { key: 'audit', icon: <AuditOutlined />, label: '审计追踪', disabled: true },
  { key: 'settings', icon: <SettingOutlined />, label: '系统设置', disabled: true },
]

function Navigation() {
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
        defaultSelectedKeys={['overview']}
        defaultOpenKeys={['identity']}
        items={menuItems}
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
  const [mobileOpen, setMobileOpen] = useState(false)

  return (
    <Layout className="app-layout">
      <Sider width={248} className="desktop-sider">
        <Navigation />
      </Sider>
      <Drawer
        placement="left"
        size={280}
        open={mobileOpen}
        onClose={() => setMobileOpen(false)}
        styles={{ body: { padding: 0, background: '#111b2d' } }}
      >
        <Navigation />
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
        </Header>
        <Content className="main-content">
          <DashboardPage />
        </Content>
      </Layout>
    </Layout>
  )
}
