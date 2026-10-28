import { ExclamationCircleFilled, LockOutlined, UnlockOutlined } from '@ant-design/icons'
import { Alert, Button, InputNumber, Skeleton, Tooltip, message } from 'antd'
import { useEffect, useMemo, useRef, useState, type MouseEvent } from 'react'

import { getDramaSourceRange, type DramaProject, type DramaSourceRange } from '../api/drama'

const PAGE_SIZE = 12000

type Marker = 'start' | 'end'
interface ReaderProps {
  project: DramaProject
  startOffset: number | null
  endOffset: number | null
  locked: boolean
  onStartChange: (value: number) => void
  onEndChange: (value: number) => void
  onUnlock: () => void
  onSave: () => void
  onClose: () => void
  saving: boolean
  saveLabel: string
}

function positionOnLine(event: MouseEvent<HTMLDivElement>, lineStart: number, lineLength: number): number {
  const content = event.currentTarget.querySelector('.drama-reader-content')
  if (!content) return lineStart
  const documentWithCaret = document as Document & { caretRangeFromPoint?: (x: number, y: number) => Range | null }
  const caret = documentWithCaret.caretRangeFromPoint?.(event.clientX, event.clientY)
  if (!caret || !content.contains(caret.startContainer)) return lineStart
  const range = document.createRange()
  range.selectNodeContents(content)
  try {
    range.setEnd(caret.startContainer, caret.startOffset)
    return lineStart + Math.min(lineLength, Array.from(range.toString()).length)
  } catch {
    return lineStart
  }
}

export function DramaSourceReader({ project, startOffset, endOffset, locked, onStartChange, onEndChange, onUnlock, onSave, onClose, saving, saveLabel }: ReaderProps) {
  const [messageApi, contextHolder] = message.useMessage()
  const [pageStart, setPageStart] = useState(Math.floor((startOffset ?? 0) / PAGE_SIZE) * PAGE_SIZE)
  const [page, setPage] = useState<DramaSourceRange | null>(null)
  const [loading, setLoading] = useState(true)
  const [tool, setTool] = useState<Marker>('start')
  const [jump, setJump] = useState<number | null>(null)
  const [navigation, setNavigation] = useState<{ offset: number; sequence: number } | null>(startOffset === null ? null : { offset: startOffset, sequence: 0 })
  const documentRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    let alive = true
    getDramaSourceRange(project.id, pageStart).then((value) => { if (alive) setPage(value) })
      .catch((error) => { if (alive) messageApi.error(error instanceof Error ? error.message : '读取原文失败') })
      .finally(() => { if (alive) setLoading(false) })
    return () => { alive = false }
  }, [project.id, pageStart, messageApi])

  const lines = useMemo(() => {
    if (!page) return []
    let offset = page.startOffset
    return page.text.split('\n').map((text) => {
      const start = offset
      const length = Array.from(text).length
      offset += length + 1
      return { text, start, length }
    })
  }, [page])
  const pageNumber = Math.floor(pageStart / PAGE_SIZE) + 1
  const pageCount = Math.max(1, Math.ceil(project.sourceLength / PAGE_SIZE))
  const selectedLength = startOffset !== null && endOffset !== null && endOffset > startOffset ? endOffset - startOffset : 0
  const invalidRange = startOffset === null || endOffset === null || endOffset <= startOffset || endOffset - startOffset > 50000
  const validJump = jump !== null && Number.isInteger(jump) && jump >= 1 && jump <= project.sourceLength

  useEffect(() => {
    if (!navigation || loading || !page || navigation.offset < page.startOffset || navigation.offset >= page.endOffset) return
    const frame = window.requestAnimationFrame(() => {
      const container = documentRef.current
      const target = container?.querySelector<HTMLElement>('.drama-reader-line.jump-target')
      if (!container || !target) return
      const top = target.getBoundingClientRect().top - container.getBoundingClientRect().top + container.scrollTop
      container.scrollTo({ top: Math.max(0, top - 96), behavior: 'smooth' })
    })
    return () => window.cancelAnimationFrame(frame)
  }, [navigation, loading, page])

  function choose(offset: number) {
    if (locked) return
    if (tool === 'start') onStartChange(offset)
    else onEndChange(offset)
  }
  function goTo(offset: number) {
    const target = Math.max(0, Math.min(project.sourceLength - 1, offset))
    const targetPageStart = Math.floor(target / PAGE_SIZE) * PAGE_SIZE
    setNavigation((current) => ({ offset: target, sequence: (current?.sequence ?? 0) + 1 }))
    if (targetPageStart !== pageStart) {
      setLoading(true)
      setPageStart(targetPageStart)
    }
  }

  return <div className="drama-reader-shell">
    {contextHolder}
    <div className="drama-reader-topbar">
      <div><strong>{project.sourceName || project.title}</strong><span>第 {pageNumber} / {pageCount} 页 · 原文 {project.sourceLength.toLocaleString()} 字符</span></div>
      <Button onClick={onClose}>关闭</Button>
    </div>
    <div className="drama-reader-layout">
      <div className="drama-reader-document" ref={documentRef}>
        <div className="drama-reader-pagebar"><Button size="small" disabled={pageStart === 0} onClick={() => goTo(pageStart - PAGE_SIZE)}>上一页</Button><span>{(pageStart + 1).toLocaleString()}–{Math.min(pageStart + PAGE_SIZE, project.sourceLength).toLocaleString()} 字符</span><Button size="small" disabled={pageNumber >= pageCount} onClick={() => goTo(pageStart + PAGE_SIZE)}>下一页</Button></div>
        {loading ? <Skeleton active paragraph={{ rows: 12 }} /> : <div className="drama-reader-text" aria-label="只读剧本原文">
          {lines.map((line, index) => {
            const lineEnd = line.start + line.length
            const selectionFrom = startOffset === null ? line.length : Math.max(0, Math.min(line.length, startOffset - line.start))
            const selectionTo = startOffset === null ? 0 : Math.max(0, Math.min(line.length, (endOffset ?? project.sourceLength) - line.start))
            const runes = Array.from(line.text)
            const highlighted = selectionTo > selectionFrom
            const jumpTarget = navigation !== null && navigation.offset >= line.start && navigation.offset <= lineEnd
            return <div key={`${line.start}-${index}`} className={`drama-reader-line ${locked ? 'locked' : 'selectable'} ${jumpTarget ? 'jump-target' : ''}`} onClick={(event) => choose(positionOnLine(event, line.start, line.length))}
              role={locked ? undefined : 'button'} tabIndex={locked ? undefined : 0} onKeyDown={(event) => { if (!locked && (event.key === 'Enter' || event.key === ' ')) { event.preventDefault(); choose(line.start) } }}>
              {startOffset !== null && startOffset >= line.start && startOffset <= lineEnd && <span className="drama-reader-marker start">开头</span>}
              {endOffset !== null && endOffset >= line.start && endOffset <= lineEnd && <span className="drama-reader-marker end">结尾</span>}
              <span className="drama-reader-content"><span>{runes.slice(0, selectionFrom).join('')}</span>{highlighted && <mark>{runes.slice(selectionFrom, selectionTo).join('')}</mark>}<span>{runes.slice(Math.max(selectionFrom, selectionTo)).join('')}</span></span>
              {line.text === '' && <br />}
            </div>
          })}
        </div>}
      </div>
      <aside className="drama-reader-tools">
        <div className="drama-reader-tool-title">范围标记</div>
        <p>点击工具，再点击正文中的位置。重复标记会替换原位置。</p>
        <Tooltip title={locked ? '点击解锁后修改' : ''}><span><Button block disabled={locked} type={tool === 'start' && !locked ? 'primary' : 'default'} onClick={() => setTool('start')}>标记开头</Button></span></Tooltip>
        <Tooltip title={locked ? '点击解锁后修改' : ''}><span><Button block disabled={locked} type={tool === 'end' && !locked ? 'primary' : 'default'} onClick={() => setTool('end')}>标记结尾</Button></span></Tooltip>
        <div className="drama-reader-range"><span>开头</span><strong>{startOffset === null ? '未标记' : `${(startOffset + 1).toLocaleString()} 字符`}</strong><span>结尾</span><strong>{endOffset === null ? '未标记' : `${endOffset.toLocaleString()} 字符`}</strong><span>已选</span><strong>{selectedLength.toLocaleString()} / 50,000 字符</strong></div>
        {selectedLength > 50000 && <Alert type="error" showIcon message="单批不得超过 5 万字，请调整标记" />}
        <div className="drama-reader-jump"><label htmlFor="drama-reader-jump">跳转到原文位置</label><InputNumber id="drama-reader-jump" min={1} max={project.sourceLength} placeholder={`输入 1–${project.sourceLength.toLocaleString()}`} value={jump} onChange={setJump} onPressEnter={() => { if (validJump) goTo(jump - 1) }} /><Button disabled={!validJump} onClick={() => { if (validJump) goTo(jump - 1) }}>跳转</Button></div>
        {(startOffset !== null || endOffset !== null) && <div className="drama-reader-quick">{startOffset !== null && <Button size="small" onClick={() => goTo(startOffset)}>查看开头标记</Button>}{endOffset !== null && <Button size="small" onClick={() => goTo(Math.max(0, endOffset - 1))}>查看结尾标记</Button>}</div>}
        {locked ? <div className="drama-reader-lock"><Alert type="warning" showIcon icon={<ExclamationCircleFilled />} message="解锁修改可能影响已解析的剧本、视频等内容" /><Button icon={<UnlockOutlined />} onClick={onUnlock}>解锁范围</Button></div> : <div className="drama-reader-save"><Button type="primary" block disabled={invalidRange} loading={saving} onClick={onSave}>{saveLabel}</Button><span>{invalidRange ? '请先标记有效的开头和结尾' : <><LockOutlined /> 原文只读，仅保存起止位置</>}</span></div>}
      </aside>
    </div>
  </div>
}
