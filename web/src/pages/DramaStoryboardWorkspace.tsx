import { CheckOutlined, DeleteOutlined, EditOutlined, HistoryOutlined, PlusOutlined, ReloadOutlined, VideoCameraOutlined } from '@ant-design/icons'
import { Alert, Button, Empty, Image, Input, InputNumber, Modal, Progress, Select, Skeleton, Space, Tag, Tooltip, message } from 'antd'
import { useEffect, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useSearchParams } from 'react-router-dom'
import { confirmDramaStoryboard, getDramaStoryboards, saveDramaStoryboard, startDramaStoryboard, type DramaStoryboardSegment, type DramaStoryboardShot } from '../api/drama'

export const storyboardStatusLabels: Record<string, string> = { pending: '待拆分', queued: '等待拆分', running: '拆分中', failed: '需重试', ready: '待确认', confirmed: '已确认', stale: '来源已变化' }
const soundLabels = { dialogue: '对白', os: '内心独白', vo: '画外音' }
const shotSizes = ['大远景', '远景', '全景', '中景', '近景', '特写', '大特写', '过肩镜头', '主观镜头']
const cameras = ['固定', '缓慢推进', '拉远', '跟拍', '左摇', '右摇', '上摇', '下摇', '环绕', '升降']
const linesOf = (text: string) => text.replace(/\r\n?/g, '\n').split('\n').map((line) => line.trim()).filter(Boolean)
const blankShot = (line: number): DramaStoryboardShot => ({ id: crypto.randomUUID(), number: 1, description: '', shotSize: '中景', camera: '固定', duration: 4, sound: '', sourceLines: [line], utterances: [] })

export function DramaStoryboardWorkspace({ projectId, episodeId, needsReview }: {projectId: string; episodeId: string; needsReview: boolean}) {
  const [params, setParams] = useSearchParams()
  const runId = params.get('storyboard') || ''
  const selectedId = params.get('segment') || ''
  const client = useQueryClient()
  const [draft, setDraft] = useState<DramaStoryboardSegment[] | null>(null)
  const [busy, setBusy] = useState(false)
  const [eventsOpen, setEventsOpen] = useState(false)
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
  const duration = (shots: DramaStoryboardShot[]) => shots.reduce((sum, s) => sum + s.duration, 0)
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
    const perform = () => void action(async () => { await startDramaStoryboard(projectId, episodeId, resume); setParams((p) => { const next = new URLSearchParams(p); next.delete('storyboard'); next.delete('segment'); return next }); })
    if (!resume && run) Modal.confirm({ title: '重新拆分这一集？', content: '会从导演规划重新开始，并建立新版本。已有分镜版本会保留，不会自动生成图片或视频。', onOk: perform, okText: '重新拆分', cancelText: '取消' })
    else perform()
  }
  function changeSegment(patch: Partial<DramaStoryboardSegment>) { if (!selected) return; setDraft((current) => current?.map((s) => s.id === selected.id ? { ...s, ...patch } : s) ?? null) }
  function changeShot(index: number, patch: Partial<DramaStoryboardShot>) { if (selected) changeSegment({ shots: selected.shots.map((s, i) => i === index ? { ...s, ...patch } : s) }) }
  function moveSegment(direction: number) { if (!draft || !selected) return; const next = [...draft]; const at = next.findIndex((s) => s.id === selected.id); const target = at + direction; if (target < 0 || target >= next.length) return; [next[at], next[target]] = [next[target], next[at]]; setDraft(next) }
  function moveShot(index: number, direction: number) { if (!selected) return; const next = [...selected.shots]; const target = index + direction; if (target < 0 || target >= next.length) return; [next[index], next[target]] = [next[target], next[index]]; changeSegment({ shots: next }) }
  function addSegment() { const id = crypto.randomUUID(); const line = Math.max(1, Math.min(lines.length, (segments.at(-1)?.endLine ?? 0) + 1)); setDraft((current) => [...(current ?? []), { id, number: segments.length + 1, title: '新片段', scene: '', purpose: '', startLine: line, endLine: line, startState: '', endState: '', status: 'ready', notes: '', formIds: [], bindings: [], shots: [blankShot(line)] }]); chooseSegment(id) }
  if (query.isPending) return <Skeleton active />
  if (query.isError) return <Alert type="error" showIcon title="读取分镜失败" description={query.error.message} action={<Button onClick={() => void query.refetch()}>重试</Button>} />
  return <>
    <div className="drama-storyboard-toolbar">
      <div><h2>分镜脚本</h2>{run && <Tag color={run.status === 'failed' ? 'error' : run.status === 'confirmed' ? 'success' : 'processing'}>{storyboardStatusLabels[run.status]}</Tag>}<span>{segments.length} 个片段 · {duration(segments.flatMap((s) => s.shots)).toFixed(1)} 秒</span></div>
      <Space wrap>
        {!!page?.runs.length && <Select aria-label="分镜历史版本" prefix={<HistoryOutlined />} value={run?.id} disabled={!!draft || busy} style={{ width: 200 }} options={page.runs.map((v, i) => ({ value: v.id, label: `版本 ${v.version} · ${storyboardStatusLabels[v.status]}${i === 0 ? '（最新）' : ''}` }))} onChange={(id) => setParams((p) => { const next = new URLSearchParams(p); if (id === page.runs[0].id) next.delete('storyboard'); else next.set('storyboard', id); next.delete('segment'); return next })} />}
        {draft ? <><Button disabled={busy} onClick={() => setDraft(null)}>取消编辑</Button><Button type="primary" loading={busy} onClick={() => void action(async () => { if (run) { await saveDramaStoryboard(projectId, episodeId, run, draft); setDraft(null); message.success('已保存为新版本') } })}>保存新版本</Button></> : <>
          {run && <Button icon={<EditOutlined />} disabled={readonly || !segments.length || busy || segments.some((s) => s.status !== 'ready')} onClick={() => setDraft(structuredClone(segments))}>编辑分镜</Button>}
          {run?.status === 'ready' && <Button icon={<CheckOutlined />} disabled={readonly} loading={busy} onClick={() => void action(async () => { await confirmDramaStoryboard(projectId, episodeId, run); message.success('分镜已确认') })}>确认分镜</Button>}
          <Button type="primary" icon={<ReloadOutlined />} loading={busy || active} disabled={needsReview || !!draft} onClick={() => start(false)}>{run ? '重新拆分' : 'AI 拆分分镜'}</Button>
        </>}
      </Space>
    </div>
    {draft && <Alert type="info" title="保存会建立新版本；缺图引用可采用当前已生成图片，已采用的图片版本保持原样。" />}
    {error && <Alert type="error" showIcon closable onClose={() => setError('')} title={error} />}
    {run?.stale && <Alert type="warning" showIcon title="来源剧本或关联资产已变化" description="当前分镜保留供查看，需要按最新剧本重新拆分。" />}
    {!latest && <Alert type="info" title="正在查看历史版本，内容只读；最新版本和失败前成果均会保留。" />}
    {run && <div className={`drama-storyboard-progress ${run.status}`}><div><span>{run.message || run.phase}</span><Button type="link" size="small" onClick={() => setEventsOpen((v) => !v)}>{eventsOpen ? '收起动态' : '创作动态'}</Button></div>
      {active && <Progress percent={segments.length ? Math.round(100 * segments.filter((s) => s.status === 'ready').length / segments.length) : 0} status="active" size="small" />}
      {run.status === 'failed' && <div><span>{run.error}</span>{latest && !run.stale && !needsReview && <Button danger size="small" loading={busy} onClick={() => start(true)}>从失败处继续</Button>}</div>}
      {eventsOpen && <ol className="drama-storyboard-events">{run.events.map((event) => <li key={event.id}><time>{new Date(event.createdAt).toLocaleTimeString()}</time>{event.message}</li>)}</ol>}
    </div>}
    {!segments.length ? <div className="drama-video-storyboard-empty"><VideoCameraOutlined /><h3>{active ? '正在规划场次与片段' : '暂未拆分分镜'}</h3><p>{active ? '通过检查的片段会陆续保存并显示，可离开后回来查看。' : '将本集剧本转换成可编辑的片段和镜头。'}</p></div> : <>
      <div className="drama-storyboard-editing-layout">
        <section className="drama-storyboard-script" aria-label="片段分镜内容">
          {selected && <>
            {draft ? <div className="drama-storyboard-segment-fields"><label>片段名称<Input value={selected.title} maxLength={120} onChange={(e) => changeSegment({ title: e.target.value })} /></label><label>场次<Input value={selected.scene} onChange={(e) => changeSegment({ scene: e.target.value })} /></label><label>起始状态<Input.TextArea autoSize value={selected.startState} onChange={(e) => changeSegment({ startState: e.target.value })} /></label><label>结束状态<Input.TextArea autoSize value={selected.endState} onChange={(e) => changeSegment({ endState: e.target.value })} /></label><label>起始剧本行<InputNumber min={1} max={lines.length} value={selected.startLine} onChange={(n) => changeSegment({ startLine: n ?? 1 })} /></label><label>结束剧本行<InputNumber min={selected.startLine} max={lines.length} value={selected.endLine} onChange={(n) => changeSegment({ endLine: n ?? 1 })} /></label></div> : <><h3>片段 {segments.indexOf(selected) + 1}：{selected.title}</h3><p className="drama-storyboard-scene">{selected.scene} · {duration(selected.shots).toFixed(1)} 秒</p><p>{selected.purpose}</p>{(selected.startState || selected.endState) && <div className="drama-storyboard-continuity"><span>开始：{selected.startState}</span><span>结束：{selected.endState}</span></div>}</>}
            <label className="drama-storyboard-form-picker">引用资产形象{draft ? <Select mode="multiple" showSearch optionFilterProp="label" value={selected.formIds} options={page?.forms.map((f) => ({ value: f.id, label: `${f.assetName} · ${f.name}${f.imageUrl ? '' : '（待素材）'}` }))} onChange={(formIds) => changeSegment({ formIds })} /> : <div>{selected.formIds.length ? selected.formIds.map((id) => { const f = page?.forms.find((item) => item.id === id); const binding = selected.bindings.find((b) => b.formId === id); return <Tag key={id}>{f ? `${f.assetName} · ${f.name}` : '历史形象'}{!binding?.imageId ? ' · 待素材' : ''}</Tag> }) : <span>无引用资产</span>}</div>}</label>
            {selected.status !== 'ready' && <Alert type="info" title="此片段尚未完成，已通过的其他片段可正常查看。" />}
            {selected.shots.map((shot, index) => <article key={shot.id || index} className="drama-storyboard-shot">
              <div className="drama-storyboard-shot-heading"><strong>镜头 {index + 1}</strong>{draft ? <Space wrap><Select aria-label={`镜头${index + 1}景别`} value={shot.shotSize} options={[...new Set([...shotSizes, shot.shotSize])].map((v) => ({ value: v, label: v }))} onChange={(shotSize) => changeShot(index, { shotSize })} /><Select aria-label={`镜头${index + 1}运镜`} value={shot.camera} options={[...new Set([...cameras, shot.camera])].map((v) => ({ value: v, label: v }))} onChange={(camera) => changeShot(index, { camera })} /><InputNumber aria-label={`镜头${index + 1}秒数`} min={0.5} max={60} step={0.5} value={shot.duration} onChange={(n) => changeShot(index, { duration: n ?? 4 })} suffix="秒" /><Button size="small" aria-label={`前移镜头${index + 1}`} disabled={index === 0} onClick={() => moveShot(index, -1)}>前移</Button><Button size="small" aria-label={`后移镜头${index + 1}`} disabled={index === selected.shots.length - 1} onClick={() => moveShot(index, 1)}>后移</Button><Button aria-label={`删除镜头${index + 1}`} danger type="text" icon={<DeleteOutlined />} disabled={selected.shots.length <= 1} onClick={() => changeSegment({ shots: selected.shots.filter((_, i) => i !== index) })} /></Space> : <span>{shot.shotSize} · {shot.camera} · {shot.duration} 秒</span>}</div>
              {draft ? <Input.TextArea aria-label={`镜头${index + 1}画面描述`} autoSize={{ minRows: 3 }} value={shot.description} onChange={(e) => changeShot(index, { description: e.target.value })} /> : <p className="drama-storyboard-shot-picture">{shot.description}</p>}
              {shot.utterances.map((u, n) => <div key={n} className="drama-storyboard-utterance">{draft ? <><Select aria-label="声音类型" value={u.kind} options={Object.entries(soundLabels).map(([value, label]) => ({ value, label }))} onChange={(kind) => changeShot(index, { utterances: shot.utterances.map((v, j) => j === n ? { ...v, kind } : v) })} /><Input aria-label="说话者" placeholder="说话者" value={u.speaker} onChange={(e) => changeShot(index, { utterances: shot.utterances.map((v, j) => j === n ? { ...v, speaker: e.target.value } : v) })} /><Input.TextArea aria-label="台词内容" autoSize value={u.text} onChange={(e) => changeShot(index, { utterances: shot.utterances.map((v, j) => j === n ? { ...v, text: e.target.value } : v) })} /><Button aria-label="删除台词" type="text" danger icon={<DeleteOutlined />} onClick={() => changeShot(index, { utterances: shot.utterances.filter((_, j) => j !== n) })} /></> : <><Tag>{soundLabels[u.kind]}</Tag><strong>{u.speaker}</strong><span>{u.text}</span></>}</div>)}
              {draft && <Button type="link" size="small" onClick={() => changeShot(index, { utterances: [...shot.utterances, { kind: 'dialogue', speaker: '', text: '', sourceLine: shot.sourceLines[0] }] })}>添加台词</Button>}
              {draft ? <label>音效<Input value={shot.sound} onChange={(e) => changeShot(index, { sound: e.target.value })} /></label> : shot.sound && <p className="drama-storyboard-sound">音效：{shot.sound}</p>}
              <details className="drama-storyboard-source"><summary>查看剧本依据 · {shot.sourceLines.map((n) => `L${n}`).join('、')}</summary>{draft && <Select mode="multiple" aria-label="镜头来源行" value={shot.sourceLines} options={lines.slice(selected.startLine - 1, selected.endLine).map((text, i) => ({ value: i + selected.startLine, label: `L${i + selected.startLine} ${text.slice(0, 45)}` }))} onChange={(sourceLines) => changeShot(index, { sourceLines })} />}{shot.sourceLines.map((n) => <p key={n}><b>L{n}</b> {lines[n - 1]}</p>)}</details>
            </article>)}
            {draft && <Space><Button icon={<PlusOutlined />} onClick={() => changeSegment({ shots: [...selected.shots, blankShot(selected.startLine)] })}>新增镜头</Button><Button onClick={() => moveSegment(-1)}>片段前移</Button><Button onClick={() => moveSegment(1)}>片段后移</Button><Button danger disabled={draft.length <= 1} onClick={() => Modal.confirm({ title: '删除当前片段？', content: '只影响本次编辑草稿，旧版本仍会保留。', onOk: () => setDraft((d) => d?.filter((s) => s.id !== selected.id) ?? null), okText: '删除', cancelText: '取消' })}>删除片段</Button></Space>}
            {selected.notes && <p className="drama-storyboard-notes">{selected.notes}</p>}
          </>}
        </section>
        <aside className="drama-storyboard-preview" aria-label="片段素材预览"><div className="drama-storyboard-preview-heading"><h3>片段预览</h3><Tooltip title="视频生成将在后续接入；当前步骤只拆分文字分镜"><Button disabled>生成视频</Button></Tooltip></div><Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="当前片段视频尚未生成" /><p>规划时长需在视频生成接入后按实际模型能力校验。</p><div className="drama-storyboard-reference-images">{selected?.formIds.map((id) => { const form = page?.forms.find((f) => f.id === id); const imageId = selected.bindings.find((b) => b.formId === id)?.imageId; return <figure key={id}>{imageId ? <Image src={`/api/v1/ai-drama/projects/${projectId}/images/${imageId}/content`} alt={form?.assetName || '采用的参考图'} /> : <div>待素材</div>}<figcaption>{form?.assetName} · {form?.name}</figcaption></figure> })}</div></aside>
      </div>
      <nav className="drama-storyboard-timeline" aria-label="片段列表">{segments.map((s, i) => <button type="button" key={s.id} aria-current={selected?.id === s.id ? 'true' : undefined} onClick={() => chooseSegment(s.id)}><strong>{i + 1}</strong><span>{s.title}</span><small>{s.status === 'ready' ? `${s.shots.length} 镜头 · ${duration(s.shots).toFixed(1)} 秒` : '待编写'}</small></button>)}{draft && <Button icon={<PlusOutlined />} onClick={addSegment}>新增片段</Button>}</nav>
    </>}
  </>
}
