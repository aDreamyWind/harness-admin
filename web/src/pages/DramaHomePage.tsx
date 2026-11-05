import {
  BookOutlined, ClockCircleOutlined, CloudUploadOutlined, DeleteOutlined,
  FileTextOutlined, FolderOpenOutlined, PictureOutlined,
  SettingOutlined, VideoCameraOutlined,
} from '@ant-design/icons'
import { Button, Card, Drawer, Empty, Input, Segmented, Select, Skeleton, Tag, message } from 'antd'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'

import {
  createDramaProject, createDramaProjectFromNovel, getDramaSettings, listDramaModels, listDramaProjects, saveDramaSettings,
  type DramaAsset, type DramaAssetKind, type DramaModel, type DramaProject, type DramaSettings,
} from '../api/drama'
import { DramaStylePicker } from './DramaStylePicker'
import { NovelCreateDialog } from './NovelCreateDialog'
import { NovelDramaSource, type NovelDramaSelection } from './NovelDramaSource'
import './drama-home.css'

const emptySettings: DramaSettings = { textModelCode: '', imageModelCode: '', videoModelCode: '' }
const phaseNames: Record<string, string> = {
  waiting: '等待解析', script: '编剧正在生成剧本', story: '编剧正在梳理故事',
  assets: '美术正在整理资产', episodes: '导演正在规划分集', complete: '解析完成', overview_done: '概览已完成', failed: '解析失败',
}
const assetKindLabels: Record<DramaAssetKind, string> = { character: '角色', scene: '场景', prop: '道具' }
type LibraryAsset = DramaAsset & { kind: DramaAssetKind; projectId: string; projectTitle: string }
type AssetFilter = DramaAssetKind | 'all'
const assetFilters: { value: AssetFilter; label: string }[] = [
  { value: 'all', label: '全部' },
  { value: 'character', label: assetKindLabels.character },
  { value: 'scene', label: assetKindLabels.scene },
  { value: 'prop', label: assetKindLabels.prop },
]

function statusTag(project: DramaProject) {
  if (project.status === 'ready') return <Tag color="success">概览已完成</Tag>
  if (project.status === 'failed') return <Tag color="error">解析失败</Tag>
  return <Tag color="processing">{phaseNames[project.phase] ?? '解析中'}</Tag>
}

function AssetThumbnail({ url }: { url?: string }) {
  const [failed, setFailed] = useState(false)
  return <div className="drama-asset-library-thumb">
    {url && !failed ? <img src={url} alt="" loading="lazy" onError={() => setFailed(true)} /> : <div className="drama-asset-placeholder">
      <PictureOutlined aria-hidden="true" />
      <span>暂无图片</span>
    </div>}
  </div>
}

function DramaAssetLibrary({ assets }: { assets: LibraryAsset[] }) {
  const [filter, setFilter] = useState<AssetFilter>('all')
  const matching = filter === 'all' ? assets : assets.filter((item) => item.kind === filter)
  const visible = [...matching].sort((a, b) => Number(Boolean(b.previewImageUrl)) - Number(Boolean(a.previewImageUrl)))
  return <>
    <div className="drama-asset-filter">
      <Segmented<AssetFilter> aria-label="资产类型" value={filter} options={assetFilters} onChange={setFilter} />
    </div>
    {visible.length > 0 ? <div className="drama-asset-library">
      {visible.map((item, index) => <article key={`${item.projectId}-${item.kind}-${item.id ?? index}`} className="drama-asset-item" data-kind={item.kind}>
        <AssetThumbnail key={item.previewImageUrl} url={item.previewImageUrl} />
        <div className="drama-asset-info">
          <div className="drama-asset-title">
            <strong title={item.name}>{item.name}</strong>
            <Tag>{assetKindLabels[item.kind]}</Tag>
          </div>
          <small title={item.description || item.projectTitle}>{item.projectTitle}</small>
        </div>
      </article>)}
    </div> : <div className="drama-empty">
      <PictureOutlined />
      <strong>{filter === 'all' ? '暂无资产' : `暂无${assetKindLabels[filter]}资产`}</strong>
      {assets.length === 0 && <span>剧本解析完成后，角色、场景和道具会出现在这里。</span>}
    </div>}
  </>
}

export function DramaHomePage() {
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const [messageApi, contextHolder] = message.useMessage()
  const fileInput = useRef<HTMLInputElement>(null)
  const [models, setModels] = useState<DramaModel[]>([])
  const [settings, setSettings] = useState<DramaSettings>(emptySettings)
  const [settingsDraft, setSettingsDraft] = useState<DramaSettings>(emptySettings)
  const [projects, setProjects] = useState<DramaProject[]>([])
  const [loading, setLoading] = useState(true)
  const [submitting, setSubmitting] = useState(false)
  const [savingSettings, setSavingSettings] = useState(false)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [tasksOpen, setTasksOpen] = useState(false)
  const [libraryTab, setLibraryTab] = useState<'projects' | 'assets'>('projects')
  const mode = searchParams.get('source') === 'novel' ? 'novel' : 'upload'
  const [novelSelection, setNovelSelection] = useState<NovelDramaSelection | undefined>(() => {
    const projectId = searchParams.get('novelId')
    return mode === 'novel' && projectId ? { projectId } : undefined
  })
  const [novelTitle, setNovelTitle] = useState<string>()
  const [creatingNovel, setCreatingNovel] = useState(false)
  const [title, setTitle] = useState('')
  const [sourceName, setSourceName] = useState('')
  const [sourceSize, setSourceSize] = useState(0)
  const [uploadText, setUploadText] = useState('')
  const [pasteOpen, setPasteOpen] = useState(false)
  const [styleId, setStyleId] = useState('ink-wash')
  const [ratio, setRatio] = useState<'9:16' | '16:9'>('9:16')
  const projectTitle = mode === 'novel' ? novelTitle ?? novelSelection?.title ?? '' : title
  const novelSource = novelSelection?.startChapter && novelSelection.endChapter ? {
    novelId: novelSelection.projectId,
    startChapter: novelSelection.startChapter,
    endChapter: novelSelection.endChapter,
  } : undefined

  function switchSource(next: 'upload' | 'novel') {
    if (next === mode) return
    const params = new URLSearchParams(searchParams)
    if (next === 'novel') {
      params.set('source', 'novel')
      if (novelSelection) params.set('novelId', novelSelection.projectId)
    } else {
      params.delete('source')
      params.delete('novelId')
    }
    setSearchParams(params, { replace: true })
  }

  function updateNovelSelection(next: NovelDramaSelection | undefined) {
    setNovelSelection(next)
    if (next?.projectId === novelSelection?.projectId) return
    setNovelTitle(undefined)
    const params = new URLSearchParams(searchParams)
    if (next) params.set('novelId', next.projectId)
    else params.delete('novelId')
    setSearchParams(params, { replace: true })
  }

  useEffect(() => {
    let active = true
    Promise.all([listDramaModels(), getDramaSettings(), listDramaProjects()]).then(([available, saved, items]) => {
      if (!active) return
      setModels(available)
      setSettings(saved)
      setSettingsDraft(saved)
      setProjects(items)
    }).catch((error) => { if (active) messageApi.error(error instanceof Error ? error.message : '读取漫剧数据失败') })
      .finally(() => { if (active) setLoading(false) })
    return () => { active = false }
  }, [messageApi])

  useEffect(() => {
    if (!projects.some((item) => item.status === 'queued' || item.status === 'running')) return
    const timer = window.setInterval(() => { listDramaProjects().then(setProjects).catch(() => {}) }, 4000)
    return () => window.clearInterval(timer)
  }, [projects])

  async function readFile(file: File) {
    if (!/\.(txt|md)$/i.test(file.name)) { messageApi.error('当前支持 TXT、MD 文本文件，也可以直接粘贴原文'); return }
    try {
      const content = await file.text()
      if (!content.trim()) { messageApi.error('文件内容为空'); return }
      setUploadText(content)
      setSourceName(file.name)
      setSourceSize(file.size)
      setPasteOpen(false)
      setTitle((current) => current || file.name.replace(/\.[^.]+$/, ''))
    } catch { messageApi.error('读取文件失败') }
  }

  function removeFile() {
    setUploadText('')
    setSourceName('')
    setSourceSize(0)
    if (fileInput.current) fileInput.current.value = ''
  }

  function openSettings() { setSettingsDraft({ ...settings }); setSettingsOpen(true) }

  async function saveSettings() {
    setSavingSettings(true)
    try {
      const saved = await saveDramaSettings(settingsDraft)
      setSettings(saved)
      setSettingsOpen(false)
      messageApi.success('模型设置已保存')
    } catch (error) { messageApi.error(error instanceof Error ? error.message : '保存模型设置失败') }
    finally { setSavingSettings(false) }
  }

  async function start() {
    if (submitting) return
    if (mode === 'upload' && (!title.trim() || !uploadText.trim())) {
      messageApi.warning('请上传或粘贴原文，并填写项目名称')
      return
    }
    if (mode === 'novel' && !novelSource) {
      messageApi.warning('请选择小说及已定稿章节')
      return
    }
    if (!settings.textModelCode || !models.some((item) => item.modelType === 'chat' && item.code === settings.textModelCode)) {
      messageApi.warning('请先在模型设置中选择可用的文本模型')
      openSettings()
      return
    }
    setSubmitting(true)
    try {
      const options = { title: projectTitle.trim(), styleId, aspectRatio: ratio }
      const project = mode === 'novel' && novelSource
        ? await createDramaProjectFromNovel({ ...options, ...novelSource })
        : await createDramaProject({ ...options, sourceName, sourceText: uploadText, sourceMode: 'upload' })
      navigate(`/ai-apps/drama/projects/${project.id}`)
    } catch (error) { messageApi.error(error instanceof Error ? error.message : '创建项目失败') }
    finally { setSubmitting(false) }
  }

  const assets = useMemo(() => projects.flatMap((project): LibraryAsset[] => {
    const info = { projectId: project.id, projectTitle: project.title }
    return [
      ...project.characters.map((item) => ({ ...item, ...info, kind: 'character' as const })),
      ...project.scenes.map((item) => ({ ...item, ...info, kind: 'scene' as const })),
      ...project.props.map((item) => ({ ...item, ...info, kind: 'prop' as const })),
    ]
  }), [projects])
  const activeTasks = projects.filter((item) => item.status === 'queued' || item.status === 'running').length

  return <main className="drama-home">
    {contextHolder}
    <div className="drama-home-tools">
      <Button icon={<SettingOutlined />} onClick={openSettings}>模型设置</Button>
      <Button icon={<ClockCircleOutlined />} onClick={() => setTasksOpen(true)}>生成任务{activeTasks > 0 ? ` ${activeTasks}` : ''}</Button>
    </div>

    <section className="drama-composer" aria-label="创建漫剧项目">
      <div className="drama-composer-tabs" role="tablist" aria-label="创作方式">
        <button type="button" role="tab" aria-selected={mode === 'upload'} className={mode === 'upload' ? 'active' : ''} disabled={submitting} onClick={() => switchSource('upload')}><CloudUploadOutlined /> 上传小说/剧本</button>
        <button type="button" role="tab" aria-selected={mode === 'novel'} className={mode === 'novel' ? 'active' : ''} disabled={submitting} onClick={() => switchSource('novel')}><BookOutlined /> 从 AI 小说选用</button>
      </div>
      <div className="drama-composer-body">
        <input ref={fileInput} className="drama-file-input" type="file" accept=".txt,.md,text/plain,text/markdown" onChange={(event) => { const file = event.target.files?.[0]; if (file) void readFile(file); event.target.value = '' }} />
        {mode === 'upload' ? <>
          {sourceName && !pasteOpen ? <div className="drama-uploaded-file"><span className="drama-file-icon"><FileTextOutlined /></span><span className="drama-file-name"><strong>{sourceName}</strong><small>{(sourceSize / 1024).toFixed(1)} KB · {uploadText.length.toLocaleString()} 字符</small></span><Button type="text" onClick={() => fileInput.current?.click()}>更换</Button><Button type="text" aria-label="移除文件" icon={<DeleteOutlined />} onClick={removeFile} /></div> : pasteOpen ? <Input.TextArea className="drama-paste-area" rows={5} value={uploadText} onChange={(event) => { setUploadText(event.target.value); setSourceName('') }} placeholder="在这里粘贴小说或剧本全文，不限制字数" /> : <button type="button" className="drama-upload-box" onClick={() => fileInput.current?.click()}><CloudUploadOutlined /><strong>点击选择小说或剧本文件</strong><span>支持 TXT、MD；也可以在下方直接粘贴原文</span></button>}
          <div className="drama-upload-secondary"><Button type="link" onClick={() => setPasteOpen((value) => !value)}>{pasteOpen ? '返回文件上传' : '直接粘贴原文'}</Button>{pasteOpen && <span>{uploadText.length.toLocaleString()} 字符</span>}</div>
        </> : <NovelDramaSource value={novelSelection} onChange={updateNovelSelection}
          onCreateNovel={() => setCreatingNovel(true)} disabled={submitting} />}
      </div>
      <div className="drama-composer-footer">
        <div className="drama-creation-options">
          <DramaStylePicker value={styleId} onChange={(style) => setStyleId(style.id)} />
          <Select className="drama-ratio-select" variant="borderless" value={ratio} onChange={setRatio} options={[{ label: '9:16 竖屏', value: '9:16' }, { label: '16:9 横屏', value: '16:9' }]} />
          <Input className="drama-title-input" value={projectTitle} maxLength={120} disabled={submitting}
            onChange={(event) => (mode === 'novel' ? setNovelTitle : setTitle)(event.target.value)} placeholder="项目名称" prefix="项目名称" />
        </div>
        <Button className="drama-create-button" type="primary" loading={submitting}
          disabled={mode === 'novel' && !novelSource} onClick={() => void start()}>开始解析</Button>
      </div>
    </section>

    <section className="drama-library" aria-label="我的创作内容">
      <div className="drama-library-tabs" role="tablist" aria-label="项目与资产">
        <button type="button" role="tab" aria-selected={libraryTab === 'projects'} className={libraryTab === 'projects' ? 'active' : ''} onClick={() => setLibraryTab('projects')}>我的项目 <span>{projects.length}</span></button>
        <button type="button" role="tab" aria-selected={libraryTab === 'assets'} className={libraryTab === 'assets' ? 'active' : ''} onClick={() => setLibraryTab('assets')}>我的资产 <span>{assets.length}</span></button>
      </div>
      {loading ? <Skeleton active /> : libraryTab === 'assets' ? <DramaAssetLibrary assets={assets} /> : projects.length === 0 ? (
        <div className="drama-empty"><FolderOpenOutlined /><strong>暂无项目</strong><span>上传原文或选用 AI 小说后，项目会出现在这里。</span></div>
      ) : <div className="drama-project-grid">{projects.map((project) => <Card key={project.id} className="drama-project-card" variant="borderless" onClick={() => navigate(`/ai-apps/drama/projects/${project.id}`)} hoverable><div className="drama-project-art"><VideoCameraOutlined /><span>{project.aspectRatio}</span></div><div className="drama-project-meta">{statusTag(project)}<span>{new Date(project.updatedAt).toLocaleDateString('zh-CN')}</span></div><h3>{project.title}</h3><p>{project.overview?.synopsis || (project.sourceMode === 'generate' ? '正在生成剧本' : '等待解析故事内容')}</p><div className="drama-project-foot">{project.episodes.length} 集 · {project.characters.length} 个角色</div></Card>)}</div>}
    </section>

    {creatingNovel && <NovelCreateDialog source="drama" onClose={() => setCreatingNovel(false)} />}
    <Drawer title="模型设置" open={settingsOpen} onClose={() => setSettingsOpen(false)} width={440} extra={<Button type="primary" loading={savingSettings} onClick={() => void saveSettings()}>保存设置</Button>}>
      <p className="drama-settings-note">为当前账号设置漫剧默认模型。新项目创建时使用这里配置的文本模型。</p>
      {([
        { key: 'textModelCode', label: '文本模型', kind: 'chat', help: '用于生成剧本、解析故事和规划分集' },
        { key: 'imageModelCode', label: '图片模型', kind: 'image', help: '用于后续角色、场景及分镜图片生成' },
        { key: 'videoModelCode', label: '视频模型', kind: 'video', help: '用于后续镜头视频生成' },
      ] as const).map((field) => <div className="drama-settings-field" key={field.key}><label htmlFor={`drama-${field.key}`}>{field.label}</label><p>{field.help}</p><Select id={`drama-${field.key}`} value={settingsDraft[field.key] || undefined} allowClear placeholder={`选择已启用的${field.label}`} options={models.filter((item) => item.modelType === field.kind).map((item) => ({ label: item.name, value: item.code }))} onChange={(value) => setSettingsDraft((current) => ({ ...current, [field.key]: value || '' }))} notFoundContent="暂无当前账号可用的模型" /></div>)}
      <p className="drama-settings-footnote">模型来自模型目录，并受当前账号的模型策略限制。修改设置不会改变已有项目保存的模型。</p>
    </Drawer>
    <Drawer title="生成任务" open={tasksOpen} onClose={() => setTasksOpen(false)} width={420}>{projects.length === 0 ? <Empty description="暂无生成任务" /> : <div className="drama-drawer-list">{projects.map((project) => <button key={project.id} type="button" onClick={() => { setTasksOpen(false); navigate(`/ai-apps/drama/projects/${project.id}`) }}><strong>{project.title}</strong>{statusTag(project)}<small>{phaseNames[project.phase] ?? project.phase}</small></button>)}</div>}</Drawer>
  </main>
}
