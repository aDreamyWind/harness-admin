import { ArrowLeftOutlined, ExclamationCircleFilled, FileSearchOutlined, PlusOutlined } from '@ant-design/icons'
import { Alert, Button, Card, Empty, Modal, Select, Tag, Tooltip, message } from 'antd'
import { useRef, useState } from 'react'

import {
  createDramaBatch, listDramaBatches, suggestDramaBatch, updateDramaBatchRange,
  type DramaBatch, type DramaBatchMode, type DramaProject,
} from '../api/drama'
import { DramaSourceReader } from './DramaSourceReader'

interface Props {
  project: DramaProject
  batches: DramaBatch[]
  onBatchesChange: (value: DramaBatch[]) => void
}

const boundaryNames: Record<DramaBatch['boundary'], string> = {
  episode: '完整分集', chapter: '章节结尾', paragraph: '段落结尾', limit: '5 万字上限', end: '原文末尾', manual: '手动标记',
}
const statusNames: Record<DramaBatch['status'], string> = { pending: '待解析', stale: '范围需核对', ready: '已解析' }

export function DramaBatchWorkspace({ project, batches, onBatchesChange }: Props) {
  const [messageApi, contextHolder] = message.useMessage()
  const [selectedId, setSelectedId] = useState<string | null>(null)
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
  const selected = batches.find((item) => item.id === selectedId) ?? null

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
    setSelectedId(batch.id)
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
    if (startOffset === null || endOffset === null || endOffset <= startOffset || endOffset - startOffset > 50000) return
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
        setSelectedId(item.id)
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
      {batches.length === 0 ? <div className="drama-empty"><Empty description="还没有批次，点击右上角新建批次" /></div> : <div className="drama-batch-grid">{batches.map((batch) => <button type="button" key={batch.id} className="drama-batch-card" onClick={() => setSelectedId(batch.id)}>
        <div><span>第 {batch.number} 批</span><Tag color={batch.status === 'stale' ? 'warning' : batch.status === 'ready' ? 'success' : 'default'}>{statusNames[batch.status]}</Tag></div>
        <strong>{(batch.endOffset - batch.startOffset).toLocaleString()} 字符</strong>
        <p>原文 {(batch.startOffset + 1).toLocaleString()}—{batch.endOffset.toLocaleString()} 字符</p>
        <small>终点：{boundaryNames[batch.boundary]}</small>
      </button>)}</div>}
    </> : <>
      <div className="drama-batch-detail-head"><Button type="text" icon={<ArrowLeftOutlined />} onClick={() => setSelectedId(null)}>返回批次</Button><Tag color={selected.status === 'stale' ? 'warning' : 'default'}>{statusNames[selected.status]}</Tag></div>
      <div className="drama-batch-detail-title"><div><h2>第 {selected.number} 批</h2><p>原文 {(selected.startOffset + 1).toLocaleString()}—{selected.endOffset.toLocaleString()} 字符 · 共 {(selected.endOffset - selected.startOffset).toLocaleString()} 字符 · {boundaryNames[selected.boundary]}</p></div><Button icon={<FileSearchOutlined />} onClick={() => openExistingReader(selected)}>查看在剧本中的位置</Button></div>
      {selected.status === 'stale' && <Alert type="warning" showIcon message="上游批次范围已调整，请核对本批次起止位置" />}
      <Card className="drama-batch-result"><h3>角色、场景、道具</h3><Empty description="已保存原文范围；确定剧集生成规则后，这里会展示本批次提取的角色、场景、道具" /></Card>
      <Card className="drama-batch-result"><h3>剧本内容</h3><Empty description="确定剧集生成规则后，这里会按集展示完整剧本" /></Card>
    </>}
    <Modal title="新建批次" open={choiceOpen} onCancel={() => setChoiceOpen(false)} onOk={() => void openCreateReader()} okText="选择原文范围" okButtonProps={{ loading: opening, disabled: mode === 'previous' && !previousBatchId }}>
      <p className="drama-batch-choice-help">选择本批从哪里开始。下一步可在原文中检查和调整终点。</p>
      <div className="drama-batch-choices" role="radiogroup" aria-label="批次起点方式">
        {([{ key: 'beginning', title: '从头开始', hint: '从原文第一个字符开始' }, { key: 'previous', title: '从某一批次结尾开始', hint: '接在所选批次的结尾之后' }, { key: 'manual', title: '从剧本中自行选择', hint: '在原文阅读器中标记开头' }] as const).map((item) =>
          <button type="button" key={item.key} role="radio" aria-checked={mode === item.key} className={mode === item.key ? 'active' : ''} onClick={() => setMode(item.key)}><strong>{item.title}</strong><span>{item.hint}</span></button>
        )}
      </div>
      {mode === 'previous' && <Select className="drama-batch-previous" placeholder="选择已有批次" value={previousBatchId} onChange={setPreviousBatchId} options={batches.filter((item) => item.endOffset < project.sourceLength).map((item) => ({ value: item.id, label: `第 ${item.number} 批 · 结束于 ${item.endOffset.toLocaleString()} 字符` }))} />}
    </Modal>
    <Modal open={readerMode !== null} footer={null} onCancel={() => { setReaderMode(null); setLocked(true) }} width="min(92vw, 1500px)" className="drama-reader-modal" title={null} closable={false} destroyOnHidden styles={{ container: { padding: 0, background: '#10141c' }, body: { padding: 0 } }}>
      {readerMode && <DramaSourceReader project={project} startOffset={startOffset} endOffset={endOffset} locked={locked}
        onStartChange={(value) => void changeStart(value)} onEndChange={(value) => { requestedStart.current = null; setEndOffset(value); setManualEnd(true) }} onUnlock={unlock}
        onSave={() => void saveRange()} onClose={() => { setReaderMode(null); setLocked(true) }} saving={saving} saveLabel={readerMode === 'create' ? '保存批次' : '保存范围'} />}
    </Modal>
  </section>
}
