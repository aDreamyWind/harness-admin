import {
  ArrowLeftOutlined, BgColorsOutlined, FileTextOutlined, RobotOutlined,
  SettingOutlined, TeamOutlined, VideoCameraOutlined,
} from '@ant-design/icons'
import { Button, Card, Drawer, Empty, Input, Skeleton, Tag, Typography, message } from 'antd'
import { useEffect, useState, type ReactNode } from 'react'
import { useNavigate, useParams } from 'react-router-dom'

import { getDramaProject, getDramaSource, updateDramaOverview,
  type DramaAsset, type DramaOverview, type DramaProject } from '../api/drama'
import './drama.css'

const phaseNames: Record<string, string> = {
  waiting: '等待解析', script: '编剧正在生成剧本', story: '编剧正在梳理故事',
  assets: '美术正在整理资产', episodes: '导演正在规划分集', complete: '解析完成', failed: '解析失败',
}

function statusTag(project: DramaProject) {
  if (project.status === 'ready') return <Tag color="success">可继续创作</Tag>
  if (project.status === 'failed') return <Tag color="error">解析失败</Tag>
  return <Tag color="processing">{phaseNames[project.phase] ?? '解析中'}</Tag>
}

type Section = 'overview' | 'characters' | 'scenes' | 'props' | 'episodes'
const navItems: { key: Section; label: string; icon: ReactNode }[] = [
  { key: 'overview', label: '剧本概览', icon: <FileTextOutlined /> },
  { key: 'characters', label: '角色', icon: <TeamOutlined /> },
  { key: 'scenes', label: '场景', icon: <BgColorsOutlined /> },
  { key: 'props', label: '道具', icon: <SettingOutlined /> },
  { key: 'episodes', label: '分集', icon: <VideoCameraOutlined /> },
]
const overviewFields: { key: keyof DramaOverview; label: string; multiline?: boolean }[] = [
  { key: 'videoStyle', label: '视频风格' }, { key: 'aspectRatio', label: '画面比例' },
  { key: 'protagonist', label: '主角' }, { key: 'genre', label: '故事类型' },
  { key: 'synopsis', label: '故事梗概', multiline: true },
  { key: 'background', label: '故事背景', multiline: true },
  { key: 'setting', label: '故事设定', multiline: true },
]

export function DramaProjectPage() {
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const [messageApi, contextHolder] = message.useMessage()
  const [project, setProject] = useState<DramaProject | null>(null)
  const [loading, setLoading] = useState(true)
  const [section, setSection] = useState<Section>('overview')
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState<DramaOverview>({})
  const [saving, setSaving] = useState(false)
  const [sourceOpen, setSourceOpen] = useState(false)
  const [sourceLoading, setSourceLoading] = useState(false)
  const [sourceText, setSourceText] = useState('')

  useEffect(() => {
    let alive = true
    getDramaProject(id).then((item) => { if (alive) setProject(item) })
      .catch((error) => { if (alive) messageApi.error(error instanceof Error ? error.message : '读取项目失败') })
      .finally(() => { if (alive) setLoading(false) })
    return () => { alive = false }
  }, [id, messageApi])
  useEffect(() => {
    if (!project || (project.status !== 'queued' && project.status !== 'running')) return
    const timer = window.setInterval(() => { getDramaProject(id).then(setProject).catch(() => {}) }, 3000)
    return () => window.clearInterval(timer)
  }, [id, project])

  async function save() {
    if (!project) return
    setSaving(true)
    try { setProject(await updateDramaOverview(project.id, draft)); setEditing(false); messageApi.success('项目概览已保存') }
    catch (error) { messageApi.error(error instanceof Error ? error.message : '保存失败') }
    finally { setSaving(false) }
  }

  async function showSource() {
    if (!project) return
    setSourceOpen(true)
    setSourceLoading(true)
    try { const result = await getDramaSource(project.id); setSourceText(result.sourceText) }
    catch (error) { messageApi.error(error instanceof Error ? error.message : '读取剧本失败') }
    finally { setSourceLoading(false) }
  }

  const phaseIndex = project?.phase === 'script' ? 0 : project?.phase === 'story' ? 1 : project?.phase === 'assets' ? 2 : project?.phase === 'episodes' ? 3 : project?.status === 'ready' ? 4 : -1
  const assets: DramaAsset[] = project ? project[section === 'characters' || section === 'scenes' || section === 'props' ? section : 'characters'] : []
  function sectionCount(item: Section): number {
    if (!project || item === 'overview') return 0
    if (item === 'episodes') return project.episodes.length
    return project[item].length
  }

  return <div className="drama-workspace">
    {contextHolder}
    <aside className="drama-workspace-sidebar"><button type="button" className="drama-back" onClick={() => navigate('/ai-apps/drama')}><ArrowLeftOutlined /> 返回 AI 漫剧</button><div className="drama-workspace-brand">漫剧创作台</div><div className="drama-workspace-nav">{navItems.map((item) => <button type="button" key={item.key} className={section === item.key ? 'active' : ''} onClick={() => setSection(item.key)}>{item.icon}<span>{item.label}</span>{project && item.key !== 'overview' && <small>{sectionCount(item.key)}</small>}</button>)}</div><div className="drama-sidebar-bottom"><span>创作流程</span><p>剧本解析 → 资产整理 → 分集创作 → 分镜生成</p></div></aside>
    <main className="drama-workspace-main">{loading ? <Skeleton active /> : !project ? <Empty description="项目不存在" /> : <>
      <header className="drama-workspace-header"><div><h1>{project.title}</h1><div className="drama-project-summary">{statusTag(project)}<span>{project.style}</span><span>{project.aspectRatio}</span><span>原文 {project.sourceLength.toLocaleString()} 字符</span></div></div><div className="drama-workspace-actions"><Button icon={<FileTextOutlined />} onClick={() => void showSource()}>{project.sourceMode === 'generate' ? '查看生成剧本' : '查看完整原文'}</Button></div></header>
      {project.status !== 'ready' && <section className="drama-agent-panel"><div><RobotOutlined /><div><h2>{project.status === 'failed' ? '解析遇到问题' : '创作团队正在工作'}</h2><p>{project.status === 'failed' ? project.error : '编剧、美术和导演会依次读取故事，整理可供后续创作的项目资料。'}</p></div></div>{project.status !== 'failed' && <div className="drama-agent-steps">{['生成剧本', '故事理解', '资产整理', '分集规划'].map((name, index) => <span key={name} className={project.sourceMode === 'upload' && index === 0 ? 'skipped' : phaseIndex >= index ? 'done' : ''}>{index + 1}. {name}</span>)}</div>}</section>}
      <div className="drama-section-head"><h2>{navItems.find((item) => item.key === section)?.label}</h2>{section === 'overview' && !editing && <Button onClick={() => { setDraft({ ...project.overview }); setEditing(true) }}>编辑概览</Button>}{section === 'overview' && editing && <div><Button onClick={() => setEditing(false)}>取消</Button> <Button type="primary" loading={saving} onClick={() => void save()}>保存修改</Button></div>}</div>
      {section === 'overview' ? <div className="drama-overview-grid">{overviewFields.map((field) => <div key={field.key} className={field.multiline ? 'wide' : ''}><span>{field.label}</span>{editing ? field.multiline ? <Input.TextArea rows={4} value={draft[field.key] ?? ''} onChange={(event) => setDraft((current) => ({ ...current, [field.key]: event.target.value }))} /> : <Input value={draft[field.key] ?? ''} onChange={(event) => setDraft((current) => ({ ...current, [field.key]: event.target.value }))} /> : <p>{project.overview?.[field.key] || (field.key === 'videoStyle' ? project.style : field.key === 'aspectRatio' ? project.aspectRatio : '等待解析结果')}</p>}</div>)}</div> : section === 'episodes' ? project.episodes.length === 0 ? <div className="drama-empty small"><Empty description="导演完成分析后，分集会出现在这里" /></div> : <div className="drama-episode-list">{project.episodes.map((episode, index) => <Card key={`${episode.title}-${index}`}><span>EPISODE {String(index + 1).padStart(2, '0')}</span><h3>{episode.title}</h3><p>{episode.synopsis}</p></Card>)}</div> : assets.length === 0 ? <div className="drama-empty small"><Empty description="美术完成分析后，相关资产会出现在这里" /></div> : <div className="drama-asset-grid">{assets.map((item, index) => <Card key={`${item.name}-${index}`}><div className="drama-asset-icon">{section === 'characters' ? <TeamOutlined /> : section === 'scenes' ? <BgColorsOutlined /> : <SettingOutlined />}</div><Typography.Title level={4}>{item.name}</Typography.Title><p>{item.description || '暂无描述'}</p></Card>)}</div>}
    </>}</main>
    <Drawer title={project?.sourceMode === 'generate' ? '生成剧本' : '完整原文'} open={sourceOpen} onClose={() => setSourceOpen(false)} width={760}>{sourceLoading ? <Skeleton active /> : <pre className="drama-source-text">{sourceText}</pre>}</Drawer>
  </div>
}
