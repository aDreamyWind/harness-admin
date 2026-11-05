import { ArrowLeftOutlined, CopyOutlined, DeleteOutlined, EditOutlined, EyeOutlined, GiftOutlined, PictureOutlined, PlusOutlined, UploadOutlined, UserOutlined } from '@ant-design/icons'
import { useQueryClient } from '@tanstack/react-query'
import { Alert, Button, Dropdown, Empty, Input, Modal, Select, Skeleton, Tag, Tooltip, message } from 'antd'
import { useCallback, useEffect, useMemo, useRef, useState, type DragEvent } from 'react'
import { useSearchParams } from 'react-router-dom'
import { Background, Controls, Handle, MiniMap, Position, ReactFlow, ReactFlowProvider, SelectionMode, applyNodeChanges, useNodesInitialized, useReactFlow, type Connection, type OnConnectEnd, type Node, type NodeProps, type Viewport, type NodeChange, type Edge, type XYPosition } from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { ApiError } from '../api/client'
import { createDramaCanvasAsset, deriveDramaForm, editDramaForm, generateDramaImage, getDramaCanvas, saveDramaCanvasLayout, uploadDramaCanvasMedia, type DramaAssetForm, type DramaAssetKind, type DramaCanvas, type DramaCanvasCard as MediaCard, type DramaCanvasCardKind as CardKind, type DramaCanvasExtras as CanvasExtras, type DramaCanvasLayout, type DramaCanvasLink, type DramaProject } from '../api/drama'
import { DramaCanvasCardEditor } from './DramaCanvasCardEditor'
import { DramaImageModelSelect, useDramaCanvas } from './DramaImageTools'
import { DramaStylePicker } from './DramaStylePicker'
import './drama-canvas.css'

type AssetNode = Node<{ form: DramaAssetForm; edit: (id: string) => void; derive: (id: string) => void; preview: (id: string) => void }, 'asset'>
const cardNames = { text: '文本', image: '图片', video: '视频', audio: '音频' } as const
type MediaNode = Node<MediaCard, 'media'>
type CanvasNode = AssetNode | MediaNode
const emptyExtras: CanvasExtras = { hidden: [], cards: [], links: [] }
type ConnectionStart = { nodeId: string; type: 'source' | 'target'; handleId?: string | null }
type CanvasMenu = { point: XYPosition; node?: CanvasNode; edge?: Edge; connection?: ConnectionStart }

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

function MediaCanvasNode({ data, selected }: NodeProps<MediaNode>) {
  const [failedURL, setFailedURL] = useState('')
  const failed = failedURL === data.content
  return <div className={`drama-canvas-node scene media ${data.kind}${selected ? ' selected' : ''}`}>
    <div className="drama-canvas-node-caption"><span>{cardNames[data.kind]} · {data.title}</span></div>
    <div className="drama-canvas-node-body">
      <Handle type="target" position={Position.Left} />
      {failed && <div className="drama-canvas-media-error">素材无法读取，可以编辑节点换一份素材。</div>}
      {!failed && data.kind === 'video' && <video className="nodrag nowheel" controls playsInline preload="metadata" src={data.content} aria-label={data.title} onError={() => setFailedURL(data.content)} />}
      {!failed && data.kind === 'audio' && <audio className="nodrag nowheel" controls preload="metadata" src={data.content} aria-label={data.title} onError={() => setFailedURL(data.content)} />}
      {!failed && data.kind === 'image' && <div className="drama-canvas-node-image"><img src={data.content} alt={data.title} loading="lazy" onError={() => setFailedURL(data.content)} /></div>}
      {data.kind === 'text' && <div className="drama-canvas-text nodrag nowheel">{data.content}</div>}
      <Handle type="source" position={Position.Right} />
    </div>
  </div>
}

const kindNames = { character: '角色', scene: '场景', prop: '道具' }
const kindIcons = { character: <UserOutlined />, scene: <PictureOutlined />, prop: <GiftOutlined /> }
const busy = (form?: DramaAssetForm) => form?.imageStatus === 'queued' || form?.imageStatus === 'running'
function AssetCanvasNode({ data, selected }: NodeProps<AssetNode>) {
  const f = data.form
  return <div className={`drama-canvas-node ${f.kind}${selected ? ' selected' : ''}`}>
    <div className="drama-canvas-node-caption">{kindIcons[f.kind]}<span>{f.assetName} · {f.name}</span></div>
    <div className="drama-canvas-node-body">
      <Handle type="target" position={Position.Left} />
      <div className="drama-canvas-node-image" onDoubleClick={() => data.preview(f.id)}>
        {f.imageUrl ? <img src={f.imageUrl} alt={`${f.assetName} · ${f.name}`} loading="lazy" /> : <div className="drama-canvas-node-placeholder">{kindIcons[f.kind]}<span>{busy(f) ? '图片生成中…' : '尚未生成图片'}</span></div>}
        {busy(f) && f.imageUrl && <Tag color="processing">图片生成中</Tag>}
        <button className="drama-canvas-preview nodrag" aria-label={`预览${f.assetName}${f.name}`} onClick={() => data.preview(f.id)}><EyeOutlined /></button>
      </div>
      <div className="drama-canvas-node-copy"><strong>{f.name}</strong><Button type="text" size="small" className="nodrag" aria-label={`编辑${f.assetName}${f.name}`} icon={<EditOutlined />} onClick={() => data.edit(f.id)} /><p title={f.description}>{f.description || '请编辑形象描述'}</p><small>{f.parentId ? '派生形象' : '原始形象'} · {f.aspectRatio}</small>{f.imageError && <Tooltip title={f.imageError}><span className="drama-canvas-node-error">生成失败，选中后可重试</span></Tooltip>}</div>
      <Handle type="source" position={Position.Right} />
      <Tooltip title="添加基于此形象的派生形象"><button className="drama-canvas-derive nodrag" aria-label={`为${f.assetName}${f.name}添加派生形象`} onClick={() => data.derive(f.id)}><PlusOutlined /></button></Tooltip>
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
  const extrasKey = `drama-canvas-extras:${project.id}`
  const [extras, setExtras] = useState(emptyExtras)
  const extrasRef = useRef(emptyExtras)
  const extrasStored = useRef(false)
  const extrasVerified = useRef(false)
  const importingLegacy = useRef(false)
  const [cardDraft, setCardDraft] = useState<{ card: MediaCard; connection?: ConnectionStart } | null>(null)
  const [contextMenu, setContextMenu] = useState<CanvasMenu | null>(null)
  const [selectedId, setSelectedId] = useState('')
  const selectionURL = useRef('')
  const selectWhenAdded = useRef('')
  const [selectedLinks, setSelectedLinks] = useState<Set<string>>(new Set())
  const [editing, setEditing] = useState<DramaAssetForm | null>(null)
  const [assetDraft, setAssetDraft] = useState<{ kind: DramaAssetKind; name: string; description: string; position: XYPosition; connection?: ConnectionStart } | null>(null)
  const [preview, setPreview] = useState<DramaAssetForm | null>(null)
  const [mediaPreview, setMediaPreview] = useState<MediaCard | null>(null)
  const [working, setWorking] = useState(false)
  const [uploading, setUploading] = useState(false)
  const uploadInProgress = useRef(false)
  const fileInput = useRef<HTMLInputElement>(null)
  const filePickerPoint = useRef<XYPosition | undefined>(undefined)
  const [dragOver, setDragOver] = useState(false)
  const dragDepth = useRef(0)
  const shell = useRef<HTMLDivElement>(null)
  const [prompt, setPrompt] = useState('')
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
  useEffect(() => {
    if (!contextMenu) return
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setContextMenu(null)
    }
    window.addEventListener('keydown', closeOnEscape)
    return () => window.removeEventListener('keydown', closeOnEscape)
  }, [contextMenu])

  function saveExtras(next: CanvasExtras) {
    extrasStored.current = true
    extrasRef.current = next
    setExtras(next)
    schedule()
  }

  function canvasCenter() {
    const bounds = shell.current?.getBoundingClientRect()
    return bounds ? { x: bounds.left + bounds.width / 2 - 90, y: bounds.top + bounds.height / 2 - 55 } : { x: 200, y: 200 }
  }

  function nodePosition(point?: XYPosition) {
    const origin = flow.screenToFlowPosition(point ?? canvasCenter())
    if (point) return origin
    const occupied = nodesRef.current
    for (let step = 0; step <= occupied.length; step++) {
      const position = { x: origin.x + (step % 3) * 440, y: origin.y + Math.floor(step / 3) * 620 }
      if (occupied.every((node) => position.x + 400 <= node.position.x || position.x >= node.position.x + (node.measured?.width ?? 400) ||
        position.y + 560 <= node.position.y || position.y >= node.position.y + (node.measured?.height ?? 560))) return position
    }
    return { ...origin, y: Math.max(origin.y, ...occupied.map((node) => node.position.y + (node.measured?.height ?? 560))) + 24 }
  }

  function openFilePicker(point?: XYPosition) {
    filePickerPoint.current = point
    fileInput.current?.click()
  }

  function openCard(kind: CardKind, point?: XYPosition, connection?: ConnectionStart) {
    setContextMenu(null)
    setCardDraft({ card: { id: crypto.randomUUID(), kind, title: '', content: '', position: nodePosition(point) }, connection })
  }

  function openAsset(kind: DramaAssetKind, point?: XYPosition, connection?: ConnectionStart) {
    setContextMenu(null)
    setAssetDraft({ kind, name: '', description: '', position: nodePosition(point), connection })
  }

  function withConnection(links: DramaCanvasLink[], target: string, origin?: ConnectionStart) {
    if (!origin || !nodesRef.current.some((node) => node.id === origin.nodeId)) return links
    return [...links, origin.type === 'source'
      ? { id: crypto.randomUUID(), source: origin.nodeId, target, sourceHandle: origin.handleId }
      : { id: crypto.randomUUID(), source: target, target: origin.nodeId, targetHandle: origin.handleId }]
  }

  function duplicateCard(card: MediaCard, position: XYPosition) {
    const copy = { ...card, id: crypto.randomUUID(), position: { x: position.x + 36, y: position.y + 36 } }
    const current = extrasRef.current
    saveExtras({ ...current, cards: [...current.cards, copy] })
    choose(copy.id)
    setContextMenu(null)
  }

  async function saveCard(draft: MediaCard, file?: File) {
    if (!cardDraft) return
    let card = draft
    if (file) {
      if (uploadInProgress.current) return
      uploadInProgress.current = true
      setUploading(true)
      try {
        const media = await uploadDramaCanvasMedia(project.id, file)
        if (media.kind !== draft.kind) throw new Error(`请选择${cardNames[draft.kind]}文件`)
        card = { ...draft, content: media.url }
      } catch (error) {
        messageApi.error(error instanceof Error ? error.message : '素材上传失败')
        return
      } finally {
        uploadInProgress.current = false
        setUploading(false)
      }
    }
    const current = extrasRef.current
    const exists = current.cards.some((item) => item.id === card.id)
    const cards = exists ? current.cards.map((item) => item.id === card.id ? card : item) : [...current.cards, card]
    saveExtras({ ...current, cards, links: withConnection(current.links, card.id, cardDraft.connection) })
    setCardDraft(null)
    choose(card.id)
  }

  function rememberSelection(id: string) {
    if (selectionURL.current === id) return
    selectionURL.current = id
    setSelectedId(id)
    setParams((current) => {
      const next = new URLSearchParams(current)
      next.delete('focus')
      if (id) next.set('selected', id)
      else next.delete('selected')
      return next
    }, { replace: true })
  }

  function choose(id: string) {
    const next = nodesRef.current.map((node) => node.selected === (node.id === id) ? node : { ...node, selected: node.id === id })
    selectWhenAdded.current = id && !next.some((node) => node.id === id) ? id : ''
    nodesRef.current = next
    setNodes(next)
    setSelectedLinks(new Set())
    rememberSelection(id)
  }
  const selected = query.data?.forms.find((f) => f.id === selectedId)
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
      ...(positions.get(form.id) || { x: form.x, y: form.y }),
    })), ...(extrasStored.current ? { extras: extrasRef.current } : {}) }
    setSaveStatus('待保存'); clearTimeout(timer.current); timer.current = setTimeout(() => void drain(), 650)
  }
  async function derive(id: string) {
    if (working || topologyChanging.current) return
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
      choose(result.id)
      setTimeout(() => void flow.fitView({ nodes: [{ id: result.id }], maxZoom: 0.9, duration: 300, padding: 0.5 }), 100)
      messageApi.success('已添加派生形象，请修改描述后生成图片')
    } catch (error) {
      messageApi.error(error instanceof Error ? error.message : '添加派生形象失败')
    } finally {
      topologyChanging.current = false
      setWorking(false)
      void drain()
    }
  }
  async function saveAsset() {
    if (!assetDraft || working) return
    setWorking(true)
    try {
      await flush()
      topologyChanging.current = true
      const { kind, name, description, position, connection } = assetDraft
      const result = await createDramaCanvasAsset(project.id, { kind, name: name.trim(), description: description.trim(), ...position, revision: revision.current })
      revision.current = result.revision
      setAssetDraft(null)
      choose(result.id)
      if (connection) {
        const graph = extrasRef.current
        saveExtras({ ...graph, links: withConnection(graph.links, result.id, connection) })
      }
      await query.refetch()
      messageApi.success(`已添加${kindNames[kind]}`)
    } catch (error) {
      messageApi.error(error instanceof Error ? error.message : '添加资产失败')
    } finally {
      topologyChanging.current = false
      setWorking(false)
      void drain()
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
        saveExtras(mergeLegacyExtras(data.extras ?? emptyExtras, local, data.forms))
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
      replacePositions = true
      if (initialized.current) void flow.setViewport(viewport.current)
    }
    const old = new Map(nodesRef.current.map((node) => [node.id, node]))
    const graph = extrasRef.current
    const next: CanvasNode[] = data.forms.filter((form) => !graph.hidden.includes(form.id)).map((form): AssetNode => ({ ...old.get(form.id), id: form.id, type: 'asset', position: (replacePositions ? undefined : old.get(form.id)?.position) ?? { x: form.x, y: form.y }, selected: form.id === selectWhenAdded.current || old.get(form.id)?.selected, data: { form, edit: (id) => setEditing(data.forms.find((f) => f.id === id) || null), preview: (id) => setPreview(data.forms.find((f) => f.id === id) || null), derive: (id) => void derive(id) } }))
    for (const card of graph.cards) {
      next.push({ ...old.get(card.id), id: card.id, type: 'media', position: (replacePositions ? undefined : old.get(card.id)?.position) ?? card.position, selected: card.id === selectWhenAdded.current || old.get(card.id)?.selected, data: card })
    }
    if (next.some((node) => node.id === selectWhenAdded.current)) selectWhenAdded.current = ''
    nodesRef.current = next
    setNodes(next)
    // 轮询只更新形象内容和生成状态，不覆盖尚未保存的拖动位置。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [query.data, extras.hidden, extras.cards])
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
    function beforeUnload(event: BeforeUnloadEvent) { if (pending.current || saving.current || uploadInProgress.current) { event.preventDefault(); event.returnValue = '' } }
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
  const selectedCard = selectedNodes.length === 1 && selectedNodes[0]?.type === 'media' ? selectedNodes[0] : undefined
  const connectedText = selected ? extras.links.flatMap((link) => {
    if (link.target !== selected.id) return []
    const card = extras.cards.find((item) => item.id === link.source && item.kind === 'text')
    return card ? [card] : []
  }) : []
  const generationPrompt = [prompt.trim(), ...connectedText.map((card) => `参考文案「${card.title}」\n${card.content}`)].filter(Boolean).join('\n\n')

  function removeNodes(removed: CanvasNode[], linkIds = new Set<string>()) {
    if (!removed.length && !linkIds.size) return
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
    const current = extrasRef.current
    if (current.cards.some((card) => moved.has(card.id))) {
      const positions = new Map(next.filter((node) => moved.has(node.id)).map((node) => [node.id, node.position]))
      saveExtras({ ...current, cards: current.cards.map((card) => positions.has(card.id) ? { ...card, position: positions.get(card.id)! } : card) })
    } else if (next.some((node) => moved.has(node.id))) {
      schedule(next)
    }
  }

  async function importFiles(files: File[], point?: XYPosition) {
    if (!files.length || !revision.current || uploadInProgress.current) return
    const position = nodePosition(point)
    uploadInProgress.current = true
    setUploading(true)
    const cards: MediaCard[] = []
    try {
      for (const file of files) {
        try {
          const media = await uploadDramaCanvasMedia(project.id, file)
          const offset = cards.length * 430
          cards.push({ id: crypto.randomUUID(), kind: media.kind, content: media.url,
            title: Array.from(file.name).slice(0, 80).join('') || cardNames[media.kind], position: { x: position.x + offset, y: position.y } })
        } catch (error) {
          messageApi.error(error instanceof Error ? `${file.name}：${error.message}` : '素材上传失败')
        }
      }
      if (cards.length) {
        const graph = extrasRef.current
        saveExtras({ ...graph, cards: [...graph.cards, ...cards] })
        choose(cards[0].id)
        messageApi.success(`已添加 ${cards.length} 个素材`)
      }
    } finally {
      uploadInProgress.current = false
      setUploading(false)
    }
  }

  const pasteFiles = useRef<(files: File[]) => void>(() => {})
  pasteFiles.current = (files) => { if (!cardDraft && !assetDraft && !editing && !preview && !mediaPreview) void importFiles(files) }
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
      if ((current.styleId || current.style) !== (style.id || style.name) || current.aspectRatio !== ratio) await editDramaForm(project.id, { ...current, styleId: style.id, style: style.name, aspectRatio: ratio })
      await generateDramaImage(project.id, current.id, generationPrompt); await query.refetch(); messageApi.success('图片生成任务已开始，可以继续操作画布')
    } catch (error) { await query.refetch(); messageApi.error(error instanceof Error ? error.message : '图片生成失败') } finally { setWorking(false) }
  }
  async function saveForm() { if (!editing) return; setWorking(true); try { await editDramaForm(project.id, editing); await query.refetch(); setEditing(null); messageApi.success('形象已保存') } catch (error) { messageApi.error(error instanceof Error ? error.message : '保存形象失败') } finally { setWorking(false) } }
  function arrange() {
    const indexes: Record<string, number> = { character: 0, scene: 0, prop: 0 }
    const next = nodesRef.current.map((node) => { if (node.type !== 'asset') return node; const k = node.data.form.kind; const index = indexes[k]++; return { ...node, position: { x: { character: 0, scene: 1250, prop: 2200 }[k] + (index % 3) * (k === 'scene' ? 420 : 320), y: Math.floor(index / 3) * (k === 'character' ? 660 : 470) } } })
    nodesRef.current = next; setNodes(next); schedule(next); setTimeout(() => void flow.fitView({ duration: 300, padding: 0.15 }), 50)
  }
  const addItems = [
    { key: 'assets', type: 'group' as const, label: '项目资产', children: Object.entries(kindNames).map(([key, label]) => ({ key, label })) },
    { key: 'media', type: 'group' as const, label: '素材节点', children: Object.entries(cardNames).map(([key, label]) => ({ key, label })) },
    { key: 'upload', label: '上传本地素材', icon: <UploadOutlined /> },
  ]
  function addItem(key: string) {
    if (key === 'upload') openFilePicker()
    else if (key in kindNames) openAsset(key as DramaAssetKind)
    else openCard(key as CardKind)
  }

  return <div className="drama-canvas-shell" ref={shell}
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
      <div><DramaImageModelSelect />
        <Dropdown trigger={['click']} menu={{ items: addItems, onClick: ({ key }) => addItem(key) }}>
          <Button icon={<PlusOutlined />} loading={uploading} disabled={!query.data || working}>添加节点</Button>
        </Dropdown>
        {extras.hidden.length > 0 && <Button onClick={() => saveExtras({ ...extrasRef.current, hidden: [] })}>恢复隐藏的形象</Button>}
        <Button onClick={arrange} disabled={!nodes.length || working || !!saveError}>整理画布</Button>
        <Button onClick={() => void flow.fitView({ duration: 300, padding: 0.15 })}>适应画布</Button>
      </div>
    </header>
    {saveError && <Alert className="drama-canvas-save-alert" type="error" showIcon title={saveError} action={<Button onClick={() => { if (conflict) window.location.reload(); else { blocked.current = false; void drain() } }}>{conflict ? '重新加载' : '重试保存'}</Button>} />}
    {query.error && query.data && !saveError && <Alert className="drama-canvas-save-alert" type="warning" showIcon title="暂时无法刷新资产状态"
      action={<Button size="small" onClick={() => void query.refetch()}>重试</Button>} />}
    {!query.data ? <div className="drama-canvas-loading">{query.error ? <Alert type="error" title="读取画布失败" description={query.error.message} action={<Button onClick={() => void query.refetch()}>重试</Button>} /> : <Skeleton active />}</div> : <>
      <ReactFlow<CanvasNode> nodes={nodes} edges={edges} nodeTypes={nodeTypes} onNodesChange={changeNodes}
        onNodeClick={() => setContextMenu(null)} onPaneClick={() => { setContextMenu(null); choose('') }}
        onNodeDoubleClick={(event, node) => {
          if (node.type !== 'media' || (event.target instanceof Element && event.target.closest('video,audio,button'))) return
          if (node.data.kind === 'text') setCardDraft({ card: { ...node.data, position: node.position } })
          else setMediaPreview(node.data)
        }}
        onPaneContextMenu={(event) => {
          event.preventDefault()
          setContextMenu({ point: { x: event.clientX, y: event.clientY } })
        }}
        onNodeContextMenu={(event, node) => {
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
          setSelectedLinks((current) => {
            const next = new Set(current)
            for (const change of changes) {
              if (change.type === 'select' && change.selected) next.add(change.id)
              else if (change.type === 'select' || change.type === 'remove') next.delete(change.id)
            }
            return next
          })
        }}
        onConnect={(connection) => connect(connection)} onConnectEnd={finishConnection}
        onReconnect={(edge, connection) => connect(connection, edge.id)}
        isValidConnection={(connection) => connection.source !== connection.target}
        selectionMode={SelectionMode.Partial} selectionKeyCode="Shift" multiSelectionKeyCode={['Meta', 'Control']}
        deleteKeyCode={['Backspace', 'Delete']} defaultEdgeOptions={{ type: 'default', interactionWidth: 22 }} minZoom={0.05} maxZoom={2} defaultViewport={viewport.current} proOptions={{ hideAttribution: false }} ariaLabelConfig={{ 'controls.ariaLabel': '画布控制', 'controls.zoomIn.ariaLabel': '放大', 'controls.zoomOut.ariaLabel': '缩小', 'controls.fitView.ariaLabel': '适应画布', 'minimap.ariaLabel': '画布缩略图' }}>
        <Background gap={22} size={1} color="#d5dce8" />
        <Controls showInteractive={false} />
        <MiniMap<CanvasNode> pannable zoomable nodeColor={(node) => node.type === 'media' ? '#e2a467' : ({ character: '#b9afe9', scene: '#a2c9be', prop: '#aec6e7' }[node.data.form.kind])} />
      </ReactFlow>
      {(selectedNodes.length > 0 || selectedLinks.size > 0) && <div className="drama-canvas-selection-tools">
        {selectedCard && <><Button size="small" icon={<EditOutlined />} onClick={() => setCardDraft({ card: { ...selectedCard.data, position: selectedCard.position } })}>编辑节点</Button>
          {selectedCard.data.kind !== 'text' && <Button size="small" icon={<EyeOutlined />} onClick={() => setMediaPreview(selectedCard.data)}>预览</Button>}
          <Button size="small" icon={<CopyOutlined />} onClick={() => duplicateCard(selectedCard.data, selectedCard.position)}>复制节点</Button></>}
        {selectedNodes.length > 1 && <span>{selectedNodes.length} 个节点</span>}
        <Button size="small" danger icon={<DeleteOutlined />} onClick={() => removeNodes(selectedNodes, selectedLinks)}>
          {selectedNodes.length === 1 && selectedNodes[0].type === 'asset' && !selectedLinks.size ? '隐藏形象' : selectedNodes.length ? '移除所选' : '断开连线'}
        </Button>
      </div>}
      {!nodes.length && <div className="drama-canvas-empty"><Empty description="添加节点、拖入文件或右键空白处开始" /></div>}
      <span className="drama-canvas-gesture-hint">拖动平移 · Shift 框选 · 滚轮缩放</span>
      {selected && <div className="drama-canvas-generation" aria-label="当前形象图片生成">
        <div className="drama-canvas-generation-heading"><span>{kindIcons[selected.kind]} {selected.assetName} · {selected.name}</span><Button type="text" size="small" icon={<EditOutlined />} disabled={busy(selected)} onClick={() => setEditing({ ...selected })}>编辑形象</Button></div>
        {selected.imageError && <div className="drama-canvas-generation-error">{selected.imageError}</div>}
        <Input.TextArea aria-label="本次生成描述" value={prompt} onChange={(event) => setPrompt(event.target.value)} autoSize={{ minRows: 2, maxRows: 4 }} placeholder="描述当前形象。本次修改只用于生成，编辑形象可保存长期描述。" maxLength={10000} disabled={busy(selected)} />
        <div className="drama-canvas-generation-toolbar"><DramaStylePicker value={style.id || style.name} valueLabel={style.name} onChange={(value) => setStyle({ id: value.id, name: value.name })} disabled={busy(selected)} /><Select aria-label="生成比例" value={ratio} onChange={setRatio} disabled={busy(selected)} options={[{ value: '9:16', label: '竖屏 9:16' }, { value: '16:9', label: '横屏 16:9' }, { value: '1:1', label: '方图 1:1' }]} /><span>{selected.parentId ? '使用原始形象作为参考图' : '生成原始形象'}{connectedText.length > 0 && ` · 引用 ${connectedText.length} 条文案`}</span><Button type="primary" icon={<PictureOutlined />} loading={working || busy(selected)} disabled={!generationPrompt || !!saveError} onClick={() => void generate()}>{selected.imageUrl ? '重新生成图片' : '生成图片'}</Button></div>
      </div>}
    </>}
    {dragOver && <div className="drama-canvas-drop"><UploadOutlined /><strong>松开后将文件添加到画布</strong></div>}
    {contextMenu && <div className="drama-canvas-menu-layer" onClick={() => setContextMenu(null)} onContextMenu={(event) => { event.preventDefault(); setContextMenu(null) }}>
      <div className="drama-canvas-menu" aria-label="画布操作"
        style={{ left: Math.max(4, Math.min(contextMenu.point.x, window.innerWidth - 182)), top: Math.max(4, Math.min(contextMenu.point.y, window.innerHeight - (contextMenu.node || contextMenu.edge ? 132 : 328))) }}
        onClick={(event) => event.stopPropagation()} onKeyDown={(event) => { if (event.key === 'Escape') setContextMenu(null) }}>
        {contextMenu.node?.type === 'asset' && <Button type="text" onClick={() => {
          if (contextMenu.node?.type === 'asset') setEditing(contextMenu.node.data.form)
          setContextMenu(null)
        }}>编辑形象</Button>}
        {contextMenu.node?.type === 'media' && <Button type="text" onClick={() => {
          if (contextMenu.node?.type === 'media') setCardDraft({ card: { ...contextMenu.node.data, position: contextMenu.node.position } })
          setContextMenu(null)
        }}>编辑节点</Button>}
        {contextMenu.node?.type === 'media' && <Button type="text" onClick={() => {
          const node = contextMenu.node
          if (node?.type === 'media') duplicateCard(node.data, node.position)
        }}>复制节点</Button>}
        {contextMenu.node && <Button type="text" danger onClick={() => {
          if (contextMenu.node) removeNodes([contextMenu.node])
          setContextMenu(null)
        }}>{contextMenu.node.type === 'asset' ? '隐藏形象' : '删除节点'}</Button>}
        {contextMenu.edge && <Button type="text" danger disabled={contextMenu.edge.deletable === false} onClick={() => {
          if (contextMenu.edge) removeNodes([], new Set([contextMenu.edge.id]))
          setContextMenu(null)
        }}>断开连线</Button>}
        {!contextMenu.node && !contextMenu.edge && <>
          {contextMenu.connection && <small>创建节点并连线</small>}
          {Object.entries(kindNames).map(([kind, label]) =>
            <Button key={kind} type="text" onClick={() => openAsset(kind as DramaAssetKind, contextMenu.point, contextMenu.connection)}>添加{label}</Button>)}
          {Object.entries(cardNames).map(([kind, label]) =>
            <Button key={kind} type="text" onClick={() => openCard(kind as CardKind, contextMenu.point, contextMenu.connection)}>添加{label}</Button>)}
          {!contextMenu.connection && <Button type="text" icon={<UploadOutlined />} onClick={() => { setContextMenu(null); openFilePicker(contextMenu.point) }}>上传本地素材</Button>}
        </>}
      </div>
    </div>}
    {cardDraft && <DramaCanvasCardEditor key={cardDraft.card.id} draft={cardDraft.card}
      editing={extras.cards.some((card) => card.id === cardDraft.card.id)} busy={uploading} onCancel={() => setCardDraft(null)} onSave={(card, file) => void saveCard(card, file)} />}
    <Modal title={assetDraft ? `添加${kindNames[assetDraft.kind]}` : '添加资产'} open={!!assetDraft}
      onCancel={() => { if (!working) setAssetDraft(null) }} onOk={() => void saveAsset()} confirmLoading={working}
      okText="添加" cancelText="取消" okButtonProps={{ disabled: !assetDraft?.name.trim() || !assetDraft.description.trim() }} destroyOnHidden>
      {assetDraft && <div className="drama-canvas-edit-form">
        <label>名称<Input maxLength={80} value={assetDraft.name} onChange={(event) => setAssetDraft({ ...assetDraft, name: event.target.value })} /></label>
        <label>形象描述<Input.TextArea rows={5} maxLength={10000} value={assetDraft.description} onChange={(event) => setAssetDraft({ ...assetDraft, description: event.target.value })} /></label>
      </div>}
    </Modal>
    <Modal title={editing ? `编辑${kindNames[editing.kind]}形象` : '编辑形象'} open={!!editing} onCancel={() => setEditing(null)} onOk={() => void saveForm()} confirmLoading={working} okText="保存" cancelText="取消" destroyOnHidden>
      {editing && <div className="drama-canvas-edit-form"><label>形象名称<Input value={editing.name} maxLength={80} onChange={(event) => setEditing({ ...editing, name: event.target.value })} /></label><label>形象描述<Input.TextArea value={editing.description} maxLength={10000} showCount rows={7} onChange={(event) => setEditing({ ...editing, description: event.target.value })} /></label><div>{editing.kind === 'character' && <label>性别<Select value={editing.gender} onChange={(value) => setEditing({ ...editing, gender: value })} options={[{ value: 'unspecified', label: '未指定' }, { value: 'male', label: '男' }, { value: 'female', label: '女' }, { value: 'other', label: '其他' }]} /></label>}<label>风格<DramaStylePicker value={editing.styleId || editing.style} valueLabel={editing.style} onChange={(style) => setEditing({ ...editing, styleId: style.id, style: style.name })} /></label><label>比例<Select value={editing.aspectRatio} onChange={(value) => setEditing({ ...editing, aspectRatio: value })} options={['9:16', '16:9', '1:1'].map((value) => ({ value, label: value }))} /></label></div></div>}
    </Modal>
    <Modal title={preview ? `${preview.assetName} · ${preview.name}` : '图片预览'} open={!!preview} footer={null} width={850} onCancel={() => setPreview(null)}>{preview?.imageUrl ? <img className="drama-canvas-preview-image" src={preview.imageUrl} alt={`${preview.assetName} · ${preview.name}`} /> : <Empty description="尚未生成图片" />}</Modal>
    <Modal title={mediaPreview?.title} open={!!mediaPreview} footer={null} width={850} onCancel={() => setMediaPreview(null)} destroyOnHidden>
      {mediaPreview?.kind === 'image' && <img className="drama-canvas-preview-image" src={mediaPreview.content} alt={mediaPreview.title} />}
      {mediaPreview?.kind === 'video' && <video className="drama-canvas-preview-image" src={mediaPreview.content} controls playsInline />}
      {mediaPreview?.kind === 'audio' && <audio className="drama-canvas-preview-audio" src={mediaPreview.content} controls />}
    </Modal>
  </div>
}
