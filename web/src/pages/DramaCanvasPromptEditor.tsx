import { useId, useImperativeHandle, useLayoutEffect, useRef, useState, type ClipboardEvent, type KeyboardEvent, type Ref } from 'react'
import { createPortal } from 'react-dom'

import { canvasMentionPopupPosition } from './DramaCanvasMentionInput'
import { DramaCanvasReferencePicker, type DramaCanvasMentionOption, type DramaCanvasReferenceCategory } from './DramaCanvasReferencePicker'
import {
  canvasPromptParts, canvasPromptRefIds, canvasPromptText, promptOffset, promptPartLength, promptSelection,
  readPrompt, replacePromptRange, samePrompt, selectPrompt, slicePrompt, type CanvasPromptPart,
  type CanvasPromptRefChanges, type PromptSelection,
} from './drama-canvas-prompt'
import './drama-canvas-prompt-editor.css'

export { canvasPromptParts, canvasPromptText, type CanvasPromptPart, type CanvasPromptRefChanges } from './drama-canvas-prompt'

export type DramaCanvasPromptHandle = { open: () => void }

type PickerToken = PromptSelection & { query: string; button?: boolean }
type PromptSnapshot = { parts: CanvasPromptPart[]; selection: PromptSelection }
const endOf = (parts: readonly CanvasPromptPart[]) => parts.reduce((end, part) => end + promptPartLength(part), 0)
const caret = (offset: number): PromptSelection => ({ start: offset, end: offset })
const containsReference = (parts: readonly CanvasPromptPart[]) => parts.some((part) => typeof part !== 'string')

function mentionAt(parts: CanvasPromptPart[], selection: PromptSelection): PickerToken | undefined {
  if (selection.start !== selection.end) return
  const prefix = slicePrompt(parts, 0, selection.start).at(-1)
  const match = typeof prefix === 'string' ? /[@＠]([^\s@＠]{0,80})$/u.exec(prefix) : null
  if (match) return { start: selection.start - match[0].length, end: selection.end, query: match[1] }
}

function sameToken(a?: PickerToken, b?: PickerToken) {
  return a?.start === b?.start && a?.end === b?.end && a?.query === b?.query && a?.button === b?.button
}

function renderChip(refId: string, option?: DramaCanvasMentionOption) {
  const chip = document.createElement('span')
  const title = option?.title ?? '引用素材'
  chip.className = 'drama-canvas-prompt-chip'
  chip.contentEditable = 'false'
  chip.dataset.canvasRef = refId
  chip.dataset.kind = option?.kind ?? ''
  chip.title = title
  if (option?.imageUrl) {
    const image = document.createElement('img')
    image.src = option.imageUrl
    image.alt = ''
    image.draggable = false
    chip.append(image)
  }
  const label = document.createElement('span')
  label.className = 'drama-canvas-prompt-chip-name'
  label.textContent = title
  const remove = document.createElement('button')
  remove.type = 'button'
  remove.dataset.removeCanvasRef = ''
  remove.setAttribute('aria-label', `删除引用 ${title}`)
  remove.textContent = '×'
  chip.append(label, remove)
  return chip
}

function writePrompt(root: HTMLElement, parts: readonly CanvasPromptPart[], options: ReadonlyMap<string, DramaCanvasMentionOption>) {
  root.replaceChildren(...parts.map((part) => typeof part === 'string'
    ? document.createTextNode(part) : renderChip(part.refId, options.get(part.refId))))
}

function staleChip(root: HTMLElement, options: ReadonlyMap<string, DramaCanvasMentionOption>) {
  return Array.from(root.querySelectorAll<HTMLElement>('[data-canvas-ref]')).some((chip) => {
    const option = options.get(chip.dataset.canvasRef ?? '')
    return chip.title !== (option?.title ?? '引用素材') || chip.dataset.kind !== (option?.kind ?? '')
      || chip.querySelector('img')?.getAttribute('src') !== option?.imageUrl
  })
}

export function DramaCanvasPromptEditor({ ariaLabel, placeholder, maxLength, disabled, value, options, linkedIds, editorRef, onChange }: {
  ariaLabel: string
  placeholder: string
  maxLength: number
  disabled?: boolean
  value: CanvasPromptPart[]
  options: DramaCanvasMentionOption[]
  linkedIds: readonly string[]
  editorRef?: Ref<DramaCanvasPromptHandle>
  onChange: (parts: CanvasPromptPart[], changes: CanvasPromptRefChanges) => boolean | void
}) {
  const editor = useRef<HTMLDivElement>(null)
  const lastSelection = useRef<PromptSelection>(undefined)
  const rendered = useRef<CanvasPromptPart[]>([])
  const composing = useRef(false)
  const dismissed = useRef<PickerToken>(undefined)
  const undo = useRef<PromptSnapshot[]>([])
  const redo = useRef<PromptSnapshot[]>([])
  const picker = useRef<HTMLDivElement>(null)
  const list = useRef<HTMLDivElement>(null)
  const listId = useId()
  const [token, setToken] = useState<PickerToken>()
  const [active, setActive] = useState(0)
  const [category, setCategory] = useState<DramaCanvasReferenceCategory>('all')
  const [pickerSearch, setPickerSearch] = useState<string>()
  const [position, setPosition] = useState<ReturnType<typeof canvasMentionPopupPosition>>(null)
  const shown = canvasPromptParts(value, linkedIds)
  const optionsById = new Map(options.map((option) => [option.id, option]))
  const linked = new Set(linkedIds)
  const open = !!token && !disabled
  const search = pickerSearch ?? token?.query ?? ''
  const query = search.trim().toLocaleLowerCase()
  const filtered = open ? options.filter((option) => (category === 'all' || option.kind === category) && option.title.toLocaleLowerCase().includes(query)) : []
  const results = [...filtered.filter((option) => linked.has(option.id)), ...filtered.filter((option) => !linked.has(option.id))]
  const activeIndex = Math.min(active, results.length - 1)
  const visible = open && !!position

  function startPicker(next: PickerToken) {
    dismissed.current = undefined
    setToken(next)
    setActive(0)
    setCategory('all')
    setPickerSearch(undefined)
  }

  useImperativeHandle(editorRef, () => ({
    open() {
      const root = editor.current
      if (!root || disabled || composing.current) return
      const parts = rendered.current
      const selection = promptSelection(root) ?? lastSelection.current ?? caret(endOf(parts))
      startPicker({ ...(mentionAt(parts, selection) ?? { ...selection, query: '' }), button: true })
    },
  }))

  useLayoutEffect(() => {
    const root = editor.current
    if (!root || composing.current) return
    if (!samePrompt(rendered.current, shown)) {
      undo.current = []
      redo.current = []
    }
    if (!samePrompt(readPrompt(root), shown) || staleChip(root, optionsById)) {
      const selection = promptSelection(root)
      writePrompt(root, shown, optionsById)
      if (selection && document.activeElement === root) selectPrompt(root, selection)
    }
    rendered.current = shown
  })

  useLayoutEffect(() => {
    function remember() {
      const selection = editor.current && promptSelection(editor.current)
      if (selection) lastSelection.current = selection
    }
    document.addEventListener('selectionchange', remember)
    return () => document.removeEventListener('selectionchange', remember)
  }, [])

  useLayoutEffect(() => {
    const root = editor.current
    if (!open || !root) return
    let frame = 0
    const place = () => setPosition(canvasMentionPopupPosition(root, true))
    function update() {
      if (frame) cancelAnimationFrame(frame)
      frame = requestAnimationFrame(place)
    }
    const closeOutside = (event: PointerEvent) => {
      if (event.target instanceof Node && !root.contains(event.target) && !picker.current?.contains(event.target)) setToken(undefined)
    }
    place()
    const anchor = root.closest('.drama-canvas-composer-anchor')
    const resize = new ResizeObserver(update)
    resize.observe(root)
    const movement = new MutationObserver(update)
    if (anchor) movement.observe(anchor, { attributeFilter: ['style'] })
    const viewport = window.visualViewport
    window.addEventListener('scroll', update, true)
    window.addEventListener('resize', update)
    document.addEventListener('pointerdown', closeOutside, true)
    viewport?.addEventListener('resize', update)
    viewport?.addEventListener('scroll', update)
    return () => {
      if (frame) cancelAnimationFrame(frame)
      resize.disconnect()
      movement.disconnect()
      window.removeEventListener('scroll', update, true)
      window.removeEventListener('resize', update)
      document.removeEventListener('pointerdown', closeOutside, true)
      viewport?.removeEventListener('resize', update)
      viewport?.removeEventListener('scroll', update)
    }
  }, [open])

  useLayoutEffect(() => {
    const menu = list.current
    const selected = menu?.querySelector('[aria-selected=true]')
    if (!menu || !selected) return
    const bounds = menu.getBoundingClientRect()
    const option = selected.getBoundingClientRect()
    if (option.top < bounds.top) menu.scrollTop += option.top - bounds.top
    if (option.bottom > bounds.bottom) menu.scrollTop += option.bottom - bounds.bottom
  }, [activeIndex, position, query, category])

  function currentSnapshot(): PromptSnapshot {
    const parts = rendered.current
    return { parts, selection: lastSelection.current ?? caret(endOf(parts)) }
  }

  function show(parts: CanvasPromptPart[], selection: PromptSelection) {
    const root = editor.current
    if (!root) return
    writePrompt(root, parts, optionsById)
    root.focus({ preventScroll: true })
    selectPrompt(root, selection)
    lastSelection.current = selection
  }

  function commit(next: CanvasPromptPart[], selection?: PromptSelection, record = true) {
    const previous = currentSnapshot()
    if (!samePrompt(next, previous.parts)) {
      const oldIds = canvasPromptRefIds(previous.parts)
      const nextIds = canvasPromptRefIds(next)
      if (canvasPromptText(next, options).length > maxLength || onChange(next, {
        added: [...nextIds].filter((id) => !oldIds.has(id)),
        removed: [...oldIds].filter((id) => !nextIds.has(id)),
      }) === false) {
        show(previous.parts, previous.selection)
        return false
      }
      if (record) {
        undo.current.push(previous)
        if (undo.current.length > 100) undo.current.shift()
        redo.current = []
      }
      rendered.current = next
    }
    if (selection) show(next, selection)
    return true
  }

  function replace(selection: PromptSelection, parts: CanvasPromptPart[]) {
    return commit(replacePromptRange(rendered.current, selection, parts), caret(selection.start + endOf(parts)))
  }

  function detect(parts: CanvasPromptPart[], selection: PromptSelection) {
    if (disabled || composing.current) return
    const next = mentionAt(parts, selection)
    if (sameToken(next, token) || (next && sameToken(next, dismissed.current))) return
    if (next) {
      if (token && !token.button && token.start === next.start) {
        setToken(next)
        setActive(0)
        setPickerSearch(undefined)
      } else startPicker(next)
    } else {
      dismissed.current = undefined
      setToken(undefined)
    }
  }

  function handleInput() {
    const root = editor.current
    if (!root || composing.current) return
    const next = readPrompt(root)
    const selection = promptSelection(root) ?? caret(endOf(next))
    if (commit(next)) {
      lastSelection.current = selection
      detect(next, selection)
    }
  }

  function choose(option: DramaCanvasMentionOption) {
    if (!token || !open) return
    if (replace(token, [{ refId: option.id }])) setToken(undefined)
  }

  function handlePickerKey(event: KeyboardEvent<HTMLElement>) {
    if (event.defaultPrevented || !open || composing.current || event.nativeEvent.isComposing || event.keyCode === 229) return false
    if (event.key === 'Escape') {
      dismissed.current = token?.button ? undefined : token
      setToken(undefined)
      const root = editor.current
      if (root && picker.current?.contains(event.target as Node)) {
        root.focus({ preventScroll: true })
        selectPrompt(root, currentSnapshot().selection)
      }
    } else if (event.target !== editor.current && !(event.target instanceof HTMLInputElement)) {
      return false
    } else if (visible && results.length && (event.key === 'ArrowDown' || event.key === 'ArrowUp')) {
      const offset = event.key === 'ArrowDown' ? 1 : -1
      setActive(Math.min(results.length - 1, Math.max(0, activeIndex + offset)))
    } else if (visible && activeIndex >= 0 && event.key === 'Enter' && !event.ctrlKey && !event.metaKey && !event.shiftKey && !event.altKey) {
      choose(results[activeIndex])
    } else {
      return false
    }
    event.preventDefault()
    event.stopPropagation()
    return true
  }

  function handleEditorKey(event: KeyboardEvent<HTMLDivElement>) {
    if (disabled || event.target !== event.currentTarget || composing.current || event.nativeEvent.isComposing || event.keyCode === 229) return
    if (handlePickerKey(event)) return
    const selection = promptSelection(event.currentTarget)
    if (!selection) return
    const { start, end } = selection
    const parts = rendered.current
    const key = event.key.toLowerCase()
    if ((event.metaKey || event.ctrlKey) && !event.altKey && (key === 'z' || key === 'y')) {
      event.preventDefault()
      const forward = key === 'y' || event.shiftKey
      const from = forward ? redo.current : undo.current
      const to = forward ? undo.current : redo.current
      const before = currentSnapshot()
      const next = from.at(-1)
      if (next && commit(next.parts, next.selection, false)) {
        from.pop()
        to.push(before)
      }
      setToken(undefined)
    } else if (event.key === 'Enter') {
      event.preventDefault()
      replace(selection, ['\n'])
      setToken(undefined)
    } else if (event.key === 'Backspace' || event.key === 'Delete') {
      const range = start !== end ? selection : event.key === 'Backspace'
        ? { start: Math.max(0, start - 1), end } : { start, end: end + 1 }
      if (containsReference(slicePrompt(parts, range.start, range.end))) {
        event.preventDefault()
        replace(range, [])
        setToken(undefined)
      }
    }
  }

  function copyOrCut(event: ClipboardEvent<HTMLDivElement>, cut: boolean) {
    const selection = promptSelection(event.currentTarget)
    if (!selection) return
    const parts = slicePrompt(rendered.current, selection.start, selection.end)
    if (!containsReference(parts)) return
    event.preventDefault()
    event.clipboardData.setData('text/plain', canvasPromptText(parts, options))
    if (cut && !disabled) {
      replace(selection, [])
      setToken(undefined)
    }
  }

  return <>
    <div ref={editor} className="drama-canvas-prompt-editor" role="textbox" aria-label={ariaLabel} aria-multiline="true"
      aria-disabled={disabled || undefined} aria-placeholder={placeholder} aria-autocomplete="list" aria-controls={visible ? listId : undefined}
      aria-activedescendant={visible && activeIndex >= 0 ? `${listId}-${activeIndex}` : undefined}
      contentEditable={!disabled} spellCheck={false} data-placeholder={placeholder} data-empty={shown.length === 0}
      onInput={(event) => { if (!(event.nativeEvent as InputEvent).isComposing) handleInput() }} onKeyDown={handleEditorKey}
      onKeyUp={(event) => {
        if (event.target !== event.currentTarget || !['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return
        const selection = promptSelection(event.currentTarget)
        if (selection) detect(rendered.current, selection)
      }}
      onBlur={(event) => {
        if (event.relatedTarget && !event.currentTarget.contains(event.relatedTarget) && !picker.current?.contains(event.relatedTarget)) setToken(undefined)
      }}
      onCompositionStart={() => { composing.current = true; setToken(undefined) }}
      onCompositionEnd={() => { composing.current = false; handleInput() }}
      onCopy={(event) => copyOrCut(event, false)} onCut={(event) => copyOrCut(event, true)}
      onPaste={(event) => {
        event.preventDefault()
        const selection = promptSelection(event.currentTarget)
        const text = event.clipboardData.getData('text/plain').replace(/\r\n?/g, '\n')
        if (!disabled && selection && text) replace(selection, [text])
      }}
      onDrop={(event) => event.preventDefault()}
      onClick={(event) => {
        if (disabled || !(event.target instanceof Element)) return
        const root = event.currentTarget
        const chip = event.target.closest('[data-remove-canvas-ref]')?.closest('[data-canvas-ref]')
        if (chip?.parentNode) {
          const index = Array.from(chip.parentNode.childNodes).indexOf(chip)
          const start = promptOffset(root, chip.parentNode, index)
          replace({ start, end: start + 1 }, [])
          setToken(undefined)
        } else {
          const selection = promptSelection(root)
          if (selection) detect(rendered.current, selection)
        }
      }} />
    {visible && createPortal(<DramaCanvasReferencePicker options={options} results={results} linkedIds={linked}
      category={category} search={search} activeIndex={activeIndex} listId={listId} style={position}
      focusSearch={!!token?.button} pickerRef={picker} listRef={list} onChoose={choose}
      onCategory={(next) => { setCategory(next); setActive(0); if (list.current) list.current.scrollTop = 0 }}
      onSearch={(next) => { setPickerSearch(next); setActive(0); if (list.current) list.current.scrollTop = 0 }}
      onKeyDown={handlePickerKey} onBlur={(event) => {
        if (event.relatedTarget && event.relatedTarget !== editor.current && !event.currentTarget.contains(event.relatedTarget)) setToken(undefined)
      }} />, document.body)}
  </>
}
