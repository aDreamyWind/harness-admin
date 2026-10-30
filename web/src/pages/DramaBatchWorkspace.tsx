import { ArrowLeftOutlined, ExclamationCircleFilled, FileSearchOutlined, PlusOutlined } from '@ant-design/icons'
import { Alert, Button, Card, Empty, Input, Modal, Select, Tag, Tooltip, message } from 'antd'
import { useEffect, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'

import {
  createDramaBatch, listDramaBatches, suggestDramaBatch, updateDramaBatchRange, renameDramaBatch, analyzeDramaBatch, resumeDramaBatch, getDramaBatchResult, getDramaBatchProgress, listDramaAssets, addDramaEpisodeAsset, removeDramaEpisodeAsset,
  type DramaBatch, type DramaBatchMode, type DramaProject, type DramaBatchResult, type DramaBatchProgress, type DramaBatchEvent, type DramaAssetKind, type DramaCatalogAsset,
} from '../api/drama'
import { DramaSourceReader } from './DramaSourceReader'

interface Props {
  project: DramaProject
  batches: DramaBatch[]
  onBatchesChange: (value: DramaBatch[]) => void
}

const boundaryNames: Record<DramaBatch['boundary'], string> = {
  episode: '完整分集', chapter: '章节结尾', paragraph: '段落结尾', limit: '10 万字上限', end: '原文末尾', manual: '手动标记',
}
const statusNames: Record<DramaBatch['status'], string> = { pending: '待解析', running: '解析中', failed: '解析失败', stale: '范围需核对', ready: '已解析' }
const stageNames: Record<string, string> = { art: '美术指导', story: '剧情整理', plan: '分集规划', name: '批次命名', context: '本批上下文', director: '导演', review: '剧本审校', repair: '局部修正' }
const assetSections = [{ key: 'character', field: 'characters', title: '角色' }, { key: 'scene', field: 'scenes', title: '场景' }, { key: 'prop', field: 'props', title: '道具' }] as const

export function DramaBatchWorkspace({ project, batches, onBatchesChange }: Props) {
  const [messageApi, contextHolder] = message.useMessage()
  const [searchParams, setSearchParams] = useSearchParams()
  const [result, setResult] = useState<DramaBatchResult | null>(null)
  const [progress, setProgress] = useState<DramaBatchProgress | null>(null)
  const [analyzing, setAnalyzing] = useState(false)
  const [editingName, setEditingName] = useState(false)
  const [nameDraft, setNameDraft] = useState('')
  const [nameSaving, setNameSaving] = useState(false)
  const renameInFlight = useRef(false)
  const cancelledRename = useRef(false)
  const [events, setEvents] = useState<DramaBatchEvent[]>([])
  const [liveScript, setLiveScript] = useState('')
  const [streamHint, setStreamHint] = useState('')
  const [now, setNow] = useState(Date.now())
  const batchesChangeRef = useRef(onBatchesChange)
  useEffect(() => { batchesChangeRef.current = onBatchesChange }, [onBatchesChange])
  const [assetPicker, setAssetPicker] = useState<{ episodeId: string; kind: DramaAssetKind } | null>(null)
  const [assetQuery, setAssetQuery] = useState('')
  const [assetOptions, setAssetOptions] = useState<DramaCatalogAsset[]>([])
  const [pickedAssets, setPickedAssets] = useState<string[]>([])
  const [savingAssets, setSavingAssets] = useState(false)
  const [choiceOpen, setChoiceOpen] = useState(false)
  const [mode, setMode] = useState<DramaBatchMode>('beginning')
  const [previousBatchId, setPreviousBatchId] = useState<string>()
  const [readerMode, setReaderMode] = useState<'create' | 'view' | null>(null)
  const [locked, setLocked] = useState(true)
  const [startOffset, setStartOffset] = useState<number | null>(null)
  const [endOffset, setEndOffset] = useState<number | null>(null)
  const [manualEnd, setManualEnd] = useState(false)
  const [saving, setSaving] = useState(false)
  const [opening, setOpening] = useState(false)
  const requestedStart = useRef<number | null>(null)
  const requestedBatchId = searchParams.get('batch')
  const selected = batches.find((item) => item.id === requestedBatchId) ?? null
  const selectedId = selected?.id ?? null
  const pickerField = assetSections.find((item) => item.key === assetPicker?.kind)?.field
  const pickerEpisode = result?.episodes.find((item) => item.id === assetPicker?.episodeId)
  const linkedAssetIds = new Set(pickerField && pickerEpisode ? pickerEpisode[pickerField].map((asset) => asset.assetId) : [])
  useEffect(() => { setEditingName(false) }, [selectedId])
  function beginRename() {
    if (!selected || nameSaving) return
    cancelledRename.current = false
    setNameDraft(selected.name || '未命名批次')
    setEditingName(true)
  }
  async function saveBatchName() {
    if (!selected || !editingName || cancelledRename.current || renameInFlight.current) return
    const name = nameDraft.trim()
    if (!name) { messageApi.warning('批次名称不能为空'); setEditingName(false); return }
    if (name === selected.name) { setEditingName(false); return }
    renameInFlight.current = true; setNameSaving(true)
    try {
      const updated = await renameDramaBatch(project.id, selected.id, name)
      onBatchesChange(batches.map((item) => item.id === updated.id ? updated : item))
      setEditingName(false); messageApi.success('批次名称已更新')
    } catch (error) { messageApi.error(error instanceof Error ? error.message : '修改批次名称失败') }
    finally { renameInFlight.current = false; setNameSaving(false) }
  }
  function selectBatch(value: string | null) {
    setSearchParams((current) => {
      const next = new URLSearchParams(current)
      if (value) next.set('batch', value)
      else next.delete('batch')
      return next
    })
  }
  useEffect(() => {
    if (!selectedId) { setResult(null); return }
    let active = true
    const refresh = async () => {
      try {
        const value = await getDramaBatchResult(project.id, selectedId)
        if (active) setResult(value)
      } catch (error) { if (active) messageApi.error(error instanceof Error ? error.message : '读取解析结果失败') }
    }
    void refresh()
    return () => { active = false }
  }, [project.id, selectedId, selected?.status, messageApi])
  useEffect(() => {
    if (!selectedId) { setProgress(null); return }
    let active = true
    const refresh = () => { void getDramaBatchProgress(project.id, selectedId).then((value) => {
      if (active) setProgress(value)
    }).catch((error) => { if (active) messageApi.error(error instanceof Error ? error.message : '读取解析进度失败') }) }
    refresh()
    const timer = selected?.status === 'running' ? window.setInterval(refresh, 15000) : null
    return () => { active = false; if (timer !== null) window.clearInterval(timer) }
  }, [project.id, selectedId, selected?.status, messageApi])
  useEffect(() => {
    if (!selectedId || selected?.status !== 'running') return
    const timer = window.setInterval(async () => {
      try { onBatchesChange(await listDramaBatches(project.id)) } catch { /* 下一轮继续查询 */ }
    }, 3000)
    return () => window.clearInterval(timer)
  }, [project.id, selectedId, selected?.status, onBatchesChange])
  useEffect(() => {
    if (!assetPicker) return
    let active = true
    const timer = window.setTimeout(() => { void listDramaAssets(project.id, assetPicker.kind, assetQuery).then((items) => { if (active) setAssetOptions(items) }).catch((error) => { if (active) messageApi.error(error instanceof Error ? error.message : '搜索项目资产失败') }) }, 180)
    return () => { active = false; window.clearTimeout(timer) }
  }, [project.id, assetPicker, assetQuery, messageApi])
  useEffect(() => {
    if (selected?.status !== 'running') return
    const timer = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(timer)
  }, [selected?.status])
  useEffect(() => {
    setEvents([]); setLiveScript(''); setStreamHint('')
    if (!selectedId) return
    let active = true
    const stream = new EventSource(`/api/v1/ai-drama/projects/${project.id}/batches/${selectedId}/events`)
    const refresh = () => { void getDramaBatchProgress(project.id, selectedId).then((value) => { if (active) setProgress(value) }).catch((error) => { if (active) setStreamHint(error instanceof Error ? error.message : '读取进度失败') }) }
    stream.addEventListener('progress', (raw) => {
      if (!active) return
      let event: DramaBatchEvent
      try { event = JSON.parse((raw as MessageEvent).data) as DramaBatchEvent } catch { setStreamHint('进度消息格式错误，请刷新重试'); return }
      setStreamHint('')
      if (event.kind === 'delta') { setLiveScript((text) => (text + event.message).slice(-60000)); return }
      if (event.kind === 'started' && event.stage === 'director') setLiveScript('')
      setEvents((items) => [...items.filter((item) => item.id !== event.id && !(event.kind === 'receiving' && item.kind === 'receiving' && item.stage === event.stage && item.stepNumber === event.stepNumber)), event].slice(-40))
      if (['completed', 'failed', 'repair'].includes(event.kind)) refresh()
    })
    stream.addEventListener('batch-state', () => {
      stream.close(); refresh()
      void listDramaBatches(project.id).then((items) => { if (active) batchesChangeRef.current(items) }).catch((error) => { if (active) setStreamHint(error instanceof Error ? error.message : '读取批次状态失败') })
    })
    stream.addEventListener('stream-error', (raw) => { if (active) setStreamHint(String((raw as MessageEvent).data || '读取实时进度失败')) })
    stream.onopen = () => { if (active) setStreamHint('') }
    stream.onerror = () => { if (active && stream.readyState !== EventSource.CLOSED) setStreamHint('实时连接暂时中断，正在恢复…') }
    return () => { active = false; stream.close() }
  }, [project.id, selectedId, selected?.status])
  const requestedScriptEpisode = searchParams.get('scriptEpisode')
  useEffect(() => {
    if (!requestedScriptEpisode || !result?.episodes.some((episode) => episode.id === requestedScriptEpisode)) return
    const frame = window.requestAnimationFrame(() => {
      const target = document.getElementById(`drama-script-episode-${requestedScriptEpisode}`) as HTMLDetailsElement | null
      if (target) { target.open = true; target.scrollIntoView({ block: 'start' }) }
    })
    return () => window.cancelAnimationFrame(frame)
  }, [requestedScriptEpisode, result])
  async function startAnalysis(mode: 'restart' | 'resume' = 'restart') {
    if (!selected) return
    setAnalyzing(true)
    try {
      if (mode === 'resume') await resumeDramaBatch(project.id, selected.id)
      else await analyzeDramaBatch(project.id, selected.id)
      setEvents([]); setLiveScript(''); setProgress(null)
      onBatchesChange(await listDramaBatches(project.id))
      messageApi.success(mode === 'resume' ? '已从未完成的步骤继续解析' : '已从头开始解析本批剧本')
    } catch (error) { messageApi.error(error instanceof Error ? error.message : '无法启动解析') }
    finally { setAnalyzing(false) }
  }
  function openAssetPicker(episodeId: string, kind: DramaAssetKind) {
    setAssetQuery(''); setAssetOptions([]); setPickedAssets([]); setAssetPicker({ episodeId, kind })
  }
  async function saveEpisodeAssets() {
    if (!assetPicker || pickedAssets.length === 0) return
    setSavingAssets(true)
    try {
      for (const assetId of pickedAssets.filter((id) => !linkedAssetIds.has(id))) await addDramaEpisodeAsset(project.id, assetPicker.episodeId, assetId)
      if (selectedId) setResult(await getDramaBatchResult(project.id, selectedId))
      setAssetPicker(null); messageApi.success('已关联到本集')
    } catch (error) { messageApi.error(error instanceof Error ? error.message : '保存资产关联失败') }
    finally { setSavingAssets(false) }
  }
  async function removeAsset(episodeId: string, linkId: string) {
    try {
      await removeDramaEpisodeAsset(project.id, episodeId, linkId)
      if (selectedId) setResult(await getDramaBatchResult(project.id, selectedId))
    } catch (error) { messageApi.error(error instanceof Error ? error.message : '移除关联失败') }
  }

  async function openCreateReader() {
    setOpening(true)
    try {
      if (mode === 'manual') {
        setStartOffset(null)
        setEndOffset(null)
      } else {
        const suggestion = await suggestDramaBatch(project.id, { mode, previousBatchId })
        setStartOffset(suggestion.startOffset)
        setEndOffset(suggestion.endOffset)
      }
      setManualEnd(false)
      setLocked(false)
      setChoiceOpen(false)
      setReaderMode('create')
    } catch (error) {
      messageApi.error(error instanceof Error ? error.message : '无法计算批次范围')
    } finally {
      setOpening(false)
    }
  }
  function openExistingReader(batch: DramaBatch) {
    selectBatch(batch.id)
    setStartOffset(batch.startOffset)
    setEndOffset(batch.endOffset)
    setLocked(true)
    setReaderMode('view')
  }
  async function changeStart(value: number) {
    setStartOffset(value)
    setEndOffset(null)
    if (readerMode === 'create') {
      setMode('manual')
      setPreviousBatchId(undefined)
    }
    requestedStart.current = value
    try {
      const suggestion = await suggestDramaBatch(project.id, { mode: 'manual', startOffset: value })
      if (requestedStart.current === value) {
        setEndOffset(suggestion.endOffset)
        setManualEnd(false)
      }
    } catch (error) {
      if (requestedStart.current === value) messageApi.error(error instanceof Error ? error.message : '无法计算建议终点')
    }
  }
  async function saveRange() {
    if (startOffset === null || endOffset === null || endOffset <= startOffset || endOffset - startOffset > 100000) return
    if (readerMode === 'view' && selected && selected.startOffset === startOffset && selected.endOffset === endOffset) {
      setReaderMode(null)
      setLocked(true)
      return
    }
    setSaving(true)
    try {
      if (readerMode === 'create') {
        const item = await createDramaBatch(project.id, {
          mode, previousBatchId: mode === 'previous' ? previousBatchId : undefined,
          startOffset: mode === 'manual' ? startOffset : undefined,
          endOffset: manualEnd ? endOffset : undefined,
        })
        onBatchesChange(await listDramaBatches(project.id))
        selectBatch(item.id)
        messageApi.success(`第 ${item.number} 批已创建`)
      } else if (selected) {
        const item = await updateDramaBatchRange(project.id, selected.id, { startOffset, endOffset, revision: selected.revision })
        onBatchesChange(await listDramaBatches(project.id))
        messageApi.success('批次范围已保存，关联批次请重新核对')
      }
      setReaderMode(null)
      setLocked(true)
    } catch (error) {
      messageApi.error(error instanceof Error ? error.message : '保存批次范围失败')
    } finally {
      setSaving(false)
    }
  }
  function unlock() {
    Modal.confirm({
      title: '解锁批次范围',
      icon: <ExclamationCircleFilled style={{ color: '#d55343' }} />,
      content: '调整开头或结尾可能影响已经解析的剧本、视频和承接此批次的后续批次。原文不会被修改。',
      okText: '解锁修改', cancelText: '保持锁定',
      onOk: () => setLocked(false),
    })
  }

  return <section className="drama-batches" aria-label="剧本分集批次">
    {contextHolder}
    {!selected ? <>
      <div className="drama-section-head drama-batches-head"><div><h2>剧本分集</h2><p>按原文范围建立批次，确认每一批的开头和结尾。</p></div><Tooltip title={project.status !== 'ready' ? '请先完成剧本概览解析' : ''}><span><Button type="primary" icon={<PlusOutlined />} disabled={project.status !== 'ready'} onClick={() => { setMode('beginning'); setPreviousBatchId(undefined); setChoiceOpen(true) }}>新建批次</Button></span></Tooltip></div>
      {batches.length === 0 ? <div className="drama-empty"><Empty description="还没有批次，点击右上角新建批次" /></div> : <div className="drama-batch-grid">{batches.map((batch) => <button type="button" key={batch.id} className="drama-batch-card" onClick={() => selectBatch(batch.id)}>
        <div><span>第 {batch.number} 批</span><Tag color={batch.status === 'stale' ? 'warning' : batch.status === 'ready' ? 'success' : batch.status === 'failed' ? 'error' : batch.status === 'running' ? 'processing' : 'default'}>{statusNames[batch.status]}</Tag></div>
        <strong>{batch.name || '未命名批次'}</strong>
        <p>{(batch.endOffset - batch.startOffset).toLocaleString()} 字符</p>
      </button>)}</div>}
    </> : <>
      <div className="drama-batch-detail-head"><Button type="text" icon={<ArrowLeftOutlined />} onClick={() => selectBatch(null)}>返回批次</Button><Tag color={selected.status === 'stale' ? 'warning' : selected.status === 'ready' ? 'success' : selected.status === 'failed' ? 'error' : selected.status === 'running' ? 'processing' : 'default'}>{statusNames[selected.status]}</Tag></div>
      <div className="drama-batch-detail-title"><div>{editingName ? <Input className="drama-batch-name-input" aria-label="批次名称" autoFocus maxLength={80} disabled={nameSaving} value={nameDraft} onFocus={(event) => event.target.select()} onChange={(event) => setNameDraft(event.target.value)} onPressEnter={() => void saveBatchName()} onBlur={() => void saveBatchName()} onKeyDown={(event) => { if (event.key === 'Escape') { cancelledRename.current = true; setEditingName(false) } }} /> : <h2 className="drama-batch-name" title="双击修改批次名称" tabIndex={0} role="button" aria-label={`批次名称：${selected.name || '未命名批次'}，双击修改`} onDoubleClick={beginRename} onKeyDown={(event) => { if (event.key === 'Enter') { event.preventDefault(); beginRename() } }}>{selected.name || '未命名批次'}</h2>}<p>原文 {(selected.startOffset + 1).toLocaleString()}—{selected.endOffset.toLocaleString()} 字符 · 共 {(selected.endOffset - selected.startOffset).toLocaleString()} 字符 · {boundaryNames[selected.boundary]}</p></div><div className="drama-batch-detail-actions"><Button icon={<FileSearchOutlined />} onClick={() => openExistingReader(selected)}>查看在剧本中的位置</Button><Button type="primary" loading={analyzing || selected.status === 'running'} disabled={selected.status === 'running' || project.status !== 'ready'} onClick={() => void startAnalysis()}>{selected.status === 'running' ? '解析中' : selected.analysisRevision > 0 || selected.status === 'ready' ? '重新解析' : 'AI解析'}</Button></div></div>
      {selected.status === 'stale' && <Alert type="warning" showIcon message="上游批次范围已调整，请核对本批次起止位置" />}
      {selected.status === 'failed' && <Alert type="error" showIcon message="本批解析失败" description={selected.errorMessage?.includes('文本模型响应超时') ? '模型响应超时，已完成的阶段成果已保留。可从失败步骤继续。' : selected.errorMessage || '请重试'} />}
      {selected.status === 'running' && <Alert type="info" showIcon message="正在逐段解析本批原文，已完成的阶段成果会显示在下方" />}
      {(events.length > 0 || streamHint || liveScript) && <Card className="drama-batch-live">
        <h3>创作动态</h3>
        {streamHint && <p className="drama-stream-hint">{streamHint}</p>}
        <div className="drama-batch-events">{events.slice(-10).map((event) => <p key={event.id} className={event.kind}><time>{new Date(event.createdAt).toLocaleTimeString('zh-CN', { hour12: false })}</time><span>{event.message}</span></p>)}</div>
        {liveScript && selected.status !== 'ready' && <details className="drama-live-script" open><summary>正在生成的剧本 · 尚待校验</summary><pre>{liveScript}</pre></details>}
      </Card>}
      {progress && progress.steps.length > 0 && <Card className="drama-batch-progress" aria-live="polite">
        <h3>解析进度</h3>
        <div className="drama-batch-progress-counts">
          {Object.entries(stageNames).filter(([stage]) => progress.steps.some((step) => step.stage === stage)).map(([stage, name]) => <span key={stage}>{name} {progress.steps.filter((step) => step.stage === stage && step.status === 'completed').length}/{progress.steps.find((step) => step.stage === stage)?.chunkTotal} {stage === 'director' || stage === 'review' || stage === 'repair' ? '集' : stage === 'plan' || stage === 'name' || stage === 'context' ? '项' : '段'}</span>)}
        </div>
        <ol className="drama-batch-step-list">{progress.steps.map((step) => <li key={`${step.stage}-${step.chunkNumber}`} className={step.status}>
          <span>{stageNames[step.stage]} · 第 {step.chunkNumber}/{step.chunkTotal} {step.stage === 'director' || step.stage === 'review' || step.stage === 'repair' ? '集' : step.stage === 'plan' || step.stage === 'name' || step.stage === 'context' ? '项' : '段'}</span>
          <strong>{step.status === 'completed' ? '已完成' : step.status === 'failed' ? '需重试' : '进行中'}</strong>
          {step.status === 'completed' && <small>{Math.round(step.durationMs / 1000)} 秒</small>}
          {step.status === 'running' && <small>已用 {Math.max(0, Math.round((now - new Date(step.startedAt).getTime()) / 1000))} 秒</small>}
          {step.status === 'failed' && selected.status === 'failed' && step === progress.steps.find((item) => item.status === 'failed') && <Button size="small" loading={analyzing} disabled={selected.revision !== selected.analysisRevision || project.status !== 'ready'} onClick={() => void startAnalysis('resume')}>从此处继续解析</Button>}
          {step.errorMessage && <small className="drama-step-error" title={step.errorMessage}>{step.errorMessage}</small>}
        </li>)}</ol>
        {selected.status === 'failed' && !progress.steps.some((step) => step.status === 'failed') && <Button className="drama-resume-fallback" loading={analyzing} disabled={selected.revision !== selected.analysisRevision || project.status !== 'ready'} onClick={() => void startAnalysis('resume')}>从未完成处继续解析</Button>}
        {progress.context && <details className="drama-grounded-context"><summary>本批上下文 · 原文 {(progress.context.startOffset + 1).toLocaleString()}—{progress.context.endOffset.toLocaleString()} 字符</summary><p>{progress.context.summary}</p>{progress.context.background && <p><strong>背景：</strong>{progress.context.background}</p>}{progress.context.setting && <p><strong>设定：</strong>{progress.context.setting}</p>}<p><strong>承接：</strong>{progress.context.continuity || '没有可确认的承接信息'}</p><small>{progress.context.precedingEnd > progress.context.precedingStart ? `承接仅参考原文 ${(progress.context.precedingStart + 1).toLocaleString()}—${progress.context.precedingEnd.toLocaleString()} 字符，未读范围不作推断。` : '本批从原文开头开始。'}</small></details>}
        {!!progress.reviewIssues?.length && <details className="drama-review-history" open><summary>累计审校问题 · {progress.reviewIssues.length} 项</summary>{progress.reviewIssues.map((issue) => <article key={issue.id} className={`drama-review-issue ${issue.status} ${issue.severity}`}><div><strong>第 {issue.episodeNumber} 集</strong><Tag color={issue.status === 'resolved' ? 'success' : issue.severity === 'suggestion' ? 'default' : 'error'}>{issue.status === 'resolved' ? '已修正' : issue.severity === 'suggestion' ? '建议，不阻断' : '待修正'}</Tag></div><p>{issue.message}</p>{issue.sourceQuote && <blockquote>原文依据（{(issue.sourceStart + 1).toLocaleString()}—{issue.sourceEnd.toLocaleString()}）：{issue.sourceQuote}</blockquote>}{issue.scriptQuote && <small>剧本相关文字：{issue.scriptQuote}</small>}</article>)}</details>}
        {progress.plans?.length > 0 && <div className="drama-batch-plan"><h4>整批分集规划 · {progress.plans.length} 集</h4>{progress.plans.map((plan, index) => <details key={index}><summary>第 {index + 1} 集：{plan.title}</summary><p>{plan.synopsis}</p>{plan.hook && <p>结尾依据：{plan.hook}</p>}{!!plan.sources?.length && <details className="drama-plan-evidence"><summary>查看原文依据（{plan.sources.length}）</summary>{plan.sources.map((fact) => <blockquote key={fact.id}><span>原文 {(fact.startOffset + 1).toLocaleString()}—{fact.endOffset.toLocaleString()}</span><p>{fact.quote}</p></blockquote>)}</details>}</details>)}</div>}
        {selected.status !== 'ready' && (progress.assets.characters.length + progress.assets.scenes.length + progress.assets.props.length > 0 || progress.episodes.length > 0) && <div className="drama-batch-progress-preview">
          <h4>已完成的阶段成果</h4>
          {assetSections.map((group) => <div key={group.key} className="drama-batch-preview-group"><strong>{group.title}（{progress.assets[group.field].length}）</strong><div>{progress.assets[group.field].map((asset, index) => <Tag key={`${asset.name}-${index}`} title={asset.description}>{asset.name}</Tag>)}</div></div>)}
          {progress.episodes.map((episode, index) => <details key={`${episode.title}-${index}`} className="drama-batch-preview-episode"><summary>已生成第 {index + 1} 集：{episode.title}</summary><p>{episode.synopsis}</p><pre>{episode.scriptText}</pre></details>)}
          <p className="drama-batch-preview-note">阶段成果已保存；本批全部完成后才会加入正式资产和剧集。</p>
        </div>}
      </Card>}
      {result?.episodes.length ? <>
      <Card className="drama-batch-result"><details className="drama-asset-section" open><summary>角色、场景、道具</summary>
        {result && assetSections.map((section) => <details className="drama-batch-asset-group" key={section.key} open><summary>{section.title}列表（{result.assets[section.field].length}）</summary>
          {result.assets[section.field].length ? <div className="drama-batch-asset-list">{result.assets[section.field].map((asset) => <div key={asset.id} className="drama-batch-asset-row"><strong>{asset.name}</strong><div className="drama-batch-asset-copy"><span>{asset.description}</span>{section.key === 'character' && asset.appearanceDescription && <small>外貌：{asset.appearanceDescription}</small>}{section.key === 'character' && asset.voiceDescription && <small>音色：{asset.voiceDescription}</small>}</div></div>)}</div> : <p className="drama-batch-muted">暂无{section.title}</p>}</details>)}
      </details></Card>
      <Card className="drama-batch-result"><h3>剧本内容</h3>
        {result?.episodes.length ? result.episodes.map((episode) => <details className="drama-batch-episode" id={`drama-script-episode-${episode.id}`} key={episode.id} open={episode.number === 1 || episode.id === requestedScriptEpisode}><summary>第{episode.number}集：{episode.title}</summary>
          <div className="drama-batch-episode-body">{assetSections.map((section) => <div className="drama-batch-episode-assets" key={section.key}><span className="drama-batch-episode-kind">{section.title}</span><div>{episode[section.field].map((asset) => <Tooltip key={asset.id} title={<div><strong>{asset.name}</strong>{asset.description && <p>{asset.description}</p>}{asset.stateLabel && <p className="drama-asset-tooltip-state">本集状态：{asset.stateLabel}</p>}</div>}><Tag data-asset-id={asset.assetId} closable onClose={(event) => { event.preventDefault(); void removeAsset(episode.id, asset.id) }}>{asset.name}</Tag></Tooltip>)}<Button size="small" type="dashed" icon={<PlusOutlined />} onClick={() => openAssetPicker(episode.id, section.key)}>添加</Button></div></div>)}
          <div className="drama-batch-script"><h4>剧本内容</h4><pre>{episode.scriptText}</pre></div></div>
        </details>) : <Empty description={selected.status === 'running' ? '正在生成分集剧本' : '暂无分集剧本，点击 AI解析 开始'} />}
      </Card>
      </> : <Card className="drama-batch-result"><Empty description={selected.status === 'running' ? '正在解析剧本，完成后显示结果' : selected.status === 'failed' ? progress?.episodes.length ? '已保存部分阶段成果，点击“从此处继续解析”保留进度继续执行' : '点击“从此处继续解析”保留进度继续执行' : '点击 AI解析 生成剧集和资产'} /></Card>}
    </>}
    <Modal title={`添加${assetPicker ? assetSections.find((item) => item.key === assetPicker.kind)?.title : ''}`} open={!!assetPicker} onCancel={() => setAssetPicker(null)} onOk={() => void saveEpisodeAssets()} okText="保存" okButtonProps={{ loading: savingAssets, disabled: pickedAssets.length === 0 }}>
      <input className="drama-asset-search" placeholder="搜索项目资产名称" value={assetQuery} onChange={(event) => setAssetQuery(event.target.value)} />
      <div className="drama-asset-picker-list">{assetOptions.map((asset) => <label key={asset.id} className={`drama-asset-picker-item${linkedAssetIds.has(asset.id) ? ' linked' : ''}`}><input type="checkbox" checked={linkedAssetIds.has(asset.id) || pickedAssets.includes(asset.id)} disabled={linkedAssetIds.has(asset.id)} onChange={(event) => setPickedAssets(event.target.checked ? [...pickedAssets, asset.id] : pickedAssets.filter((id) => id !== asset.id))} /><span><strong>{asset.name}</strong><small>{asset.description}</small></span>{linkedAssetIds.has(asset.id) && <small className="drama-asset-linked-label">已关联</small>}</label>)}{assetOptions.length === 0 && <Empty description="项目资产库暂无匹配项" />}</div>
    </Modal>
    <Modal title="新建批次" open={choiceOpen} onCancel={() => setChoiceOpen(false)} onOk={() => void openCreateReader()} okText="选择原文范围" okButtonProps={{ loading: opening, disabled: mode === 'previous' && !previousBatchId }}>
      <p className="drama-batch-choice-help">选择本批从哪里开始。下一步可在原文中检查和调整终点。</p>
      <div className="drama-batch-choices" role="radiogroup" aria-label="批次起点方式">
        {([{ key: 'beginning', title: '从头开始', hint: '从原文第一个字符开始' }, { key: 'previous', title: '从某一批次结尾开始', hint: '接在所选批次的结尾之后' }, { key: 'manual', title: '从剧本中自行选择', hint: '在原文阅读器中标记开头' }] as const).map((item) =>
          <button type="button" key={item.key} role="radio" aria-checked={mode === item.key} className={mode === item.key ? 'active' : ''} onClick={() => setMode(item.key)}><strong>{item.title}</strong><span>{item.hint}</span></button>
        )}
      </div>
      {mode === 'previous' && <Select className="drama-batch-previous" placeholder="选择已有批次" value={previousBatchId} onChange={setPreviousBatchId} options={batches.filter((item) => item.endOffset < project.sourceLength).map((item) => ({ value: item.id, label: `第 ${item.number} 批 · 结束于 ${item.endOffset.toLocaleString()} 字符` }))} />}
    </Modal>
    <Modal open={readerMode !== null} footer={null} onCancel={() => { setReaderMode(null); setLocked(true) }} width="min(92vw, 1500px)" className="drama-reader-modal" title={null} closable={false} destroyOnHidden styles={{ container: { padding: 0, background: '#fff' }, body: { padding: 0 } }}>
      {readerMode && <DramaSourceReader project={project} startOffset={startOffset} endOffset={endOffset} locked={locked}
        onStartChange={(value) => void changeStart(value)} onEndChange={(value) => { requestedStart.current = null; setEndOffset(value); setManualEnd(true) }} onUnlock={unlock}
        onSave={() => void saveRange()} onClose={() => { setReaderMode(null); setLocked(true) }} saving={saving} saveLabel={readerMode === 'create' ? '保存批次' : '保存范围'} />}
    </Modal>
  </section>
}
