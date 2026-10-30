import {
  ClockCircleOutlined, CloudUploadOutlined, DeleteOutlined,
  FileTextOutlined, FolderOpenOutlined, PictureOutlined, RobotOutlined,
  SettingOutlined, VideoCameraOutlined,
} from '@ant-design/icons'
import { Button, Card, Drawer, Empty, Input, Select, Skeleton, Tag, message } from 'antd'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'

import {
  createDramaProject, getDramaSettings, listDramaModels, listDramaProjects, saveDramaSettings,
  type DramaModel, type DramaProject, type DramaSettings,
} from '../api/drama'
import { DramaStylePicker } from './DramaStylePicker'
import './drama-home.css'

const emptySettings: DramaSettings = { textModelCode: '', imageModelCode: '', videoModelCode: '' }
const phaseNames: Record<string, string> = {
  waiting: '等待解析', script: '编剧正在生成剧本', story: '编剧正在梳理故事',
  assets: '美术正在整理资产', episodes: '导演正在规划分集', complete: '解析完成', overview_done: '概览已完成', failed: '解析失败',
}

function statusTag(project: DramaProject) {
  if (project.status === 'ready') return <Tag color="success">概览已完成</Tag>
  if (project.status === 'failed') return <Tag color="error">解析失败</Tag>
  return <Tag color="processing">{phaseNames[project.phase] ?? '解析中'}</Tag>
}

export function DramaHomePage() {
  const navigate = useNavigate()
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
  const [mode, setMode] = useState<'upload' | 'generate'>('upload')
  const [title, setTitle] = useState('')
  const [sourceName, setSourceName] = useState('')
  const [sourceSize, setSourceSize] = useState(0)
  const [uploadText, setUploadText] = useState('')
  const [ideaText, setIdeaText] = useState('')
  const [pasteOpen, setPasteOpen] = useState(false)
  const [styleId, setStyleId] = useState('ink-wash')
  const [ratio, setRatio] = useState<'9:16' | '16:9'>('9:16')

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
    const sourceText = mode === 'upload' ? uploadText : ideaText
    if (!title.trim() || !sourceText.trim()) { messageApi.warning(mode === 'upload' ? '请上传或粘贴原文，并填写项目名称' : '请填写项目名称和故事创意'); return }
    if (!settings.textModelCode || !models.some((item) => item.modelType === 'chat' && item.code === settings.textModelCode)) {
      messageApi.warning('请先在模型设置中选择可用的文本模型')
      openSettings()
      return
    }
    setSubmitting(true)
    try {
      const project = await createDramaProject({ title: title.trim(), sourceName: mode === 'upload' ? sourceName : '', sourceText, sourceMode: mode, styleId, aspectRatio: ratio })
      navigate(`/ai-apps/drama/projects/${project.id}`)
    } catch (error) { messageApi.error(error instanceof Error ? error.message : '创建项目失败') }
    finally { setSubmitting(false) }
  }

  const assets = useMemo(() => projects.flatMap((project) => [
    ...project.characters.map((item) => ({ ...item, kind: '角色', project: project.title })),
    ...project.scenes.map((item) => ({ ...item, kind: '场景', project: project.title })),
    ...project.props.map((item) => ({ ...item, kind: '道具', project: project.title })),
  ]), [projects])
  const activeTasks = projects.filter((item) => item.status === 'queued' || item.status === 'running').length

  return <main className="drama-home">
    {contextHolder}
    <div className="drama-home-tools">
      <Button icon={<SettingOutlined />} onClick={openSettings}>模型设置</Button>
      <Button icon={<ClockCircleOutlined />} onClick={() => setTasksOpen(true)}>生成任务{activeTasks > 0 ? ` ${activeTasks}` : ''}</Button>
    </div>

    <section className="drama-composer" aria-label="创建漫剧项目">
      <div className="drama-composer-tabs" role="tablist" aria-label="创作方式">
        <button type="button" role="tab" aria-selected={mode === 'upload'} className={mode === 'upload' ? 'active' : ''} onClick={() => setMode('upload')}><CloudUploadOutlined /> 上传小说/剧本</button>
        <button type="button" role="tab" aria-selected={mode === 'generate'} className={mode === 'generate' ? 'active' : ''} onClick={() => setMode('generate')}><RobotOutlined /> AI 生成剧本</button>
      </div>
      <div className="drama-composer-body">
        <input ref={fileInput} className="drama-file-input" type="file" accept=".txt,.md,text/plain,text/markdown" onChange={(event) => { const file = event.target.files?.[0]; if (file) void readFile(file); event.target.value = '' }} />
        {mode === 'upload' ? <>
          {sourceName && !pasteOpen ? <div className="drama-uploaded-file"><span className="drama-file-icon"><FileTextOutlined /></span><span className="drama-file-name"><strong>{sourceName}</strong><small>{(sourceSize / 1024).toFixed(1)} KB · {uploadText.length.toLocaleString()} 字符</small></span><Button type="text" onClick={() => fileInput.current?.click()}>更换</Button><Button type="text" aria-label="移除文件" icon={<DeleteOutlined />} onClick={removeFile} /></div> : pasteOpen ? <Input.TextArea className="drama-paste-area" rows={5} value={uploadText} onChange={(event) => { setUploadText(event.target.value); setSourceName('') }} placeholder="在这里粘贴小说或剧本全文，不限制字数" /> : <button type="button" className="drama-upload-box" onClick={() => fileInput.current?.click()}><CloudUploadOutlined /><strong>点击选择小说或剧本文件</strong><span>支持 TXT、MD；也可以在下方直接粘贴原文</span></button>}
          <div className="drama-upload-secondary"><Button type="link" onClick={() => setPasteOpen((value) => !value)}>{pasteOpen ? '返回文件上传' : '直接粘贴原文'}</Button>{pasteOpen && <span>{uploadText.length.toLocaleString()} 字符</span>}</div>
        </> : <Input.TextArea className="drama-idea-area" rows={6} value={ideaText} onChange={(event) => setIdeaText(event.target.value)} placeholder="写下故事主角、冲突和你希望呈现的情节，AI 将据此生成第一集剧本" />}
      </div>
      <div className="drama-composer-footer">
        <div className="drama-creation-options">
          <DramaStylePicker value={styleId} onChange={(style) => setStyleId(style.id)} />
          <Select className="drama-ratio-select" variant="borderless" value={ratio} onChange={setRatio} options={[{ label: '9:16 竖屏', value: '9:16' }, { label: '16:9 横屏', value: '16:9' }]} />
          <Input className="drama-title-input" value={title} maxLength={120} onChange={(event) => setTitle(event.target.value)} placeholder="项目名称" prefix="项目名称" />
        </div>
        <Button className="drama-create-button" type="primary" loading={submitting} onClick={() => void start()}>开始解析</Button>
      </div>
    </section>

    <section className="drama-library" aria-label="我的创作内容">
      <div className="drama-library-tabs" role="tablist" aria-label="项目与资产">
        <button type="button" role="tab" aria-selected={libraryTab === 'projects'} className={libraryTab === 'projects' ? 'active' : ''} onClick={() => setLibraryTab('projects')}>我的项目 <span>{projects.length}</span></button>
        <button type="button" role="tab" aria-selected={libraryTab === 'assets'} className={libraryTab === 'assets' ? 'active' : ''} onClick={() => setLibraryTab('assets')}>我的资产 <span>{assets.length}</span></button>
      </div>
      {loading ? <Skeleton active /> : libraryTab === 'projects' ? projects.length === 0 ? <div className="drama-empty"><FolderOpenOutlined /><strong>暂无项目</strong><span>上传剧本或输入创意后，项目会出现在这里。</span></div> : <div className="drama-project-grid">{projects.map((project) => <Card key={project.id} className="drama-project-card" variant="borderless" onClick={() => navigate(`/ai-apps/drama/projects/${project.id}`)} hoverable><div className="drama-project-art"><VideoCameraOutlined /><span>{project.aspectRatio}</span></div><div className="drama-project-meta">{statusTag(project)}<span>{new Date(project.updatedAt).toLocaleDateString('zh-CN')}</span></div><h3>{project.title}</h3><p>{project.overview?.synopsis || (project.sourceMode === 'generate' ? '正在生成剧本' : '等待解析故事内容')}</p><div className="drama-project-foot">{project.episodes.length} 集 · {project.characters.length} 个角色</div></Card>)}</div> : assets.length === 0 ? <div className="drama-empty"><PictureOutlined /><strong>暂无资产</strong><span>剧本解析完成后，角色、场景和道具会出现在这里。</span></div> : <div className="drama-asset-library">{assets.map((item, index) => <div key={`${item.project}-${item.kind}-${index}`} className="drama-asset-item"><div className="drama-asset-library-icon"><PictureOutlined /></div><div><Tag>{item.kind}</Tag><strong>{item.name}</strong><small>{item.project} · {item.description || '暂无描述'}</small></div></div>)}</div>}
    </section>

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
