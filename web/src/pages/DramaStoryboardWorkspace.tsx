import { ClockCircleOutlined, DeleteOutlined, EditOutlined, HistoryOutlined, PlusOutlined, ReloadOutlined, VideoCameraOutlined } from '@ant-design/icons'
import { Alert, Button, Empty, Image, Input, InputNumber, Modal, Progress, Select, Skeleton, Space, Tag, Tooltip, message } from 'antd'
import { useEffect, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useSearchParams } from 'react-router-dom'
import { optimizeDramaStoryboardPacing, type DramaRepairTicket, getDramaStoryboards, saveDramaStoryboard, startDramaStoryboard, type DramaStoryboardSegment, type DramaStoryboardShot, type DramaStoryboardUtterance, type DramaAssetForm } from '../api/drama'

import { DramaSegmentVideoPreview } from './DramaSegmentVideoPreview'
import { DramaStoryboardTextEditor } from './DramaStoryboardTextEditor'
import { inferStoryboardMentions, materializeStoryboardMentions, type StoryboardTextDocument } from './storyboardMentions'

export const storyboardStatusLabels: Record<string, string> = { pending: '待拆分', queued: '等待拆分', running: '拆分中', failed: '需重试', partial: '待继续', ready: '已保存', confirmed: '已保存', stale: '来源已变化' }
const soundLabels = { dialogue: '对白', os: '内心独白', vo: '画外音' }
const soundDescriptions = { dialogue: '角色在故事里说出口的话', os: '角色心里想的话，其他角色听不到', vo: '来自画面之外的声音，可以是旁白或未入镜的角色' }
const choicePopup = { popupMatchSelectWidth: 184, virtual: false, classNames: { popup: { root: 'drama-storyboard-choice-popup' } } }
const shotSizes = ['大远景', '远景', '全景', '中景', '近景', '特写', '大特写', '过肩镜头', '主观镜头']
const cameras = ['固定', '缓慢推进', '拉远', '跟拍', '左摇', '右摇', '上摇', '下摇', '环绕', '升降']
const linesOf = (text: string) => text.replace(/\r\n?/g, '\n').split('\n').map((line) => line.trim()).filter(Boolean)
function repairLabel(ticket: DramaRepairTicket) {
 if (ticket.node === 'pacing') return ({ assigned: '优化中', resolved: '已采纳并修改', feedback: '继续优化', blocked: '已中断', declined: '未采纳，保留原稿' })[ticket.status]
 return ({ assigned: '已派工', resolved: '复核通过', feedback: '继续修正', blocked: '需人工处理', declined: '未采纳' })[ticket.status]
}
const blankShot = (line: number): DramaStoryboardShot => ({ id: crypto.randomUUID(), number: 1, description: '', shotSize: '中景', camera: '固定', duration: 4, sound: '', sourceLines: [line], utterances: [] })

export function DramaStoryboardWorkspace({ projectId, episodeId, needsReview, onFormsChange }: {projectId: string; episodeId: string; needsReview: boolean; onFormsChange?: (forms: DramaAssetForm[]) => void}) {
  const [messageApi, messageHolder] = message.useMessage()
  const [params, setParams] = useSearchParams()
  const runId = params.get('storyboard') || ''
  const selectedId = params.get('segment') || ''
  const client = useQueryClient()
  const [draft, setDraft] = useState<DramaStoryboardSegment[] | null>(null)
  const [busy, setBusy] = useState(false)
  const [structureEditing, setStructureEditing] = useState(false)
  const [discardOpen, setDiscardOpen] = useState(false)
  const [snapshotHistoryOpen, setSnapshotHistoryOpen] = useState(false)
  const [recordView, setRecordView] = useState<'events' | 'repairs' | null>(null)
  const [error, setError] = useState('')
  const draftRef = useRef(draft); draftRef.current = draft
  const query = useQuery({ queryKey: ['drama-storyboards', projectId, episodeId, runId], queryFn: () => getDramaStoryboards(projectId, episodeId, runId), refetchInterval: (q) => ['queued', 'running'].includes(q.state.data?.runs[0]?.status || '') ? 2000 : false, refetchOnWindowFocus: !draft })
  const page = query.data
  const run = page?.current
  const latest = !run || run.id === page?.runs[0]?.id
  const active = ['running', 'queued'].includes(page?.runs[0]?.status || '')
  const readonly = !latest || active || !!run?.stale || needsReview
  const segments = draft ?? run?.segments ?? []
  const selected = segments.find((s) => s.id === selectedId) ?? segments[0]
  const lines = linesOf(run?.scriptText || '')
  const pacingHistory = run?.repairs?.filter((ticket) => ticket.node === 'pacing' && ticket.item === selected?.number) ?? []
  const catalog = (page?.catalogForms ?? page?.forms ?? []).map((form) => {
    const binding = selected?.bindings.find((b) => b.formId === form.id)
    return binding?.imageId ? { ...form, imageUrl: `/api/v1/ai-drama/projects/${projectId}/images/${binding.imageId}/content` } : form
  })
  const boundForms = catalog.filter((f) => selected?.formIds.includes(f.id))
  const inlineEditable = !readonly && !busy && !!run && segments.length > 0 && segments.every((s) => s.status === 'ready')
  const structural = !!draft && structureEditing
  const usedFormKey = [...new Set(segments.flatMap((segment) => segment.formIds))].sort().join('|')
  const duration = (shots: DramaStoryboardShot[]) => shots.reduce((sum, s) => sum + s.duration, 0)
  useEffect(() => {
    if (!run || !onFormsChange) return
    const used = new Set(usedFormKey.split('|'))
    onFormsChange((page?.catalogForms ?? page?.forms ?? []).filter((form) => used.has(form.id)))
  }, [usedFormKey, run?.id, page?.catalogForms, page?.forms, onFormsChange])
  useEffect(() => {
    if (!draft) return
    const guard = (event: BeforeUnloadEvent) => event.preventDefault()
    // 制作台外的返回、来源和画布入口会卸载编辑器；离开前提醒保存。
    const guardNavigation = (event: MouseEvent) => {
      const target = event.target instanceof Element ? event.target.closest('button,a') : null
      if (!draftRef.current || !target || target.closest('[aria-label="本集分镜制作区域"]')) return
      if ((target.matches('[aria-label="返回视频分集"]') || /来源剧本|进入画布/.test(target.textContent || '')) && !window.confirm('分镜有未保存的修改，确定离开并放弃修改吗？')) { event.preventDefault(); event.stopPropagation() }
    }
    window.addEventListener('beforeunload', guard)
    document.addEventListener('click', guardNavigation, true)
    return () => { window.removeEventListener('beforeunload', guard); document.removeEventListener('click', guardNavigation, true) }
  }, [draft])
  function chooseSegment(id: string) { setParams((p) => { const next = new URLSearchParams(p); next.set('segment', id); return next }, { replace: true }) }
  async function refresh() { await client.invalidateQueries({ queryKey: ['drama-storyboards', projectId, episodeId] }); await client.invalidateQueries({ queryKey: ['drama-video-episodes', projectId] }) }
  async function action(fn: () => Promise<unknown>) { setBusy(true); setError(''); try { await fn(); await refresh() } catch (failure) { setError(failure instanceof Error ? failure.message : '操作失败，请重试') } finally { setBusy(false) } }
  function start(resume: boolean) {
    const perform = () => void action(async () => { const started = await startDramaStoryboard(projectId, episodeId, resume); setParams((p) => { const next = new URLSearchParams(p); next.delete('storyboard'); const pending = started.segments.find((s) => s.status !== 'ready'); if (resume && pending) next.set('segment', pending.id); else next.delete('segment'); return next }); if (resume && run && started.id !== run.id) messageApi.success('已保留完成片段，建立新的续跑版本'); })
    if (!resume && run) Modal.confirm({ title: '重新拆分这一集？', content: '会从导演规划重新开始，并建立新版本。已有分镜版本会保留，不会自动生成图片或视频。', onOk: perform, okText: '重新拆分', cancelText: '取消' })
    else perform()
  }
  function optimizePacing() {
    if (!run || !selected) return
    void action(async () => {
      const next = await optimizeDramaStoryboardPacing(projectId, episodeId, run, selected.id)
      setParams((p) => { const value = new URLSearchParams(p); value.set('storyboard', next.id); const target = next.segments[selected.number - 1]; if (target) value.set('segment', target.id); return value })
    })
  }
  function changeSegment(patch: Partial<DramaStoryboardSegment>) { if (!selected) return; setDraft((current) => (current ?? run?.segments ?? []).map((s) => s.id === selected.id ? { ...s, ...patch } : s)) }
  function changeShot(index: number, patch: Partial<DramaStoryboardShot>) { if (selected) changeSegment({ shots: selected.shots.map((s, i) => i === index ? { ...s, ...patch } : s) }) }
  function inlineSegment(field: 'scene' | 'purpose', value: StoryboardTextDocument) {
    if (!selected) return
    changeSegment({ [field]: value.text, [field === 'scene' ? 'sceneMentions' : 'purposeMentions']: value.mentions, formIds: [...new Set([...selected.formIds, ...value.mentions.map((r) => r.formId)])] })
  }
  function inlineShot(index: number, field: 'description' | 'sound', value: StoryboardTextDocument) {
    if (!selected) return
    changeSegment({ shots: selected.shots.map((shot, i) => i === index ? { ...shot, [field]: value.text, [field === 'description' ? 'descriptionMentions' : 'soundMentions']: value.mentions } : shot), formIds: [...new Set([...selected.formIds, ...value.mentions.map((r) => r.formId)])] })
  }
  function inlineVoice(index: number, number: number, field: 'text' | 'speaker', value: StoryboardTextDocument) {
    if (!selected) return
    changeSegment({ shots: selected.shots.map((shot, i) => i === index ? { ...shot, utterances: shot.utterances.map((u, j) => j === number ? { ...u, [field]: value.text, [field === 'text' ? 'mentions' : 'speakerMentions']: value.mentions } : u) } : shot), formIds: [...new Set([...selected.formIds, ...value.mentions.map((r) => r.formId)])] })
  }
  function textDocument(text: string, mentions: DramaStoryboardUtterance['mentions']) {
    return { text, mentions: inferStoryboardMentions(text, boundForms, mentions) }
  }
  function changeForms(formIds: string[]) {
    if (!selected) return
    const next = materializeStoryboardMentions([selected], catalog)[0]
    const keep = (refs: DramaStoryboardUtterance['mentions']) => refs?.filter((ref) => formIds.includes(ref.formId))
    next.formIds = formIds
    next.sceneMentions = keep(next.sceneMentions); next.purposeMentions = keep(next.purposeMentions)
    for (const shot of next.shots) {
      shot.descriptionMentions = keep(shot.descriptionMentions); shot.soundMentions = keep(shot.soundMentions)
      for (const voice of shot.utterances) { voice.mentions = keep(voice.mentions); voice.speakerMentions = keep(voice.speakerMentions) }
    }
    changeSegment(next)
  }
  function discardDraft() {
    setDraft(null)
    setStructureEditing(false)
    setError('')
    setDiscardOpen(false)
  }
  function moveSegment(direction: number) { if (!draft || !selected) return; const next = [...draft]; const at = next.findIndex((s) => s.id === selected.id); const target = at + direction; if (target < 0 || target >= next.length) return; [next[at], next[target]] = [next[target], next[at]]; setDraft(next) }
  function moveShot(index: number, direction: number) { if (!selected) return; const next = [...selected.shots]; const target = index + direction; if (target < 0 || target >= next.length) return; [next[index], next[target]] = [next[target], next[index]]; changeSegment({ shots: next }) }
  function addSegment() { const id = crypto.randomUUID(); const line = Math.max(1, Math.min(lines.length, (segments.at(-1)?.endLine ?? 0) + 1)); setDraft((current) => [...(current ?? []), { id, number: segments.length + 1, title: '新片段', scene: '', purpose: '', startLine: line, endLine: line, startState: '', endState: '', status: 'ready', notes: '', formIds: [], bindings: [], shots: [blankShot(line)] }]); chooseSegment(id) }
  if (query.isPending) return <Skeleton active />
  if (query.isError) return <Alert type="error" showIcon title="读取分镜失败" description={query.error.message} action={<Button onClick={() => void query.refetch()}>重试</Button>} />
  return <>
    {messageHolder}
    <Modal mask={{ closable: false }} open={discardOpen} title="放弃未保存的修改？" okText="放弃修改" cancelText="继续编辑"
      okButtonProps={{ danger: true }} onOk={discardDraft} onCancel={() => setDiscardOpen(false)} />
    {run && <Modal open={recordView !== null} title={recordView === 'events' ? '创作动态' : '中心调度'}
      width={760} footer={null} onCancel={() => setRecordView(null)}>
      {recordView === 'events'
        ? (run.events.length ? <ol className="drama-storyboard-events">{run.events.map((event) => <li key={event.id}><time>{new Date(event.createdAt).toLocaleTimeString()}</time>{event.message}</li>)}</ol> : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无创作动态" />)
        : (run.repairs?.length ? <div className="drama-storyboard-repair-history drama-storyboard-records">{run.repairs.map((ticket) => <article key={ticket.id}><Tag color={ticket.status === 'resolved' ? 'success' : ticket.status === 'blocked' ? 'error' : 'processing'}>{repairLabel(ticket)}</Tag><strong>{ticket.reporter} → {({ pacing: '节奏优化', timing: '时长校正', writer: '分镜编写', planner: '导演规划', reviewer: '审阅', previous: '前片段修正', pause: '人工处理' } as Record<string, string>)[ticket.node] || ticket.node} · 目标 {ticket.targetItem}</strong><p>{ticket.reason}</p><small>{ticket.outcome || ticket.instructions} · 返回{ticket.returnTo}</small></article>)}</div>
          : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无自动修正记录" />)}
    </Modal>}
    <Modal open={snapshotHistoryOpen} title="整集快照" footer={null} onCancel={() => setSnapshotHistoryOpen(false)}>
      <p>查看整集的片段排列和当时的内容。查看单个片段的历史，请用片段标题旁的「片段版本」。</p>
      <Select aria-label="选择整集快照" value={run?.id} style={{ width: '100%' }}
        options={page?.runs.map((snapshot, index) => ({
          value: snapshot.id,
          label: `整集快照 ${snapshot.version} · ${storyboardStatusLabels[snapshot.status]}${index === 0 ? '（最新）' : ''}`,
        }))}
        onChange={(id) => {
          setParams((current) => {
            const next = new URLSearchParams(current)
            if (id === page?.runs[0]?.id) next.delete('storyboard')
            else next.set('storyboard', id)
            next.delete('segment')
            return next
          })
          setSnapshotHistoryOpen(false)
        }} />
    </Modal>
    <div className="drama-storyboard-toolbar">
      <div><h2>分镜脚本</h2>{run && <Tag color={run.status === 'failed' ? 'error' : (run.status === 'ready' || run.status === 'confirmed') ? 'success' : 'processing'}>{storyboardStatusLabels[run.status]}</Tag>}<span>{segments.length} 个片段 · {duration(segments.flatMap((s) => s.shots)).toFixed(1)} 秒</span>
        {run && <Space className="drama-storyboard-record-actions" wrap>
          <Button size="small" onClick={() => setRecordView('events')}>创作动态</Button>
          <Button size="small" onClick={() => setRecordView('repairs')}>中心调度</Button>
        </Space>}
      </div>
      <Space wrap>
        {!!page?.runs.length && <Button icon={<HistoryOutlined />} disabled={!!draft || busy}
          onClick={() => setSnapshotHistoryOpen(true)}>整集快照</Button>}
        {run && <Button icon={<EditOutlined />} aria-pressed={structural} disabled={!inlineEditable} onClick={() => {
          if (!structural && !draft) setDraft(segments)
          setStructureEditing(!structural)
        }}>管理片段与镜头</Button>}
        <Button type="primary" icon={<ReloadOutlined />} loading={active} disabled={needsReview || !!draft || busy} onClick={() => start(false)}>{run ? '重新拆分' : 'AI 拆分分镜'}</Button>
      </Space>
    </div>
    {draft && <div className="drama-storyboard-draft-bar">
      <span>{structural ? '可增删、排序或调整剧本来源。再次点击「管理片段与镜头」可收起。' : '有未保存的修改。'}</span>
      <Space wrap>
        <Button disabled={busy} onClick={() => setDiscardOpen(true)}>放弃修改</Button>
        <Button type="primary" loading={busy} onClick={() => void action(async () => {
          if (!run) return
          const saved = await saveDramaStoryboard(projectId, episodeId, run, draft)
          setDraft(null)
          setStructureEditing(false)
          setParams((p) => { const next = new URLSearchParams(p); next.delete('storyboard'); const chosen = saved.segments[segments.indexOf(selected)]; if (chosen) next.set('segment', chosen.id); return next })
          messageApi.success('已保存片段修改')
        })}>保存片段修改</Button>
      </Space>
    </div>}
    {error && <Alert type="error" showIcon closable onClose={() => setError('')} title={error} />}
    {run?.stale && <Alert type="warning" showIcon title="来源剧本或关联资产已变化" description="当前分镜保留供查看，需要按最新剧本重新拆分。" />}
    {!latest && <Alert type="info" title="正在查看历史内容，此处只读。" />}
    {run && run.status !== 'ready' && run.status !== 'confirmed' && <div className={`drama-storyboard-progress ${run.status}`}><div><span>{run.status === 'failed' ? '自动修正暂未完成，已保留成果和修正记录' : run.message || run.phase}</span></div>
      {active && <Progress percent={segments.length ? Math.round(100 * segments.filter((s) => s.status === 'ready').length / segments.length) : 0} status="active" size="small" />}
      {['failed', 'partial'].includes(run.status) && <div><span>{run.error}</span>{latest && !run.stale && !needsReview && <Button danger={run.status === 'failed'} size="small" loading={busy} onClick={() => start(true)}>{run.status === 'partial' ? '继续未完成片段' : '从失败处继续'}</Button>}</div>}
    </div>}
    {!segments.length ? <div className="drama-video-storyboard-empty"><VideoCameraOutlined /><h3>{active ? '正在规划场次与片段' : '暂未拆分分镜'}</h3><p>{active ? '通过检查的片段会陆续保存并显示，可离开后回来查看。' : '将本集剧本转换成可编辑的片段和镜头。'}</p></div> : <>
      <div className="drama-storyboard-editing-layout">
        <section className={`drama-storyboard-script${structural ? ' editing' : ' reading'}`} aria-label="片段分镜内容">
          {selected && <>
            {structural ? <div className="drama-storyboard-segment-fields"><label>片段名称<Input value={selected.title} maxLength={120} onChange={(e) => changeSegment({ title: e.target.value })} /></label><label>场次<Input value={selected.scene} onChange={(e) => changeSegment({ scene: e.target.value, sceneMentions: undefined })} /></label><label>起始状态<Input.TextArea autoSize value={selected.startState} onChange={(e) => changeSegment({ startState: e.target.value })} /></label><label>结束状态<Input.TextArea autoSize value={selected.endState} onChange={(e) => changeSegment({ endState: e.target.value })} /></label><label>起始剧本行<InputNumber min={1} max={lines.length} value={selected.startLine} onChange={(n) => changeSegment({ startLine: n ?? 1 })} /></label><label>结束剧本行<InputNumber min={selected.startLine} max={lines.length} value={selected.endLine} onChange={(n) => changeSegment({ endLine: n ?? 1 })} /></label></div> : <><div className="drama-storyboard-segment-heading"><h3>片段 {segments.indexOf(selected) + 1}：{selected.title}</h3>{selected.version && <Select
                size="small" aria-label="片段版本" value={selected.version} disabled={!!draft || busy}
                options={(selected.versions ?? []).map((item) => ({ value: item.version, label: `片段版本 ${item.version}` }))}
                onChange={(version) => {
                  const item = selected.versions?.find((entry) => entry.version === version)
                  if (!item) return
                  setParams((current) => {
                    const next = new URLSearchParams(current)
                    if (item.runId === page?.runs[0]?.id) next.delete('storyboard')
                    else next.set('storyboard', item.runId)
                    next.set('segment', item.segmentId)
                    return next
                  })
                }} />}<Tooltip title="创建新版本，仅优化当前片段；其他片段及采用图片保留"><Button size="small" loading={busy} disabled={readonly || !!draft || !run || !['ready', 'confirmed', 'failed', 'partial'].includes(run.status) || selected.status !== 'ready'} onClick={optimizePacing}>优化当前片段</Button></Tooltip></div></>}
            {structural && <label className="drama-storyboard-form-picker">引用资产形象<Select mode="multiple" showSearch optionFilterProp="label" value={selected.formIds} options={catalog.map((f) => ({ value: f.id, label: `${f.assetName} · ${f.name}${f.imageUrl ? '' : '（待素材）'}` }))} onChange={changeForms} /></label>}

            {!!pacingHistory.length && <details className="drama-storyboard-pacing-history" aria-label="节奏优化记录"><summary>节奏优化记录 · {pacingHistory.length}</summary>
              {pacingHistory.map((ticket) => <article key={ticket.id}>
                <div><Tag color={ticket.status === 'resolved' ? 'success' : ticket.status === 'declined' ? 'warning' : 'processing'}>{repairLabel(ticket)}</Tag>
                  {ticket.beforeShotCount !== undefined && ticket.afterShotCount !== undefined && <span>{ticket.beforeShotCount} → {ticket.afterShotCount} 个镜头 · {ticket.beforeDuration?.toFixed(1)} → {ticket.afterDuration?.toFixed(1)} 秒</span>}</div>
                <p>{ticket.outcome || ticket.instructions}</p>
              </article>)}
            </details>}
            {selected.status !== 'ready' && <Alert type="info" title="此片段尚未完成，已通过的其他片段可正常查看。" />}
            <div className={structural ? 'drama-storyboard-editable-shots' : 'drama-storyboard-reading-sheet'}>
              {!structural && <div className="drama-storyboard-reading-context">
                <p><strong>整体背景：</strong><DramaStoryboardTextEditor label="片段场次" value={textDocument(selected.scene, selected.sceneMentions)} forms={catalog} episodeFormIds={page?.forms.map((f) => f.id) ?? []} editable={inlineEditable} placeholder="填写场次，@ 引用场景" onChange={(value) => inlineSegment('scene', value)} />；<DramaStoryboardTextEditor label="片段剧情职责" value={textDocument(selected.purpose, selected.purposeMentions)} forms={catalog} episodeFormIds={page?.forms.map((f) => f.id) ?? []} editable={inlineEditable} placeholder="填写本段剧情职责" onChange={(value) => inlineSegment('purpose', value)} /><span> · {selected.shots.length} 个分镜 · {duration(selected.shots).toFixed(1)} 秒</span></p>
                {inlineEditable && <p className="drama-storyboard-inline-hint">点击文字或秒数直接编辑，输入 @ 引用资产，点击标签可替换。修改后可保存为新版本。</p>}
              </div>}
            {selected.shots.map((shot, index) => <article key={shot.id || index} className="drama-storyboard-shot">
              {structural && <div className="drama-storyboard-shot-heading"><strong>镜头 {index + 1}</strong><Space wrap><Select {...choicePopup} aria-label={`镜头${index + 1}景别`} value={shot.shotSize} options={[...new Set([...shotSizes, shot.shotSize])].map((v) => ({ value: v, label: v }))} onChange={(shotSize) => changeShot(index, { shotSize })} /><Select {...choicePopup} aria-label={`镜头${index + 1}运镜`} value={shot.camera} options={[...new Set([...cameras, shot.camera])].map((v) => ({ value: v, label: v }))} onChange={(camera) => changeShot(index, { camera })} /><InputNumber aria-label={`镜头${index + 1}秒数`} min={0.5} max={60} step={0.5} value={shot.duration} onChange={(n) => changeShot(index, { duration: n ?? 4 })} suffix="秒" /><Button size="small" aria-label={`前移镜头${index + 1}`} disabled={index === 0} onClick={() => moveShot(index, -1)}>前移</Button><Button size="small" aria-label={`后移镜头${index + 1}`} disabled={index === selected.shots.length - 1} onClick={() => moveShot(index, 1)}>后移</Button><Button aria-label={`删除镜头${index + 1}`} danger type="text" icon={<DeleteOutlined />} disabled={selected.shots.length <= 1} onClick={() => changeSegment({ shots: selected.shots.filter((_, i) => i !== index) })} /></Space></div>}
              {structural ? <Input.TextArea aria-label={`镜头${index + 1}画面描述`} autoSize={{ minRows: 3 }} value={shot.description} onChange={(e) => changeShot(index, { description: e.target.value, descriptionMentions: undefined })} /> : <div className="drama-storyboard-shot-picture"><span className="drama-storyboard-shot-prefix"><strong>分镜{index + 1}</strong><span className="drama-storyboard-shot-time"><ClockCircleOutlined />{inlineEditable ? <InputNumber aria-label={`分镜${index + 1}时长`} value={shot.duration} min={0.5} max={60} step={0.5} controls={false} variant="borderless" onChange={(value) => { if (value !== null) changeShot(index, { duration: value }) }} /> : shot.duration} 秒</span>{inlineEditable ? <><Select {...choicePopup} className="drama-inline-choice" aria-label={`分镜${index + 1}景别`} size="small" variant="borderless" value={shot.shotSize} options={[...new Set([...shotSizes, shot.shotSize])].map((value) => ({ value, label: value }))} onChange={(shotSize) => changeShot(index, { shotSize })} /><Select {...choicePopup} className="drama-inline-choice" aria-label={`分镜${index + 1}运镜`} size="small" variant="borderless" value={shot.camera} options={[...new Set([...cameras, shot.camera])].map((value) => ({ value, label: value }))} onChange={(camera) => changeShot(index, { camera })} /></> : <span>{shot.shotSize} · {shot.camera}</span>}</span><div className="drama-storyboard-line"><span className="drama-storyboard-line-label">画面</span><div><DramaStoryboardTextEditor label={`分镜${index + 1}画面`} value={textDocument(shot.description, shot.descriptionMentions)} forms={catalog} episodeFormIds={page?.forms.map((f) => f.id) ?? []} editable={inlineEditable} onChange={(value) => inlineShot(index, 'description', value)} /></div></div></div>}
              {shot.utterances.map((u, n) => u.delivery !== 'visual' && <div key={n} className="drama-storyboard-voice-entry">
                <div className="drama-storyboard-line drama-storyboard-voice">
                  {inlineEditable && !u.sourceText ? <Select
                    {...choicePopup}
                    className="drama-inline-choice"
                    aria-label={`分镜${index + 1}声音${n + 1}类型`}
                    size="small"
                    variant="borderless"
                    value={u.kind}
                    options={Object.entries(soundLabels).map(([value, label]) => ({ value, label, title: soundDescriptions[value as keyof typeof soundDescriptions] }))}
                    onChange={(kind) => changeShot(index, { utterances: shot.utterances.map((v, j) => j === n ? { ...v, kind } : v) })}
                  /> : <span className="drama-storyboard-line-label">{soundLabels[u.kind]}</span>}
                  <div>
                    <strong className="drama-storyboard-speaker"><DramaStoryboardTextEditor label={`分镜${index + 1}声音${n + 1}说话者`} value={textDocument(u.speaker, u.speakerMentions)} forms={catalog} episodeFormIds={page?.forms.map((f) => f.id) ?? []} editable={inlineEditable && !u.sourceText} placeholder="说话者" onChange={(value) => inlineVoice(index, n, 'speaker', value)} />：</strong>
                    <DramaStoryboardTextEditor label={`分镜${index + 1}声音${n + 1}台词`} value={textDocument(u.text, u.mentions)} forms={catalog} episodeFormIds={page?.forms.map((f) => f.id) ?? []} editable={inlineEditable} placeholder="输入台词" onChange={(value) => inlineVoice(index, n, 'text', value)} />
                  </div>
                  {structural && <Button size="small" aria-label="删除台词" type="text" danger icon={<DeleteOutlined />} onClick={() => changeShot(index, { utterances: shot.utterances.filter((_, j) => j !== n) })} />}
                </div>
              </div>)}
              {structural && <Button type="link" size="small" onClick={() => changeShot(index, { utterances: [...shot.utterances, { kind: 'dialogue', speaker: '', text: '', sourceLine: shot.sourceLines[0] }] })}>添加台词</Button>}
              {structural ? <label>音效<Input value={shot.sound} onChange={(e) => changeShot(index, { sound: e.target.value, soundMentions: undefined })} /></label> : (shot.sound || inlineEditable) && <div className="drama-storyboard-line drama-storyboard-sound"><span className="drama-storyboard-line-label">音效</span><div><DramaStoryboardTextEditor label={`分镜${index + 1}音效`} value={textDocument(shot.sound, shot.soundMentions)} forms={catalog} episodeFormIds={page?.forms.map((f) => f.id) ?? []} editable={inlineEditable} placeholder="填写音效" onChange={(value) => inlineShot(index, 'sound', value)} /></div></div>}
              {structural && <details className="drama-storyboard-source"><summary>查看剧本依据 · {shot.sourceLines.map((n) => `L${n}`).join('、')}</summary><Select mode="multiple" aria-label="镜头来源行" value={shot.sourceLines} options={lines.slice(selected.startLine - 1, selected.endLine).map((text, i) => ({ value: i + selected.startLine, label: `L${i + selected.startLine} ${text.slice(0, 45)}` }))} onChange={(sourceLines) => changeShot(index, { sourceLines })} />{shot.sourceLines.map((n) => <p key={n}><b>L{n}</b> {lines[n - 1]}</p>)}</details>}
            </article>)}
            </div>
            {structural && <Space><Button icon={<PlusOutlined />} onClick={() => changeSegment({ shots: [...selected.shots, blankShot(selected.startLine)] })}>新增镜头</Button><Button onClick={() => moveSegment(-1)}>片段前移</Button><Button onClick={() => moveSegment(1)}>片段后移</Button><Button danger disabled={draft.length <= 1} onClick={() => Modal.confirm({ title: '删除当前片段？', content: '只影响本次编辑草稿，旧版本仍会保留。', onOk: () => setDraft((d) => d?.filter((s) => s.id !== selected.id) ?? null), okText: '删除', cancelText: '取消' })}>删除片段</Button></Space>}
            {!structural && <details key={selected.id} className="drama-storyboard-details"><summary>片段详情与剧本依据</summary>
              {selected.startState && <p><strong>开始状态</strong>{selected.startState}</p>}
              {selected.endState && <p><strong>结束状态</strong>{selected.endState}</p>}
              {selected.shots.map((shot, index) => <details key={shot.id || index} className="drama-storyboard-source"><summary>分镜{index + 1} · 查看剧本依据</summary>{shot.sourceLines.map((n) => <p key={n}><b>L{n}</b> {lines[n - 1]}</p>)}</details>)}
              {selected.notes && <div className="drama-storyboard-notes"><strong>创作备注</strong><p>{selected.notes}</p></div>}
            </details>}
            {structural && selected.notes && <p className="drama-storyboard-notes">{selected.notes}</p>}
          </>}
        </section>
        <aside className="drama-storyboard-preview" aria-label="片段素材预览">
          {run && selected && <DramaSegmentVideoPreview key={`${run.id}/${selected.id}`} projectId={projectId} episodeId={episodeId}
            runId={run.id} segmentId={selected.id} revision={run.revision} segmentVersion={selected.version}
            missingReferences={(selected.videoReferences ?? []).filter((ref) => !ref.imageId).map((ref) => ref.name)}
            canGenerate={!readonly && !draft && !busy && ['ready', 'confirmed'].includes(run.status) && selected.status === 'ready'} />}
          {!!selected?.videoReferences?.length && <details className="drama-storyboard-preview-assets">
            <summary>本片段参考图 · {selected.videoReferences.length}</summary>
            <div className="drama-storyboard-reference-images">
              {selected.videoReferences.map((ref) => <figure key={ref.formId}>
                {ref.imageId ? <Image src={`/api/v1/ai-drama/projects/${projectId}/images/${ref.imageId}/content`} alt={ref.name} /> : <div>待生成</div>}
                <figcaption>{ref.name}</figcaption>
              </figure>)}
            </div>
          </details>}
        </aside>
      </div>
      <nav className="drama-storyboard-timeline" aria-label="片段列表">{segments.map((s, i) => <button type="button" key={s.id} aria-current={selected?.id === s.id ? 'true' : undefined} onClick={() => chooseSegment(s.id)}><strong>{i + 1}</strong><span>{s.title}</span><small>{s.status === 'ready' ? `${s.shots.length} 镜头 · ${duration(s.shots).toFixed(1)} 秒` : '待编写'}</small></button>)}{structural && <Button icon={<PlusOutlined />} onClick={addSegment}>新增片段</Button>}</nav>
    </>}
  </>
}
