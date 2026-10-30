import { ArrowLeftOutlined, EditOutlined, EyeOutlined, GiftOutlined, PictureOutlined, PlusOutlined, UserOutlined } from '@ant-design/icons'
import { Alert, Button, Empty, Input, Modal, Select, Skeleton, Tag, Tooltip, message } from 'antd'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { Background, Controls, Handle, MiniMap, Position, ReactFlow, ReactFlowProvider, applyNodeChanges, useNodesInitialized, useReactFlow, type Node, type NodeProps, type Viewport, type NodeChange } from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { ApiError } from '../api/client'
import { deriveDramaForm, editDramaForm, generateDramaImage, getDramaCanvas, saveDramaCanvasLayout, type DramaAssetForm, type DramaCanvasLayout, type DramaProject } from '../api/drama'
import { DramaImageModelSelect, useDramaCanvas } from './DramaImageTools'
import './drama-canvas.css'

type AssetNode = Node<{ form: DramaAssetForm; edit: (id: string) => void; derive: (id: string) => void; preview: (id: string) => void }, 'asset'>
const kindNames = { character: '角色', scene: '场景', prop: '道具' }
const kindIcons = { character: <UserOutlined />, scene: <PictureOutlined />, prop: <GiftOutlined /> }
const busy = (form?: DramaAssetForm) => form?.imageStatus === 'queued' || form?.imageStatus === 'running'
function AssetCanvasNode({ data, selected }: NodeProps<AssetNode>) {
  const f = data.form
  return <div className={`drama-canvas-node ${f.kind}${selected ? ' selected' : ''}`}>
    <div className="drama-canvas-node-caption">{kindIcons[f.kind]}<span>{f.assetName} · {f.name}</span></div>
    <div className="drama-canvas-node-body">
      <Handle type="target" position={Position.Left} isConnectable={false} />
      <div className="drama-canvas-node-image" onDoubleClick={() => data.preview(f.id)}>
        {f.imageUrl ? <img src={f.imageUrl} alt={`${f.assetName} · ${f.name}`} loading="lazy" /> : <div className="drama-canvas-node-placeholder">{kindIcons[f.kind]}<span>{busy(f) ? '图片生成中…' : '尚未生成图片'}</span></div>}
        {busy(f) && f.imageUrl && <Tag color="processing">图片生成中</Tag>}
        <button className="drama-canvas-preview nodrag" aria-label={`预览${f.assetName}${f.name}`} onClick={() => data.preview(f.id)}><EyeOutlined /></button>
      </div>
      <div className="drama-canvas-node-copy"><strong>{f.name}</strong><Button type="text" size="small" className="nodrag" aria-label={`编辑${f.assetName}${f.name}`} icon={<EditOutlined />} onClick={() => data.edit(f.id)} /><p title={f.description}>{f.description || '请编辑形象描述'}</p><small>{f.parentId ? '派生形象' : '原始形象'} · {f.aspectRatio}</small>{f.imageError && <Tooltip title={f.imageError}><span className="drama-canvas-node-error">生成失败，选中后可重试</span></Tooltip>}</div>
      <Handle type="source" position={Position.Right} isConnectable={false} />
      <Tooltip title="添加基于此形象的派生形象"><button className="drama-canvas-derive nodrag" aria-label={`为${f.assetName}${f.name}添加派生形象`} onClick={() => data.derive(f.id)}><PlusOutlined /></button></Tooltip>
    </div>
  </div>
}
const nodeTypes = { asset: AssetCanvasNode }

export function DramaAssetCanvas({ project }: { project: DramaProject }) { return <ReactFlowProvider><AssetCanvas project={project} /></ReactFlowProvider> }
function AssetCanvas({ project }: { project: DramaProject }) {
  const [params, setParams] = useSearchParams()
  const query = useDramaCanvas(project.id)
  const flow = useReactFlow<AssetNode>()
  const measured = useNodesInitialized()
  const [messageApi, holder] = message.useMessage()
  const [nodes, setNodes] = useState<AssetNode[]>([])
  const nodesRef = useRef(nodes)
  const [selectedId, setSelectedId] = useState('')
  const [editing, setEditing] = useState<DramaAssetForm | null>(null)
  const [preview, setPreview] = useState<DramaAssetForm | null>(null)
  const [working, setWorking] = useState(false)
  const [prompt, setPrompt] = useState('')
  const [style, setStyle] = useState('')
  const [ratio, setRatio] = useState<DramaAssetForm['aspectRatio']>('9:16')
  const [saveStatus, setSaveStatus] = useState('已保存')
  const [saveError, setSaveError] = useState('')
  const [conflict, setConflict] = useState(false)
  const revision = useRef(0)
  const viewport = useRef<Viewport>({ x: 0, y: 0, zoom: 0.6 })
  const pending = useRef<Omit<DramaCanvasLayout, 'revision'> | null>(null)
  const saving = useRef<Promise<void> | null>(null)
  const blocked = useRef(false)
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const initialized = useRef(false)
  function choose(id: string) {
    setSelectedId(id)
    setParams((current) => { const next = new URLSearchParams(current); next.delete('focus'); if (id) next.set('selected', id); else next.delete('selected'); return next }, { replace: true })
  }
  const selected = query.data?.forms.find((f) => f.id === selectedId)
  const selectedRef = useRef(selected)
  selectedRef.current = selected
  const drain = useCallback(async () => {
    if (saving.current) return saving.current
    if (blocked.current) return
    const task = (async () => {
      while (pending.current && !blocked.current) {
        const layout = pending.current; pending.current = null; setSaveStatus('保存中…')
        try { const result = await saveDramaCanvasLayout(project.id, { ...layout, revision: revision.current }); revision.current = result.revision; setSaveStatus(pending.current ? '待保存' : '已保存'); setSaveError('') }
        catch (error) { pending.current = pending.current || layout; blocked.current = true; setSaveStatus('尚未保存'); setConflict(error instanceof ApiError && error.status === 409); setSaveError(error instanceof Error ? error.message : '布局保存失败') }
      }
    })()
    saving.current = task
    try { await task } finally { saving.current = null }
  }, [project.id])
  async function flush() { clearTimeout(timer.current); await drain(); if (blocked.current) throw new Error('布局尚未保存，请先处理保存错误') }
  function schedule(nextNodes = nodesRef.current) {
    if (!initialized.current) return
    pending.current = { ...viewport.current, positions: nextNodes.map((n) => ({ id: n.id, ...n.position })) }
    setSaveStatus('待保存'); clearTimeout(timer.current); timer.current = setTimeout(() => void drain(), 650)
  }
  async function derive(id: string) {
    if (working) return
    setWorking(true)
    try { await flush(); const result = await deriveDramaForm(project.id, id); const data = await getDramaCanvas(project.id); revision.current = data.revision; await query.refetch(); choose(result.id); setTimeout(() => void flow.fitView({ nodes: [{ id: result.id }], maxZoom: 0.9, duration: 300, padding: 0.5 }), 100); messageApi.success('已添加派生形象，请修改描述后生成图片') }
    catch (error) { messageApi.error(error instanceof Error ? error.message : '添加派生形象失败') } finally { setWorking(false) }
  }
  useEffect(() => {
    const data = query.data
    if (!data) return
    if (revision.current === 0) { revision.current = data.revision; viewport.current = { x: data.x, y: data.y, zoom: data.zoom } }
    setNodes((current) => {
      const old = new Map(current.map((n) => [n.id, n]))
      const next = data.forms.map((form): AssetNode => ({ ...old.get(form.id), id: form.id, type: 'asset', position: old.get(form.id)?.position || { x: form.x, y: form.y }, selected: form.id === selectedId, data: { form, edit: (id) => setEditing(data.forms.find((f) => f.id === id) || null), preview: (id) => setPreview(data.forms.find((f) => f.id === id) || null), derive: (id) => void derive(id) } }))
      nodesRef.current = next; return next
    })
    // 轮询只更新形象内容和生成状态，不覆盖尚未保存的拖动位置。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [query.data, selectedId])
  useEffect(() => {
    if (!measured || initialized.current || !query.data) return
    initialized.current = true
    const focus = params.get('focus')
    const target = query.data.forms.find((f) => f.id === focus || f.assetId === focus)
    const restored = query.data.forms.find((f) => f.id === params.get('selected'))
    if (target) { choose(target.id); void flow.fitView({ nodes: [{ id: target.id }], maxZoom: 0.85, padding: 0.5 }) }
    else { if (restored) setSelectedId(restored.id); void flow.setViewport(viewport.current) }
  }, [measured, flow, params, query.data])
  useEffect(() => { setPrompt(selected?.description || ''); setStyle(selected?.style || ''); setRatio(selected?.aspectRatio || '9:16') }, [selectedId, selected?.revision])
  useEffect(() => {
    function beforeUnload(event: BeforeUnloadEvent) { if (pending.current || saving.current) { event.preventDefault(); event.returnValue = '' } }
    window.addEventListener('beforeunload', beforeUnload)
    return () => { window.removeEventListener('beforeunload', beforeUnload); clearTimeout(timer.current); void drain() }
  }, [drain])
  const edges = useMemo(() => query.data?.forms.filter((f) => f.parentId).map((f) => ({ id: `${f.parentId}-${f.id}`, source: f.parentId!, target: f.id, type: 'smoothstep', style: { stroke: '#8a79de', strokeWidth: 2 } })) || [], [query.data])
  function changeNodes(changes: NodeChange<AssetNode>[]) {
    const next = applyNodeChanges(changes, nodesRef.current); nodesRef.current = next; setNodes(next)
    if (changes.some((c) => c.type === 'position' && c.dragging === false)) schedule(next)
  }
  async function leave() { try { await flush(); setParams((current) => { const next = new URLSearchParams(current); next.delete('canvas'); next.delete('focus'); next.delete('selected'); return next }) } catch (error) { messageApi.error(error instanceof Error ? error.message : '保存失败') } }
  async function generate() {
    const current = selectedRef.current
    if (!current || working || busy(current)) return
    setWorking(true)
    try {
      if (current.style !== style || current.aspectRatio !== ratio) await editDramaForm(project.id, { ...current, style, aspectRatio: ratio })
      await generateDramaImage(project.id, current.id, prompt); await query.refetch(); messageApi.success('图片生成任务已开始，可以继续操作画布')
    } catch (error) { await query.refetch(); messageApi.error(error instanceof Error ? error.message : '图片生成失败') } finally { setWorking(false) }
  }
  async function saveForm() { if (!editing) return; setWorking(true); try { await editDramaForm(project.id, editing); await query.refetch(); setEditing(null); messageApi.success('形象已保存') } catch (error) { messageApi.error(error instanceof Error ? error.message : '保存形象失败') } finally { setWorking(false) } }
  function arrange() {
    const indexes: Record<string, number> = { character: 0, scene: 0, prop: 0 }
    const next = nodesRef.current.map((node) => { const k = node.data.form.kind; const index = indexes[k]++; return { ...node, position: { x: { character: 0, scene: 1250, prop: 2200 }[k] + (index % 3) * (k === 'scene' ? 420 : 320), y: Math.floor(index / 3) * (k === 'character' ? 660 : 470) } } })
    nodesRef.current = next; setNodes(next); schedule(next); setTimeout(() => void flow.fitView({ duration: 300, padding: 0.15 }), 50)
  }
  return <div className="drama-canvas-shell">
    {holder}
    <header className="drama-canvas-header"><div><Button icon={<ArrowLeftOutlined />} type="text" onClick={() => void leave()} aria-label="返回资产列表" /><strong>{project.title}</strong><span className={saveError ? 'save-error' : ''}>{saveStatus}</span></div><div><DramaImageModelSelect /><Button onClick={arrange} disabled={!nodes.length || working || !!saveError}>整理画布</Button><Button onClick={() => void flow.fitView({ duration: 300, padding: 0.15 })}>适应画布</Button></div></header>
    {saveError && <Alert className="drama-canvas-save-alert" type="error" showIcon title={saveError} action={<Button onClick={() => { if (conflict) window.location.reload(); else { blocked.current = false; void drain() } }}>{conflict ? '重新加载' : '重试保存'}</Button>} />}
    {query.isLoading ? <div className="drama-canvas-loading"><Skeleton active /></div> : query.error ? <div className="drama-canvas-loading"><Alert type="error" title="读取画布失败" description={query.error.message} action={<Button onClick={() => void query.refetch()}>重试</Button>} /></div> : <>
      <ReactFlow<AssetNode> nodes={nodes} edges={edges} nodeTypes={nodeTypes} onNodesChange={changeNodes} onNodeDragStart={(_, node) => choose(node.id)} onNodeClick={(_, node) => choose(node.id)} onPaneClick={() => choose('')} onMoveEnd={(_, next) => { viewport.current = next; schedule() }} nodesConnectable={false} edgesReconnectable={false} deleteKeyCode={null} minZoom={0.05} maxZoom={2} defaultViewport={viewport.current} proOptions={{ hideAttribution: false }} ariaLabelConfig={{ 'controls.ariaLabel': '画布控制', 'controls.zoomIn.ariaLabel': '放大', 'controls.zoomOut.ariaLabel': '缩小', 'controls.fitView.ariaLabel': '适应画布', 'minimap.ariaLabel': '画布缩略图' }}>
        <Background gap={22} size={1} color="#d5dce8" />
        <Controls showInteractive={false} />
        <MiniMap<AssetNode> pannable zoomable nodeColor={(node) => ({ character: '#b9afe9', scene: '#a2c9be', prop: '#aec6e7' }[node.data.form.kind])} />
      </ReactFlow>
      {!nodes.length && <div className="drama-canvas-empty"><Empty description="剧本解析提取资产后，形象会出现在画布中" /></div>}
      {selected && <div className="drama-canvas-generation" aria-label="当前形象图片生成">
        <div className="drama-canvas-generation-heading"><span>{kindIcons[selected.kind]} {selected.assetName} · {selected.name}</span><Button type="text" size="small" icon={<EditOutlined />} disabled={busy(selected)} onClick={() => setEditing({ ...selected })}>编辑形象</Button></div>
        {selected.imageError && <div className="drama-canvas-generation-error">{selected.imageError}</div>}
        <Input.TextArea aria-label="本次生成描述" value={prompt} onChange={(event) => setPrompt(event.target.value)} autoSize={{ minRows: 2, maxRows: 4 }} placeholder="描述当前形象。本次修改只用于生成，编辑形象可保存长期描述。" maxLength={10000} disabled={busy(selected)} />
        <div className="drama-canvas-generation-toolbar"><Input aria-label="生成风格" value={style} placeholder="画面风格" maxLength={120} onChange={(event) => setStyle(event.target.value)} disabled={busy(selected)} /><Select aria-label="生成比例" value={ratio} onChange={setRatio} disabled={busy(selected)} options={[{ value: '9:16', label: '竖屏 9:16' }, { value: '16:9', label: '横屏 16:9' }, { value: '1:1', label: '方图 1:1' }]} /><span>{selected.parentId ? '使用原始形象作为参考图' : '生成原始形象'}</span><Button type="primary" icon={<PictureOutlined />} loading={working || busy(selected)} disabled={!prompt.trim() || !!saveError} onClick={() => void generate()}>{selected.imageUrl ? '重新生成图片' : '生成图片'}</Button></div>
      </div>}
    </>}
    <Modal title={editing ? `编辑${kindNames[editing.kind]}形象` : '编辑形象'} open={!!editing} onCancel={() => setEditing(null)} onOk={() => void saveForm()} confirmLoading={working} okText="保存" cancelText="取消" destroyOnHidden>
      {editing && <div className="drama-canvas-edit-form"><label>形象名称<Input value={editing.name} maxLength={80} onChange={(event) => setEditing({ ...editing, name: event.target.value })} /></label><label>形象描述<Input.TextArea value={editing.description} maxLength={10000} showCount rows={7} onChange={(event) => setEditing({ ...editing, description: event.target.value })} /></label><div>{editing.kind === 'character' && <label>性别<Select value={editing.gender} onChange={(value) => setEditing({ ...editing, gender: value })} options={[{ value: 'unspecified', label: '未指定' }, { value: 'male', label: '男' }, { value: 'female', label: '女' }, { value: 'other', label: '其他' }]} /></label>}<label>风格<Input value={editing.style} maxLength={120} onChange={(event) => setEditing({ ...editing, style: event.target.value })} /></label><label>比例<Select value={editing.aspectRatio} onChange={(value) => setEditing({ ...editing, aspectRatio: value })} options={['9:16', '16:9', '1:1'].map((value) => ({ value, label: value }))} /></label></div></div>}
    </Modal>
    <Modal title={preview ? `${preview.assetName} · ${preview.name}` : '图片预览'} open={!!preview} footer={null} width={850} onCancel={() => setPreview(null)}>{preview?.imageUrl ? <img className="drama-canvas-preview-image" src={preview.imageUrl} alt={`${preview.assetName} · ${preview.name}`} /> : <Empty description="尚未生成图片" />}</Modal>
  </div>
}
