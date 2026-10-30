import {
  ArrowLeftOutlined, FileTextOutlined, OrderedListOutlined,
  TeamOutlined, VideoCameraOutlined,
} from '@ant-design/icons'
import { Button, Drawer, Empty, Input, Select, Skeleton, Tag, message } from 'antd'
import { useEffect, useState, type ReactNode } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'

import {
  getDramaProject, getDramaSource, listDramaBatches, reanalyzeDramaOverview, retryDramaProject, updateDramaOverview,
  type DramaAsset, type DramaBatch, type DramaOverview, type DramaProject,
} from '../api/drama'
import { DramaVideoEpisodes } from './DramaVideoEpisodes'
import { DramaVideoEpisodeStudio } from './DramaVideoEpisodeStudio'
import { DramaAssetGallery } from './DramaAssetGallery'
import { DramaBatchWorkspace } from './DramaBatchWorkspace'
import './drama.css'
import './drama-video.css'

const phaseNames: Record<string, string> = {
  waiting: '等待解析', script: '编剧正在生成剧本', story: '正在理解故事',
  assets: '正在整理角色、场景和道具', episodes: '正在规划分集', complete: '解析完成', overview_done: '概览已完成', failed: '解析失败',
}
const stylePresets = ['电影写实', '国风水墨', '日系动漫', '三维动画', '暗黑漫画', '奇幻绘本', '美式漫画', '怀旧动画', '赛博霓虹']
const ratioOptions = [{ value: '9:16', label: '竖屏 9:16' }, { value: '16:9', label: '横屏 16:9' }]

type Section = 'overview' | 'episodes' | 'assets' | 'videoEpisodes'
type AssetSection = 'characters' | 'scenes' | 'props'
const navItems: { key: Section; label: string; icon: ReactNode }[] = [
  { key: 'overview', label: '剧本概览', icon: <FileTextOutlined /> },
  { key: 'episodes', label: '剧本分集', icon: <OrderedListOutlined /> },
  { key: 'assets', label: '角色、场景、道具', icon: <TeamOutlined /> },
  { key: 'videoEpisodes', label: '视频分集', icon: <VideoCameraOutlined /> },
]

function selectedAspectRatio(project: DramaProject) {
  return ratioOptions.some((item) => item.value === project.overview?.aspectRatio) ? project.overview.aspectRatio : project.aspectRatio
}

function statusTag(project: DramaProject) {
  if (project.status === 'ready') return <Tag color="success">概览已完成</Tag>
  if (project.status === 'failed') return <Tag color="error">解析失败</Tag>
  return <Tag color="processing">{phaseNames[project.phase] ?? '解析中'}</Tag>
}

export function DramaProjectPage() {
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const [messageApi, contextHolder] = message.useMessage()
  const [project, setProject] = useState<DramaProject | null>(null)
  const [batches, setBatches] = useState<DramaBatch[]>([])
  const [loading, setLoading] = useState(true)
  const requestedSection = searchParams.get('section')
  const section: Section = navItems.some((item) => item.key === requestedSection) ? requestedSection as Section : 'overview'
  const requestedAssetSection = searchParams.get('asset')
  const assetSection: AssetSection = requestedAssetSection === 'scenes' || requestedAssetSection === 'props' ? requestedAssetSection : 'characters'
  function selectSection(value: Section) {
    setSearchParams((current) => {
      const next = new URLSearchParams(current)
      next.delete('episode')
      if (value === 'overview') next.delete('section')
      else next.set('section', value)
      return next
    })
    setEditing(false)
  }
  function selectAssetSection(value: AssetSection) {
    setSearchParams((current) => {
      const next = new URLSearchParams(current)
      if (value === 'characters') next.delete('asset')
      else next.set('asset', value)
      return next
    })
  }
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState<DramaOverview>({})
  const [saving, setSaving] = useState(false)
  const [retrying, setRetrying] = useState(false)
  const [reanalyzing, setReanalyzing] = useState(false)
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
    let alive = true
    listDramaBatches(id).then((items) => { if (alive) setBatches(items) })
      .catch((error) => { if (alive) messageApi.error(error instanceof Error ? error.message : '读取剧本批次失败') })
    return () => { alive = false }
  }, [id, messageApi])
  useEffect(() => {
    if (!project || (project.status !== 'queued' && project.status !== 'running')) return
    const timer = window.setInterval(() => { getDramaProject(id).then(setProject).catch(() => {}) }, 3000)
    return () => window.clearInterval(timer)
  }, [id, project])

  useEffect(() => {
    if (section !== 'assets') return
    let active = true
    void getDramaProject(id).then((item) => { if (active) setProject(item) }).catch((error) => { if (active) messageApi.error(error instanceof Error ? error.message : '刷新资产来源失败') })
    return () => { active = false }
  }, [id, section, messageApi])
  function beginEditing() {
    if (!project) return
    setDraft({ ...project.overview, videoStyle: project.overview?.videoStyle || project.style, aspectRatio: selectedAspectRatio(project) })
    setEditing(true)
  }
  function changeDraft(key: keyof DramaOverview, value: string) {
    setDraft((current) => ({ ...current, [key]: value }))
  }
  function changeSelection(key: 'videoStyle' | 'aspectRatio', value: string) {
    if (!project) return
    if (!editing) {
      setDraft({ ...project.overview, videoStyle: project.overview?.videoStyle || project.style, aspectRatio: selectedAspectRatio(project), [key]: value })
      setEditing(true)
      return
    }
    changeDraft(key, value)
  }
  async function save() {
    if (!project) return
    setSaving(true)
    try { setProject(await updateDramaOverview(project.id, draft)); setEditing(false); messageApi.success('项目概览已保存') }
    catch (error) { messageApi.error(error instanceof Error ? error.message : '保存失败') }
    finally { setSaving(false) }
  }
  async function retry() {
    if (!project) return
    setRetrying(true)
    try { setProject(await retryDramaProject(project.id)); messageApi.success('已重新开始概览解析') }
    catch (error) { messageApi.error(error instanceof Error ? error.message : '重新解析失败') }
    finally { setRetrying(false) }
  }
  async function reanalyze() {
    if (!project) return
    setReanalyzing(true)
    try { setProject(await reanalyzeDramaOverview(project.id)); messageApi.success('已开始重新解析概览') }
    catch (error) { messageApi.error(error instanceof Error ? error.message : '重新解析失败') }
    finally { setReanalyzing(false) }
  }
  async function showSource() {
    if (!project) return
    setSourceOpen(true)
    setSourceLoading(true)
    try { const result = await getDramaSource(project.id); setSourceText(result.sourceText) }
    catch (error) { messageApi.error(error instanceof Error ? error.message : '读取剧本失败') }
    finally { setSourceLoading(false) }
  }

  const assets: DramaAsset[] = project ? project[assetSection] : []
  const overview = project?.overview ?? {}
  const display = (key: keyof DramaOverview, fallback = '等待解析结果') => overview[key]?.trim() || fallback
  const styleOptions = Array.from(new Set([...stylePresets, project?.style, overview.videoStyle].filter((item): item is string => Boolean(item)))).map((item) => ({ value: item, label: item }))
  const videoEpisodeId = searchParams.get('episode')
  if (section === 'videoEpisodes' && videoEpisodeId && project) return <>{contextHolder}<DramaVideoEpisodeStudio project={project} episodeId={videoEpisodeId} /></>
  return <div className="drama-workspace">
    {contextHolder}
    <aside className="drama-workspace-sidebar">
      <button type="button" className="drama-back" onClick={() => navigate('/ai-apps/drama')}><ArrowLeftOutlined /> 返回 AI 漫剧</button>
      <div className="drama-workspace-brand">漫剧创作台</div>
      <nav className="drama-workspace-nav" aria-label="漫剧项目内容">
        {navItems.map((item) => <button type="button" key={item.key} className={section === item.key ? 'active' : ''} onClick={() => selectSection(item.key)}>
          {item.icon}<span>{item.label}</span>
        </button>)}
      </nav>
    </aside>
    <main className="drama-workspace-main">{loading ? <Skeleton active /> : !project ? <Empty description="项目不存在" /> : <>
      <header className="drama-workspace-header">
        <div><h1>{project.title}</h1><div className="drama-project-summary">{statusTag(project)}<span>{overview.videoStyle || project.style}</span><span>{selectedAspectRatio(project)}</span><span>原文 {project.sourceLength.toLocaleString()} 字符</span></div></div>
        <Button icon={<FileTextOutlined />} onClick={() => void showSource()}>{project.sourceMode === 'generate' ? '查看生成剧本' : '查看完整原文'}</Button>
      </header>
      <section className="drama-workflow" aria-label="剧本处理流程">
        <div className={`drama-workflow-step ${project.status === 'ready' ? 'complete' : project.status === 'failed' ? 'failed' : 'active'}`}>
          <span className="drama-workflow-number">01</span>
          <div className="drama-workflow-body"><div className="drama-workflow-title"><strong>概览解析</strong><span>{project.status === 'ready' ? '已完成' : project.status === 'failed' ? '需要重试' : '进行中'}</span></div>
            <p>创建项目后自动读取原文前 10 万字，整理主角、故事类型、梗概与背景设定。</p>
            {project.status === 'failed' && <div className="drama-workflow-error">{project.error}{project.sourceMode === 'upload' && !overview.protagonist && <Button size="small" loading={retrying} onClick={() => void retry()}>重新解析概览</Button>}</div>}
          </div>
        </div>
        <div className="drama-workflow-step pending">
          <span className="drama-workflow-number">02</span>
          <div className="drama-workflow-body"><div className="drama-workflow-title"><strong>剧本解析</strong><span>美术指导与导演</span></div>
            <p>每批最多 10 万字，终点取范围内最接近上限的下一章或下一集开头；若没有标题，再找段落结尾。AI先提取角色、场景、道具，再拆分剧集并生成剧本。</p>
          </div>
        </div>
      </section>

      {section === 'overview' ? <section className="drama-overview" aria-label="剧本概览内容">
        <div className="drama-overview-toolbar">
          <div className="drama-overview-controls">
            <label><span>视频风格</span><Select value={editing ? draft.videoStyle : display('videoStyle', project.style)} options={styleOptions} onChange={(value) => changeSelection('videoStyle', value)} /></label>
            <label><span>画面比例</span><Select value={editing ? draft.aspectRatio : selectedAspectRatio(project)} options={ratioOptions} onChange={(value) => changeSelection('aspectRatio', value)} /></label>
          </div>
          <div className="drama-overview-actions">{editing ? <><Button onClick={() => setEditing(false)}>取消</Button><Button type="primary" loading={saving} onClick={() => void save()}>保存修改</Button></> : <><Button loading={reanalyzing} disabled={project.status === 'queued' || project.status === 'running' || (project.status === 'failed' && !overview.protagonist)} onClick={() => void reanalyze()}>AI重新解析</Button><Button onClick={beginEditing}>编辑概览</Button></>}</div>
        </div>
        <article className="drama-synopsis-card">
          <div className="drama-story-facts">
            <div><span className="drama-field-label">主角</span>{editing ? <Input value={draft.protagonist ?? ''} onChange={(event) => changeDraft('protagonist', event.target.value)} /> : <strong>{display('protagonist')}</strong>}</div>
            <div><span className="drama-field-label">故事类型</span>{editing ? <Input value={draft.genre ?? ''} onChange={(event) => changeDraft('genre', event.target.value)} /> : <strong>{display('genre')}</strong>}</div>
          </div>
          <span className="drama-field-label">故事梗概</span>
          {editing ? <Input.TextArea autoSize={{ minRows: 5, maxRows: 12 }} value={draft.synopsis ?? ''} onChange={(event) => changeDraft('synopsis', event.target.value)} /> : <p className="drama-synopsis-text">{display('synopsis')}</p>}
        </article>
        <div className="drama-overview-panels">
          {([{ key: 'background', label: '故事背景' }, { key: 'setting', label: '故事设定' }] as const).map((field) => <article key={field.key} className="drama-overview-panel"><span className="drama-field-label">{field.label}</span>{editing ? <Input.TextArea autoSize={{ minRows: 5, maxRows: 12 }} value={draft[field.key] ?? ''} onChange={(event) => changeDraft(field.key, event.target.value)} /> : <p>{display(field.key)}</p>}</article>)}
        </div>
      </section> : section === 'episodes' ? <DramaBatchWorkspace project={project} batches={batches} onBatchesChange={(items) => { setBatches(items); void getDramaProject(project.id).then(setProject).catch(() => {}) }} /> : section === 'assets' ? <section aria-label="角色、场景、道具">
        <div className="drama-asset-tabs" role="tablist" aria-label="资产类型">
          {([{ key: 'characters', label: '角色', count: project.characters.length }, { key: 'scenes', label: '场景', count: project.scenes.length }, { key: 'props', label: '道具', count: project.props.length }] as const).map((item) =>
            <button key={item.key} type="button" role="tab" aria-selected={assetSection === item.key} className={assetSection === item.key ? 'active' : ''} onClick={() => selectAssetSection(item.key)}>{item.label}<span>{item.count}</span></button>
          )}
        </div>
        <div role="tabpanel" aria-label={assetSection === 'characters' ? '角色' : assetSection === 'scenes' ? '场景' : '道具'}>
          {assets.length === 0 ? <div className="drama-empty"><Empty description="剧本解析完成后，内容会出现在这里" /></div> : <DramaAssetGallery projectId={project.id} kind={assetSection === 'characters' ? 'character' : assetSection === 'scenes' ? 'scene' : 'prop'} assets={assets} />}
        </div>
      </section> : <DramaVideoEpisodes projectId={project.id} />}
    </>}</main>
    <Drawer title={project?.sourceMode === 'generate' ? '生成剧本' : '完整原文'} open={sourceOpen} onClose={() => setSourceOpen(false)} size={760}>{sourceLoading ? <Skeleton active /> : <pre className="drama-source-text">{sourceText}</pre>}</Drawer>
  </div>
}
