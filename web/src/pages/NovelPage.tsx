import { BookOutlined, EditOutlined, PictureOutlined, PlusOutlined, RobotOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Alert, Button, Card, Empty, Form, Input, InputNumber, Modal, Pagination, Popconfirm, Radio, Select, Space, Spin, Tag } from 'antd'
import { useEffect, useRef, useState } from 'react'
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom'

import { ApiError } from '../api/client'
import { applyNovelAction, createNovel, getNovel, listNovelModels, listNovels, type NovelAction, type NovelBeat, type NovelCreate, type NovelOutline, type NovelProject } from '../api/novel'
import { useAuth } from '../auth/AuthContext'
import { NovelAppendChapters, novelChapterLimit } from './NovelAppendChapters'
import { NovelCoverEditor } from './NovelCoverEditor'
import { NovelIdeaRefinement } from './NovelIdeaRefinement'
import './novel.css'

const { TextArea } = Input
const defaultChapterCount = 3
const chapterPageSize = 20
type CreationMode = 'ai' | 'author'
type NewNovelLocation = { outlineModel?: string }
type NovelMutation = {
  p: NovelProject
  input: NovelAction
  scope: [organizationId: string | undefined, userId: string | undefined]
  automatic?: boolean
}
const messages: Record<NovelAction['kind'], string> = {
  generate_outline: '正在生成大纲',
  save_outline: '正在保存大纲',
  confirm_outline: '正在确认大纲',
  append_chapters: '正在追加章节',
  write: '正在生成正文和连续性摘要',
  review: '正在审稿',
  save_draft: '正在保存草稿',
  finalize: '正在定稿',
}

function errorMessage(error: Error) {
  return error.message || '操作失败，请稍后重试'
}

export function NovelPage() {
  const navigate = useNavigate()
  const { user } = useAuth()
  const cache = useQueryClient()
  const listKey = ['novel-books', user?.organizationId, user?.id]
  const books = useQuery({ queryKey: listKey, queryFn: listNovels })
  const [creating, setCreating] = useState(false)
  const [mode, setMode] = useState<CreationMode>('ai')
  const [selectedModel, setSelectedModel] = useState('')
  const [createForm] = Form.useForm<NovelCreate>()
  const brief = Form.useWatch('brief', createForm) ?? ''
  const aiMode = mode === 'ai'
  const models = useQuery({
    queryKey: ['novel-models', user?.organizationId, user?.id],
    queryFn: listNovelModels,
    enabled: creating,
  })
  const availableModels = models.data ?? []
  const defaultModel = availableModels.find((model) => model.code === selectedModel) ?? availableModels[0]
  const create = useMutation({
    mutationFn: ({ input }: { input: NovelCreate; outlineModel?: string }) => createNovel(input),
    onSuccess: (project, { outlineModel }) => {
      void cache.invalidateQueries({ queryKey: listKey })
      navigate('/ai-apps/novel/' + project.id, { state: { outlineModel } satisfies NewNovelLocation })
    },
  })

  function openCreate() {
    create.reset()
    setMode('ai')
    setSelectedModel('')
    setCreating(true)
  }

  function submitCreate(values: NovelCreate) {
    if (create.isPending) return
    if (aiMode) {
      if (!defaultModel) return
      create.mutate({
        input: { title: values.title, brief: values.brief, chapterCount: values.chapterCount, suggestTitle: true },
        outlineModel: defaultModel.code,
      })
      return
    }
    create.mutate({ input: values })
  }

  return (
    <main className="novel-page novel-library-page">
      <header className="novel-heading">
        <h1>AI 小说</h1>
        <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>新建小说</Button>
      </header>
      <section aria-labelledby="novel-library-title" className="novel-library">
        <div className="novel-library-heading">
          <h2 id="novel-library-title">我的作品</h2>
          {books.data && <span>{books.data.length} 本</span>}
        </div>
        {books.isPending && <Spin aria-label="正在读取作品" />}
        {books.isError && <Alert type="error" title={errorMessage(books.error)} action={<Button onClick={() => void books.refetch()}>重试</Button>} />}
        {books.data?.length === 0 && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="还没有作品，先记下一条故事灵感。" />}
        <ul className="novel-shelf" role="list" aria-hidden={!books.data?.length || undefined}>
          {books.data?.map((book) => (
            <li key={book.id}>
              <Link className="novel-book" to={'/ai-apps/novel/' + book.id} title={book.title}>
                <span className="novel-book-cover">
                  {book.cover ? <img className="novel-book-art" src={book.cover.url} alt="" loading="lazy" />
                    : <BookOutlined aria-hidden="true" />}
                  <strong>{book.title}</strong>
                  <span className="novel-book-meta">
                    <span>计划 {book.chapterCount} 章</span>
                    <time dateTime={book.updatedAt}>{new Date(book.updatedAt).toLocaleDateString('zh-CN')}</time>
                  </span>
                </span>
              </Link>
            </li>
          ))}
        </ul>
      </section>
      <Modal className="novel-create-modal" title="新建小说" open={creating} footer={null} destroyOnHidden closable={!create.isPending}
        mask={{ closable: !create.isPending }} onCancel={() => { if (!create.isPending) setCreating(false) }}>
        <Radio.Group className="novel-mode-options" aria-label="大纲规划方式" value={mode}
          onChange={(event) => setMode(event.target.value)} disabled={create.isPending}>
          <Radio className="novel-mode-option" value="ai">
            <span><RobotOutlined /> AI 帮我规划</span>
            <small>给一句灵感，AI 拟书名、设定和各章大纲。</small>
          </Radio>
          <Radio className="novel-mode-option" value="author">
            <span><EditOutlined /> 我自己规划</span>
            <small>从空白大纲开始，自己决定人物和剧情。</small>
          </Radio>
        </Radio.Group>
        <Form<NovelCreate> form={createForm} layout="vertical"
          onFinish={submitCreate} disabled={create.isPending} clearOnDestroy>
          {!aiMode && <Form.Item label="作品名称" name="title" rules={[{ required: true, whitespace: true, message: '请输入作品名称' }]}>
            <Input maxLength={120} />
          </Form.Item>}
          <Form.Item label={aiMode ? '故事灵感' : '故事构想'} name="brief"
            rules={[{ required: true, whitespace: true, message: '请写下你的故事想法' }]}>
            <TextArea rows={aiMode ? 4 : 5} maxLength={4000} showCount placeholder={aiMode
              ? '例如：一个只负责归还遗失物的夜班店员，收到了一把来自明天的钥匙。'
              : '谁想做什么？什么阻止了他？也可以写下题材、视角和写作要求。'} />
          </Form.Item>
          <NovelIdeaRefinement brief={brief} model={defaultModel} disabled={create.isPending}
            onApply={(value) => createForm.setFieldValue('brief', value)} />
          <div className={aiMode ? 'novel-create-settings' : undefined}>
            {aiMode ? <details>
              <summary>生成选项{defaultModel ? <small> · 默认 {defaultChapterCount} 章，可换模型或调整章数</small> : null}</summary>
              <div className="novel-create-settings-fields">
                <Form.Item label="作品名称（选填）" name="title">
                  <Input maxLength={120} placeholder="留空由 AI 拟定" />
                </Form.Item>
                <Form.Item label="首批章数" name="chapterCount" initialValue={defaultChapterCount}
                  extra="确认大纲后可以继续分批追加。" rules={[{ required: true, message: '请输入 1 到 30 之间的章数' }]}>
                  <InputNumber min={1} max={30} precision={0} />
                </Form.Item>
                <Form.Item label="文本模型" htmlFor="novel-create-model">
                  <Select id="novel-create-model" value={defaultModel?.code} onChange={setSelectedModel}
                    loading={models.isPending} placeholder="暂无可用模型"
                    options={availableModels.map((model) => ({ value: model.code, label: model.name }))} />
                </Form.Item>
              </div>
            </details> : (
              <Form.Item label="首批章数" name="chapterCount" initialValue={defaultChapterCount}
                extra="确认大纲后可以继续分批追加。" rules={[{ required: true, message: '请输入 1 到 30 之间的章数' }]}>
                <InputNumber min={1} max={30} precision={0} />
              </Form.Item>
            )}
          </div>
          {aiMode && models.isError && <Alert type="error" title={errorMessage(models.error)}
            action={<Button size="small" onClick={() => void models.refetch()}>重试</Button>} />}
          {aiMode && models.isSuccess && !defaultModel && <Alert type="info" title="当前没有可用的文本模型，可以切换为自己规划，或请管理员配置模型。" />}
          {create.error && <Alert type="error" title={errorMessage(create.error)} />}
          <div className="novel-create-footer">
            {aiMode && defaultModel && <span>先规划首批章节，后续可分批追加。使用 {defaultModel.name} 生成。</span>}
            <Button type="primary" htmlType="submit" loading={create.isPending} disabled={aiMode && !defaultModel}>
              {aiMode ? '生成小说大纲' : '创建作品'}
            </Button>
          </div>
        </Form>
      </Modal>
    </main>
  )
}

export function NovelProjectPage() {
  const { id = '' } = useParams()
  const location = useLocation()
  const navigate = useNavigate()
  const { outlineModel } = (location.state ?? {}) as NewNovelLocation
  const started = useRef<string | undefined>(undefined)
  const { user } = useAuth()
  const organizationId = user?.organizationId
  const userId = user?.id
  const cache = useQueryClient()
  const key = ['novel-project', organizationId, userId, id]
  const project = useQuery({ queryKey: key, queryFn: () => getNovel(id), refetchOnWindowFocus: false, refetchOnReconnect: false })
  const models = useQuery({ queryKey: ['novel-models', organizationId, userId], queryFn: listNovelModels })
  const action = useMutation({
    mutationFn: ({ p, input }: NovelMutation) => applyNovelAction(p, input),
    onSuccess: async (updated, { p, scope }) => {
      const updatedKey = ['novel-project', ...scope, updated.id]
      await cache.cancelQueries({ queryKey: updatedKey })
      cache.setQueryData<NovelProject>(updatedKey, (current) => {
        const cover = current?.cover
        return cover && cover.url !== p.cover?.url && updated.cover?.url === p.cover?.url ? { ...updated, cover } : updated
      })
      void cache.invalidateQueries({ queryKey: ['novel-books', ...scope] })
    },
  })

  const { mutate } = action
  useEffect(() => {
    if (!outlineModel || !project.data || started.current === id) return
    started.current = id
    if (!project.data.outline && !project.data.confirmed) {
      mutate({ p: project.data, input: { kind: 'generate_outline', modelCode: outlineModel },
        scope: [organizationId, userId], automatic: true })
    }
    navigate(location.pathname, { replace: true, state: null })
  }, [id, location.pathname, mutate, navigate, organizationId, outlineModel, project.data, userId])

  const activeAction = action.variables?.p.id === id ? action.variables : undefined
  const automaticAction = activeAction?.automatic ? activeAction.input : undefined
  const initialModel = outlineModel ?? (automaticAction?.kind === 'generate_outline' ? automaticAction.modelCode : undefined)

  return (
    <main className="novel-page">
      {project.isPending && <Spin aria-label="正在加载作品" />}
      {project.isError && <Alert type="error" title={errorMessage(project.error)} action={<Button onClick={() => void project.refetch()}>重试</Button>} />}
      {project.data && (
        <NovelWorkspace key={project.data.id} project={project.data} models={models.data ?? []}
          initialModel={initialModel}
          modelsNotice={models.isPending ? '正在查询可用模型' : models.isError ? errorMessage(models.error) : '当前账号没有可用的文本模型，请联系管理员配置'}
          run={(input) => action.mutate({ p: project.data, input, scope: [organizationId, userId] })}
          append={(p, beats) => action.mutateAsync({ p, input: { kind: 'append_chapters', beats }, scope: [organizationId, userId] })}
          pending={action.isPending ? activeAction?.input.kind : undefined}
          revisingOutline={action.isPending && activeAction?.input.kind === 'generate_outline' && !!activeAction.input.instruction}
          autoOutlining={action.isPending && !!automaticAction}
          failure={activeAction && activeAction.input.kind !== 'append_chapters' ? action.error : null}
          reload={() => { action.reset(); void project.refetch() }}
        />
      )}
    </main>
  )
}

type RunAction = (action: NovelAction) => void

function NovelWorkspace({ project, models, modelsNotice, initialModel, run, append, pending, revisingOutline, autoOutlining, failure, reload }: {
  project: NovelProject
  models: { code: string; name: string }[]
  modelsNotice: string
  initialModel?: string
  run: RunAction
  append: (p: NovelProject, beats: NovelBeat[]) => Promise<NovelProject>
  pending?: NovelAction['kind']
  revisingOutline?: boolean
  autoOutlining?: boolean
  failure: Error | null
  reload: () => void
}) {
  const firstUnfinished = project.chapters.findIndex((chapter) => !chapter.final)
  const [selectedIndex, setSelected] = useState(() => project.confirmed ? (firstUnfinished < 0 ? project.chapterCount - 1 : firstUnfinished) : -1)
  const selected = Math.min(selectedIndex, project.chapters.length - 1)
  const [chapterPage, setChapterPage] = useState(() => Math.floor(Math.max(selected, 0) / chapterPageSize) + 1)
  const [selectedModel, setModel] = useState<string>()
  const [editingCover, setEditingCover] = useState(false)
  const [appendProject, setAppendProject] = useState<NovelProject>()
  const [dirtyVersion, setDirtyVersion] = useState<number>()
  const dirty = dirtyVersion === project.version
  const markDirty = (value: boolean) => setDirtyVersion(value ? project.version : undefined)
  const busy = !!pending
  const preferredModel = selectedModel ?? initialModel ?? project.modelCode
  const defaultModelCode = !project.outline ? models[0]?.code : undefined
  const modelCode = models.some((item) => item.code === preferredModel) ? preferredModel : defaultModelCode ?? ''
  const finalized = project.chapters.filter((chapter) => chapter.final).length
  const canAppend = project.confirmed && project.chapterCount < novelChapterLimit
  const visibleChapterPage = Math.min(chapterPage, Math.ceil(project.chapterCount / chapterPageSize))
  const chapterOffset = (visibleChapterPage - 1) * chapterPageSize
  const openAppend = () => { if (!busy && !dirty) setAppendProject(project) }

  function switchSection(index: number) {
    if (selected === index || busy) return
    setSelected(index)
    if (index >= 0) setChapterPage(Math.floor(index / chapterPageSize) + 1)
    markDirty(false)
  }

  function sectionButton(index: number, label: string, hint?: string, locked = false) {
    const button = <button type="button" className="novel-index-item" disabled={busy || locked} aria-current={selected === index ? 'step' : undefined}
      onClick={dirty && selected !== index ? undefined : () => switchSection(index)}>
      <span>{label}</span>{hint && <small>{hint}</small>}
    </button>
    return dirty && selected !== index && !locked ? (
      <Popconfirm title="放弃当前未保存的修改？" onConfirm={() => switchSection(index)}>{button}</Popconfirm>
    ) : button
  }

  return (
    <>
      <div className="novel-back">
        <Link to="/ai-apps/novel" onClick={(event) => {
          if ((dirty || busy) && !window.confirm(busy ? '当前操作仍在进行，确认离开？' : '放弃当前未保存的修改？')) event.preventDefault()
        }}>← 返回我的作品</Link>
      </div>
      <header className="novel-heading">
        <div><h1>{project.title}</h1><span className="novel-heading-progress">已定稿 {finalized} / {project.chapterCount} 章</span></div>
        <div className="novel-heading-actions">
          <Button icon={<PictureOutlined />} disabled={busy} onClick={() => setEditingCover(true)}>设置封面</Button>
          <div className="novel-model">
            <label htmlFor="novel-model">文本模型</label>
            <Select id="novel-model" value={modelCode || undefined} onChange={setModel} disabled={busy || !models.length}
              options={models.map((item) => ({ value: item.code, label: item.name }))} placeholder="选择模型" />
          </div>
        </div>
      </header>
      {editingCover && <NovelCoverEditor book={project} onClose={() => setEditingCover(false)} />}
      {appendProject && <NovelAppendChapters project={appendProject} model={models.find((item) => item.code === modelCode) ?? models[0]}
        onClose={() => setAppendProject(undefined)} onReload={() => { setAppendProject(undefined); reload() }}
        onAppend={async (beats) => {
          await append(appendProject, beats)
          setAppendProject(undefined)
          if (appendProject.chapters.every((chapter) => chapter.final)) switchSection(appendProject.chapterCount)
        }} />}
      {!models.length && <Alert type="info" title={modelsNotice} />}
      {pending && <Alert type="info" showIcon icon={<Spin size="small" />} title={revisingOutline ? '正在按意见修改大纲' : messages[pending]} />}
      {failure && <Alert type="error" showIcon title={errorMessage(failure)}
        description={initialModel && !project.outline ? '灵感已保存，可以重试生成大纲，也可以自己填写。' : undefined}
        action={failure instanceof ApiError && failure.code === 'novel_conflict'
          ? <Popconfirm title="重新加载会清除当前未保存的修改，继续吗？" onConfirm={reload}><Button size="small">重新加载</Button></Popconfirm>
          : initialModel && !project.outline && <Button size="small" disabled={busy || dirty || !modelCode}
              onClick={() => run({ kind: 'generate_outline', modelCode })}>重试生成</Button>} />}
      <div className="novel-desk">
        <nav className="novel-index" aria-label="作品目录">
          {sectionButton(-1, '作品大纲', project.confirmed ? '已确认' : '待确认')}
          {canAppend && <button type="button" className="novel-index-item novel-append-trigger" disabled={busy || dirty}
            title={dirty ? '请先保存当前草稿' : undefined} onClick={openAppend}><span><PlusOutlined /> 追加章节</span></button>}
          {project.chapters.slice(chapterOffset, chapterOffset + chapterPageSize).map((chapter, pageIndex) => {
            const index = chapterOffset + pageIndex
            const locked = !project.confirmed || (index > 0 && !project.chapters[index - 1].final)
            return <div key={index}>
              {sectionButton(index, `第 ${index + 1} 章`, chapter.final ? '已定稿' : locked ? '待解锁' : chapter.body ? '草稿' : '待写', locked)}
            </div>
          })}
          {project.chapterCount > chapterPageSize && <Pagination className="novel-chapter-pagination" aria-label="章节目录分页" size="small"
            simple current={visibleChapterPage} total={project.chapterCount} pageSize={chapterPageSize}
            showSizeChanger={false} onChange={setChapterPage} />}
        </nav>
        <Card className="novel-paper" variant="borderless">
          {selected === -1 && autoOutlining && !project.outline
            ? <div className="novel-generating-outline">
                <h2>正在规划你的故事</h2>
                <p>AI 会根据这条灵感拟好故事设定、人物关系和 {project.chapterCount} 章的写作目标。</p>
              </div>
            : selected === -1
            ? <OutlineEditor key={project.version} project={project} modelCode={modelCode} busy={busy} run={run}
                dirty={dirty} markDirty={markDirty} onStart={() => switchSection(Math.max(0, firstUnfinished))} />
            : <ChapterEditor key={project.version + ':' + selected} project={project} index={selected}
                modelCode={modelCode} busy={busy} run={run} dirty={dirty} markDirty={markDirty}
                onNext={() => switchSection(selected + 1)} onAppend={canAppend ? openAppend : undefined} />}
        </Card>
      </div>
    </>
  )
}

function OutlineEditor({ project, modelCode, busy, dirty, markDirty, run, onStart }: {
  project: NovelProject; modelCode: string; busy: boolean; dirty: boolean; markDirty: (value: boolean) => void; run: RunAction; onStart: () => void
}) {
  const [outline, setOutline] = useState<NovelOutline>(() => project.outline ?? {
    synopsis: '', world: '', characters: '', beats: Array.from({ length: project.chapterCount }, () => ({ title: '', goal: '' })),
  })
  const [showOutlineFeedback, setShowOutlineFeedback] = useState(false)
  const [outlineFeedback, setOutlineFeedback] = useState('')
  const [beatPage, setBeatPage] = useState(1)
  const paginateBeats = project.confirmed && outline.beats.length > chapterPageSize
  const beatOffset = (beatPage - 1) * chapterPageSize
  const visibleBeats = paginateBeats ? outline.beats.slice(beatOffset, beatOffset + chapterPageSize) : outline.beats
  const complete = !!(outline.synopsis.trim() && outline.world.trim() && outline.characters.trim()
    && outline.beats.every((beat) => beat.title.trim() && beat.goal.trim()))
  const generate = () => run({ kind: 'generate_outline', modelCode })
  const generateButton = <Button icon={<RobotOutlined />} disabled={busy || !modelCode} onClick={project.outline || dirty ? undefined : generate}>
    {project.outline ? '重新生成大纲' : '生成大纲'}
  </Button>
  const changeOutline = (next: NovelOutline) => { setOutline(next); markDirty(true) }

  return (
    <>
      <h2>作品大纲</h2>
      <details className="novel-brief" open={!project.outline}><summary>故事构想</summary><p>{project.brief}</p></details>
      {!project.confirmed && <p className="novel-hint">{project.outline
        ? '重新生成会从最初的故事灵感出发。要在当前大纲上纠正或补充细节，可以按意见修改。'
        : '可先生成大纲，也可手动填写。确认后开放第一章，并锁定大纲。'}</p>}
      {!project.confirmed && complete && (
        <div className="novel-outline-feedback">
          <Button icon={<RobotOutlined />} disabled={busy} aria-expanded={showOutlineFeedback}
            onClick={() => setShowOutlineFeedback((value) => !value)}>
            {showOutlineFeedback ? '收起修改意见' : '按意见修改大纲'}
          </Button>
          {showOutlineFeedback && (
            <div className="novel-ai-panel">
              <label htmlFor="novel-outline-feedback">你想改哪里？</label>
              <TextArea id="novel-outline-feedback" rows={3} maxLength={1200} showCount disabled={busy}
                value={outlineFeedback} onChange={(event) => setOutlineFeedback(event.target.value)}
                placeholder="例如：按我下面提供的史料，修正世界设定里的官职，并同步调整第二章。保留主角动机和其他章节。" />
              <p className="novel-hint">AI 会同时看到故事灵感、你正在编辑的大纲和修改意见。年代、事实依据或需要保留的内容都可以写在这里。</p>
              <Button type="primary" disabled={busy || !modelCode || !outlineFeedback.trim()}
                onClick={() => run({ kind: 'generate_outline', modelCode, outline, instruction: outlineFeedback.trim() })}>
                提交意见并修改
              </Button>
            </div>
          )}
        </div>
      )}
      <Form layout="vertical" disabled={busy}>
        {([{ key: 'synopsis', label: '故事梗概' }, { key: 'world', label: '世界设定' }, { key: 'characters', label: '主要人物与关系' }] as const).map(({ key, label }) => (
          <Form.Item key={key} label={label} htmlFor={'novel-outline-' + key}>
            <TextArea id={'novel-outline-' + key} rows={3} value={outline[key]} readOnly={project.confirmed} maxLength={4000}
              onChange={(event) => changeOutline({ ...outline, [key]: event.target.value })} />
          </Form.Item>
        ))}
        <h3>章节蓝图</h3>
        {visibleBeats.map((beat, pageIndex) => {
          const index = paginateBeats ? beatOffset + pageIndex : pageIndex
          return (
            <fieldset className="novel-beat" key={index}>
              <legend>第 {index + 1} 章</legend>
              <Form.Item label="标题" htmlFor={`novel-beat-title-${index}`}>
                <Input id={`novel-beat-title-${index}`} value={beat.title} readOnly={project.confirmed} maxLength={120} onChange={(event) =>
                  changeOutline({ ...outline, beats: outline.beats.map((item, i) => i === index ? { ...item, title: event.target.value } : item) })} />
              </Form.Item>
              <Form.Item label="本章目标" htmlFor={`novel-beat-goal-${index}`}>
                <TextArea id={`novel-beat-goal-${index}`} rows={2} value={beat.goal} readOnly={project.confirmed} maxLength={1200} onChange={(event) =>
                  changeOutline({ ...outline, beats: outline.beats.map((item, i) => i === index ? { ...item, goal: event.target.value } : item) })} />
              </Form.Item>
            </fieldset>
          )
        })}
        {paginateBeats && <Pagination aria-label="章节蓝图分页" size="small" current={beatPage} total={outline.beats.length}
          pageSize={chapterPageSize} showSizeChanger={false} onChange={setBeatPage} />}
      </Form>
      <div className="novel-actions">
        {project.confirmed ? <Button type="primary" onClick={onStart}>进入正文</Button> : <Space wrap>
          {project.outline || dirty
            ? <Popconfirm title="根据最初的故事灵感重新生成？"
              description="新结果会替换当前大纲。想在现有大纲上改细节，可以用「按意见修改大纲」。"
              onConfirm={generate}>{generateButton}</Popconfirm>
            : generateButton}
          <Button disabled={busy || !complete || !dirty} onClick={() => run({ kind: 'save_outline', outline })}>保存大纲</Button>
          <Popconfirm title="确认后将锁定大纲，开始逐章写作？" onConfirm={() => run({ kind: 'confirm_outline', outline })}>
            <Button type="primary" disabled={busy || !complete}>确认大纲</Button>
          </Popconfirm>
        </Space>}
      </div>
    </>
  )
}

function ChapterEditor({ project, index, modelCode, busy, dirty, markDirty, run, onNext, onAppend }: {
  project: NovelProject; index: number; modelCode: string; busy: boolean; dirty: boolean
  markDirty: (value: boolean) => void; run: RunAction; onNext: () => void; onAppend?: () => void
}) {
  const chapter = project.chapters[index]
  const beat = project.outline?.beats[index]
  const [body, setBody] = useState(chapter.body)
  const [summary, setSummary] = useState(chapter.summary)
  const [instruction, setInstruction] = useState('')
  const number = index + 1
  const generate = () => run({ kind: 'write', chapter: number, modelCode, instruction })
  const writeButton = <Button icon={<RobotOutlined />} disabled={busy || dirty || !modelCode}
    onClick={chapter.body ? undefined : generate}>{chapter.body ? 'AI 改写' : '生成正文'}</Button>

  return (
    <>
      <header className="novel-chapter-title">
        <p>第 {number} 章</p><h2>{beat?.title}</h2>
        {chapter.final && <Tag color="green">已定稿</Tag>}
      </header>
      <p className="novel-goal">{beat?.goal}</p>
      <Form layout="vertical" disabled={busy}>
        <Form.Item label="正文" htmlFor="novel-body">
          <TextArea id="novel-body" className="novel-prose" autoSize={{ minRows: 12, maxRows: 25 }} value={body}
            readOnly={chapter.final} maxLength={8000} showCount onChange={(event) => {
              setBody(event.target.value); markDirty(event.target.value !== chapter.body || summary !== chapter.summary)
            }} />
        </Form.Item>
        <Form.Item label="连续性摘要" htmlFor="novel-summary" extra="正文改动后请同步核对摘要。后续章节会引用这里定稿的事件、人物变化和未收束的伏笔。">
          <TextArea id="novel-summary" rows={3} value={summary} readOnly={chapter.final} maxLength={500} onChange={(event) => {
            setSummary(event.target.value); markDirty(body !== chapter.body || event.target.value !== chapter.summary)
          }} />
        </Form.Item>
        {!chapter.final && <Form.Item label="给 AI 的写作或改写要求（可选）" htmlFor="novel-instruction">
          <TextArea id="novel-instruction" rows={2} value={instruction} maxLength={1200} onChange={(event) => setInstruction(event.target.value)} />
        </Form.Item>}
      </Form>
      {chapter.review && <aside className="novel-review"><h3>{body === chapter.body ? '审稿意见' : '上一版草稿的审稿意见'}</h3><p>{chapter.review}</p></aside>}
      <div className="novel-actions">
        {chapter.final ? number < project.chapterCount ? <Button type="primary" onClick={onNext}>进入下一章</Button>
          : onAppend && <Button type="primary" icon={<PlusOutlined />} disabled={busy} onClick={onAppend}>追加章节</Button> : <Space wrap>
          {chapter.body ? <Popconfirm title="用改写结果替换已保存的草稿？" onConfirm={generate}>{writeButton}</Popconfirm> : writeButton}
          <Button disabled={busy || !dirty || !body.trim()} onClick={() => run({ kind: 'save_draft', chapter: number, body, summary })}>保存草稿</Button>
          <Button disabled={busy || dirty || !chapter.body || !modelCode} onClick={() => run({ kind: 'review', chapter: number, modelCode })}>AI 审稿</Button>
          <Popconfirm title="定稿后本章将锁定，摘要会传给后续章节。确认定稿？"
            onConfirm={() => run({ kind: 'finalize', chapter: number, body, summary })}>
            <Button type="primary" disabled={busy || !body.trim() || !summary.trim()}>确认定稿</Button>
          </Popconfirm>
          {dirty && <span className="novel-hint">有未保存的修改。保存后可调用 AI。</span>}
        </Space>}
      </div>
    </>
  )
}
