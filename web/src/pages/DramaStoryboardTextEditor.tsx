import { GiftOutlined, PictureOutlined, SearchOutlined, UserOutlined } from '@ant-design/icons'
import { Empty, Input } from 'antd'
import { useEffect, useId, useLayoutEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import type { DramaAssetForm, DramaAssetKind } from '../api/drama'
import { formLabel, type StoryboardTextDocument } from './storyboardMentions'
import './drama-storyboard-editor.css'

const length = (text: string) => Array.from(text).length
const kindNames = { character: '角色', scene: '场景', prop: '道具' }
const icons = { character: <UserOutlined />, scene: <PictureOutlined />, prop: <GiftOutlined /> }
const clipboardType = 'application/x-harness-storyboard-mentions'
type Cursor = { start: number; end: number }
type Snapshot = StoryboardTextDocument & { cursor: Cursor }

// DOM只接受纯文字及程序创建的原子引用，不读取粘贴的HTML。
function read(root: Node): StoryboardTextDocument {
  let text = ''; const mentions: StoryboardTextDocument['mentions'] = []
  function walk(node: Node) {
    if (node.nodeType === Node.TEXT_NODE) { text += node.textContent ?? ''; return }
    if (node instanceof HTMLElement && node.dataset.dramaForm) {
      const label = node.dataset.dramaLabel ?? node.textContent ?? '', start = length(text)
      text += label; mentions.push({ start, end: length(text), formId: node.dataset.dramaForm }); return
    }
    if (node.nodeName === 'BR') { if (!(node instanceof HTMLElement && node.dataset.dramaTail)) text += '\n'; return }
    node.childNodes.forEach(walk)
  }
  root.childNodes.forEach(walk)
  return { text, mentions }
}
function cursor(root: HTMLElement): Cursor | null {
  const selection = window.getSelection()
  if (!selection?.rangeCount) return null
  const range = selection.getRangeAt(0)
  if (!root.contains(range.startContainer) || !root.contains(range.endContainer)) return null
  const start = range.cloneRange(); start.selectNodeContents(root); start.setEnd(range.startContainer, range.startOffset)
  const end = range.cloneRange(); end.selectNodeContents(root); end.setEnd(range.endContainer, range.endOffset)
  return { start: length(read(start.cloneContents()).text), end: length(read(end.cloneContents()).text) }
}
function point(root: HTMLElement, offset: number): [Node, number] {
  let remaining = offset, found: [Node, number] | null = null
  function walk(node: Node) {
    if (found) return
    if (node instanceof HTMLElement && node.dataset.dramaForm) {
      const n = length(node.dataset.dramaLabel ?? node.textContent ?? '')
      if (remaining < n) { found = [node.parentNode!, Array.from(node.parentNode!.childNodes).indexOf(node as ChildNode)]; return }
      remaining -= n; return
    }
    if (node.nodeName === 'BR') {
      const parent = node.parentNode!, index = Array.from(parent.childNodes).indexOf(node as ChildNode)
      if (node instanceof HTMLElement && node.dataset.dramaTail) { if (remaining === 0) found = [parent, index]; return }
      if (remaining === 0) found = [parent, index]
      else { remaining--; if (remaining === 0) found = [parent, index + 1] }
      return
    }
    if (node.nodeType === Node.TEXT_NODE) {
      const chars = Array.from(node.textContent ?? '')
      if (remaining <= chars.length) { found = [node, chars.slice(0, remaining).join('').length]; return }
      remaining -= chars.length; return
    }
    node.childNodes.forEach(walk)
  }
  root.childNodes.forEach(walk)
  return found ?? [root, root.childNodes.length]
}
function select(root: HTMLElement, where: Cursor) {
  const range = document.createRange(), a = point(root, where.start), b = point(root, where.end)
  range.setStart(...a); range.setEnd(...b)
  const selection = window.getSelection(); selection?.removeAllRanges(); selection?.addRange(range)
}
function fragment(value: StoryboardTextDocument, forms: DramaAssetForm[]) {
  const result = document.createDocumentFragment(), chars = Array.from(value.text)
  const appendText = (text: string) => text.split('\n').forEach((line, index) => { if (index) result.append(document.createElement('br')); result.append(document.createTextNode(line)) })
  let at = 0
  for (const ref of value.mentions) {
    if (ref.start < at || ref.end > chars.length || ref.end <= ref.start) continue
    appendText(chars.slice(at, ref.start).join(''))
    const form = forms.find((f) => f.id === ref.formId)
    const chip = document.createElement('span')
    chip.className = `drama-inline-asset ${form?.kind ?? 'character'}`
    chip.dataset.dramaForm = ref.formId; chip.dataset.dramaLabel = chars.slice(ref.start, ref.end).join(''); chip.contentEditable = 'false'
    chip.title = form ? `${form.assetName} · ${form.name}` : '历史资产形象'
    if (form?.imageUrl) { const image = document.createElement('img'); image.src = form.imageUrl; image.alt = ''; image.draggable = false; chip.append(image) }
    const original = chip.dataset.dramaLabel
    chip.append(document.createTextNode(form && original === form.assetName ? formLabel(form) : original))
    result.append(chip); at = ref.end
  }
  appendText(chars.slice(at).join(''))
  return result
}

// 末尾换行需要占位BR才能把光标放到新行；占位不属于保存的正文。
function normalizeTail(root: HTMLElement) {
  root.querySelectorAll('br[data-drama-tail]').forEach((node) => node.remove())
  if (read(root).text.endsWith('\n')) { const br = document.createElement('br'); br.dataset.dramaTail = 'true'; root.append(br) }
}

export function DramaStoryboardTextEditor({ value, forms, episodeFormIds, editable, label, placeholder = '输入文字，@ 引用资产', onChange }: {
  value: StoryboardTextDocument; forms: DramaAssetForm[]; episodeFormIds: string[]; editable: boolean; label: string; placeholder?: string
  onChange: (value: StoryboardTextDocument) => void
}) {
  const root = useRef<HTMLSpanElement>(null), composing = useRef(false)
  const initialized = useRef(false)
  const current = useRef(value), caret = useRef<Cursor>({ start: 0, end: 0 })
  const history = useRef<{ undo: Snapshot[]; redo: Snapshot[] }>({ undo: [], redo: [] })
  const [picker, setPicker] = useState<{ start: number; end: number; left: number; top: number; replacementFormId?: string } | null>(null)
  const [scope, setScope] = useState<'episode' | 'project'>('episode'), [kind, setKind] = useState<DramaAssetKind>('character')
  const [search, setSearch] = useState(''), [active, setActive] = useState(0)
  const listId = useId()
  const pickerElement = useRef<HTMLDivElement>(null)
  const eligible = forms.filter((f) => (scope === 'project' || episodeFormIds.includes(f.id)) && f.kind === kind && `${f.assetName} ${f.name}`.toLowerCase().includes(search.toLowerCase()))
  function paint(doc: StoryboardTextDocument, where?: Cursor) {
    const el = root.current; if (!el) return
    el.replaceChildren(fragment(doc, forms)); normalizeTail(el); el.dataset.empty = doc.text ? 'false' : 'true'
    if (where) { el.focus({ preventScroll: true }); select(el, where) }
  }
  useLayoutEffect(() => {
    if (!root.current) return
    if (JSON.stringify(current.current) !== JSON.stringify(value) || !initialized.current) { current.current = value; history.current = { undo: [], redo: [] }; paint(value); initialized.current = true }
  })
  useEffect(() => {
    if (!picker) return
    const outside = (event: MouseEvent) => { const target = event.target as Node; if (!root.current?.contains(target) && !pickerElement.current?.contains(target)) setPicker(null) }
    const scroll = (event: Event) => { if (!(event.target instanceof Node && pickerElement.current?.contains(event.target))) setPicker(null) }
    document.addEventListener('mousedown', outside); document.addEventListener('scroll', scroll, true)
    window.addEventListener('resize', scroll)
    return () => { document.removeEventListener('mousedown', outside); document.removeEventListener('scroll', scroll, true); window.removeEventListener('resize', scroll) }
  }, [!!picker])
  useEffect(() => { pickerElement.current?.querySelector('[role=option][aria-selected=true]')?.scrollIntoView({ block: 'nearest' }) }, [active, search, scope, kind])
  function publish(doc: StoryboardTextDocument, nextCursor: Cursor, remember = true) {
    if (JSON.stringify(doc) !== JSON.stringify(current.current)) {
      if (remember) { history.current.undo.push({ ...current.current, cursor: caret.current }); history.current.undo = history.current.undo.slice(-100); history.current.redo = [] }
      current.current = doc; onChange(doc)
    }
    caret.current = nextCursor
    if (root.current) root.current.dataset.empty = doc.text ? 'false' : 'true'
  }
  function moveHistory(back: boolean) {
    const source = back ? history.current.undo : history.current.redo, target = back ? history.current.redo : history.current.undo
    const previous = source.pop(); if (!previous) return
    target.push({ ...current.current, cursor: caret.current }); setPicker(null)
    paint(previous, previous.cursor); publish({ text: previous.text, mentions: previous.mentions }, previous.cursor, false)
  }
  function openPicker(where: Cursor, rect: DOMRect, query: string, replacementFormId?: string) {
    const fallback = root.current!.getBoundingClientRect()
    const left = Math.max(12, Math.min(rect.width || rect.x ? rect.x : fallback.x, innerWidth - Math.min(480, innerWidth - 24) - 12))
    const top = Math.max(12, Math.min(rect.bottom || fallback.bottom, innerHeight - 400)) + 6
    setPicker({ ...where, left, top, replacementFormId })
    setSearch(query)
    setActive(0)
  }
  function replaceMention(event: React.MouseEvent<HTMLSpanElement>) {
    const el = root.current
    const chip = event.target instanceof Element ? event.target.closest<HTMLElement>('[data-drama-form]') : null
    if (!editable || !el) return
    if (!chip || !el.contains(chip) || !chip.dataset.dramaForm) {
      if (picker?.replacementFormId) setPicker(null)
      return
    }
    const before = document.createRange()
    before.selectNodeContents(el)
    before.setEndBefore(chip)
    const start = length(read(before.cloneContents()).text)
    const end = start + length(chip.dataset.dramaLabel ?? '')
    const form = forms.find((item) => item.id === chip.dataset.dramaForm)
    setKind(form?.kind ?? 'character')
    setScope(episodeFormIds.includes(chip.dataset.dramaForm) ? 'episode' : 'project')
    openPicker({ start, end }, chip.getBoundingClientRect(), '', chip.dataset.dramaForm)
  }
  function detect() {
    const el = root.current, selection = window.getSelection(), where = el && cursor(el)
    if (!el || !where || !selection?.rangeCount || where.start !== where.end) { setPicker(null); return }
    const prefix = Array.from(read(el).text).slice(0, where.start).join(''), match = /@([^\s@]{0,40})$/u.exec(prefix)
    if (!match) { setPicker(null); return }
    openPicker({ start: where.start - length(match[0]), end: where.end }, selection.getRangeAt(0).getBoundingClientRect(), match[1])
  }
  function changed() {
    const el = root.current; if (!el || composing.current || !editable) return
    const doc = read(el); normalizeTail(el); const where = cursor(el) ?? caret.current
    publish(doc, where); detect()
  }
  function insert(doc: StoryboardTextDocument, where = root.current && cursor(root.current)) {
    const el = root.current; if (!el || !where || !editable) return
    el.focus({ preventScroll: true }); select(el, where)
    const range = window.getSelection()!.getRangeAt(0); range.deleteContents(); range.insertNode(fragment(doc, forms)); normalizeTail(el)
    const next = { start: where.start + length(doc.text), end: where.start + length(doc.text) }
    select(el, next); publish(read(el), next); setPicker(null)
  }
  function choose(form: DramaAssetForm) {
    if (!picker) return
    if (picker.replacementFormId === form.id) { setPicker(null); return }
    const name = form.assetName
    insert({ text: picker.replacementFormId ? name : `${name} `, mentions: [{ start: 0, end: length(name), formId: form.id }] }, { start: picker.start, end: picker.end })
  }
  useEffect(() => {
    const el = root.current
    if (!el) return
    const beforeInput = (event: InputEvent) => {
      if (!editable || event.isComposing) return
      if (event.inputType === 'insertParagraph' || event.inputType === 'insertLineBreak') { event.preventDefault(); insert({ text: '\n', mentions: [] }) }
      if (event.inputType === 'historyUndo' || event.inputType === 'historyRedo') { event.preventDefault(); moveHistory(event.inputType === 'historyUndo') }
    }
    el.addEventListener('beforeinput', beforeInput)
    return () => el.removeEventListener('beforeinput', beforeInput)
  })
  function copy(event: React.ClipboardEvent<HTMLSpanElement>, cut = false) {
    const where = root.current && cursor(root.current); if (!where) return
    const doc = read(root.current!), chars = Array.from(doc.text)
    const part = { text: chars.slice(where.start, where.end).join(''), mentions: doc.mentions.filter((r) => r.start >= where.start && r.end <= where.end).map((r) => ({ ...r, start: r.start - where.start, end: r.end - where.start })) }
    event.preventDefault(); event.clipboardData.setData('text/plain', part.text); event.clipboardData.setData(clipboardType, JSON.stringify(part))
    if (cut && editable) insert({ text: '', mentions: [] }, where)
  }
  function key(event: React.KeyboardEvent) {
    if (event.nativeEvent.isComposing || event.nativeEvent.keyCode === 229) return
    if (editable && root.current?.contains(event.target as Node) && (event.ctrlKey || event.metaKey) && ['z', 'y'].includes(event.key.toLowerCase())) { event.preventDefault(); moveHistory(event.key.toLowerCase() === 'z' && !event.shiftKey); return }
    if (editable && root.current?.contains(event.target as Node) && event.key === 'Enter' && !picker) { event.preventDefault(); insert({ text: '\n', mentions: [] }); return }
    if (!picker) return
    if (event.key === 'Escape') { event.preventDefault(); setPicker(null) }
    if (['ArrowDown', 'ArrowUp'].includes(event.key)) { event.preventDefault(); setActive((v) => Math.max(0, Math.min(eligible.length - 1, v + (event.key === 'ArrowDown' ? 1 : -1)))) }
    if (event.key === 'Enter') { event.preventDefault(); if (eligible.length) choose(eligible[active] ?? eligible[0]) }
  }
  return <><span ref={root} role="textbox" aria-label={label} aria-multiline="true" aria-readonly={!editable} aria-autocomplete="list" aria-expanded={!!picker} aria-controls={picker ? listId : undefined}
    className={`drama-inline-editor${editable ? ' editable' : ''}`} contentEditable={editable} suppressContentEditableWarning spellCheck={false} tabIndex={editable ? 0 : undefined} data-placeholder={placeholder}
    onClick={replaceMention} onFocus={() => { if (root.current) caret.current = cursor(root.current) ?? caret.current }} onMouseUp={() => { if (root.current) caret.current = cursor(root.current) ?? caret.current }}
    onKeyUp={(e) => { if (root.current) caret.current = cursor(root.current) ?? caret.current; if (!picker?.replacementFormId && !['ArrowDown', 'ArrowUp', 'Escape', 'Enter'].includes(e.key) && !composing.current && editable) detect() }} onKeyDown={key}
    onInput={changed} onCompositionStart={() => { composing.current = true; setPicker(null) }} onCompositionEnd={() => { composing.current = false; changed() }}

    onBlur={(e) => { if (!(e.relatedTarget instanceof Element && e.relatedTarget.closest('.drama-mention-picker'))) setPicker(null) }}
    onCopy={copy} onCut={(e) => copy(e, true)} onPaste={(e) => {
      if (!editable) return; e.preventDefault(); let doc: StoryboardTextDocument = { text: e.clipboardData.getData('text/plain'), mentions: [] }
      try { const saved = JSON.parse(e.clipboardData.getData(clipboardType)); if (saved.text === doc.text && Array.isArray(saved.mentions)) doc = { text: doc.text, mentions: saved.mentions.filter((r: StoryboardTextDocument['mentions'][number]) => forms.some((f) => f.id === r.formId) && r.start >= 0 && r.end > r.start && r.end <= length(doc.text)).sort((a: {start:number}, b: {start:number}) => a.start - b.start) } } catch { /* 外部纯文字不带内部引用格式。 */ }
      insert(doc)
    }} onDrop={(e) => { e.preventDefault(); if (editable) insert({ text: e.dataTransfer.getData('text/plain'), mentions: [] }) }} />
    {picker && editable && createPortal(<div ref={pickerElement} className="drama-mention-picker" style={{ left: picker.left, top: picker.top }} onKeyDown={key}>
      <div className="drama-mention-scope">{(['episode', 'project'] as const).map((v) => <button type="button" key={v} aria-pressed={scope === v} onMouseDown={(e) => e.preventDefault()} onClick={() => { setScope(v); setActive(0) }}>{v === 'episode' ? '本集资产' : '全剧资产'}</button>)}<span>↑↓ 选择 · Enter {picker.replacementFormId ? '替换' : '插入'}</span></div>
      <div className="drama-mention-kinds">{(Object.keys(kindNames) as DramaAssetKind[]).map((v) => <button type="button" key={v} aria-pressed={kind === v} onMouseDown={(e) => e.preventDefault()} onClick={() => { setKind(v); setActive(0) }}>{kindNames[v]}</button>)}</div>
      <Input aria-label="搜索引用资产" prefix={<SearchOutlined />} value={search} placeholder="搜索资产或形象" onChange={(e) => { setSearch(e.target.value); setActive(0) }} />
      <div id={listId} role="listbox" aria-label="可引用的资产形象" className="drama-mention-options">{eligible.length ? eligible.map((form, i) => <button type="button" role="option" aria-label={`${form.assetName} · ${form.name}`} aria-selected={i === active} key={form.id} title={`${form.assetName} · ${form.name}`} onMouseDown={(e) => e.preventDefault()} onClick={() => choose(form)}>
        <span className={`drama-mention-thumbnail ${form.kind}`}>{form.imageUrl ? <img src={form.imageUrl} alt="" loading="lazy" /> : icons[form.kind]}</span><strong>{form.assetName}</strong><small>{form.name}</small>
      </button>) : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="没有匹配的资产形象" />}</div>
    </div>, document.body)}
  </>
}
