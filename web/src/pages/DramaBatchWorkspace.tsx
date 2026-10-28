import { ArrowLeftOutlined, ExclamationCircleFilled, FileSearchOutlined, PlusOutlined } from '@ant-design/icons'
import { Alert, Button, Card, Empty, Modal, Select, Tag, Tooltip, message } from 'antd'
import { useEffect, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'

import {
  createDramaBatch, listDramaBatches, suggestDramaBatch, updateDramaBatchRange, analyzeDramaBatch, getDramaBatchResult, getDramaBatchProgress, listDramaAssets, addDramaEpisodeAsset, removeDramaEpisodeAsset,
  type DramaBatch, type DramaBatchMode, type DramaProject, type DramaBatchResult, type DramaBatchProgress, type DramaAssetKind, type DramaCatalogAsset,
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
const assetSections = [{ key: 'character', field: 'characters', title: '角色' }, { key: 'scene', field: 'scenes', title: '场景' }, { key: 'prop', field: 'props', title: '道具' }] as const

export function DramaBatchWorkspace({ project, batches, onBatchesChange }: Props) {
  const [messageApi, contextHolder] = message.useMessage()
  const [searchParams, setSearchParams] = useSearchParams()
  const [result, setResult] = useState<DramaBatchResult | null>(null)
  const [progress, setProgress] = useState<DramaBatchProgress | null>(null)
  const [analyzing, setAnalyzing] = useState(false)
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
    const timer = selected?.status === 'running' ? window.setInterval(refresh, 3000) : null
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
  async function startAnalysis() {
    if (!selected) return
    setAnalyzing(true)
    try {
      await analyzeDramaBatch(project.id, selected.id)
      onBatchesChange(await listDramaBatches(project.id))
      messageApi.success('已开始解析本批剧本')
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
      for (const assetId of pickedAssets) await addDramaEpisodeAsset(project.id, assetPicker.episodeId, assetId)
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
        <strong>{(batch.endOffset - batch.startOffset).toLocaleString()} 字符</strong>
        <p>原文 {(batch.startOffset + 1).toLocaleString()}—{batch.endOffset.toLocaleString()} 字符</p>
        <small>终点：{boundaryNames[batch.boundary]}</small>
      </button>)}</div>}
    </> : <>
      <div className="drama-batch-detail-head"><Button type="text" icon={<ArrowLeftOutlined />} onClick={() => selectBatch(null)}>返回批次</Button><Tag color={selected.status === 'stale' ? 'warning' : selected.status === 'ready' ? 'success' : selected.status === 'failed' ? 'error' : selected.status === 'running' ? 'processing' : 'default'}>{statusNames[selected.status]}</Tag></div>
      <div className="drama-batch-detail-title"><div><h2>第 {selected.number} 批</h2><p>原文 {(selected.startOffset + 1).toLocaleString()}—{selected.endOffset.toLocaleString()} 字符 · 共 {(selected.endOffset - selected.startOffset).toLocaleString()} 字符 · {boundaryNames[selected.boundary]}</p></div><div className="drama-batch-detail-actions"><Button icon={<FileSearchOutlined />} onClick={() => openExistingReader(selected)}>查看在剧本中的位置</Button><Button type="primary" loading={analyzing || selected.status === 'running'} disabled={selected.status === 'running' || project.status !== 'ready'} onClick={() => void startAnalysis()}>{selected.status === 'ready' ? 'AI重新解析' : 'AI解析'}</Button></div></div>
      {selected.status === 'stale' && <Alert type="warning" showIcon message="上游批次范围已调整，请核对本批次起止位置" />}
      {selected.status === 'failed' && <Alert type="error" showIcon message="本批解析失败" description={selected.errorMessage?.includes('文本模型响应超时') ? '文本模型在等待时间内没有返回结果。这次解析没有产生新剧集，请点击 AI解析 重试。' : selected.errorMessage || '请重试'} />}
      {selected.status === 'running' && <Alert type="info" showIcon message="正在逐段解析本批原文，已完成的阶段成果会显示在下方" />}
      {progress && progress.steps.length > 0 && <Card className="drama-batch-progress" aria-live="polite">
        <h3>解析进度</h3>
        <div className="drama-batch-progress-counts">
          <span>美术指导 {progress.steps.filter((step) => step.stage === 'art' && step.status === 'completed').length}/{progress.steps.find((step) => step.stage === 'art')?.chunkTotal ?? '?'} 段</span>
          <span>导演 {progress.steps.filter((step) => step.stage === 'director' && step.status === 'completed').length}/{progress.steps.find((step) => step.stage === 'art')?.chunkTotal ?? '?'} 段</span>
        </div>
        <ol className="drama-batch-step-list">{progress.steps.map((step) => <li key={`${step.stage}-${step.chunkNumber}`} className={step.status}>
          <span>{step.stage === 'art' ? '美术指导' : '导演'} · 第 {step.chunkNumber}/{step.chunkTotal} 段</span>
          <strong>{step.status === 'completed' ? '已完成' : step.status === 'failed' ? '需重试' : '进行中'}</strong>
          {step.status === 'completed' && <small>{Math.round(step.durationMs / 1000)} 秒</small>}
          {step.errorMessage && <small title={step.errorMessage}>{step.errorMessage}</small>}
        </li>)}</ol>
        {selected.status !== 'ready' && (progress.assets.characters.length + progress.assets.scenes.length + progress.assets.props.length > 0 || progress.episodes.length > 0) && <div className="drama-batch-progress-preview">
          <h4>已完成的阶段成果</h4>
          {assetSections.map((group) => <div key={group.key} className="drama-batch-preview-group"><strong>{group.title}（{progress.assets[group.field].length}）</strong><div>{progress.assets[group.field].map((asset, index) => <Tag key={`${asset.name}-${index}`} title={asset.description}>{asset.name}</Tag>)}</div></div>)}
          {progress.episodes.map((episode, index) => <details key={`${episode.title}-${index}`} className="drama-batch-preview-episode"><summary>已生成第 {index + 1} 集：{episode.title}</summary><p>{episode.synopsis}</p><pre>{episode.scriptText}</pre></details>)}
          <p className="drama-batch-preview-note">阶段成果已保存；本批全部完成后才会加入正式资产和剧集。</p>
        </div>}
      </Card>}
      {result?.episodes.length ? <>
      <Card className="drama-batch-result"><h3>角色、场景、道具</h3>
        {result && assetSections.map((section) => <div className="drama-batch-asset-group" key={section.key}><h4>{section.title}列表（{result.assets[section.field].length}）</h4>
          {result.assets[section.field].length ? <div className="drama-batch-asset-list">{result.assets[section.field].map((asset) => <div key={asset.id} className="drama-batch-asset-row"><strong>{asset.name}</strong><div className="drama-batch-asset-copy"><span>{asset.description}</span>{section.key === 'character' && asset.appearanceDescription && <small>外貌：{asset.appearanceDescription}</small>}{section.key === 'character' && asset.voiceDescription && <small>音色：{asset.voiceDescription}</small>}</div></div>)}</div> : <p className="drama-batch-muted">暂无{section.title}</p>}</div>)}
      </Card>
      <Card className="drama-batch-result"><h3>剧本内容</h3>
        {result?.episodes.length ? result.episodes.map((episode) => <details className="drama-batch-episode" key={episode.id} open={episode.number === 1}><summary>第{episode.number}集：{episode.title}</summary>
          <div className="drama-batch-episode-body">{assetSections.map((section) => <div className="drama-batch-episode-assets" key={section.key}><span className="drama-batch-episode-kind">{section.title}</span><div>{episode[section.field].map((asset) => <Tag key={asset.id} closable onClose={(event) => { event.preventDefault(); void removeAsset(episode.id, asset.id) }}>{asset.name}{asset.stateLabel ? `—${asset.stateLabel}` : ''}</Tag>)}<Button size="small" type="dashed" icon={<PlusOutlined />} onClick={() => openAssetPicker(episode.id, section.key)}>添加</Button></div></div>)}
          <div className="drama-batch-script"><h4>剧本内容</h4><pre>{episode.scriptText}</pre></div></div>
        </details>) : <Empty description={selected.status === 'running' ? '正在生成分集剧本' : '暂无分集剧本，点击 AI解析 开始'} />}
      </Card>
      </> : <Card className="drama-batch-result"><Empty description={selected.status === 'running' ? '正在解析剧本，完成后显示结果' : selected.status === 'failed' ? progress?.episodes.length ? '已保存部分阶段成果，点击 AI解析 从失败步骤继续' : '点击 AI解析 从失败步骤继续' : '点击 AI解析 生成剧集和资产'} /></Card>}
    </>}
    <Modal title={`添加${assetPicker ? assetSections.find((item) => item.key === assetPicker.kind)?.title : ''}`} open={!!assetPicker} onCancel={() => setAssetPicker(null)} onOk={() => void saveEpisodeAssets()} okText="保存" okButtonProps={{ loading: savingAssets, disabled: pickedAssets.length === 0 }}>
      <input className="drama-asset-search" placeholder="搜索项目资产名称" value={assetQuery} onChange={(event) => setAssetQuery(event.target.value)} />
      <div className="drama-asset-picker-list">{assetOptions.map((asset) => <label key={asset.id} className="drama-asset-picker-item"><input type="checkbox" checked={pickedAssets.includes(asset.id)} onChange={(event) => setPickedAssets(event.target.checked ? [...pickedAssets, asset.id] : pickedAssets.filter((id) => id !== asset.id))} /><span><strong>{asset.name}</strong><small>{asset.description}</small></span></label>)}{assetOptions.length === 0 && <Empty description="项目资产库暂无匹配项" />}</div>
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
