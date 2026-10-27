import {
  ApiOutlined,
  ArrowRightOutlined,
  AuditOutlined,
  CloudServerOutlined,
  FileTextOutlined,
  LinkOutlined,
  ReloadOutlined,
  SafetyCertificateOutlined,
} from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { Button } from 'antd'
import { Link } from 'react-router-dom'

import { listMCPServices } from '../api/mcp'
import { listModelProviders, listModels } from '../api/models'
import { listSkills } from '../api/skills'
import { getSystemReadiness } from '../api/system'
import { useAuth } from '../auth/AuthContext'

export function DashboardPage() {
  const { user, hasPermission } = useAuth()
  const canReadProviders = hasPermission('model.provider.read')
  const canReadModels = hasPermission('model.catalog.read')
  const canReadMCP = hasPermission('mcp.service.read')
  const canReadSkills = hasPermission('skill.asset.read')

  const providers = useQuery({ queryKey: ['model-providers'], queryFn: listModelProviders, enabled: canReadProviders })
  const models = useQuery({ queryKey: ['model-catalog'], queryFn: listModels, enabled: canReadModels })
  const mcpServices = useQuery({ queryKey: ['mcp-services'], queryFn: listMCPServices, enabled: canReadMCP })
  const skills = useQuery({ queryKey: ['enterprise-skills'], queryFn: listSkills, enabled: canReadSkills })
  const readiness = useQuery({ queryKey: ['system-readiness'], queryFn: getSystemReadiness, refetchInterval: 60_000 })

  const resources = [
    { key: 'providers', title: '供应商连接', path: '/ai-resources/models?tab=providers', icon: <CloudServerOutlined />,
      visible: canReadProviders, active: providers.data?.filter((item) => item.status === 'active').length,
      total: providers.data?.length, loading: providers.isPending, error: providers.isError, unit: '已启用' },
    { key: 'models', title: '企业模型', path: '/ai-resources/models?tab=models', icon: <ApiOutlined />,
      visible: canReadModels, active: models.data?.filter((item) => item.status === 'active').length,
      total: models.data?.length, loading: models.isPending, error: models.isError, unit: '已启用' },
    { key: 'mcp', title: 'MCP 服务', path: '/ai-resources/mcp', icon: <LinkOutlined />,
      visible: canReadMCP, active: mcpServices.data?.filter((item) => item.status === 'active').length,
      total: mcpServices.data?.length, loading: mcpServices.isPending, error: mcpServices.isError, unit: '已启用' },
    { key: 'skills', title: 'Skills', path: '/ai-resources/skills', icon: <FileTextOutlined />,
      visible: canReadSkills, active: skills.data?.filter((item) => item.status === 'published').length,
      total: skills.data?.length, loading: skills.isPending, error: skills.isError, unit: '已发布' },
  ].filter((item) => item.visible)

  const failedProviders = providers.data?.filter((item) => item.status === 'active' && item.lastProbeStatus === 'failed').length ?? 0
  const failedDeployments = mcpServices.data?.filter((item) => item.deploymentStatus === 'failed').length ?? 0
  const draftSkills = skills.data?.filter((item) => item.status === 'draft').length ?? 0
  const attention = [
    { key: 'provider', count: failedProviders, title: '供应商连接测试失败', path: '/ai-resources/models?tab=providers' },
    { key: 'deployment', count: failedDeployments, title: 'MCP 服务构建失败', path: '/ai-resources/mcp' },
    { key: 'draft', count: draftSkills, title: 'Skill 草稿待审阅', path: '/ai-resources/skills' },
  ].filter((item) => item.count > 0)

  const shortcuts = [
    { key: 'applications', title: '应用接入', description: '管理客户端与授权范围', path: '/ai-resources/applications',
      icon: <LinkOutlined />, visible: hasPermission('oidc.client.read') },
    { key: 'gateway', title: '模型网关', description: '查看调用密钥、记录与用量', path: '/ai-resources/gateway',
      icon: <ApiOutlined />, visible: hasPermission('model.gateway.read') },
    { key: 'policies', title: '模型策略', description: '管理可用模型与调用额度', path: '/ai-resources/policies',
      icon: <SafetyCertificateOutlined />, visible: hasPermission('model.policy.read') },
    { key: 'audit', title: '审计追踪', description: '查找管理操作与变更记录', path: '/audit',
      icon: <AuditOutlined />, visible: hasPermission('audit.event.read') },
  ].filter((item) => item.visible)

  function refresh() {
    if (canReadProviders) void providers.refetch()
    if (canReadModels) void models.refetch()
    if (canReadMCP) void mcpServices.refetch()
    if (canReadSkills) void skills.refetch()
    void readiness.refetch()
  }

  return <main className="dashboard-page">
    <div className="dashboard-header">
      <div>
        <span className="dashboard-organization">{user?.organization}</span>
        <h1>首页</h1>
        <p>查看当前资源，进入常用管理页面。</p>
      </div>
      <Button icon={<ReloadOutlined />} onClick={refresh}>刷新数据</Button>
    </div>

    <section className="dashboard-section" aria-labelledby="dashboard-resources-title">
      <div className="dashboard-section-heading">
        <h2 id="dashboard-resources-title">资源概况</h2>
        <span>按当前账号的查看权限显示</span>
      </div>
      {resources.length > 0 ? <div className="dashboard-resource-grid">
        {resources.map((item) => <Link className="dashboard-resource" to={item.path} key={item.key}>
          <span className="dashboard-resource-icon" aria-hidden="true">{item.icon}</span>
          <span className="dashboard-resource-name">{item.title}</span>
          <strong>{item.error ? '—' : item.loading ? '…' : item.active}</strong>
          <span className="dashboard-resource-meta">{item.error ? '读取失败，进入页面查看' : item.loading ? '正在读取' :
            `${item.unit} / 共 ${item.total} 个`}</span>
          <ArrowRightOutlined className="dashboard-resource-arrow" aria-hidden="true" />
        </Link>)}
      </div> : <div className="dashboard-empty">当前账号没有可查看的 AI 资源。</div>}
    </section>

    <div className="dashboard-lower-grid">
      <section className="dashboard-panel" aria-labelledby="dashboard-attention-title">
        <div className="dashboard-panel-heading"><h2 id="dashboard-attention-title">需要关注</h2></div>
        {attention.length > 0 ? <div className="dashboard-attention-list">
          {attention.map((item) => <Link to={item.path} key={item.key} className="dashboard-attention-row">
            <span>{item.title}</span><strong>{item.count} 个</strong><ArrowRightOutlined aria-hidden="true" />
          </Link>)}
        </div> : <p className="dashboard-panel-empty">{resources.some((item) => item.error)
          ? '部分资源读取失败，请刷新或进入对应页面查看。' : '目前没有需要处理的资源项。'}</p>}
        <div className="dashboard-health">
          <span className={`dashboard-health-dot${readiness.data ? ' ready' : ''}`} aria-hidden="true" />
          <span>基础服务</span>
          <strong>{readiness.isPending ? '检查中' : readiness.data ? '运行正常' : '连接异常'}</strong>
        </div>
      </section>

      {shortcuts.length > 0 && <section className="dashboard-panel" aria-labelledby="dashboard-shortcuts-title">
        <div className="dashboard-panel-heading"><h2 id="dashboard-shortcuts-title">常用入口</h2></div>
        <div className="dashboard-shortcuts">
          {shortcuts.map((item) => <Link to={item.path} className="dashboard-shortcut" key={item.key}>
            <span className="dashboard-shortcut-icon" aria-hidden="true">{item.icon}</span>
            <span><strong>{item.title}</strong><small>{item.description}</small></span>
            <ArrowRightOutlined aria-hidden="true" />
          </Link>)}
        </div>
      </section>}
    </div>
  </main>
}
