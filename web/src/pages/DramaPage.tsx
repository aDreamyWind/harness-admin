import {
  ArrowLeftOutlined, ArrowRightOutlined, BgColorsOutlined, ClockCircleOutlined,
  CloudUploadOutlined, FileTextOutlined, FolderOpenOutlined, PlayCircleOutlined,
  RobotOutlined, SettingOutlined, TeamOutlined, VideoCameraOutlined,
} from '@ant-design/icons'
import { Button, Card, Drawer, Empty, Input, Segmented, Select, Skeleton, Tag, Typography, message } from 'antd'
import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { useNavigate, useParams } from 'react-router-dom'

import {
  createDramaProject, getDramaProject, getDramaSource, listDramaModels, listDramaProjects, updateDramaOverview,
  type DramaAsset, type DramaOverview, type DramaProject, type DramaModel,
} from '../api/drama'
import './drama.css'

const styles = ['国风水墨', '二维动漫', '三维动画', '写实电影', '赛博朋克', '奇幻绘本']
const phaseNames: Record<string, string> = {
  waiting: '等待解析', script: '编剧正在生成剧本', story: '编剧正在梳理故事',
  assets: '美术正在整理资产', episodes: '导演正在规划分集', complete: '解析完成', failed: '解析失败',
}

function statusTag(project: DramaProject) {
  if (project.status === 'ready') return <Tag color="success">可继续创作</Tag>
  if (project.status === 'failed') return <Tag color="error">解析失败</Tag>
  return <Tag color="processing">{phaseNames[project.phase] ?? '解析中'}</Tag>
}

export function DramaHomePage() {
  const navigate = useNavigate()
  const [messageApi, contextHolder] = message.useMessage()
  const [models, setModels] = useState<DramaModel[]>([])
  const [projects, setProjects] = useState<DramaProject[]>([])
  const [loading, setLoading] = useState(true)
  const [submitting, setSubmitting] = useState(false)
  const [drawer, setDrawer] = useState<'tasks' | 'assets' | null>(null)
  const [mode, setMode] = useState<'upload' | 'generate'>('upload')
  const [title, setTitle] = useState('')
  const [sourceName, setSourceName] = useState('')
  const [sourceText, setSourceText] = useState('')
  const [style, setStyle] = useState(styles[0])
  const [ratio, setRatio] = useState<'9:16' | '16:9'>('9:16')
  const [modelCode, setModelCode] = useState('')

  useEffect(() => {
    let alive = true
    Promise.all([listDramaModels(), listDramaProjects()]).then(([available, items]) => {
      if (!alive) return
      setModels(available)
      setProjects(items)
      setModelCode((current) => current || available[0]?.code || '')
    }).catch((error) => { if (alive) messageApi.error(error instanceof Error ? error.message : '读取漫剧项目失败') })
      .finally(() => { if (alive) setLoading(false) })
    return () => { alive = false }
  }, [messageApi])

  useEffect(() => {
    if (!projects.some((item) => item.status === 'queued' || item.status === 'running')) return
    const timer = window.setInterval(() => { listDramaProjects().then(setProjects).catch(() => {}) }, 4000)
    return () => window.clearInterval(timer)
  }, [projects])

  async function readFile(file: File) {
    if (!/\.(txt|md)$/i.test(file.name)) {
      messageApi.error('当前版本支持 TXT、MD 文本文件，也可以直接粘贴原文')
      return
    }
    try {
      const content = await file.text()
      if (!content.trim()) { messageApi.error('文件内容为空'); return }
      setSourceText(content)
      setSourceName(file.name)
      setTitle((current) => current || file.name.replace(/\.[^.]+$/, ''))
    } catch { messageApi.error('读取文件失败') }
  }

  async function start() {
    if (!title.trim() || !sourceText.trim()) { messageApi.warning('请填写项目名称和原文或创意'); return }
    if (!modelCode) { messageApi.warning('请先选择已启用的剧本模型'); return }
    setSubmitting(true)
    try {
      const project = await createDramaProject({ title, sourceName: mode === 'generate' ? '' : sourceName, sourceText, sourceMode: mode, style, aspectRatio: ratio, modelCode })
      navigate(`/ai-apps/drama/projects/${project.id}`)
    } catch (error) { messageApi.error(error instanceof Error ? error.message : '创建项目失败') }
    finally { setSubmitting(false) }
  }

  const assets = useMemo(() => projects.flatMap((project) => [
    ...project.characters.map((item) => ({ ...item, kind: '角色', project: project.title })),
    ...project.scenes.map((item) => ({ ...item, kind: '场景', project: project.title })),
    ...project.props.map((item) => ({ ...item, kind: '道具', project: project.title })),
  ]), [projects])

  return <main className="drama-home">
    {contextHolder}
    <div className="drama-home-head">
      <div><span className="drama-eyebrow">AI APPLICATION / STORY STUDIO</span><h1>AI 漫剧</h1><p>从故事出发，让每一幕有形可见。</p></div>
      <Button icon={<ClockCircleOutlined />} onClick={() => setDrawer('tasks')}>生成任务{projects.filter((item) => item.status === 'queued' || item.status === 'running').length > 0 ? ` · ${projects.filter((item) => item.status === 'queued' || item.status === 'running').length}` : ''}</Button>
    </div>
    <section className="drama-create-panel" aria-label="创建漫剧项目">
      <div className="drama-create-intro"><span className="drama-step">01 / 开始创作</span><h2>把一个故事，变成一部漫剧</h2><p>上传小说或剧本，也可以写下创意，让 AI 编剧先生成第一集剧本。原文完整保存，解析取开头三万字。</p></div>
      <div className="drama-mode"><Button type={mode === 'upload' ? 'primary' : 'text'} icon={<CloudUploadOutlined />} onClick={() => setMode('upload')}>上传小说 / 剧本</Button><Button type={mode === 'generate' ? 'primary' : 'text'} icon={<RobotOutlined />} onClick={() => setMode('generate')}>AI 生成剧本</Button></div>
      <div className="drama-form-grid">
        <div className="drama-main-input">
          <label htmlFor="drama-title">项目名称</label>
          <Input id="drama-title" size="large" placeholder="给你的漫剧起个名字" value={title} maxLength={120} onChange={(event) => setTitle(event.target.value)} />
          <div className="drama-field-label"><label htmlFor="drama-source">{mode === 'upload' ? '故事原文' : '故事创意'}</label>{mode === 'upload' && <label className="drama-upload-link"><CloudUploadOutlined /> 选择 TXT / MD 文件<input type="file" accept=".txt,.md,text/plain,text/markdown" onChange={(event) => { const file = event.target.files?.[0]; if (file) void readFile(file); event.target.value = '' }} /></label>}</div>
          <Input.TextArea id="drama-source" rows={7} value={sourceText} onChange={(event) => setSourceText(event.target.value)} placeholder={mode === 'upload' ? '粘贴完整原文，或选择文件上传。这里不限制字数。' : '例如：一个失忆的画师发现自己能走进每一幅画，却在最后一幅画里看见了未来的自己……'} />
          <span className="drama-helper">{sourceName ? `${sourceName} · ` : ''}{sourceText.length.toLocaleString()} 字符{mode === 'upload' ? ' · 原文完整保存' : ' · 将先由 AI 生成第一集剧本'}</span>
        </div>
        <div className="drama-options">
          <label>视频风格</label>
          <Select size="large" value={style} options={styles.map((item) => ({ label: item, value: item }))} onChange={setStyle} />
          <label>画面比例</label>
          <Segmented block size="large" options={[{ label: '竖屏 9:16', value: '9:16' }, { label: '横屏 16:9', value: '16:9' }]} value={ratio} onChange={(value) => setRatio(value as '9:16' | '16:9')} />
          <label>剧本解析模型</label>
          <Select size="large" value={modelCode || undefined} placeholder="选择模型目录中已启用的模型" options={models.map((item) => ({ label: item.name, value: item.code }))} onChange={setModelCode} notFoundContent="暂无可调用的对话模型，请先在模型策略中授权" />
          <Button className="drama-start" type="primary" size="large" icon={<PlayCircleOutlined />} loading={submitting} onClick={() => void start()}>开始解析 <ArrowRightOutlined /></Button>
        </div>
      </div>
    </section>
    <div className="drama-library-head"><div><span className="drama-step">02 / 创作空间</span><h2>你的故事，都在这里</h2></div><div className="drama-library-tabs"><Button type="text" onClick={() => setDrawer('assets')}>我的资产 <span>{assets.length}</span></Button><Button type="text" onClick={() => setDrawer('tasks')}>全部任务 <ArrowRightOutlined /></Button></div></div>
    {loading ? <Skeleton active /> : projects.length === 0 ? <div className="drama-empty"><FolderOpenOutlined /><h3>从第一个故事开始</h3><p>项目创建后会在这里出现，你可以随时继续创作。</p></div> : <div className="drama-project-grid">{projects.map((project) => <Card key={project.id} className="drama-project-card" variant="borderless" onClick={() => navigate(`/ai-apps/drama/projects/${project.id}`)} hoverable><div className="drama-project-art"><span>{project.aspectRatio}</span><VideoCameraOutlined /></div><div className="drama-project-meta">{statusTag(project)}<span>{new Date(project.updatedAt).toLocaleDateString('zh-CN')}</span></div><h3>{project.title}</h3><p>{project.overview?.synopsis || (project.sourceMode === 'generate' ? 'AI 正在把创意写成剧本' : '剧本解析后将在这里呈现故事梗概')}</p><div className="drama-project-foot"><span>{project.episodes.length} 集 · {project.characters.length} 个角色</span><ArrowRightOutlined /></div></Card>)}</div>}
    <Drawer title="生成任务" open={drawer === 'tasks'} onClose={() => setDrawer(null)} width={420}>{projects.length === 0 ? <Empty description="暂无生成任务" /> : <div className="drama-drawer-list">{projects.map((project) => <button key={project.id} type="button" onClick={() => { setDrawer(null); navigate(`/ai-apps/drama/projects/${project.id}`) }}><strong>{project.title}</strong>{statusTag(project)}<small>{phaseNames[project.phase] ?? project.phase}</small></button>)}</div>}</Drawer>
    <Drawer title={`我的资产 · ${assets.length}`} open={drawer === 'assets'} onClose={() => setDrawer(null)} width={420}>{assets.length === 0 ? <Empty description="解析完成后，角色、场景和道具会出现在这里" /> : <div className="drama-drawer-list">{assets.map((item, index) => <div key={`${item.project}-${item.kind}-${index}`}><strong>{item.name}</strong><Tag>{item.kind}</Tag><small>{item.project} · {item.description}</small></div>)}</div>}</Drawer>
  </main>
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
    <aside className="drama-workspace-sidebar"><button type="button" className="drama-back" onClick={() => navigate('/ai-apps/drama')}><ArrowLeftOutlined /> 返回 AI 漫剧</button><div className="drama-workspace-brand"><span className="drama-brand-symbol">✦</span><span>漫剧创作台<small>STORY STUDIO</small></span></div><div className="drama-workspace-nav">{navItems.map((item) => <button type="button" key={item.key} className={section === item.key ? 'active' : ''} onClick={() => setSection(item.key)}>{item.icon}<span>{item.label}</span>{project && item.key !== 'overview' && <small>{sectionCount(item.key)}</small>}</button>)}</div><div className="drama-sidebar-bottom"><span>创作流程</span><p>剧本解析 → 资产整理 → 分集创作 → 分镜生成</p></div></aside>
    <main className="drama-workspace-main">{loading ? <Skeleton active /> : !project ? <Empty description="项目不存在" /> : <>
      <header className="drama-workspace-header"><div><span className="drama-eyebrow">PROJECT WORKSPACE</span><h1>{project.title}</h1><div className="drama-project-summary">{statusTag(project)}<span>{project.style}</span><span>{project.aspectRatio}</span><span>原文 {project.sourceLength.toLocaleString()} 字符</span></div></div><div className="drama-workspace-actions"><Button icon={<FileTextOutlined />} onClick={() => void showSource()}>{project.sourceMode === 'generate' ? '查看生成剧本' : '查看完整原文'}</Button></div></header>
      {project.status !== 'ready' && <section className="drama-agent-panel"><div><RobotOutlined /><div><h2>{project.status === 'failed' ? '解析遇到问题' : '创作团队正在工作'}</h2><p>{project.status === 'failed' ? project.error : '编剧、美术和导演会依次读取故事，整理可供后续创作的项目资料。'}</p></div></div>{project.status !== 'failed' && <div className="drama-agent-steps">{['生成剧本', '故事理解', '资产整理', '分集规划'].map((name, index) => <span key={name} className={project.sourceMode === 'upload' && index === 0 ? 'skipped' : phaseIndex >= index ? 'done' : ''}>{index + 1}. {name}</span>)}</div>}</section>}
      <div className="drama-section-head"><div><span className="drama-step">{navItems.find((item) => item.key === section)?.label}</span><h2>{section === 'overview' ? '故事的起点' : section === 'episodes' ? '分集规划' : `${navItems.find((item) => item.key === section)?.label}资产`}</h2></div>{section === 'overview' && !editing && <Button onClick={() => { setDraft({ ...project.overview }); setEditing(true) }}>编辑概览</Button>}{section === 'overview' && editing && <div><Button onClick={() => setEditing(false)}>取消</Button> <Button type="primary" loading={saving} onClick={() => void save()}>保存修改</Button></div>}</div>
      {section === 'overview' ? <div className="drama-overview-grid">{overviewFields.map((field) => <div key={field.key} className={field.multiline ? 'wide' : ''}><span>{field.label}</span>{editing ? field.multiline ? <Input.TextArea rows={4} value={draft[field.key] ?? ''} onChange={(event) => setDraft((current) => ({ ...current, [field.key]: event.target.value }))} /> : <Input value={draft[field.key] ?? ''} onChange={(event) => setDraft((current) => ({ ...current, [field.key]: event.target.value }))} /> : <p>{project.overview?.[field.key] || (field.key === 'videoStyle' ? project.style : field.key === 'aspectRatio' ? project.aspectRatio : '等待解析结果')}</p>}</div>)}</div> : section === 'episodes' ? project.episodes.length === 0 ? <div className="drama-empty small"><Empty description="导演完成分析后，分集会出现在这里" /></div> : <div className="drama-episode-list">{project.episodes.map((episode, index) => <Card key={`${episode.title}-${index}`}><span>EPISODE {String(index + 1).padStart(2, '0')}</span><h3>{episode.title}</h3><p>{episode.synopsis}</p></Card>)}</div> : assets.length === 0 ? <div className="drama-empty small"><Empty description="美术完成分析后，相关资产会出现在这里" /></div> : <div className="drama-asset-grid">{assets.map((item, index) => <Card key={`${item.name}-${index}`}><div className="drama-asset-icon">{section === 'characters' ? <TeamOutlined /> : section === 'scenes' ? <BgColorsOutlined /> : <SettingOutlined />}</div><Typography.Title level={4}>{item.name}</Typography.Title><p>{item.description || '暂无描述'}</p></Card>)}</div>}
    </>}</main>
    <Drawer title={project?.sourceMode === 'generate' ? '生成剧本' : '完整原文'} open={sourceOpen} onClose={() => setSourceOpen(false)} width={760}>{sourceLoading ? <Skeleton active /> : <pre className="drama-source-text">{sourceText}</pre>}</Drawer>
  </div>
}
