import { ReloadOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, Card, Modal, Space, Table, Tag, Typography, message } from 'antd'
import { useState } from 'react'

import type { MCPService } from '../api/mcp'
import { changeMCPDeployment, getMCPRuntimeStatus, listMCPDeploymentVersions,
  rollbackMCPDeployment, type MCPDeploymentVersion } from '../api/mcpDeployments'
import { MCPDeploymentDialog } from './MCPDeploymentDialog'
import { buildMCPDeployment, type MCPDeploymentInput } from '../api/mcpDeployments'

interface Props {
  service: MCPService
  onUpdated: () => Promise<void>
  onBuildSubmitted: (versionId: string) => void
  onReady: (serviceId: string) => Promise<number>
}

export function MCPDeploymentPanel({ service, onUpdated, onBuildSubmitted, onReady }: Props) {
  const [messageApi, contextHolder] = message.useMessage()
  const queryClient = useQueryClient()
  const [versionOpen, setVersionOpen] = useState(false)
  const [versionDefaults, setVersionDefaults] = useState<Partial<MCPDeploymentInput>>()
  const [visibleLog, setVisibleLog] = useState<MCPDeploymentVersion | null>(null)
  const versions = useQuery({ queryKey: ['mcp-deployment-versions', service.id], queryFn: () => listMCPDeploymentVersions(service.id),
	refetchInterval: (query) => query.state.data?.some((version) => version.buildStatus === 'building') ? 3000 : false })
  const runtime = useQuery({ queryKey: ['mcp-deployment-runtime', service.id], queryFn: () => getMCPRuntimeStatus(service.id),
    enabled: Boolean(service.currentRevision), refetchInterval: service.deploymentStatus === 'running' ? 30000 : false })
  const action = useMutation({ mutationFn: (kind: 'start' | 'stop' | 'restart') => changeMCPDeployment(service.id, kind),
    onSuccess: async (_, kind) => {
      await onUpdated()
      await runtime.refetch()
      if (kind === 'stop') {
        messageApi.success('运行状态已更新')
        return
      }
      try {
        const count = await onReady(service.id)
        messageApi.success(`MCP 已运行，发现 ${count} 个工具`)
      } catch (error) {
        messageApi.warning(`MCP 已运行，但自动获取工具失败：${error instanceof Error ? error.message : '请检查 MCP 服务'}`)
      }
    },
    onError: (error: Error) => messageApi.error(error.message) })
  const rollback = useMutation({ mutationFn: (versionId: string) => rollbackMCPDeployment(service.id, versionId),
    onSuccess: async () => {
      await onUpdated()
      await runtime.refetch()
      try {
        const count = await onReady(service.id)
        messageApi.success(`已切换版本，发现 ${count} 个工具`)
      } catch (error) {
        messageApi.warning(`已切换版本，但自动获取工具失败：${error instanceof Error ? error.message : '请检查 MCP 服务'}`)
      }
    },
    onError: (error: Error) => messageApi.error(error.message) })
  const build = useMutation({ mutationFn: (input: MCPDeploymentInput) => buildMCPDeployment(input, service.id),
    onSuccess: async (result) => {
      setVersionOpen(false)
      onBuildSubmitted(result.version.id)
      await queryClient.invalidateQueries({ queryKey: ['mcp-deployment-versions', service.id] })
      await onUpdated()
	  messageApi.success('已提交后台构建，可在版本列表查看进度和日志')
    }, onError: (error: Error) => messageApi.error(error.message) })
	const status = service.deploymentStatus === 'building' || service.deploymentStatus === 'failed'
	  ? service.deploymentStatus : runtime.data?.status ?? service.deploymentStatus ?? 'stopped'
	const currentLog = versions.data?.find((version) => version.id === visibleLog?.id) ?? visibleLog

  return <Card className="content-card mcp-deployment-card" variant="borderless">
    {contextHolder}
    <div className="mcp-section-head"><h2>部署与运行</h2><Button size="small" icon={<ReloadOutlined />} onClick={() => {
      runtime.refetch(); versions.refetch()
    }}>刷新状态</Button></div>
    <div className="mcp-deployment-strip">
      <div><span>容器状态</span><Tag color={status === 'running' ? 'green' : status === 'failed' ? 'red' : 'default'}>{statusLabel(status)}</Tag></div>
      <div><span>MCP 健康</span><Tag color={runtime.data?.protocolHealthy ? 'green' : 'default'}>{runtime.data?.protocolHealthy ? '可连接' : runtime.data?.running ? '握手失败' : '未运行'}</Tag></div>
      <div><span>当前版本</span><code title={service.currentRevision}>{service.currentRevision?.slice(0, 12) || '尚未构建'}</code></div>
      <div><span>运行环境</span><strong>{service.runtimeType === 'node' ? 'Node.js' : 'Python'} · {service.entryFile}</strong></div>
    </div>
    {service.deploymentError && <Typography.Text type="danger">{service.deploymentError}</Typography.Text>}
    {runtime.data?.protocolError && <Typography.Text type="danger">{runtime.data.protocolError}</Typography.Text>}
    <Space wrap className="mcp-deployment-actions">
	  <Button type="primary" disabled={!service.currentRevision || status === 'running' || status === 'building'} loading={action.isPending} onClick={() => action.mutate('start')}>启动</Button>
      <Button disabled={status !== 'running'} loading={action.isPending} onClick={() => action.mutate('stop')}>停止</Button>
      <Button disabled={status !== 'running'} loading={action.isPending} onClick={() => action.mutate('restart')}>重启</Button>
      <Button disabled={status === 'running' || status === 'building' || versions.isLoading} onClick={() => {
        const previous = versions.data?.[0]
        setVersionDefaults({ sourceType: previous?.sourceType === 'upload' ? 'upload' : 'repository',
          repositoryUrl: previous?.sourceType === 'upload' ? undefined : previous?.sourceRef,
          ref: previous?.buildStatus === 'failed' && previous.sourceType !== 'upload' ? previous.revision : '',
          runtimeType: previous?.runtimeType ?? service.runtimeType as 'node' | 'python',
          entryFile: previous?.entryFile || service.entryFile })
        setVersionOpen(true)
      }}>构建新版本</Button>
    </Space>
    <div className="mcp-deployment-subhead"><strong>构建版本</strong><Typography.Text type="secondary">构建日志仅管理员可见</Typography.Text></div>
    <Table<MCPDeploymentVersion> rowKey="id" size="small" loading={versions.isLoading} dataSource={versions.data ?? []}
      pagination={{ pageSize: 5 }} columns={[
        { title: '版本', render: (_, item) => <code title={item.revision}>{item.revision.slice(0, 12)}</code> },
        { title: '来源', render: (_, item) => item.sourceType === 'github' ? `GitHub · ${item.sourceRef}` : item.sourceType === 'gitee' ? `Gitee · ${item.sourceRef}` : `上传 · ${item.sourceRef}` },
        { title: '构建', render: (_, item) => <Tag color={item.buildStatus === 'succeeded' ? 'green' : item.buildStatus === 'failed' ? 'red' : 'blue'}>{item.buildStatus === 'succeeded' ? '成功' : item.buildStatus === 'failed' ? '失败' : '进行中'}</Tag> },
        { title: '操作', render: (_, item) => <Space size="small"><Button type="link" onClick={() => setVisibleLog(item)}>日志</Button>
          {item.buildStatus === 'succeeded' && item.revision !== service.currentRevision && <Button type="link" disabled={status === 'running'}
            loading={rollback.isPending} onClick={() => Modal.confirm({ title: '回退到此版本？', content: '会切换镜像、启动服务并自动刷新工具列表。',
              onOk: async () => { await rollback.mutateAsync(item.id) } })}>回退</Button>}</Space> },
      ]} />
    {runtime.data?.logs && <details className="mcp-deployment-logs"><summary>容器日志</summary><pre>{runtime.data.logs}</pre></details>}
    <Typography.Text type="secondary">健康检查会尝试 MCP 握手与工具发现；运行容器默认断网。</Typography.Text>
	<Modal title={`构建日志 · ${currentLog?.revision.slice(0, 12) ?? ''}`} open={Boolean(visibleLog)} footer={null} width={760}
	  onCancel={() => setVisibleLog(null)}><pre className="mcp-deployment-log-output">{currentLog?.buildLog || currentLog?.errorMessage || '构建进行中，完成后会自动更新'}</pre></Modal>
    <MCPDeploymentDialog open={versionOpen} isVersion initialValues={versionDefaults} loading={build.isPending}
      onClose={() => setVersionOpen(false)} onSubmit={(input) => build.mutate(input)} />
  </Card>
}

function statusLabel(value: string) {
  return ({ running: '运行中', stopped: '已停止', failed: '失败', building: '构建中' } as Record<string, string>)[value] ?? value
}
