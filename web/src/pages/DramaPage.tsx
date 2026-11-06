import {
  ArrowLeftOutlined, ArrowRightOutlined, FileTextOutlined, OrderedListOutlined,
  TeamOutlined, VideoCameraOutlined,
} from '@ant-design/icons'
import { Alert, Button, Drawer, Empty, Skeleton, Tag, message } from 'antd'
import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'

import {
  getDramaProject, getDramaSource, listDramaBatches, reanalyzeDramaOverview, renameDramaProject, retryDramaProject,
  type DramaBatch, type DramaCoverGeneration, type DramaProject,
} from '../api/drama'
import { DramaVideoEpisodes } from './DramaVideoEpisodes'
import { DramaVideoEpisodeStudio } from './DramaVideoEpisodeStudio'
import { DramaAssetCanvas } from './DramaAssetCanvas'
import { DramaProjectAssets } from './DramaProjectAssets'
import { DramaBatchWorkspace } from './DramaBatchWorkspace'
import { DramaProjectOverview, selectedAspectRatio, type DramaOverviewChange } from './DramaProjectOverview'
import { DramaProjectTitle } from './DramaProjectTitle'
import { ProjectCoverBackdrop } from './ProjectCoverBackdrop'
import './drama.css'
import './drama-video.css'

const phaseNames: Record<string, string> = {
  waiting: '等待解析', script: '编剧正在生成剧本', story: '正在理解故事',
  assets: '正在整理角色、场景和道具', episodes: '正在规划分集', complete: '解析完成', overview_done: '概览已完成', failed: '解析失败',
}
type Section = 'overview' | 'episodes' | 'assets' | 'videoEpisodes'
const navItems: { key: Section; label: string; icon: ReactNode }[] = [
  { key: 'overview', label: '剧本概览', icon: <FileTextOutlined aria-hidden="true" /> },
  { key: 'episodes', label: '剧本分集', icon: <OrderedListOutlined aria-hidden="true" /> },
  { key: 'assets', label: '角色、场景、道具', icon: <TeamOutlined aria-hidden="true" /> },
  { key: 'videoEpisodes', label: '视频分集', icon: <VideoCameraOutlined aria-hidden="true" /> },
]

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
  const editRevision = useRef(0)
  const receiveProject = useCallback(() => {
    const revision = editRevision.current
    return (received: DramaProject) => {
      setProject((current) => revision !== editRevision.current && current?.id === received.id ? {
        ...received, title: current.title, style: current.style, styleId: current.styleId,
        aspectRatio: current.aspectRatio, overview: current.overview,
      } : received)
    }
  }, [])
  const [batches, setBatches] = useState<DramaBatch[]>([])
  const [loading, setLoading] = useState(true)
  const requestedSection = searchParams.get('section')
  const section: Section = navItems.some((item) => item.key === requestedSection) ? requestedSection as Section : 'overview'
  function selectSection(value: Section) {
    setSearchParams((current) => {
      const next = new URLSearchParams(current)
      next.delete('episode')
      if (value === 'overview') next.delete('section')
      else next.set('section', value)
      return next
    })
  }
  const [retrying, setRetrying] = useState(false)
  const [reanalyzing, setReanalyzing] = useState(false)
  const [sourceOpen, setSourceOpen] = useState(false)
  const [sourceLoading, setSourceLoading] = useState(false)
  const [sourceText, setSourceText] = useState('')
  const syncCover = useCallback((state: DramaCoverGeneration) => {
    const coverGenerationStatus = state.status === 'running' ? state.status : undefined
    setProject((current) => {
      if (!current || current.id !== id || (current.coverUrl === state.coverUrl && current.coverGenerationStatus === coverGenerationStatus)) return current
      return { ...current, coverUrl: state.coverUrl, coverGenerationStatus }
    })
  }, [id])

  useEffect(() => {
    let alive = true
    const receive = receiveProject()
    getDramaProject(id).then((item) => { if (alive) receive(item) })
      .catch((error) => { if (alive) messageApi.error(error instanceof Error ? error.message : '读取项目失败') })
      .finally(() => { if (alive) setLoading(false) })
    return () => { alive = false }
  }, [id, messageApi, receiveProject])
  useEffect(() => {
    let alive = true
    listDramaBatches(id).then((items) => { if (alive) setBatches(items) })
      .catch((error) => { if (alive) messageApi.error(error instanceof Error ? error.message : '读取剧本批次失败') })
    return () => { alive = false }
  }, [id, messageApi])
  useEffect(() => {
    const analyzing = project?.status === 'queued' || project?.status === 'running'
    const generatingCover = section !== 'overview' && project?.coverGenerationStatus === 'running'
    if (!analyzing && !generatingCover) return
    let alive = true
    const timer = window.setInterval(() => {
      const receive = receiveProject()
      getDramaProject(id).then((item) => { if (alive) receive(item) }).catch(() => {})
    }, 3000)
    return () => { alive = false; window.clearInterval(timer) }
  }, [id, project?.status, project?.coverGenerationStatus, section, receiveProject])

  useEffect(() => {
    if (section !== 'assets') return
    let active = true
    const receive = receiveProject()
    void getDramaProject(id).then((item) => { if (active) receive(item) }).catch((error) => { if (active) messageApi.error(error instanceof Error ? error.message : '刷新资产来源失败') })
    return () => { active = false }
  }, [id, section, messageApi, receiveProject])
  async function retry() {
    if (!project) return
    setRetrying(true)
    const receive = receiveProject()
    try { receive(await retryDramaProject(project.id)); messageApi.success('已重新开始概览解析') }
    catch (error) { messageApi.error(error instanceof Error ? error.message : '重新解析失败') }
    finally { setRetrying(false) }
  }
  async function reanalyze() {
    if (!project) return
    setReanalyzing(true)
    const receive = receiveProject()
    try { receive(await reanalyzeDramaOverview(project.id)); messageApi.success('已开始重新解析概览') }
    catch (error) { messageApi.error(error instanceof Error ? error.message : '重新解析失败') }
    finally { setReanalyzing(false) }
  }

  function commitProjectEdit(projectId: string, change: DramaOverviewChange & Partial<Pick<DramaProject, 'title'>>) {
    editRevision.current++
    setProject((current) => current?.id === projectId
      ? { ...current, ...change, overview: { ...current.overview, ...change.overview } }
      : current)
  }

  async function rename(title: string) {
    if (project?.id !== id) throw new Error('项目正在切换，请稍后再修改标题')
    const result = await renameDramaProject(id, title)
    commitProjectEdit(id, result)
  }
  async function showSource() {
    if (!project) return
    setSourceOpen(true)
    setSourceLoading(true)
    try { const result = await getDramaSource(project.id); setSourceText(result.sourceText) }
    catch (error) { messageApi.error(error instanceof Error ? error.message : '读取剧本失败') }
    finally { setSourceLoading(false) }
  }

  const overview = project?.overview ?? {}
  const coverURL = project?.coverUrl
  if (searchParams.get('canvas') === '1' && project) return <DramaAssetCanvas project={project} onRenameProject={rename} />
  const videoEpisodeId = searchParams.get('episode')
  if (section === 'videoEpisodes' && videoEpisodeId && project) return <>{contextHolder}<DramaVideoEpisodeStudio project={project} episodeId={videoEpisodeId} /></>
  return <div className="drama-workspace">
    {contextHolder}
    <aside className="drama-workspace-sidebar">
      <button type="button" className="drama-back" onClick={() => navigate('/ai-apps/drama')}><ArrowLeftOutlined aria-hidden="true" /> 返回 AI 漫剧</button>
      <div className="drama-workspace-brand">漫剧创作台</div>
      <nav className="drama-workspace-nav" aria-label="漫剧项目内容">
        {navItems.map((item) => <button type="button" key={item.key} aria-current={section === item.key ? 'page' : undefined}
          className={section === item.key ? 'active' : ''} onClick={() => selectSection(item.key)}>
          {item.icon}<span>{item.label}</span>
        </button>)}
      </nav>
    </aside>
    <main className={coverURL ? 'drama-workspace-main drama-workspace-with-cover' : 'drama-workspace-main'}>
      {coverURL && <ProjectCoverBackdrop url={coverURL} />}
      {loading ? <Skeleton active /> : !project ? <Empty description="项目不存在" /> : <>
      <header className="drama-workspace-header">
        <div className="drama-workspace-heading">
          <h1><DramaProjectTitle key={project.id} title={project.title} onRename={rename} /></h1>
          <div className="drama-project-summary">
            {statusTag(project)}<span>{overview.videoStyle || project.style}</span><span>{selectedAspectRatio(project)}</span>
            <span>原文 {project.sourceLength.toLocaleString()} 字符</span>
            {project.episodes.length > 0 && <span>已解析 {project.episodes.length} 集</span>}
          </div>
        </div>
        <div className="drama-workspace-header-actions">
          <Button icon={<FileTextOutlined aria-hidden="true" />} onClick={() => void showSource()}>{project.sourceMode === 'generate' ? '查看生成剧本' : '查看完整原文'}</Button>
          {section === 'overview' && project.status === 'ready' && <Button type="primary" icon={<ArrowRightOutlined aria-hidden="true" />} iconPlacement="end"
            onClick={() => selectSection('episodes')}>前往剧本分集</Button>}
        </div>
      </header>
      {project.status === 'failed' && <Alert className="drama-project-analysis-error" type="error" showIcon title="概览解析失败"
        description={project.error} action={project.sourceMode === 'upload' && !overview.protagonist &&
          <Button size="small" loading={retrying} onClick={() => void retry()}>重新解析概览</Button>} />}

      {section === 'overview' ? <DramaProjectOverview key={project.id} project={project} reanalyzing={reanalyzing}
        onReanalyze={() => void reanalyze()} onChange={(change) => commitProjectEdit(project.id, change)} onCoverChange={syncCover} />
        : section === 'episodes' ? <DramaBatchWorkspace project={project} batches={batches} onBatchesChange={(items) => {
          setBatches(items)
          const receive = receiveProject()
          void getDramaProject(project.id).then(receive).catch(() => {})
        }} /> : section === 'assets' ? <DramaProjectAssets project={project} batches={batches} /> : <DramaVideoEpisodes projectId={project.id} />}
    </>}</main>
    <Drawer title={project?.sourceMode === 'generate' ? '生成剧本' : '完整原文'} open={sourceOpen} onClose={() => setSourceOpen(false)} size={760}>{sourceLoading ? <Skeleton active /> : <pre className="drama-source-text">{sourceText}</pre>}</Drawer>
  </div>
}
