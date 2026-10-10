import {
  ApiOutlined,
  CheckCircleFilled,
  DatabaseOutlined,
  DeploymentUnitOutlined,
  SafetyCertificateOutlined,
} from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { Alert, Card, Col, Progress, Row, Skeleton, Tag, Typography } from 'antd'

import { getSystemBootstrap } from '../api/system'

const { Title, Paragraph, Text } = Typography

const signalSteps = [
  { key: 'identity', eyebrow: 'IDENTITY', title: '确认是谁', description: '用户、设备与会话' },
  { key: 'policy', eyebrow: 'POLICY', title: '判断能做什么', description: '角色、权限与审批' },
  { key: 'gateway', eyebrow: 'GATEWAY', title: '路由到哪里', description: '模型、额度与密钥' },
  { key: 'audit', eyebrow: 'AUDIT', title: '留下证据', description: '调用、工具与产物' },
]

const buildSequence = [
  { title: '工程与数据底座', description: '启动、迁移、健康检查、前端外壳', state: 'done' },
  { title: '身份与权限', description: '登录、组织、用户、角色、会话', state: 'active' },
  { title: 'Harness 统一登录', description: 'OIDC、PKCE、设备身份与吊销', state: 'planned' },
  { title: 'AI 治理链路', description: '模型网关、额度、审计与追踪', state: 'planned' },
]

export function DashboardPage() {
  const bootstrapQuery = useQuery({
    queryKey: ['system-bootstrap'],
    queryFn: getSystemBootstrap,
  })

  const services = bootstrapQuery.data?.services
  const readyServices = services ? Object.values(services).filter(Boolean).length : 0
  const serviceCount = services ? Object.keys(services).length : 3

  return (
    <main>
      <section className="page-heading">
        <div>
          <Text className="eyebrow">ENTERPRISE AGENT CONTROL PLANE</Text>
          <Title level={1}>先把治理链路接通。</Title>
          <Paragraph>
            Harness 负责执行，控制面负责身份、策略、模型和追踪。所有企业能力从同一条可信链路进入。
          </Paragraph>
        </div>
        <div className="heading-meta">
          <span>当前里程碑</span>
          <strong>Foundation</strong>
          <small>基础设施与工程骨架</small>
        </div>
      </section>

      <Card className="signal-card" variant="borderless">
        <div className="signal-header">
          <div>
            <Text className="eyebrow">REQUEST PATH</Text>
            <Title level={3}>一次企业 Agent 请求应当经过什么</Title>
          </div>
          <Tag color="processing">设计基线</Tag>
        </div>
        <div className="signal-path">
          {signalSteps.map((step, index) => (
            <div className="signal-step" key={step.key}>
              <div className="signal-node">
                <span>{index + 1}</span>
              </div>
              <div>
                <Text className="signal-eyebrow">{step.eyebrow}</Text>
                <strong>{step.title}</strong>
                <small>{step.description}</small>
              </div>
            </div>
          ))}
        </div>
      </Card>

      {bootstrapQuery.isError && (
        <Alert
          className="connection-alert"
          type="warning"
          showIcon
          message="管理端已启动，后端服务尚未连接"
          description="启动 PostgreSQL、Redis 和 Go 服务后，本页会自动显示真实运行状态。"
        />
      )}

      <Row gutter={[18, 18]} className="status-grid">
        <Col xs={24} sm={12} xl={6}>
          <StatusCard
            icon={<ApiOutlined />}
            label="控制面 API"
            value={bootstrapQuery.isSuccess ? '已连接' : '等待连接'}
            tone={bootstrapQuery.isSuccess ? 'success' : 'neutral'}
          />
        </Col>
        <Col xs={24} sm={12} xl={6}>
          <StatusCard
            icon={<DatabaseOutlined />}
            label="基础服务"
            value={`${readyServices} / ${serviceCount}`}
            tone={readyServices === serviceCount ? 'success' : 'neutral'}
          />
        </Col>
        <Col xs={24} sm={12} xl={6}>
          <StatusCard icon={<SafetyCertificateOutlined />} label="身份模块" value="用户管理可用" tone="success" />
        </Col>
        <Col xs={24} sm={12} xl={6}>
          <StatusCard icon={<DeploymentUnitOutlined />} label="模型网关" value="待开始" tone="neutral" />
        </Col>
      </Row>

      <Row gutter={[18, 18]}>
        <Col xs={24} xl={15}>
          <Card className="content-card" variant="borderless">
            <div className="section-title">
              <div>
                <Text className="eyebrow">BUILD SEQUENCE</Text>
                <Title level={3}>建设序列</Title>
              </div>
              <Progress type="circle" percent={35} size={52} strokeColor="#2457d6" />
            </div>
            <div className="build-sequence">
              {buildSequence.map((item, index) => (
                <div className={`sequence-row ${item.state}`} key={item.title}>
                  <div className="sequence-index">
                    {item.state === 'active' || item.state === 'done' ? <CheckCircleFilled /> : String(index + 1).padStart(2, '0')}
                  </div>
                  <div>
                    <strong>{item.title}</strong>
                    <span>{item.description}</span>
                  </div>
                  <Tag>{item.state === 'done' ? '已完成' : item.state === 'active' ? '当前' : '规划中'}</Tag>
                </div>
              ))}
            </div>
          </Card>
        </Col>
        <Col xs={24} xl={9}>
          <Card className="content-card runtime-card" variant="borderless">
            <Text className="eyebrow">RUNTIME CONTRACT</Text>
            <Title level={3}>底座约束</Title>
            {bootstrapQuery.isLoading ? (
              <Skeleton active paragraph={{ rows: 5 }} />
            ) : (
              <ul className="contract-list">
                <li><CheckCircleFilled /> 业务模块不感知 Gin</li>
                <li><CheckCircleFilled /> SQL 与数据库迁移显式提交</li>
                <li><CheckCircleFilled /> 每次请求携带 Request ID</li>
                <li><CheckCircleFilled /> PostgreSQL 与 Redis 有就绪检查</li>
                <li><CheckCircleFilled /> OpenAPI 契约随接口同步维护</li>
              </ul>
            )}
            <div className="runtime-stack">
              <span>Go 1.26</span>
              <span>PostgreSQL</span>
              <span>Redis</span>
              <span>React 19</span>
            </div>
          </Card>
        </Col>
      </Row>
    </main>
  )
}

interface StatusCardProps {
  icon: React.ReactNode
  label: string
  value: string
  tone: 'success' | 'active' | 'neutral'
}

function StatusCard({ icon, label, value, tone }: StatusCardProps) {
  return (
    <Card className={`status-card ${tone}`} variant="borderless">
      <div className="status-icon">{icon}</div>
      <div>
        <span>{label}</span>
        <strong>{value}</strong>
      </div>
    </Card>
  )
}
