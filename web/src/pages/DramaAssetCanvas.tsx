import { ArrowLeftOutlined, CopyOutlined, DeleteOutlined, EditOutlined, EllipsisOutlined, EyeOutlined, GiftOutlined, PictureOutlined, PlusOutlined, UploadOutlined, UserOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Alert, Button, Dropdown, Empty, Input, Modal, Select, Skeleton, Tag, Tooltip, message } from 'antd'
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ComponentProps, type DragEvent } from 'react'
import { useSearchParams } from 'react-router-dom'
import { Background, Handle, MiniMap, Position, ReactFlow, ReactFlowProvider, SelectionMode, applyNodeChanges, useNodesInitialized, useReactFlow, type Connection, type OnConnectEnd, type Node, type NodeProps, type Viewport, type NodeChange, type Edge, type XYPosition } from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { ApiError } from '../api/client'
import { createDramaCanvasAsset, deriveDramaForm, editDramaForm, generateDramaImage, getDramaCanvas, listDramaCanvasGenerations, saveDramaCanvasLayout, startDramaCanvasGeneration, uploadDramaCanvasMedia, type DramaAssetForm, type DramaAssetKind, type DramaCanvas, type DramaCanvasCard as MediaCard, type DramaCanvasCardKind as CardKind, type DramaCanvasExtras as CanvasExtras, type DramaCanvasGenerationInput, type DramaCanvasGenerationJob, type DramaCanvasGenerationKind, type DramaCanvasLayout, type DramaCanvasLink, type DramaProject } from '../api/drama'
import { DramaCanvasCreateMenu, type CanvasCreateKind } from './DramaCanvasCreateMenu'
import { DramaCanvasInlineEditor, type DramaCanvasInlineInput } from './DramaCanvasInlineEditor'
import { DramaCanvasFloatingPanel } from './DramaCanvasFloatingPanel'
import { DramaCanvasMediaGeneration, type CanvasGenerationReference } from './DramaCanvasMediaGeneration'
import { DramaCanvasNodeToolbar, DramaCanvasToolDock, DramaCanvasViewTools, type DramaCanvasTool } from './DramaCanvasTools'
import { DramaImageModelSelect, useDramaCanvas } from './DramaImageTools'
import { DramaStylePicker } from './DramaStylePicker'
import { arrangeCanvasNodes, canvasNodeSize, findCanvasNodePosition, revealCanvasNode } from './drama-canvas-geometry'
import { useCanvasHistory } from './drama-canvas-history'
import './drama-canvas.css'

type AssetNode = Node<{ form: DramaAssetForm }, 'asset'>
const cardNames = { text: '文本', image: '图片', video: '视频', audio: '音频' } as const
type MediaNode = Node<MediaCard, 'media'>
type CanvasNode = AssetNode | MediaNode
type NodeKind = DramaAssetKind | CardKind
const emptyExtras: CanvasExtras = { hidden: [], cards: [], links: [] }
type ConnectionStart = { nodeId: string; type: 'source' | 'target'; handleId?: string | null }
type CanvasMenu = { point: XYPosition; node?: CanvasNode; edge?: Edge; connection?: ConnectionStart; toolbar?: boolean }
type CanvasSnapshot = { extras: CanvasExtras; formPositions: Map<string, XYPosition> }
type InlineEdit = { id: string; kind: NodeKind; title: string; content: string } & (
  { mode: 'media' } | { mode: 'asset'; form: DramaAssetForm }
)
type NodeEditorProps = ComponentProps<typeof DramaCanvasInlineEditor>
const CanvasCardControls = createContext<{
  activeAsset: string
  edit: (id: string) => void
  imageSettings: (id: string) => void
  derive: (id: string) => void
  preview: (id: string) => void
  duplicate: (id: string) => void
  remove: (id: string) => void
} | null>(null)
const CanvasMedia = createContext<{
  jobs: Map<string, DramaCanvasGenerationJob>
  active?: { id: string; references: CanvasGenerationReference[]; submitting: boolean; error?: string }
  open: (id: string) => void
  close: () => void
  edit: (id: string) => void
  generate: (id: string, input: DramaCanvasGenerationInput) => void
  adopt: (id: string, url: string) => void
} | null>(null)
const kindNames = { character: '角色', scene: '图片', prop: '道具' }
const kindIcons = { character: <UserOutlined />, scene: <PictureOutlined />, prop: <GiftOutlined /> }

function assetFormLabel(form: DramaAssetForm) {
  return form.parentId ? `${form.assetName} · ${form.name}` : form.assetName
}

function isGenerationKind(kind: NodeKind): kind is DramaCanvasGenerationKind {
  return kind === 'image' || kind === 'video'
}

function nodeGeometry(node: CanvasNode) {
  const group = node.type === 'asset' ? node.data.form.kind : node.data.kind
  return { id: node.id, group, position: node.position, ...canvasNodeSize(group, node.measured) }
}

function readExtras(key: string): CanvasExtras {
  try {
    const raw = localStorage.getItem(key)
    if (!raw) return emptyExtras
    const saved = JSON.parse(raw) as CanvasExtras & { videos?: (Omit<MediaCard, 'kind' | 'content'> & { url: string })[] }
    const { videos, ...current } = saved
    return {
      hidden: current.hidden ?? [],
      cards: current.cards ?? (videos ?? []).map(({ url, ...card }) => ({ ...card, kind: 'video', content: url })),
      links: (current.links ?? []).map(({ id, source, target, sourceHandle, targetHandle }) => ({ id, source, target, sourceHandle, targetHandle })),
    }
  } catch {
    return emptyExtras
  }
}

function mergeLegacyExtras(saved: CanvasExtras, local: CanvasExtras, forms: DramaAssetForm[]): CanvasExtras {
  const knownForms = new Set(forms.map((form) => form.id))
  const cards = [...saved.cards, ...local.cards.filter((card) => !saved.cards.some((item) => item.id === card.id))]
  const endpoints = new Set([...knownForms, ...cards.map((card) => card.id)])
  const links = [...saved.links]
  for (const link of local.links) {
    if (endpoints.has(link.source) && endpoints.has(link.target) && !links.some((item) => item.id === link.id || (item.source === link.source && item.target === link.target))) links.push(link)
  }
  return { cards, links, hidden: [...new Set([...saved.hidden, ...local.hidden.filter((id) => knownForms.has(id))])] }
}

function sameConnection(link: DramaCanvasLink, connection: Pick<Connection, 'source' | 'target'>) {
  return link.source === connection.source && link.target === connection.target
}

function MediaCanvasNode({ id, data, selected }: NodeProps<MediaNode>) {
  const [failedURL, setFailedURL] = useState('')
  const controls = useContext(CanvasCardControls)
  const generation = useContext(CanvasMedia)
  const job = generation?.jobs.get(id)
  const content = data.content || job?.resultUrl || ''
  const failed = Boolean(content) && failedURL === content
  const active = generation?.active?.id === id ? generation.active : undefined
  const canGenerate = isGenerationKind(data.kind)
  const showGeneration = active && canGenerate
  return <div className={`drama-canvas-node scene media ${data.kind}${selected ? ' selected' : ''}${showGeneration ? ' is-generating' : ''}`}>
    <div className="drama-canvas-node-caption"><span>{data.title === cardNames[data.kind] ? data.title : `${cardNames[data.kind]} · ${data.title}`}</span></div>
    <div className="drama-canvas-node-body">
      <Handle type="target" position={Position.Left} />
      {failed && <div className="drama-canvas-media-error">素材无法读取，可以编辑节点换一份素材。</div>}
      {!failed && content && data.kind === 'video' && <video className="nodrag nowheel" controls playsInline preload="metadata" src={content} aria-label={data.title} onError={() => setFailedURL(content)} />}
      {!failed && content && data.kind === 'audio' && <audio className="nodrag nowheel" controls preload="metadata" src={content} aria-label={data.title} onError={() => setFailedURL(content)} />}
      {!failed && content && data.kind === 'image' && <div className="drama-canvas-node-image"><img src={content} alt={data.title} loading="lazy" onError={() => setFailedURL(content)} /></div>}
      {data.kind === 'text' && content && <div className="drama-canvas-text nodrag nowheel">{content}</div>}
      {!content && !showGeneration && <div className={`drama-canvas-media-placeholder ${data.kind}`}>
        {canGenerate && <PictureOutlined />}
        <span>{job?.status === 'running' ? '后台生成中' : job?.status === 'failed' ? '生成失败' : data.kind === 'text' ? '还没有文本内容' : `暂无${cardNames[data.kind]}`}</span>
      </div>}
      {content && !showGeneration && (job?.status === 'running' || job?.status === 'failed' || (job?.resultUrl && job.resultUrl !== content)) &&
        <div className="drama-canvas-media-empty" role="status">{job.status === 'running' ? '后台生成中' : job.status === 'failed' ? '生成失败' : '生成结果待查看'}</div>}
      {!showGeneration && controls && <div className="drama-canvas-card-actions nodrag" aria-label={`${data.title}操作`}>
        <Button size="small" type="text" icon={<EditOutlined />} onClick={() => controls.edit(id)}>
          {data.kind === 'text' ? '编辑文本' : !content && data.kind === 'audio' ? '导入音频' : '编辑节点'}
        </Button>
        {canGenerate && <Button size="small" type="text" className="drama-canvas-card-generate" icon={<PictureOutlined />} onClick={() => generation?.open(id)}>{job ? '查看生成' : 'AI 生成'}</Button>}
        <Dropdown trigger={['click']} menu={{ items: [
          ...(data.kind !== 'text' && content ? [{ key: 'preview', icon: <EyeOutlined />, label: '预览' }] : []),
          { key: 'copy', icon: <CopyOutlined />, label: '复制节点' }, { key: 'remove', icon: <DeleteOutlined />, label: '删除节点', danger: true },
        ], onClick: ({ key, domEvent }) => { domEvent.stopPropagation(); if (key === 'preview') controls.preview(id); if (key === 'copy') controls.duplicate(id); if (key === 'remove') controls.remove(id) } }}>
          <Button size="small" type="text" className="drama-canvas-card-more" icon={<EllipsisOutlined />} aria-label="更多节点操作" />
        </Dropdown>
      </div>}
      {showGeneration && generation && active && isGenerationKind(data.kind) &&
        <DramaCanvasMediaGeneration kind={data.kind} content={data.content} job={job} references={active.references} busy={active.submitting}
          error={active.error} onGenerate={(input) => generation.generate(id, input)} onAdopt={(url) => generation.adopt(id, url)}
          onImport={() => generation.edit(id)} onClose={generation.close} />}
      <Handle type="source" position={Position.Right} />
    </div>
  </div>
}

const busy = (form?: DramaAssetForm) => form?.imageStatus === 'queued' || form?.imageStatus === 'running'
function AssetCanvasNode({ data, selected }: NodeProps<AssetNode>) {
  const f = data.form
  const controls = useContext(CanvasCardControls)
  const title = assetFormLabel(f)
  return <div className={`drama-canvas-node drama-canvas-asset-card ${f.kind}${selected ? ' selected' : ''}`}>
    <div className="drama-canvas-node-body">
      <Handle type="target" position={Position.Left} />
      <div className="drama-canvas-node-caption">{kindIcons[f.kind]}<strong title={title}>{title}</strong><small>{f.parentId && f.kind !== 'scene' ? '派生形象' : kindNames[f.kind]}</small></div>
      <div className="drama-canvas-node-image">
        {f.imageUrl ? <img src={f.imageUrl} alt={title} loading="lazy" /> : <div className="drama-canvas-node-placeholder">{kindIcons[f.kind]}<span>{busy(f) ? '图片生成中…' : '尚未生成图片'}</span></div>}
        {busy(f) && f.imageUrl && <Tag color="processing">图片生成中</Tag>}
        {f.imageUrl && <button className="drama-canvas-preview nodrag" aria-label={`预览${title}`} onClick={() => controls?.preview(f.id)}><EyeOutlined /></button>}
      </div>
      <div className="drama-canvas-node-copy">
        <p title={f.description}>{f.description || (f.kind === 'scene' ? '还没有图片描述' : '还没有形象描述')}</p>
        {f.imageError && <Tooltip title={f.imageError}><span className="drama-canvas-node-error">生成失败，可在生图设置中重试</span></Tooltip>}
      </div>
      {controls && <div className="drama-canvas-card-actions nodrag" aria-label={`${title}操作`}>
        <Button type="text" size="small" icon={<EditOutlined />} disabled={busy(f)} onClick={() => controls.edit(f.id)}>{f.kind === 'scene' ? '编辑图片' : '编辑形象'}</Button>
        <Button type="text" size="small" className="drama-canvas-card-generate" icon={<PictureOutlined />} aria-pressed={controls.activeAsset === f.id} onClick={() => controls.imageSettings(f.id)}>生图设置</Button>
        <Dropdown trigger={['click']} menu={{ items: [
          { key: 'derive', icon: <PlusOutlined />, label: f.kind === 'scene' ? '添加派生图片' : '添加派生形象' },
          { key: 'hide', icon: <DeleteOutlined />, label: f.kind === 'scene' ? '隐藏图片' : '隐藏形象', danger: true },
        ], onClick: ({ key, domEvent }) => { domEvent.stopPropagation(); if (key === 'derive') controls.derive(f.id); if (key === 'hide') controls.remove(f.id) } }}>
          <Button type="text" size="small" className="drama-canvas-card-more" icon={<EllipsisOutlined />} aria-label="更多节点操作" />
        </Dropdown>
      </div>}
      <Handle type="source" position={Position.Right} />
    </div>
  </div>
}

const nodeTypes = { asset: AssetCanvasNode, media: MediaCanvasNode }

export function DramaAssetCanvas({ project }: { project: DramaProject }) { return <ReactFlowProvider><AssetCanvas key={project.id} project={project} /></ReactFlowProvider> }
function AssetCanvas({ project }: { project: DramaProject }) {
  const [params, setParams] = useSearchParams()
  const query = useDramaCanvas(project.id)
  const cache = useQueryClient()
  const flow = useReactFlow<CanvasNode>()
  const measured = useNodesInitialized()
  const [messageApi, holder] = message.useMessage()
  const [modal, modalHolder] = Modal.useModal()
  const [nodes, setNodes] = useState<CanvasNode[]>([])
  const nodesRef = useRef(nodes)
  const formPositions = useRef(new Map<string, XYPosition>())
  const history = useCanvasHistory<CanvasSnapshot>()
  const revealAfterAdd = useRef('')
  const revealEditor = useRef('')
  const [tool, setTool] = useState<DramaCanvasTool>(() => window.matchMedia('(pointer: coarse)').matches ? 'pan' : 'select')
  const [spacePan, setSpacePan] = useState(false)
  const [showMinimap, setShowMinimap] = useState(() => window.innerWidth > 1050)
  const [searchOpen, setSearchOpen] = useState(false)
  const [selecting, setSelecting] = useState(false)
  const selectingRef = useRef(false)
  const [modelSettingsOpen, setModelSettingsOpen] = useState(false)
  const extrasKey = `drama-canvas-extras:${project.id}`
  const [extras, setExtras] = useState(emptyExtras)
  const extrasRef = useRef(emptyExtras)
  const [mediaGeneratorId, setMediaGeneratorId] = useState('')
  const mediaSubmitting = useRef(new Set<string>())
  const [submittingMedia, setSubmittingMedia] = useState<Set<string>>(new Set())
  const [mediaErrors, setMediaErrors] = useState<Record<string, string>>({})
  const extrasStored = useRef(false)
  const extrasVerified = useRef(false)
  const importingLegacy = useRef(false)
  const [inlineEdit, setInlineEdit] = useState<InlineEdit | null>(null)
  const inlineRef = useRef<InlineEdit | null>(null)
  const [inlineError, setInlineError] = useState('')
  const [inlineSaving, setInlineSaving] = useState(false)
  const inlineSavingRef = useRef(false)
  const [contextMenu, setContextMenu] = useState<CanvasMenu | null>(null)
  const [selectedId, setSelectedId] = useState('')
  const selectionURL = useRef('')
  const selectWhenAdded = useRef(new Set<string>())
  const [selectedLinks, setSelectedLinks] = useState<Set<string>>(new Set())
  const [preview, setPreview] = useState<DramaAssetForm | null>(null)
  const [mediaPreview, setMediaPreview] = useState<MediaCard | null>(null)
  const [working, setWorking] = useState(false)
  const [uploading, setUploading] = useState(false)
  const uploadInProgress = useRef(false)
  const fileInput = useRef<HTMLInputElement>(null)
  const addButton = useRef<HTMLButtonElement>(null)
  const filePickerPoint = useRef<XYPosition | undefined>(undefined)
  const [dragOver, setDragOver] = useState(false)
  const dragDepth = useRef(0)
  const shell = useRef<HTMLDivElement>(null)
  const [prompt, setPrompt] = useState('')
  const [generationForId, setGenerationForId] = useState('')
  const [style, setStyle] = useState({ id: '', name: '' })
  const [ratio, setRatio] = useState<DramaAssetForm['aspectRatio']>('9:16')
  const [saveStatus, setSaveStatus] = useState('加载中…')
  const [saveError, setSaveError] = useState('')
  const [conflict, setConflict] = useState(false)
  const revision = useRef(0)
  const viewport = useRef<Viewport>({ x: 0, y: 0, zoom: 0.6 })
  const pending = useRef<Omit<DramaCanvasLayout, 'revision'> | null>(null)
  const saving = useRef<Promise<void> | null>(null)
  const blocked = useRef(false)
  const topologyChanging = useRef(false)
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const initialized = useRef(false)
  const isPanning = spacePan || tool === 'pan'
  const generationKey = ['drama-canvas-generations', project.id]
  const generations = useQuery({
    queryKey: generationKey,
    queryFn: () => listDramaCanvasGenerations(project.id),
    enabled: extras.cards.some((card) => isGenerationKind(card.kind)),
    refetchInterval: (query) => query.state.data?.some((job) => job.status === 'running') ? 2500 : false,
  })
  const generationJobs = useMemo(() => new Map((generations.data ?? []).map((job) => [job.nodeId, job])), [generations.data])

  function snapshot(): CanvasSnapshot {
    return { extras: extrasRef.current, formPositions: new Map(formPositions.current) }
  }

  function saveExtras(next: CanvasExtras, remember = true) {
    if (remember) history.push(snapshot())
    extrasStored.current = true
    extrasRef.current = next
    setExtras(next)
    schedule()
  }

  function canvasCenter() {
    const bounds = shell.current?.getBoundingClientRect()
    return bounds ? { x: bounds.left + bounds.width / 2 - 90, y: bounds.top + bounds.height / 2 - 55 } : { x: 200, y: 200 }
  }

  function viewSafeArea() {
    const bounds = shell.current?.getBoundingClientRect()
    const width = bounds?.width ?? window.innerWidth
    const height = bounds?.height ?? window.innerHeight
    return { x: 75, y: 112, width: Math.max(1, width - 100), height: Math.max(1, height - (generationForId ? 270 : 180)) }
  }

  function visibleCanvasArea() {
    const safe = viewSafeArea()
    const { x, y, zoom } = flow.getViewport()
    return { x: (safe.x - x) / zoom, y: (safe.y - y) / zoom, width: safe.width / zoom, height: safe.height / zoom }
  }

  function openFilePicker(point?: XYPosition) {
    filePickerPoint.current = point
    fileInput.current?.click()
  }

  function setEditor(next: InlineEdit | null) {
    inlineRef.current = next
    setInlineEdit(next)
  }

  function openEditor(next: InlineEdit) {
    setContextMenu(null)
    const current = inlineRef.current
    if (current) {
      if (current.id !== next.id) messageApi.info('请先保存或取消正在编辑的节点')
      return
    }
    setInlineError('')
    setGenerationForId('')
    setMediaGeneratorId('')
    setTool('select')
    revealEditor.current = ''
    choose(next.id)
    setEditor(next)
  }

  function startEditing(id: string) {
    const node = nodesRef.current.find((item) => item.id === id)
    if (!node) return
    if (node.type === 'media') {
      openEditor({ id, mode: 'media', kind: node.data.kind, title: node.data.title, content: node.data.content })
    } else if (!busy(node.data.form)) {
      const form = node.data.form
      openEditor({ id, mode: 'asset', kind: form.kind, title: form.parentId ? form.name : form.assetName, content: form.description, form })
    }
  }

  function cancelEditor() {
    if (inlineSavingRef.current) return
    revealEditor.current = ''
    setEditor(null)
  }

  function openMediaGenerator(id: string) {
    if (inlineRef.current) { messageApi.info('请先保存或取消正在编辑的节点'); return }
    setTool('select')
    choose(id)
    setMediaGeneratorId(id)
    revealEditor.current = id
  }

  function openAssetGeneration(id: string) {
    if (inlineRef.current) { messageApi.info('请先保存或取消正在编辑的节点'); return }
    setTool('select')
    choose(id)
    setMediaGeneratorId('')
    setGenerationForId(generationForId === id ? '' : id)
  }

  function previewCard(card: MediaCard) {
    const content = card.content || generationJobs.get(card.id)?.resultUrl
    if (content) setMediaPreview({ ...card, content })
  }

  function adoptMediaResult(id: string, url: string) {
    if (generationJobs.get(id)?.resultUrl !== url) return
    const graph = extrasRef.current
    saveExtras({ ...graph, cards: graph.cards.map((card) => card.id === id ? { ...card, content: url } : card) })
  }

  async function generateMedia(id: string, input: DramaCanvasGenerationInput) {
    if (mediaSubmitting.current.has(id) || generationJobs.get(id)?.status === 'running') return
    mediaSubmitting.current.add(id)
    setSubmittingMedia(new Set(mediaSubmitting.current))
    setMediaErrors((errors) => ({ ...errors, [id]: '' }))
    try {
      if (topologyChanging.current) throw new Error('画布正在更新，请稍后提交')
      await flush()
      if (pending.current) throw new Error('请等画布保存完成再提交生成')
      const job = await startDramaCanvasGeneration(project.id, id, revision.current, input)
      await cache.cancelQueries({ queryKey: generationKey })
      cache.setQueryData<DramaCanvasGenerationJob[]>(generationKey, (jobs) => [...(jobs ?? []).filter((item) => item.nodeId !== id), job])
      void cache.invalidateQueries({ queryKey: generationKey })
    } catch (error) {
      const canvasChanged = error instanceof ApiError && error.status === 409
      setMediaErrors((errors) => ({ ...errors, [id]: canvasChanged ? '画布有更新，请核对连线后再生成。'
        : error instanceof Error ? error.message : '提交生成任务失败，请重试' }))
      if (canvasChanged) void query.refetch()
      void cache.invalidateQueries({ queryKey: generationKey })
    } finally {
      mediaSubmitting.current.delete(id)
      setSubmittingMedia(new Set(mediaSubmitting.current))
    }
  }

  function withConnection(links: DramaCanvasLink[], target: string, origin?: ConnectionStart) {
    if (!origin || !nodesRef.current.some((node) => node.id === origin.nodeId)) return links
    return [...links, origin.type === 'source'
      ? { id: crypto.randomUUID(), source: origin.nodeId, target, sourceHandle: origin.handleId }
      : { id: crypto.randomUUID(), source: target, target: origin.nodeId, targetHandle: origin.handleId }]
  }

  function duplicateCards(source: MediaNode[]) {
    if (!source.length) return
    const current = extrasRef.current
    const occupied = nodesRef.current.map(nodeGeometry)
    const ids = new Map<string, string>()
    const visible = visibleCanvasArea()
    const copies = source.map((node) => {
      const size = canvasNodeSize(node.data.kind, node.measured)
      const anchor = { x: node.position.x + size.width + 36, y: node.position.y }
      const position = findCanvasNodePosition(anchor, size, occupied, visible)
      const copy = { ...node.data, id: crypto.randomUUID(), position, content: node.data.content || generationJobs.get(node.id)?.resultUrl || '' }
      ids.set(node.id, copy.id)
      occupied.push({ id: copy.id, group: copy.kind, position, ...size })
      return copy
    })
    const links = current.links.flatMap((link) => {
      const target = ids.get(link.target)
      return target ? [{ ...link, id: crypto.randomUUID(), source: ids.get(link.source) ?? link.source, target }] : []
    })
    saveExtras({ ...current, cards: [...current.cards, ...copies], links: [...current.links, ...links] })
    revealAfterAdd.current = copies[0].id
    chooseNodes(copies.map((card) => card.id))
    setContextMenu(null)
  }

  function rememberSelection(id: string) {
    if (selectionURL.current === id) return
    selectionURL.current = id
    setSelectedId(id)
    setGenerationForId('')
    setMediaGeneratorId((current) => current === id ? current : '')
    setParams((current) => {
      const next = new URLSearchParams(current)
      next.delete('focus')
      if (id) next.set('selected', id)
      else next.delete('selected')
      return next
    }, { replace: true })
  }

  function chooseNodes(ids: string[]) {
    const picked = new Set(ids)
    const next = nodesRef.current.map((node) => node.selected === picked.has(node.id) ? node : { ...node, selected: picked.has(node.id) })
    selectWhenAdded.current = new Set(picked)
    for (const node of next) selectWhenAdded.current.delete(node.id)
    nodesRef.current = next
    setNodes(next)
    setSelectedLinks(new Set())
    rememberSelection(ids.length === 1 ? ids[0] : '')
  }

  function choose(id: string) { chooseNodes(id ? [id] : []) }

  function restoreHistory(direction: 'undo' | 'redo') {
    if (inlineRef.current || working || uploadInProgress.current || nodesRef.current.some((node) => node.dragging) || blocked.current) return
    const state = history[direction](snapshot())
    if (!state) return
    formPositions.current = new Map([...formPositions.current, ...state.formPositions])
    const cards = new Map(state.extras.cards.map((card) => [card.id, card.position]))
    nodesRef.current = nodesRef.current.map((node) => ({ ...node, position: (node.type === 'asset' ? formPositions.current : cards).get(node.id) ?? node.position }))
    choose('')
    saveExtras(state.extras, false)
  }

  const selected = nodes.some((node) => node.id === selectedId && node.selected) ? query.data?.forms.find((f) => f.id === selectedId) : undefined
  const selectedRef = useRef(selected)
  selectedRef.current = selected
  const drain = useCallback(async () => {
    if (saving.current) return saving.current
    if (blocked.current || topologyChanging.current) return
    const task = (async () => {
      while (pending.current && !blocked.current && !topologyChanging.current) {
        const layout = pending.current
        pending.current = null
        setSaveStatus('保存中…')
        try {
          const result = await saveDramaCanvasLayout(project.id, { ...layout, revision: revision.current })
          revision.current = result.revision
          if (layout.extras && !extrasVerified.current) {
            // 旧版后端会忽略 extras。先读回一次，再清理浏览器旧数据。
            const stored = await getDramaCanvas(project.id)
            if (!stored.extras) throw new Error('服务端暂未保存素材和连线，请稍后重试。')
            extrasVerified.current = true
          }
          const positions = new Map(layout.positions.map((position) => [position.id, position]))
          cache.setQueryData<DramaCanvas>(['drama-canvas', project.id], (current) => {
            if (!current || current.revision > result.revision) return current
            return { ...current, x: layout.x, y: layout.y, zoom: layout.zoom, revision: result.revision,
              ...(layout.extras ? { extras: layout.extras } : {}),
              forms: current.forms.map((form) => {
                const position = positions.get(form.id)
                return position ? { ...form, x: position.x, y: position.y } : form
              }) }
          })
          if (importingLegacy.current && layout.extras) {
            importingLegacy.current = false
            try { localStorage.removeItem(extrasKey) } catch { /* 服务器已经保存 */ }
          }
          setSaveStatus(pending.current ? '待保存' : '已保存')
          setSaveError('')
        } catch (error) {
          pending.current = pending.current || layout
          blocked.current = true
          setSaveStatus('尚未保存')
          setConflict(error instanceof ApiError && error.status === 409)
          setSaveError(error instanceof Error ? error.message : '画布保存失败')
        }
      }
    })()
    saving.current = task
    try { await task } finally { saving.current = null }
  }, [cache, project.id, extrasKey])
  async function flush() { clearTimeout(timer.current); await drain(); if (blocked.current) throw new Error('画布尚未保存，请先处理保存错误') }
  function schedule(nextNodes = nodesRef.current, forms = query.data?.forms ?? []) {
    if (!revision.current) return
    const positions = new Map(nextNodes.map((node) => [node.id, node.position]))
    pending.current = { ...viewport.current, positions: forms.map((form) => ({
      id: form.id,
      ...(formPositions.current.get(form.id) ?? positions.get(form.id) ?? { x: form.x, y: form.y }),
    })), ...(extrasStored.current ? { extras: extrasRef.current } : {}) }
    setSaveStatus('待保存'); clearTimeout(timer.current); timer.current = setTimeout(() => void drain(), 650)
  }
  async function derive(id: string) {
    if (working || topologyChanging.current || inlineSavingRef.current) return
    setWorking(true)
    try {
      await flush()
      const expected = revision.current + 1
      topologyChanging.current = true
      const result = await deriveDramaForm(project.id, id)
      const refreshed = await query.refetch({ throwOnError: true })
      if (refreshed.data?.revision !== expected) {
        blocked.current = true
        setConflict(true)
        setSaveStatus('尚未同步')
        setSaveError('派生形象已创建，画布版本同时发生变化，请重新加载。')
        return
      }
      revision.current = expected
      revealAfterAdd.current = result.id
      choose(result.id)
      messageApi.success('已添加派生形象，请修改描述后生成图片')
    } catch (error) {
      messageApi.error(error instanceof Error ? error.message : '添加派生形象失败')
    } finally {
      topologyChanging.current = false
      setWorking(false)
      void drain()
    }
  }
  async function saveInlineCard(id: string, kind: CardKind, input: DramaCanvasInlineInput) {
    let content = input.content
    if (input.file) {
      if (uploadInProgress.current) throw new Error('请等本次素材上传完成')
      uploadInProgress.current = true
      try {
        const media = await uploadDramaCanvasMedia(project.id, input.file)
        if (media.kind !== kind) throw new Error(`请选择${cardNames[kind]}文件`)
        content = media.url
      } finally {
        uploadInProgress.current = false
      }
    }
    const graph = extrasRef.current
    if (!graph.cards.some((card) => card.id === id)) throw new Error('当前节点已经移除')
    saveExtras({ ...graph, cards: graph.cards.map((card) => card.id === id ? { ...card, title: input.title, content } : card) })
  }

  async function saveInline(input: DramaCanvasInlineInput) {
    const edit = inlineRef.current
    if (!edit || inlineSavingRef.current) return
    inlineSavingRef.current = true
    setInlineSaving(true)
    setInlineError('')
    try {
      if (edit.mode === 'asset') {
        await editDramaForm(project.id, { ...edit.form, name: input.title, description: input.content })
        await query.refetch()
      } else {
        const node = nodesRef.current.find((item) => item.id === edit.id)
        if (node?.type !== 'media') throw new Error('当前节点已经移除')
        await saveInlineCard(edit.id, node.data.kind, input)
      }
      setEditor(null)
      choose(edit.id)
    } catch (error) {
      setInlineError(error instanceof Error ? error.message : '节点保存失败，请重试')
    } finally {
      inlineSavingRef.current = false
      setInlineSaving(false)
    }
  }
  useEffect(() => {
    const data = query.data
    if (!data) return
    if (data.extras) extrasVerified.current = true
    let replacePositions = false
    if (revision.current === 0) {
      revision.current = data.revision
      viewport.current = { x: data.x, y: data.y, zoom: data.zoom }
      const local = readExtras(extrasKey)
      if (local.cards.length || local.links.length || local.hidden.length) {
        importingLegacy.current = true
        saveExtras(mergeLegacyExtras(data.extras ?? emptyExtras, local, data.forms), false)
      } else {
        extrasStored.current = Boolean(data.extras)
        extrasRef.current = data.extras ?? emptyExtras
        setExtras(extrasRef.current)
        setSaveStatus('已保存')
      }
    } else if (data.revision > revision.current && !pending.current && !saving.current && !blocked.current && !topologyChanging.current && !nodesRef.current.some((node) => node.dragging)) {
      revision.current = data.revision
      viewport.current = { x: data.x, y: data.y, zoom: data.zoom }
      extrasStored.current = Boolean(data.extras)
      extrasRef.current = data.extras ?? emptyExtras
      setExtras(extrasRef.current)
      history.clear()
      replacePositions = true
      if (initialized.current) void flow.setViewport(viewport.current)
    }
    if (replacePositions) formPositions.current.clear()
    for (const form of data.forms) {
      if (!formPositions.current.has(form.id)) formPositions.current.set(form.id, { x: form.x, y: form.y })
    }
    const old = new Map(nodesRef.current.map((node) => [node.id, node]))
    const graph = extrasRef.current
    const next: CanvasNode[] = data.forms.filter((form) => !graph.hidden.includes(form.id)).map((form): AssetNode => ({ ...old.get(form.id), id: form.id, type: 'asset', position: (replacePositions ? undefined : old.get(form.id)?.position) ?? formPositions.current.get(form.id)!, selected: selectWhenAdded.current.has(form.id) || old.get(form.id)?.selected, data: { form } }))
    for (const card of graph.cards) {
      next.push({ ...old.get(card.id), id: card.id, type: 'media', position: (replacePositions ? undefined : old.get(card.id)?.position) ?? card.position, selected: selectWhenAdded.current.has(card.id) || old.get(card.id)?.selected, data: card })
    }
    const visible = new Set(next.map((node) => node.id))
    for (const id of visible) selectWhenAdded.current.delete(id)
    if (selectedId && !visible.has(selectedId) && !selectWhenAdded.current.has(selectedId)) rememberSelection('')
    const editor = inlineRef.current
    if (editor && !visible.has(editor.id) && !inlineSavingRef.current) cancelEditor()
    const validLinks = new Set(graph.links.filter((link) => visible.has(link.source) && visible.has(link.target)).map((link) => link.id))
    setSelectedLinks((current) => {
      const kept = [...current].filter((id) => validLinks.has(id))
      return kept.length === current.size ? current : new Set(kept)
    })
    nodesRef.current = next
    setNodes(next)
    // 轮询只更新形象内容和生成状态，不覆盖尚未保存的拖动位置。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [query.data, extras.hidden, extras.cards])
  useEffect(() => {
    if (!measured || !revealAfterAdd.current) return
    const node = nodes.find((item) => item.id === revealAfterAdd.current)
    if (!node) return
    revealAfterAdd.current = ''
    const { position, width, height } = nodeGeometry(node)
    const current = flow.getViewport()
    const next = revealCanvasNode({ ...position, width, height }, current, viewSafeArea())
    if (next !== current) void flow.setViewport(next, { duration: 180 })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [flow, measured, nodes])
  useEffect(() => {
    const active = mediaGeneratorId
    if (!active || revealEditor.current !== active) return
    const node = flow.getNode(active)
    const element = shell.current?.querySelector<HTMLElement>(`.react-flow__node[data-id="${CSS.escape(active)}"]`)
    if (!node || !element?.offsetWidth || !element.offsetHeight) return
    revealEditor.current = ''
    const { position } = node
    const { offsetWidth: width, offsetHeight: height } = element
    const current = flow.getViewport()
    const area = viewSafeArea()
    const zoom = Math.max(current.zoom, Math.min(0.9, area.width / width))
    const readable = zoom === current.zoom ? current : {
      x: current.x + position.x * (current.zoom - zoom), y: current.y + position.y * (current.zoom - zoom), zoom,
    }
    const next = revealCanvasNode({ ...position, width, height }, readable, area)
    if (next !== current) void flow.setViewport(next, { duration: 180 })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [flow, mediaGeneratorId, measured, nodes])
  useEffect(() => {
    if (initialized.current || !query.data || (!measured && (query.data.forms.some((form) => !extras.hidden.includes(form.id)) || extras.cards.length))) return
    initialized.current = true
    const focus = params.get('focus')
    const target = nodes.find((node) => node.type === 'asset' && (node.id === focus || node.data.form.assetId === focus))
    const restored = nodes.find((node) => node.id === params.get('selected'))
    if (target) { choose(target.id); void flow.fitView({ nodes: [{ id: target.id }], maxZoom: 0.85, padding: 0.5 }) }
    else { if (restored) choose(restored.id); void flow.setViewport(viewport.current) }
  }, [measured, flow, params, query.data, nodes, extras.hidden, extras.cards.length])
  useEffect(() => { setPrompt(selected?.description || ''); setStyle({ id: selected?.styleId || '', name: selected?.style || '' }); setRatio(selected?.aspectRatio || '9:16') }, [selectedId, selected?.revision])
  useEffect(() => {
    function beforeUnload(event: BeforeUnloadEvent) { if (pending.current || saving.current || uploadInProgress.current || inlineRef.current) { event.preventDefault(); event.returnValue = '' } }
    window.addEventListener('beforeunload', beforeUnload)
    return () => { window.removeEventListener('beforeunload', beforeUnload); clearTimeout(timer.current); void drain() }
  }, [drain])
  const edges = useMemo(() => {
    const visible = new Map(nodes.map((node) => [node.id, node]))
    const lineage = (query.data?.forms || []).flatMap((form): Edge[] =>
      form.parentId ? [{ id: `lineage-${form.id}`, source: form.parentId, target: form.id, className: 'drama-canvas-lineage', deletable: false, reconnectable: false, selectable: false }] : [],
    )
    const links = extras.links.map((link): Edge => {
      const source = visible.get(link.source)
      const reference = source?.type === 'media' && source.data.kind === 'text' && visible.get(link.target)?.type === 'asset'
      return { ...link, selected: selectedLinks.has(link.id), className: reference ? 'drama-canvas-reference' : undefined,
        label: reference ? '描述参考' : selectedLinks.has(link.id) ? '素材关联' : undefined }
    })
    return [...lineage, ...links].filter((edge) => visible.has(edge.source) && visible.has(edge.target))
  }, [nodes, query.data, extras.links, selectedLinks])

  const selectedNodes = nodes.filter((node) => node.selected)
  const connectedText = selected ? extras.links.flatMap((link) => {
    if (link.target !== selected.id) return []
    const card = extras.cards.find((item) => item.id === link.source && item.kind === 'text' && item.content.trim())
    return card ? [card] : []
  }) : []
  const generationPrompt = [prompt.trim(), ...connectedText.map((card) => `参考文案「${card.title}」\n${card.content}`)].filter(Boolean).join('\n\n')
  const generationSource = selected?.parentId ? selected.kind === 'scene' ? '使用原始图片作为参考图' : '使用原始形象作为参考图' : ''

  function removeNodes(removed: CanvasNode[], linkIds = new Set<string>()) {
    if (!removed.length && !linkIds.size) return
    const edit = inlineRef.current
    if (removed.some((node) => node.id === edit?.id)) {
      messageApi.info('请先保存或取消正在编辑的节点')
      return
    }
    const remove = () => {
      const current = extrasRef.current
      const cards = new Set(removed.filter((node) => node.type === 'media').map((node) => node.id))
      const assets = removed.filter((node) => node.type === 'asset').map((node) => node.id)
      saveExtras({
        hidden: [...new Set([...current.hidden, ...assets])],
        cards: current.cards.filter((card) => !cards.has(card.id)),
        links: current.links.filter((link) => !linkIds.has(link.id) && !cards.has(link.source) && !cards.has(link.target)),
      })
      choose('')
    }
    const count = removed.filter((node) => node.type === 'media').length
    if (!count) { remove(); return }
    modal.confirm({ title: `删除选中的 ${count} 个素材节点？`, content: '相关连线也会删除。', okText: '删除', cancelText: '取消', okButtonProps: { danger: true }, onOk: remove })
  }

  function connect(connection: Connection, edgeId?: string) {
    const current = extrasRef.current
    if (connection.source === connection.target || current.links.some((link) => link.id !== edgeId && sameConnection(link, connection))) return
    const link = { id: edgeId ?? crypto.randomUUID(), ...connection }
    saveExtras({ ...current, links: edgeId ? current.links.map((item) => item.id === edgeId ? link : item) : [...current.links, link] })
  }

  const finishConnection: OnConnectEnd = (event, state) => {
    const target = event.target
    if (state.isValid || !state.fromHandle || !(target instanceof Element) || !target.closest('.react-flow__pane') || target.closest('.react-flow__node, .react-flow__handle')) return
    const pointer = 'changedTouches' in event ? event.changedTouches[0] : event
    if (!pointer) return
    setContextMenu({ point: { x: pointer.clientX, y: pointer.clientY }, connection: {
      nodeId: state.fromHandle.nodeId, type: state.fromHandle.type, handleId: state.fromHandle.id,
    } })
  }

  function changeNodes(changes: NodeChange<CanvasNode>[]) {
    const next = applyNodeChanges(changes.filter((change) => change.type !== 'remove'), nodesRef.current)
    nodesRef.current = next
    setNodes(next)
    if (changes.some((change) => change.type === 'select')) {
      const selected = next.filter((node) => node.selected)
      rememberSelection(selected.length === 1 ? selected[0].id : '')
    }
    const moved = new Set(changes.flatMap((change) => change.type === 'position' && change.position && !change.dragging ? [change.id] : []))
    if (!moved.size) return
    commitPositions(next, moved, snapshot())
  }

  function commitPositions(next: CanvasNode[], candidates: Set<string>, before: CanvasSnapshot) {
    const current = extrasRef.current
    const previousCards = new Map(before.extras.cards.map((card) => [card.id, card.position]))
    const movedCards = new Map<string, XYPosition>()
    let movedForm = false
    for (const node of next) {
      if (!candidates.has(node.id)) continue
      const prior = (node.type === 'asset' ? before.formPositions : previousCards).get(node.id)
      if (prior?.x === node.position.x && prior.y === node.position.y) continue
      if (node.type === 'asset') {
        formPositions.current.set(node.id, node.position)
        movedForm = true
      } else movedCards.set(node.id, node.position)
    }
    if (!movedForm && !movedCards.size) return
    history.push(before)
    if (movedCards.size) saveExtras({ ...current, cards: current.cards.map((card) => {
      const position = movedCards.get(card.id)
      return position ? { ...card, position } : card
    }) }, false)
    else schedule(next)
  }

  async function importFiles(files: File[], point?: XYPosition) {
    if (!files.length || !revision.current || uploadInProgress.current) return
    const anchor = flow.screenToFlowPosition(point ?? canvasCenter())
    const occupied = nodesRef.current.map(nodeGeometry)
    const visible = visibleCanvasArea()
    uploadInProgress.current = true
    setUploading(true)
    const cards: MediaCard[] = []
    try {
      for (const file of files) {
        try {
          const media = await uploadDramaCanvasMedia(project.id, file)
          const size = canvasNodeSize(media.kind)
          const position = point && !cards.length ? anchor : findCanvasNodePosition(anchor, size, occupied, visible)
          const card = { id: crypto.randomUUID(), kind: media.kind, content: media.url,
            title: Array.from(file.name).slice(0, 80).join('') || cardNames[media.kind], position }
          cards.push(card)
          occupied.push({ id: card.id, group: card.kind, position, ...size })
        } catch (error) {
          messageApi.error(error instanceof Error ? `${file.name}：${error.message}` : '素材上传失败')
        }
      }
      if (cards.length) {
        const graph = extrasRef.current
        saveExtras({ ...graph, cards: [...graph.cards, ...cards] })
        revealAfterAdd.current = cards[0].id
        chooseNodes(cards.map((card) => card.id))
        messageApi.success(`已添加 ${cards.length} 个素材`)
      }
    } finally {
      uploadInProgress.current = false
      setUploading(false)
    }
  }

  const pasteFiles = useRef<(files: File[]) => void>(() => {})
  pasteFiles.current = (files) => { if (!inlineRef.current && !preview && !mediaPreview) void importFiles(files) }
  useEffect(() => {
    const paste = (event: ClipboardEvent) => {
      if (event.target instanceof Element && event.target.closest('input,textarea,[contenteditable],.ant-modal')) return
      const files = Array.from(event.clipboardData?.files ?? [])
      if (!files.length) return
      event.preventDefault()
      pasteFiles.current(files)
    }
    window.addEventListener('paste', paste)
    return () => window.removeEventListener('paste', paste)
  }, [])

  function dropFiles(event: DragEvent<HTMLDivElement>) {
    if (!event.dataTransfer.types.includes('Files')) return
    event.preventDefault()
    dragDepth.current = 0
    setDragOver(false)
    void importFiles(Array.from(event.dataTransfer.files), { x: event.clientX, y: event.clientY })
  }

  async function leave() {
    if (uploadInProgress.current) { messageApi.info('素材正在上传，请稍后返回'); return }
    if (topologyChanging.current) { messageApi.info('画布正在添加形象，请稍后返回'); return }
    if (inlineSavingRef.current) { messageApi.info('节点正在保存，请稍后返回'); return }
    if (inlineRef.current && !window.confirm('当前节点尚未保存，确定返回吗？')) return
    try { await flush(); setParams((current) => { const next = new URLSearchParams(current); next.delete('canvas'); next.delete('focus'); next.delete('selected'); return next }) }
    catch (error) { messageApi.error(error instanceof Error ? error.message : '保存失败') }
  }
  async function generate() {
    const current = selectedRef.current
    if (!current || working || busy(current) || !generationPrompt) return
    if (Array.from(generationPrompt).length > 10000) { messageApi.warning('生成描述与连接的文案合计最多 10000 字'); return }
    setWorking(true)
    try {
      await flush()
      if ((current.styleId || current.style) !== (style.id || style.name) || current.aspectRatio !== ratio) await editDramaForm(project.id, { ...current, name: current.parentId ? current.name : current.assetName, styleId: style.id, style: style.name, aspectRatio: ratio })
      await generateDramaImage(project.id, current.id, generationPrompt); await query.refetch(); messageApi.success('图片生成任务已开始，可以继续操作画布')
    } catch (error) { await query.refetch(); messageApi.error(error instanceof Error ? error.message : '图片生成失败') } finally { setWorking(false) }
  }
  function fitNodes(ids?: string[]) {
    void flow.fitView({ duration: 250, padding: 0.2, ...(ids ? { nodes: ids.map((id) => ({ id })), maxZoom: 1 } : {}) })
  }

  function focusNode(id: string) {
    setSearchOpen(false)
    setTool('select')
    choose(id)
    fitNodes([id])
  }

  function arrange() {
    const before = snapshot()
    const positions = arrangeCanvasNodes(nodesRef.current.map(nodeGeometry))
    const next = nodesRef.current.map((node) => ({ ...node, position: positions.get(node.id) ?? node.position }))
    nodesRef.current = next
    setNodes(next)
    commitPositions(next, new Set(positions.keys()), before)
    requestAnimationFrame(() => fitNodes())
  }
  async function createAssetCard(kind: 'character' | 'prop', position: XYPosition, connection?: ConnectionStart) {
    if (working || topologyChanging.current) return
    setWorking(true)
    let created = false
    let locked = false
    try {
      await flush()
      if (topologyChanging.current) return
      topologyChanging.current = locked = true
      const names = new Set(query.data?.forms.filter((form) => form.kind === kind).map((form) => form.assetName))
      const base = `新${kindNames[kind]}`
      let name = base
      for (let suffix = 2; names.has(name); suffix++) name = `${base} ${suffix}`
      const result = await createDramaCanvasAsset(project.id, { kind, name, description: '', ...position, revision: revision.current })
      created = true
      revision.current = result.revision
      if (connection) {
        const graph = extrasRef.current
        saveExtras({ ...graph, links: withConnection(graph.links, result.id, connection) })
      }
      revealAfterAdd.current = result.id
      setTool('select')
      choose(result.id)
      await query.refetch({ throwOnError: true })
    } catch (error) {
      if (created) messageApi.warning('节点已创建，刷新画布后可以查看')
      else if (error instanceof ApiError && error.status === 409) {
        void query.refetch()
        messageApi.info('画布有更新，请确认后重新添加节点')
      } else messageApi.error(error instanceof Error ? error.message : '添加节点失败，请重试')
    } finally {
      if (locked) { topologyChanging.current = false; void drain() }
      setWorking(false)
    }
  }

  function addItem(kind: CanvasCreateKind, menu: CanvasMenu) {
    const point = menu.toolbar ? undefined : menu.point
    setContextMenu(null)
    if (kind === 'upload') {
      openFilePicker(point)
      return
    }
    if (inlineRef.current) { messageApi.info('请先保存或取消正在编辑的节点'); return }
    const anchor = flow.screenToFlowPosition(point ?? canvasCenter())
    const position = findCanvasNodePosition(anchor, canvasNodeSize(kind), nodesRef.current.map(nodeGeometry), point ? undefined : visibleCanvasArea())
    if (kind === 'character' || kind === 'prop') {
      void createAssetCard(kind, position, menu.connection)
      return
    }
    const graph = extrasRef.current
    const card = { id: crypto.randomUUID(), kind, title: cardNames[kind], content: '', position }
    saveExtras({ ...graph, cards: [...graph.cards, card], links: withConnection(graph.links, card.id, menu.connection) })
    revealAfterAdd.current = card.id
    setTool('select')
    choose(card.id)
  }

  function updateAssetOptions(update: Partial<DramaAssetForm>) {
    const current = inlineRef.current
    if (current?.mode === 'asset') setEditor({ ...current, form: { ...current.form, ...update } })
  }

  const editorActions = useRef({ save: saveInline, cancel: cancelEditor, options: updateAssetOptions })
  editorActions.current = { save: saveInline, cancel: cancelEditor, options: updateAssetOptions }
  const editId = inlineEdit?.id
  const editKind = inlineEdit?.kind
  const editTitle = inlineEdit?.title
  const editContent = inlineEdit?.content
  const formOptions = inlineEdit?.mode === 'asset' ? inlineEdit.form : undefined
  const editor = useMemo((): NodeEditorProps | null => {
    if (!editKind) return null
    const props: NodeEditorProps = { kind: editKind, title: editTitle ?? '', content: editContent ?? '',
      saving: inlineSaving, error: inlineError, onSave: (input) => void editorActions.current.save(input), onCancel: () => editorActions.current.cancel() }
    if (!formOptions) return props
    const label = kindNames[formOptions.kind]
    props.heading = formOptions.kind === 'scene' ? '编辑图片' : '编辑形象'
    props.titleLabel = formOptions.parentId && formOptions.kind !== 'scene' ? '形象名称' : `${label}名称`
    if (formOptions.kind === 'scene') props.descriptionLabel = '图片描述'
    if (formOptions.kind === 'character') props.children = <label className="drama-canvas-inline-field">性别
      <Select size="small" aria-label="性别" disabled={inlineSaving} value={formOptions.gender}
        onChange={(gender) => editorActions.current.options({ gender })}
        options={[{ value: 'unspecified', label: '未指定' }, { value: 'male', label: '男' }, { value: 'female', label: '女' }, { value: 'other', label: '其他' }]} />
    </label>
    return props
  }, [editKind, editTitle, editContent, formOptions, inlineError, inlineSaving])
  const editingNode = editId ? nodes.find((node) => node.id === editId) : undefined

  const activeMediaReferences = useMemo(() => {
    if (!mediaGeneratorId) return []
    const cards = new Map(extras.cards.map((card) => [card.id, card]))
    const forms = new Map((query.data?.forms ?? []).filter((form) => !extras.hidden.includes(form.id)).map((form) => [form.id, form]))
    const seen = new Set<string>()
    return extras.links.flatMap((link): CanvasGenerationReference[] => {
      if (link.target !== mediaGeneratorId || seen.has(link.source)) return []
      seen.add(link.source)
      const card = cards.get(link.source)
      if (card?.kind === 'text' && card.content.trim()) return [{ id: card.id, title: card.title, kind: 'text', content: card.content }]
      if (card?.kind === 'image') return [{ id: card.id, title: card.title, kind: 'image', url: card.content || generationJobs.get(card.id)?.resultUrl }]
      const form = forms.get(link.source)
      return form?.imageUrl ? [{ id: form.id, title: assetFormLabel(form), kind: 'image', url: form.imageUrl }] : []
    })
  }, [extras, generationJobs, mediaGeneratorId, query.data?.forms])
  const mediaActions = useRef({ open: openMediaGenerator, edit: startEditing, generate: generateMedia, adopt: adoptMediaResult })
  mediaActions.current = { open: openMediaGenerator, edit: startEditing, generate: generateMedia, adopt: adoptMediaResult }
  const mediaContext = useMemo(() => ({
    jobs: generationJobs,
    active: mediaGeneratorId ? { id: mediaGeneratorId, references: activeMediaReferences, submitting: submittingMedia.has(mediaGeneratorId), error: mediaErrors[mediaGeneratorId] } : undefined,
    open: (id: string) => mediaActions.current.open(id), edit: (id: string) => mediaActions.current.edit(id),
    close: () => setMediaGeneratorId(''), generate: (id: string, input: DramaCanvasGenerationInput) => void mediaActions.current.generate(id, input),
    adopt: (id: string, url: string) => mediaActions.current.adopt(id, url),
  }), [activeMediaReferences, generationJobs, mediaErrors, mediaGeneratorId, submittingMedia])

  const cardActions = useRef({ edit: startEditing, imageSettings: openAssetGeneration, derive, preview: previewNode, duplicate: duplicateNode, remove: removeNode })
  cardActions.current = { edit: startEditing, imageSettings: openAssetGeneration, derive, preview: previewNode, duplicate: duplicateNode, remove: removeNode }
  const cardControls = useMemo(() => ({
    activeAsset: generationForId,
    edit: (id: string) => cardActions.current.edit(id), imageSettings: (id: string) => cardActions.current.imageSettings(id),
    derive: (id: string) => void cardActions.current.derive(id), preview: (id: string) => cardActions.current.preview(id),
    duplicate: (id: string) => cardActions.current.duplicate(id), remove: (id: string) => cardActions.current.remove(id),
  }), [generationForId])

  function previewNode(id: string) {
    const node = nodesRef.current.find((item) => item.id === id)
    if (node?.type === 'asset') setPreview(node.data.form)
    else if (node?.type === 'media') previewCard(node.data)
  }

  function duplicateNode(id: string) {
    const node = nodesRef.current.find((item) => item.id === id)
    if (node?.type === 'media') duplicateCards([node])
  }

  function removeNode(id: string) {
    const node = nodesRef.current.find((item) => item.id === id)
    if (node) removeNodes([node])
  }

  const modalOpen = Boolean(preview || mediaPreview || modelSettingsOpen)
  const historyDisabled = Boolean(inlineEdit) || working || uploading || !!saveError || nodes.some((node) => node.dragging)
  const keyboard = useRef<(event: KeyboardEvent) => void>(() => {})
  keyboard.current = (event) => {
    if (event.isComposing || event.keyCode === 229 || event.defaultPrevented || modalOpen) return
    const target = event.target instanceof Element ? event.target : null
    if (target?.closest('[role="dialog"],.drama-canvas-inline-editor')) return
    if (event.key === 'Escape') {
      if (searchOpen) setSearchOpen(false)
      else if (contextMenu) setContextMenu(null)
      else choose('')
      return
    }
    if (!query.data || target?.closest('input,textarea,select,[contenteditable],[role="combobox"]')) return
    const key = event.key.toLowerCase()
    const modifier = event.ctrlKey || event.metaKey
    if (modifier && !event.altKey) {
      if (key === 'z') { event.preventDefault(); restoreHistory(event.shiftKey ? 'redo' : 'undo') }
      else if (key === 'y' && !event.shiftKey) { event.preventDefault(); restoreHistory('redo') }
      else if (key === 'k') { event.preventDefault(); setSearchOpen((open) => !open) }
      else if (key === 'a' && !event.shiftKey && !window.getSelection()?.toString()) {
        event.preventDefault()
        setTool('select')
        chooseNodes(nodesRef.current.map((node) => node.id))
      } else if (key === 'd' && !event.shiftKey) {
        const picked = nodesRef.current.filter((node): node is MediaNode => node.type === 'media' && node.selected === true)
        if (picked.length && !historyDisabled) { event.preventDefault(); duplicateCards(picked) }
      }
      return
    }
    if (event.altKey || event.shiftKey || searchOpen || contextMenu) return
    if (key === 'v' || key === 'h') { event.preventDefault(); setTool(key === 'v' ? 'select' : 'pan') }
    if (event.code === 'Space' && !target?.closest('button:focus-visible,[role="button"]:focus-visible')) {
      event.preventDefault()
      setSpacePan(true)
    }
  }
  useEffect(() => {
    const keydown = (event: KeyboardEvent) => keyboard.current(event)
    const release = () => setSpacePan(false)
    const keyup = (event: KeyboardEvent) => { if (event.code === 'Space') release() }
    window.addEventListener('keydown', keydown)
    window.addEventListener('keyup', keyup)
    window.addEventListener('blur', release)
    return () => {
      window.removeEventListener('keydown', keydown)
      window.removeEventListener('keyup', keyup)
      window.removeEventListener('blur', release)
    }
  }, [])

  const selectedMedia = selectedNodes.filter((node): node is MediaNode => node.type === 'media')
  const selectionTools = <div className="drama-canvas-selection-tools" role="toolbar" aria-label="所选节点操作">
    {selectedMedia.length > 0 && <Button size="small" icon={<CopyOutlined />} onClick={() => duplicateCards(selectedMedia)}>{selectedMedia.length < selectedNodes.length ? '复制素材节点' : '复制节点'}</Button>}
    {selectedNodes.length > 1 && <span>{selectedNodes.length} 个节点</span>}
    <Button size="small" danger icon={<DeleteOutlined />} onClick={() => removeNodes(selectedNodes, selectedLinks)}>
      {selectedNodes.length ? '移除所选' : '断开连线'}
    </Button>
  </div>

  return <div className="drama-canvas-shell" ref={shell}
    onDoubleClick={(event) => {
      if (!isPanning && event.target instanceof Element && event.target.classList.contains('react-flow__pane')) {
        setContextMenu({ point: { x: event.clientX, y: event.clientY } })
      }
    }}
    onDragEnter={(event) => {
      if (!event.dataTransfer.types.includes('Files')) return
      event.preventDefault(); dragDepth.current++; setDragOver(true)
    }}
    onDragOver={(event) => {
      if (!event.dataTransfer.types.includes('Files')) return
      event.preventDefault(); event.dataTransfer.dropEffect = 'copy'
    }}
    onDragLeave={(event) => {
      if (!event.dataTransfer.types.includes('Files')) return
      dragDepth.current = Math.max(0, dragDepth.current - 1)
      if (!dragDepth.current) setDragOver(false)
    }} onDrop={dropFiles}>
    {holder}{modalHolder}
    <input ref={fileInput} type="file" multiple hidden accept="image/png,image/jpeg,image/gif,image/webp,video/mp4,video/webm,audio/mpeg,audio/wav,audio/ogg,.mp3,.wav,.ogg"
      onChange={(event) => { void importFiles(Array.from(event.target.files ?? []), filePickerPoint.current); event.target.value = '' }} />
    <header className="drama-canvas-header">
      <div><Button icon={<ArrowLeftOutlined />} type="text" onClick={() => void leave()} aria-label="返回资产列表" />
        <strong>{project.title}</strong><span className={saveError ? 'save-error' : ''} role="status">{saveStatus}</span></div>
      <div><div className="drama-canvas-wide-action"><DramaImageModelSelect /></div>
        <Button ref={addButton} icon={<PlusOutlined />} loading={uploading} disabled={!query.data || working} aria-expanded={!!contextMenu?.toolbar}
          onClick={(event) => {
            const anchor = event.currentTarget.getBoundingClientRect()
            setContextMenu({ toolbar: true, point: { x: anchor.right - 292, y: anchor.bottom + 8 } })
          }}>添加节点</Button>
        {extras.hidden.length > 0 && <Button className="drama-canvas-wide-action" onClick={() => saveExtras({ ...extrasRef.current, hidden: [] })}>恢复隐藏的形象</Button>}
        <Button className="drama-canvas-wide-action" onClick={arrange} disabled={!nodes.length || working || !!saveError}>整理画布</Button>
        <Dropdown trigger={['click']} menu={{ items: [
          { key: 'model', label: '图片模型设置' },
          { key: 'arrange', label: '整理画布', disabled: !nodes.length || working || !!saveError },
          { key: 'restore', label: '恢复隐藏的形象', disabled: !extras.hidden.length },
        ], onClick: ({ key }) => {
          if (key === 'model') setModelSettingsOpen(true)
          else if (key === 'arrange') arrange()
          else saveExtras({ ...extrasRef.current, hidden: [] })
        } }}><Button className="drama-canvas-project-more" icon={<EllipsisOutlined />} aria-label="更多画布操作" /></Dropdown>
      </div>
    </header>
    {saveError && <Alert className="drama-canvas-save-alert" type="error" showIcon title={saveError} action={<Button onClick={() => { if (conflict) window.location.reload(); else { blocked.current = false; void drain() } }}>{conflict ? '重新加载' : '重试保存'}</Button>} />}
    {query.error && query.data && !saveError && <Alert className="drama-canvas-save-alert" type="warning" showIcon title="暂时无法刷新资产状态"
      action={<Button size="small" onClick={() => void query.refetch()}>重试</Button>} />}
    {!query.data ? <div className="drama-canvas-loading">{query.error ? <Alert type="error" title="读取画布失败" description={query.error.message} action={<Button onClick={() => void query.refetch()}>重试</Button>} /> : <Skeleton active />}</div> : <>
      <CanvasMedia.Provider value={mediaContext}><CanvasCardControls.Provider value={cardControls}>
      <ReactFlow<CanvasNode> className={`drama-canvas-flow${isPanning ? ' is-panning' : ''}`} nodes={nodes} edges={edges} nodeTypes={nodeTypes} onNodesChange={changeNodes}
        onNodeClick={() => setContextMenu(null)} onPaneClick={() => {
          if (isPanning) return
          choose('')
          setContextMenu(null)
        }}
        onNodeDoubleClick={(event, node) => {
          if (event.target instanceof Element && event.target.closest('video,audio,button,input,textarea,.drama-canvas-media-generation')) return
          startEditing(node.id)
        }}
        onPaneContextMenu={(event) => {
          event.preventDefault()
          setContextMenu({ point: { x: event.clientX, y: event.clientY } })
        }}
        onNodeContextMenu={(event, node) => {
          if (event.target instanceof Element && event.target.closest('.drama-canvas-media-generation')) return
          event.preventDefault()
          choose(node.id)
          setContextMenu({ point: { x: event.clientX, y: event.clientY }, node })
        }}
        onEdgeContextMenu={(event, edge) => {
          event.preventDefault()
          setContextMenu({ point: { x: event.clientX, y: event.clientY }, edge })
        }} onMoveEnd={(_, next) => {
          const previous = viewport.current
          if (!initialized.current || (previous.x === next.x && previous.y === next.y && previous.zoom === next.zoom)) return
          viewport.current = next
          schedule()
        }} onBeforeDelete={async ({ nodes: removed, edges: removedEdges }) => {
          removeNodes(removed, new Set(removedEdges.filter((edge) => edge.selected && edge.deletable !== false).map((edge) => edge.id)))
          return false
        }}
        onEdgesChange={(changes) => {
          const duringBoxSelection = selectingRef.current
          setSelectedLinks((current) => {
            const next = new Set(current)
            for (const change of changes) {
              if (change.type === 'select' && change.selected && !duringBoxSelection) next.add(change.id)
              else if (change.type === 'select' || change.type === 'remove') next.delete(change.id)
            }
            return next
          })
        }}
        onConnect={(connection) => connect(connection)} onConnectEnd={finishConnection}
        onReconnect={(edge, connection) => connect(connection, edge.id)}
        isValidConnection={(connection) => connection.source !== connection.target}
        panOnDrag={isPanning ? true : [1]} panActivationKeyCode={null} selectionOnDrag={!isPanning}
        selectionMode={SelectionMode.Partial} selectionKeyCode={isPanning ? null : 'Shift'} multiSelectionKeyCode={['Meta', 'Control']}
        onSelectionStart={() => { selectingRef.current = true; setSelecting(true); setSelectedLinks(new Set()) }}
        onSelectionEnd={() => { selectingRef.current = false; setSelecting(false) }}
        nodesDraggable={!isPanning} nodesConnectable={!isPanning} elementsSelectable={!isPanning}
        panOnScroll zoomOnScroll={false} zoomActivationKeyCode={['Control', 'Meta']} zoomOnDoubleClick={false}
        deleteKeyCode={modalOpen || searchOpen || isPanning ? null : ['Backspace', 'Delete']}
        defaultEdgeOptions={{ type: 'default', interactionWidth: 22 }} minZoom={0.05} maxZoom={2} defaultViewport={viewport.current}
        proOptions={{ hideAttribution: false }} ariaLabelConfig={{ 'minimap.ariaLabel': '画布缩略图' }}>
        <Background gap={22} size={1} color="#d5dce8" />
        {showMinimap && <MiniMap<CanvasNode> pannable zoomable nodeColor={(node) => node.type === 'media' ? '#e2a467' : ({ character: '#b9afe9', scene: '#a2c9be', prop: '#aec6e7' }[node.data.form.kind])} />}
        {!isPanning && !selecting && !inlineEdit && !nodes.some((node) => node.dragging) && selectedNodes.length > 1 &&
          <DramaCanvasNodeToolbar nodes={selectedNodes}>{selectionTools}</DramaCanvasNodeToolbar>}
      </ReactFlow>
      </CanvasCardControls.Provider></CanvasMedia.Provider>
      {editingNode && editor && <DramaCanvasFloatingPanel key={editingNode.id} node={nodeGeometry(editingNode)} shell={shell}>
        <DramaCanvasInlineEditor {...editor} />
      </DramaCanvasFloatingPanel>}
      {!isPanning && !selectedNodes.length && selectedLinks.size > 0 && <div className="drama-canvas-edge-tools">{selectionTools}</div>}
      <DramaCanvasToolDock tool={isPanning ? 'pan' : tool} onTool={setTool}
        canUndo={history.canUndo && !historyDisabled} canRedo={history.canRedo && !historyDisabled}
        onUndo={() => restoreHistory('undo')} onRedo={() => restoreHistory('redo')}
        items={nodes.map((node) => node.type === 'asset'
          ? { id: node.id, name: assetFormLabel(node.data.form), category: kindNames[node.data.form.kind] }
          : { id: node.id, name: node.data.title, category: cardNames[node.data.kind] })}
        searchOpen={searchOpen} onSearchOpen={setSearchOpen} onFocusNode={focusNode} />
      <DramaCanvasViewTools hasNodes={nodes.length > 0} hasSelection={selectedNodes.length > 0} minimap={showMinimap}
        onMinimap={() => setShowMinimap((shown) => !shown)} onFit={() => fitNodes()} onFocusSelection={() => fitNodes(selectedNodes.map((node) => node.id))} />
      {!nodes.length && <div className="drama-canvas-empty"><Empty description="添加节点、拖入文件或右键空白处开始" /></div>}
      <span className="drama-canvas-gesture-hint">{isPanning ? '拖动平移' : '拖动空白框选 · 空格临时抓手'} · 滚轮平移 · Ctrl / ⌘ + 滚轮缩放</span>
      {selected && generationForId === selected.id && !inlineEdit && <div className="drama-canvas-generation" aria-label="生图设置">
        <div className="drama-canvas-generation-heading"><span>{kindIcons[selected.kind]} {assetFormLabel(selected)}</span><Button type="text" size="small" onClick={() => setGenerationForId('')}>收起</Button></div>
        {selected.imageError && <div className="drama-canvas-generation-error">{selected.imageError}</div>}
        <Input.TextArea aria-label="本次生成描述" value={prompt} onChange={(event) => setPrompt(event.target.value)} autoSize={{ minRows: 2, maxRows: 4 }} placeholder={selected.kind === 'scene' ? '描述想生成的图片' : '描述想生成的形象'} maxLength={10000} disabled={busy(selected)} />
        <div className="drama-canvas-generation-toolbar"><DramaStylePicker value={style.id || style.name} valueLabel={style.name} onChange={(value) => setStyle({ id: value.id, name: value.name })} disabled={busy(selected)} /><Select aria-label="生成比例" value={ratio} onChange={setRatio} disabled={busy(selected)} options={[{ value: '9:16', label: '竖屏 9:16' }, { value: '16:9', label: '横屏 16:9' }, { value: '1:1', label: '方图 1:1' }]} /><span>{[generationSource, connectedText.length ? `引用 ${connectedText.length} 条文案` : ''].filter(Boolean).join(' · ')}</span><Button type="primary" icon={<PictureOutlined />} loading={working || busy(selected)} disabled={!generationPrompt || !!saveError} onClick={() => void generate()}>{selected.imageUrl ? '重新生成图片' : '生成图片'}</Button></div>
      </div>}
    </>}
    {dragOver && <div className="drama-canvas-drop"><UploadOutlined /><strong>松开后将文件添加到画布</strong></div>}
    <Modal title="图片模型设置" open={modelSettingsOpen} footer={null} onCancel={() => setModelSettingsOpen(false)}><DramaImageModelSelect /></Modal>
    {contextMenu && <div className="drama-canvas-menu-layer" onClick={() => setContextMenu(null)} onContextMenu={(event) => { event.preventDefault(); setContextMenu(null) }}>
      {contextMenu.node || contextMenu.edge ? <div className="drama-canvas-menu" aria-label="画布操作"
        style={{ left: Math.max(4, Math.min(contextMenu.point.x, window.innerWidth - 182)), top: Math.max(4, Math.min(contextMenu.point.y, window.innerHeight - 132)) }}
        onClick={(event) => event.stopPropagation()} onKeyDown={(event) => { if (event.key === 'Escape') setContextMenu(null) }}>
        {contextMenu.node?.type === 'asset' && <Button type="text" onClick={() => {
          if (contextMenu.node?.type === 'asset') startEditing(contextMenu.node.id)
        }}>{contextMenu.node.data.form.kind === 'scene' ? '编辑图片' : '编辑形象'}</Button>}
        {contextMenu.node?.type === 'media' && <Button type="text" onClick={() => {
          if (contextMenu.node?.type === 'media') startEditing(contextMenu.node.id)
        }}>编辑节点</Button>}
        {contextMenu.node?.type === 'media' && <Button type="text" onClick={() => {
          const node = contextMenu.node
          if (node?.type === 'media') duplicateCards([node])
        }}>复制节点</Button>}
        {contextMenu.node && <Button type="text" danger onClick={() => {
          if (contextMenu.node) removeNodes([contextMenu.node])
          setContextMenu(null)
        }}>{contextMenu.node.type === 'asset' ? contextMenu.node.data.form.kind === 'scene' ? '隐藏图片' : '隐藏形象' : '删除节点'}</Button>}
        {contextMenu.edge && <Button type="text" danger disabled={contextMenu.edge.deletable === false} onClick={() => {
          if (contextMenu.edge) removeNodes([], new Set([contextMenu.edge.id]))
          setContextMenu(null)
        }}>断开连线</Button>}
      </div> : <div className="drama-canvas-create-popover" onClick={(event) => event.stopPropagation()}
        style={{ left: Math.max(8, Math.min(contextMenu.point.x, window.innerWidth - 300)), top: Math.max(8, Math.min(contextMenu.point.y, window.innerHeight - (contextMenu.connection ? 232 : 280))) }}>
        <DramaCanvasCreateMenu connected={!!contextMenu.connection} onChoose={(kind) => addItem(kind, contextMenu)} onClose={() => {
          setContextMenu(null)
          if (contextMenu.toolbar) addButton.current?.focus()
        }} />
      </div>}
    </div>}
    <Modal title={preview ? assetFormLabel(preview) : '图片预览'} open={!!preview} footer={null} width={850} onCancel={() => setPreview(null)}>{preview?.imageUrl ? <img className="drama-canvas-preview-image" src={preview.imageUrl} alt={assetFormLabel(preview)} /> : <Empty description="尚未生成图片" />}</Modal>
    <Modal title={mediaPreview?.title} open={!!mediaPreview} footer={null} width={850} onCancel={() => setMediaPreview(null)} destroyOnHidden>
      {mediaPreview?.kind === 'image' && <img className="drama-canvas-preview-image" src={mediaPreview.content} alt={mediaPreview.title} />}
      {mediaPreview?.kind === 'video' && <video className="drama-canvas-preview-image" src={mediaPreview.content} controls playsInline />}
      {mediaPreview?.kind === 'audio' && <audio className="drama-canvas-preview-audio" src={mediaPreview.content} controls />}
    </Modal>
  </div>
}
