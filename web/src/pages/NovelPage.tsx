import { BookOutlined, EditOutlined, PictureOutlined, PlusOutlined, RobotOutlined } from '@ant-design/icons'
import { useMutation, useMutationState, useQuery, useQueryClient } from '@tanstack/react-query'
import { Alert, Button, Card, Empty, Form, Input, Modal, Pagination, Popconfirm, Select, Space, Spin, Tag } from 'antd'
import { useEffect, useRef, useState } from 'react'
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom'

import { ApiError } from '../api/client'
import { applyNovelAction, getNovel, listNovelModels, listNovels, type NovelAction, type NovelBeat, type NovelOutline, type NovelProject } from '../api/novel'
import { useAuth } from '../auth/AuthContext'
import { NovelAppendChapters, novelChapterLimit } from './NovelAppendChapters'
import { NovelCoverEditor } from './NovelCoverEditor'
import { NovelCreateDialog } from './NovelCreateDialog'
import { NovelTitleEditor } from './NovelTitleEditor'
import './novel.css'

const { TextArea } = Input
const chapterPageSize = 20
type NewNovelLocation = { outlineModel?: string }
type NovelMutation = {
  p: NovelProject
  input: NovelAction
  scope: [organizationId: string | undefined, userId: string | undefined]
  automatic?: boolean
}
const messages: Record<NovelAction['kind'], string> = {
  generate_outline: '正在生成并检查大纲',
  check_outline: '正在检查大纲因果',
  rename: '正在保存书名',
  suggest_title: '正在拟定新书名',
  save_author_guidance: '正在保存写作约定',
  save_outline: '正在保存大纲',
  confirm_outline: '正在确认大纲',
  append_chapters: '正在追加章节',
  write: '正在生成正文并审稿',
  review: '正在审稿',
  revise_review: '正在按审稿意见修改并复核',
  save_draft: '正在保存草稿',
  finalize: '正在定稿',
}

function errorMessage(error: Error) {
  return error.message || '操作失败，请稍后重试'
}

export function NovelPage() {
  const { user } = useAuth()
  const listKey = ['novel-books', user?.organizationId, user?.id]
  const books = useQuery({ queryKey: listKey, queryFn: listNovels })
  const [creating, setCreating] = useState(false)

  return (
    <main className="novel-page novel-library-page">
      <section aria-labelledby="novel-library-title" className="novel-library">
        <header className="novel-library-heading">
          <div className="novel-library-heading-title">
            <h1 id="novel-library-title">我的作品</h1>
            {books.data && <span>{books.data.length} 本</span>}
          </div>
          <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreating(true)}>新建小说</Button>
        </header>
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
                    <span>{book.openingPending ? '待规划开篇' : `计划 ${book.chapterCount} 章`}</span>
                    <time dateTime={book.updatedAt}>{new Date(book.updatedAt).toLocaleDateString('zh-CN')}</time>
                  </span>
                </span>
              </Link>
            </li>
          ))}
        </ul>
      </section>
      {creating && <NovelCreateDialog onClose={() => setCreating(false)} />}
    </main>
  )
}

export function NovelProjectPage() {
  const { id = '' } = useParams()
  const location = useLocation()
  const navigate = useNavigate()
  const { outlineModel } = (location.state ?? {}) as NewNovelLocation
  const fromDrama = new URLSearchParams(location.search).get('from') === 'drama'
  const started = useRef<string | undefined>(undefined)
  const { user } = useAuth()
  const organizationId = user?.organizationId
  const userId = user?.id
  const cache = useQueryClient()
  const key = ['novel-project', organizationId, userId, id]
  const mutationKey = ['novel-action', organizationId, userId, id]
  const [dismissedFailure, setDismissedFailure] = useState<number>()
  const project = useQuery({ queryKey: key, queryFn: () => getNovel(id), refetchOnWindowFocus: false, refetchOnReconnect: false })
  const models = useQuery({ queryKey: ['novel-models', organizationId, userId], queryFn: listNovelModels })
  const action = useMutation({
    mutationKey,
    mutationFn: ({ p, input }: NovelMutation) => applyNovelAction(p, input),
    onSuccess: async (updated, { p, scope }) => {
      const updatedKey = ['novel-project', ...scope, updated.id]
      await cache.cancelQueries({ queryKey: updatedKey })
      cache.setQueryData<NovelProject>(updatedKey, (current) => {
        if (current && current.version > updated.version) return current
        const cover = current?.cover
        return cover && cover.url !== p.cover?.url && updated.cover?.url === p.cover?.url ? { ...updated, cover } : updated
      })
      void cache.invalidateQueries({ queryKey: ['novel-books', ...scope] })
    },
  })
  const actionStates = useMutationState({
    filters: { mutationKey: ['novel-action'] },
    select: (mutation) => ({
      id: mutation.mutationId,
      status: mutation.state.status,
      variables: mutation.state.variables as NovelMutation | undefined,
      error: mutation.state.error,
    }),
  }).filter(({ variables }) => variables?.p.id === id && variables.scope[0] === organizationId && variables.scope[1] === userId)
  const latestAction = actionStates.at(-1)
  const pendingAction = actionStates.find((item) => item.status === 'pending')?.variables
  const automaticAction = latestAction?.variables?.automatic ? latestAction.variables.input : undefined

  const { mutate } = action
  useEffect(() => {
    if (!outlineModel || !project.data || started.current === id) return
    started.current = id
    if (!cache.isMutating({ mutationKey: ['novel-action', organizationId, userId, id], exact: true }) &&
      !project.data.outline && !project.data.confirmed) {
      mutate({ p: project.data, input: { kind: 'generate_outline', modelCode: outlineModel },
        scope: [organizationId, userId], automatic: true })
    }
    navigate({ pathname: location.pathname, search: location.search }, { replace: true, state: null })
  }, [cache, id, location.pathname, location.search, mutate, navigate, organizationId, outlineModel, project.data, userId])

  const initialModel = outlineModel ?? (automaticAction?.kind === 'generate_outline' ? automaticAction.modelCode : undefined)
  const failure = latestAction && latestAction.id !== dismissedFailure && latestAction.variables?.input.kind !== 'append_chapters'
    ? latestAction.error : null
  function applyAction(p: NovelProject, input: NovelAction) {
    if (cache.isMutating({ mutationKey, exact: true })) {
      return Promise.reject(new Error('当前作品还有操作正在进行'))
    }
    return action.mutateAsync({ p, input, scope: [organizationId, userId] })
  }
  const coverURL = project.data?.cover?.url

  return (
    <main className={coverURL ? 'novel-page novel-project-with-cover' : 'novel-page'}>
      {coverURL && <div className="novel-cover-backdrop" aria-hidden="true">
        <div className="novel-cover-backdrop-frame">
          <img key={coverURL} src={coverURL} alt="" onError={(event) => { event.currentTarget.hidden = true }} />
        </div>
      </div>}
      {project.isPending && <Spin aria-label="正在加载作品" />}
      {project.isError && <Alert type="error" title={errorMessage(project.error)} action={<Button onClick={() => void project.refetch()}>重试</Button>} />}
      {project.data && (
        <NovelWorkspace key={project.data.id} project={project.data} models={models.data ?? []}
          initialModel={initialModel} fromDrama={fromDrama}
          modelsNotice={models.isPending ? '正在查询可用模型' : models.isError ? errorMessage(models.error) : '当前账号没有可用的文本模型，请联系管理员配置'}
          run={(input) => {
            if (!cache.isMutating({ mutationKey, exact: true })) {
              mutate({ p: project.data, input, scope: [organizationId, userId] })
            }
          }}
          runTitle={(input) => applyAction(project.data, input)}
          saveAuthorGuidance={(p, authorGuidance) => applyAction(p, { kind: 'save_author_guidance', authorGuidance })}
          append={(p, beats) => applyAction(p, { kind: 'append_chapters', beats })}
          pending={pendingAction?.input.kind}
          revisingOutline={pendingAction?.input.kind === 'generate_outline' && !!pendingAction.input.instruction}
          autoOutlining={!!pendingAction?.automatic}
          failure={failure}
          reload={() => { setDismissedFailure(latestAction?.id); void project.refetch() }}
        />
      )}
    </main>
  )
}

type RunAction = (action: NovelAction) => void
type RunTitleAction = (action: Extract<NovelAction, { kind: 'rename' | 'suggest_title' }>) => Promise<NovelProject>

function NovelWorkspace({ project, models, modelsNotice, initialModel, fromDrama, run, runTitle, saveAuthorGuidance, append, pending, revisingOutline, autoOutlining, failure, reload }: {
  project: NovelProject
  models: { code: string; name: string }[]
  modelsNotice: string
  initialModel?: string
  fromDrama: boolean
  run: RunAction
  runTitle: RunTitleAction
  saveAuthorGuidance: (p: NovelProject, authorGuidance: string) => Promise<NovelProject>
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
  const [guidanceProject, setGuidanceProject] = useState<NovelProject>()
  const [appendProject, setAppendProject] = useState<NovelProject>()
  const [dirtyVersion, setDirtyVersion] = useState<number>()
  const [instructionDraft, setInstructionDraft] = useState<{ index: number; saved: string; value: string }>()
  const dirty = dirtyVersion === project.version
  const markDirty = (value: boolean) => setDirtyVersion(value ? project.version : undefined)
  const busy = !!pending
  const preferredModel = selectedModel ?? initialModel ?? project.modelCode
  const defaultModelCode = !project.outline ? models[0]?.code : undefined
  const modelCode = models.some((item) => item.code === preferredModel) ? preferredModel : defaultModelCode ?? ''
  const finalized = project.chapters.filter((chapter) => chapter.final).length
  const serial = project.storyMode === 'serial'
  const awaitingOpening = serial && !project.outline
  const backPath = fromDrama
    ? '/ai-apps/drama?' + new URLSearchParams({ source: 'novel', novelId: project.id })
    : '/ai-apps/novel'
  const canAppend = project.confirmed && project.chapterCount < novelChapterLimit
  const visibleChapterPage = Math.min(chapterPage, Math.ceil(project.chapterCount / chapterPageSize))
  const chapterOffset = (visibleChapterPage - 1) * chapterPageSize
  const openAppend = () => { if (!busy && !dirty) setAppendProject(project) }
  const savedInstruction = project.chapters[selected]?.authorInstruction ?? ''
  const chapterInstruction = instructionDraft?.index === selected && instructionDraft.saved === savedInstruction
    ? instructionDraft.value : savedInstruction

  function switchSection(index: number) {
    if (selected === index || busy) return
    setSelected(index)
    if (index >= 0) setChapterPage(Math.floor(index / chapterPageSize) + 1)
    setInstructionDraft(undefined)
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
        <Link to={backPath} onClick={(event) => {
          if (dirty && !window.confirm('放弃当前未保存的修改？')) event.preventDefault()
        }}>← {fromDrama ? '返回 AI 漫剧' : '返回我的作品'}</Link>
      </div>
      <header className="novel-heading">
        <div className="novel-heading-name">
          <NovelTitleEditor title={project.title} modelCode={modelCode || models[0]?.code}
            busy={busy} suggesting={pending === 'suggest_title'} dirty={dirty} run={runTitle} />
          <div className="novel-heading-meta">
            {serial && <Tag>长篇连载</Tag>}
            <span className="novel-heading-progress">{awaitingOpening ? '待规划开篇' : `已定稿 ${finalized} / ${project.chapterCount} 章`}</span>
          </div>
        </div>
        <div className="novel-heading-actions">
          <Button icon={<EditOutlined />} disabled={busy || dirty} title={dirty ? '请先保存当前草稿' : undefined}
            onClick={() => setGuidanceProject(project)}>写作约定</Button>
          <Button icon={<PictureOutlined />} disabled={busy} onClick={() => setEditingCover(true)}>设置封面</Button>
          <div className="novel-model">
            <label htmlFor="novel-model">文本模型</label>
            <Select id="novel-model" value={modelCode || undefined} onChange={setModel} disabled={busy || !models.length}
              options={models.map((item) => ({ value: item.code, label: item.name }))} placeholder="选择模型" />
          </div>
        </div>
      </header>
      {editingCover && <NovelCoverEditor book={project} onClose={() => setEditingCover(false)} />}
      {guidanceProject && <NovelAuthorGuidance initialValue={guidanceProject.authorGuidance ?? ''} saving={pending === 'save_author_guidance'}
        onClose={() => setGuidanceProject(undefined)} onSave={async (value) => {
          await saveAuthorGuidance(guidanceProject, value)
          setGuidanceProject(undefined)
        }} />}
      {appendProject && <NovelAppendChapters project={appendProject} model={models.find((item) => item.code === modelCode) ?? models[0]}
        onClose={() => setAppendProject(undefined)} onReload={() => { setAppendProject(undefined); reload() }}
        onAppend={async (beats) => {
          await append(appendProject, beats)
          setAppendProject(undefined)
          if (appendProject.chapters.every((chapter) => chapter.final)) switchSection(appendProject.chapterCount)
        }} />}
      {!models.length && <Alert type="info" title={modelsNotice} />}
      {pending && <Alert type="info" showIcon icon={<Spin size="small" />} title={revisingOutline ? '正在按意见修改并检查大纲'
        : awaitingOpening && pending === 'generate_outline' ? '正在规划并检查长篇开篇' : messages[pending]}
        description={pending === 'generate_outline' || pending === 'check_outline'
          ? 'AI 会核对章节之间的前因后果，并尝试修正明确的矛盾。'
          : pending === 'write' || pending === 'review' || pending === 'revise_review' ? '可以先在站内切换页面，稍后回来查看结果。' : undefined} />}
      {failure && <Alert type="error" showIcon title={errorMessage(failure)}
        description={awaitingOpening ? '灵感已保存，可以重试规划。'
          : initialModel && !project.outline ? '灵感已保存，可以重试生成大纲，也可以自己填写。' : undefined}
        action={failure instanceof ApiError && failure.code === 'novel_conflict'
          ? <Popconfirm title="重新加载会清除当前未保存的修改，继续吗？" onConfirm={reload}><Button size="small">重新加载</Button></Popconfirm>
          : initialModel && !project.outline && <Button size="small" disabled={busy || dirty || !modelCode}
              onClick={() => run({ kind: 'generate_outline', modelCode })}>重试生成</Button>} />}
      <div className="novel-desk">
        <nav className="novel-index" aria-label="作品目录">
          {sectionButton(-1, '作品大纲', project.confirmed ? '已确认' : '待确认')}
          {canAppend && <button type="button" className="novel-index-item novel-append-trigger" disabled={busy || dirty}
            title={dirty ? '请先保存当前草稿' : undefined} onClick={openAppend}><span><PlusOutlined /> 追加章节</span></button>}
          {!awaitingOpening && project.chapters.slice(chapterOffset, chapterOffset + chapterPageSize).map((chapter, pageIndex) => {
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
          {selected === -1 && !project.outline && (autoOutlining || (awaitingOpening && pending === 'generate_outline'))
            ? <div className="novel-generating-outline">
                <h2>正在规划你的故事</h2>
                <p>{serial ? 'AI 正在拟定全书主线，并根据开篇需要安排首批章节。'
                  : `AI 会根据这条灵感拟好故事设定、人物关系和 ${project.chapterCount} 章的写作目标。`}</p>
              </div>
            : selected === -1
            ? <OutlineEditor key={project.version} project={project} modelCode={modelCode} busy={busy} run={run}
                dirty={dirty} markDirty={markDirty} onStart={() => switchSection(Math.max(0, firstUnfinished))} />
            : <ChapterEditor key={project.version + ':' + selected} project={project} index={selected}
                instruction={chapterInstruction} setInstruction={(value) => setInstructionDraft({ index: selected, saved: savedInstruction, value })}
                modelCode={modelCode} busy={busy} run={run} dirty={dirty} markDirty={markDirty}
                onNext={() => switchSection(selected + 1)} onAppend={canAppend ? openAppend : undefined} />}
        </Card>
      </div>
    </>
  )
}

function NovelAuthorGuidance({ initialValue, saving, onClose, onSave }: {
  initialValue: string; saving: boolean; onClose: () => void; onSave: (value: string) => Promise<void>
}) {
  const [value, setValue] = useState(initialValue)
  const [error, setError] = useState('')
  const guidance = value.trim()

  async function save() {
    if (saving || guidance === initialValue) return
    setError('')
    try {
      await onSave(guidance)
    } catch (error) {
      setError(error instanceof Error ? errorMessage(error) : '保存失败，请稍后重试')
    }
  }

  return (
    <Modal title="写作约定" open width={540} okText="保存约定" cancelText="取消" onOk={() => void save()}
      onCancel={() => { if (!saving) onClose() }} confirmLoading={saving} closable={!saving} keyboard={!saving}
      mask={{ closable: false }} okButtonProps={{ disabled: guidance === initialValue }} cancelButtonProps={{ disabled: saving }}>
      <Form layout="vertical" disabled={saving}>
        <Form.Item label="全书约定" htmlFor="novel-author-guidance"
          extra="适用于后续章节的规划、写作和审稿。保存后可按新约定重新检查现有草稿。">
          <TextArea id="novel-author-guidance" value={value} onChange={(event) => setValue(event.target.value)}
            rows={5} maxLength={2000} showCount placeholder="例如：主角统一称太子为「皇兄」，不用「哥」。" />
        </Form.Item>
      </Form>
      {error && <Alert type="error" showIcon title={error} />}
    </Modal>
  )
}

function OutlineEditor({ project, modelCode, busy, dirty, markDirty, run, onStart }: {
  project: NovelProject; modelCode: string; busy: boolean; dirty: boolean; markDirty: (value: boolean) => void; run: RunAction; onStart: () => void
}) {
  const serial = project.storyMode === 'serial'
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
  const outlineCheck = dirty ? undefined : project.outlineCheck
  const confirmTitle = outlineCheck?.status === 'needs_review' ? '部分因果待确认，仍要锁定当前大纲？'
    : outlineCheck?.status === 'unavailable' ? '因果检查未完成，仍要锁定当前大纲？' : '确认后将锁定大纲，开始逐章写作？'
  const generate = () => run({ kind: 'generate_outline', modelCode })
  const generateButton = <Button icon={<RobotOutlined />} disabled={busy || !modelCode} onClick={project.outline || dirty ? undefined : generate}>
    {project.outline ? '重新生成大纲' : serial ? '规划长篇开篇' : '生成大纲'}
  </Button>
  const changeOutline = (next: NovelOutline) => { setOutline(next); markDirty(true) }
  const brief = <details className="novel-brief" open={!project.outline}><summary>故事构想</summary><p>{project.brief}</p></details>

  if (serial && !project.outline) {
    return <>
      <h2>作品大纲</h2>
      {brief}
      <p className="novel-hint">AI 会根据灵感规划全书主线，并建议开篇章数。</p>
      {generateButton}
    </>
  }

  return (
    <>
      <h2>作品大纲</h2>
      {brief}
      {!project.confirmed && <p className="novel-hint">{project.outline
        ? '重新生成会从最初的故事灵感出发。要在当前大纲上纠正或补充细节，可以按意见修改。'
        : '可先生成大纲，也可手动填写。确认后开放第一章，并锁定大纲。'}</p>}
      {complete && (!project.confirmed || outlineCheck) && (
        <section className="novel-outline-check" aria-label="大纲因果检查">
          <div className="novel-outline-check-header">
            {outlineCheck?.status === 'passed'
              ? <Tag color="success">{outlineCheck.repaired ? '已自动修正，因果检查通过' : '因果检查通过'}</Tag>
              : outlineCheck?.status === 'needs_review' ? <Tag color="warning">部分因果需要确认</Tag>
              : outlineCheck?.status === 'unavailable' ? <Tag color="warning">因果检查未完成</Tag>
              : <Tag>{dirty ? '修改后待检查' : '尚未检查因果'}</Tag>}
            {!project.confirmed && outlineCheck?.status !== 'passed' && (
              <Button size="small" disabled={busy || !modelCode}
                onClick={() => run({ kind: 'check_outline', modelCode, outline: dirty ? outline : undefined })}>
                {dirty ? '检查并保存' : outlineCheck ? '重新检查' : 'AI 检查因果'}
              </Button>
            )}
            {dirty && <small className="novel-hint">AI 会检查当前修改，并尝试修正明确的矛盾。</small>}
          </div>
          {outlineCheck?.status === 'needs_review' && outlineCheck.issues.length > 0 && (
            <ul className="novel-outline-issues">
              {outlineCheck.issues.map((issue, index) => (
                <li key={index}><strong>第 {issue.chapter} 章</strong>{issue.message}
                  {issue.quote && <q>{issue.quote}</q>}
                </li>
              ))}
            </ul>
          )}
        </section>
      )}
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
        {([{ key: 'synopsis', label: serial ? '全书主线' : '故事梗概' }, { key: 'world', label: '世界设定' }, { key: 'characters', label: '主要人物与关系' }] as const).map(({ key, label }) => (
          <Form.Item key={key} label={label} htmlFor={'novel-outline-' + key}>
            <TextArea id={'novel-outline-' + key} rows={3} value={outline[key]} readOnly={project.confirmed} maxLength={4000}
              onChange={(event) => changeOutline({ ...outline, [key]: event.target.value })} />
          </Form.Item>
        ))}
        <h3>{serial && !project.confirmed ? `开篇蓝图 · AI 建议 ${project.chapterCount} 章` : '章节蓝图'}</h3>
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
          <Popconfirm title={confirmTitle} onConfirm={() => run({ kind: 'confirm_outline', outline })}>
            <Button type="primary" disabled={busy || !complete}>确认大纲</Button>
          </Popconfirm>
        </Space>}
      </div>
    </>
  )
}

function ChapterEditor({ project, index, instruction, setInstruction, modelCode, busy, dirty, markDirty, run, onNext, onAppend }: {
  project: NovelProject; index: number; modelCode: string; busy: boolean; dirty: boolean
  instruction: string; setInstruction: (value: string) => void
  markDirty: (value: boolean) => void; run: RunAction; onNext: () => void; onAppend?: () => void
}) {
  const chapter = project.chapters[index]
  const beat = project.outline?.beats[index]
  const [body, setBody] = useState(chapter.body)
  const [summary, setSummary] = useState(chapter.summary)
  const number = index + 1
  const summaryMissing = !!chapter.body.trim() && !chapter.summary.trim()
  const awaitingRecheck = chapter.reviewFailed && !!chapter.review
  const reviewPassed = chapter.reviewPassed && !dirty && !chapter.reviewFailed && !summaryMissing
  const writeDisabled = busy || dirty || !modelCode
  const generate = () => run({ kind: 'write', chapter: number, modelCode, instruction })
  const reviseFromReview = () => run({ kind: 'revise_review', chapter: number, modelCode })
  const writeButton = <Button icon={<RobotOutlined />} disabled={writeDisabled}
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
        {!chapter.final && <Form.Item label="本章写作要求（可选）" htmlFor="novel-instruction"
          extra="生成正文或 AI 改写时会保存这些要求，用于本章后续写作和审稿。">
          <TextArea id="novel-instruction" rows={2} value={instruction} maxLength={1200} onChange={(event) => setInstruction(event.target.value)} />
        </Form.Item>}
      </Form>
      {reviewPassed && !chapter.final && <Alert type="success" showIcon title="审稿通过，可以定稿" />}
      {chapter.review && !reviewPassed && <aside className="novel-review">
        <div className="novel-review-header">
          <h3>{dirty ? '上一版草稿的审稿意见' : awaitingRecheck ? '上轮意见待复核' : '审稿意见'}</h3>
          {!chapter.final && !awaitingRecheck && !summaryMissing && <Popconfirm title="按这份审稿意见修改？"
            description="会更新本章草稿和连续性摘要，之后只核对原问题及本次修改引入的问题。"
            okText="开始修改" cancelText="取消" onConfirm={reviseFromReview}>
            <Button type="primary" size="small" icon={<RobotOutlined />} disabled={writeDisabled}>按审稿意见修改</Button>
          </Popconfirm>}
        </div>
        <p>{chapter.review}</p>
      </aside>}
      {summaryMissing && !chapter.final && <Alert type="warning" showIcon title="正文已保存，连续性摘要待补全"
        description="可以点击下方「AI 补摘要并审稿」，保留已保存的正文。" />}
      {chapter.reviewFailed && !chapter.final && !summaryMissing && <Alert type="warning" showIcon
        title={awaitingRecheck ? '修改已保存，复核未完成' : '正文已保存，自动审稿未完成'}
        description={awaitingRecheck ? '可以点击下方「重试复核」核对上轮意见。' : '可以点击下方「AI 审稿」重新检查这版草稿。'} />}
      <div className="novel-actions">
        {chapter.final ? number < project.chapterCount ? <Button type="primary" onClick={onNext}>进入下一章</Button>
          : onAppend && <Button type="primary" icon={<PlusOutlined />} disabled={busy} onClick={onAppend}>追加章节</Button> : <Space wrap>
          {chapter.body ? <Popconfirm title="用改写结果替换已保存的草稿？" onConfirm={generate}>{writeButton}</Popconfirm> : writeButton}
          <Button disabled={busy || !dirty || !body.trim()} onClick={() => run({ kind: 'save_draft', chapter: number, body, summary })}>保存草稿</Button>
          <Button disabled={busy || dirty || !chapter.body || !modelCode} onClick={() => run({ kind: 'review', chapter: number, modelCode })}>
            {summaryMissing ? 'AI 补摘要并审稿' : awaitingRecheck ? '重试复核' : 'AI 审稿'}
          </Button>
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
