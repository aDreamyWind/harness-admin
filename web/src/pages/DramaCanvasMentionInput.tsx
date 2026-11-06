import { Input } from 'antd'
import { useId, useLayoutEffect, useRef, useState, type ComponentPropsWithoutRef, type ComponentRef, type KeyboardEvent } from 'react'
import { createPortal } from 'react-dom'

import { dramaCanvasReferenceKinds, type DramaCanvasMentionOption } from './DramaCanvasReferencePicker'
import './drama-canvas-mention-input.css'

export type { DramaCanvasMentionOption } from './DramaCanvasReferencePicker'

type MentionInputProps = Omit<ComponentPropsWithoutRef<typeof Input.TextArea>, 'defaultValue' | 'onChange' | 'value'> & {
  value: string
  options: DramaCanvasMentionOption[]
  onChange: (value: string) => void
  onMention: (id: string) => void
}

type Token = { start: number; end: number; query: string }
const maxOptions = 40

function tokenAt(value: string, start: number, end: number): Token | null {
  if (start !== end) return null
  const match = /[@＠]([^\s@＠]{0,80})$/u.exec(value.slice(0, start))
  return match ? { start: start - match[0].length, end: start, query: match[1] } : null
}

function sameToken(a: Token | null, b: Token | null) {
  return a?.start === b?.start && a?.end === b?.end && a?.query === b?.query
}

export function canvasMentionPopupPosition(field: HTMLElement, gallery: boolean) {
  const rect = field.getBoundingClientRect()
  const viewport = window.visualViewport
  const leftEdge = (viewport?.offsetLeft ?? 0) + 12
  const topEdge = (viewport?.offsetTop ?? 0) + 12
  const rightEdge = leftEdge + (viewport?.width ?? window.innerWidth) - 24
  const bottomEdge = topEdge + (viewport?.height ?? window.innerHeight) - 24
  if (rect.bottom < topEdge || rect.top > bottomEdge || rect.right < leftEdge || rect.left > rightEdge) return null

  const width = Math.min(gallery ? 496 : 340, rightEdge - leftEdge, Math.max(gallery ? 440 : 232, rect.width))
  const left = Math.min(Math.max(leftEdge, rect.left), rightEdge - width)
  const gap = 6
  const spaceBelow = bottomEdge - rect.bottom - gap
  const spaceAbove = rect.top - topEdge - gap
  const preferredHeight = gallery ? 415 : 270
  const opensAbove = spaceBelow < preferredHeight && spaceAbove > spaceBelow
  const room = opensAbove ? spaceAbove : spaceBelow
  const maxHeight = Math.min(preferredHeight, Math.max(gallery ? 215 : 140, room), bottomEdge - topEdge)
  const top = opensAbove ? Math.max(topEdge + maxHeight, rect.top - gap) : Math.min(bottomEdge - maxHeight, rect.bottom + gap)
  return { left, top, width, maxHeight, transform: opensAbove ? 'translateY(-100%)' : undefined }
}

export function DramaCanvasMentionInput({
  value, options, onChange, onMention, onKeyDown, onSelect, onBlur, onClick,
  onCompositionStart, onCompositionEnd, maxLength, disabled, readOnly, ...props
}: MentionInputProps) {
  const listId = useId()
  const textarea = useRef<ComponentRef<typeof Input.TextArea>>(null)
  const composing = useRef(false)
  const dismissed = useRef<Token>(null)
  const cursor = useRef<{ value: string; index: number }>(null)
  const [token, setToken] = useState<Token | null>(null)
  const [active, setActive] = useState(0)
  const [position, setPosition] = useState<ReturnType<typeof canvasMentionPopupPosition>>(null)
  const field = () => textarea.current?.resizableTextArea?.textArea
  const open = !!token && !disabled && !readOnly
  const query = token?.query.trim().toLocaleLowerCase() ?? ''
  const filtered = open ? options.filter((option) => option.title.toLocaleLowerCase().includes(query)) : []
  const results = filtered.slice(0, maxOptions)
  const activeIndex = Math.min(active, results.length - 1)
  const list = useRef<HTMLDivElement>(null)

  function detect(input: HTMLTextAreaElement) {
    if (composing.current || disabled || readOnly) return
    const next = tokenAt(input.value, input.selectionStart, input.selectionEnd)
    if (sameToken(next, token) || (dismissed.current && sameToken(next, dismissed.current))) return
    dismissed.current = null
    setToken(next)
    if (next) setActive(0)
  }

  useLayoutEffect(() => {
    const input = textarea.current?.resizableTextArea?.textArea
    const target = cursor.current
    if (!input || !target || target.value !== value) return
    cursor.current = null
    input.focus({ preventScroll: true })
    input.setSelectionRange(target.index, target.index)
  }, [value, token])

  useLayoutEffect(() => {
    const input = textarea.current?.resizableTextArea?.textArea
    if (!open || !input) return
    let frame = 0
    function update() {
      if (frame) cancelAnimationFrame(frame)
      frame = requestAnimationFrame(() => setPosition(canvasMentionPopupPosition(input!, false)))
    }
    setPosition(canvasMentionPopupPosition(input, false))
    const viewport = window.visualViewport
    const resize = new ResizeObserver(update)
    resize.observe(input)
    const anchor = input.closest('.react-flow__viewport, .drama-canvas-floating-panel')
    const movement = anchor && new MutationObserver(update)
    if (anchor && movement) movement.observe(anchor, { attributeFilter: ['style'] })
    window.addEventListener('scroll', update, true)
    window.addEventListener('resize', update)
    viewport?.addEventListener('resize', update)
    viewport?.addEventListener('scroll', update)
    return () => {
      if (frame) cancelAnimationFrame(frame)
      resize.disconnect()
      movement?.disconnect()
      window.removeEventListener('scroll', update, true)
      window.removeEventListener('resize', update)
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
  }, [activeIndex, position, query])

  function choose(option: DramaCanvasMentionOption) {
    const input = field()
    if (!input || !token || !open) return
    if (!sameToken(token, tokenAt(input.value, input.selectionStart, input.selectionEnd))) return
    const suffix = input.value.slice(token.end)
    const mention = `@${option.title}`
    const separator = /^\s/u.test(suffix) || (maxLength !== undefined && input.value.length - (token.end - token.start) + mention.length >= maxLength) ? '' : ' '
    const next = input.value.slice(0, token.start) + mention + separator + suffix
    if (maxLength !== undefined && next.length > maxLength) return
    const index = token.start + mention.length + separator.length
    cursor.current = { value: next, index }
    dismissed.current = tokenAt(next, index, index)
    setToken(null)
    if (next !== input.value) onChange(next)
    onMention(option.id)
  }

  function handlePickerKey(event: KeyboardEvent<HTMLElement>) {
    if (event.defaultPrevented || !open || composing.current || event.nativeEvent.isComposing || event.keyCode === 229) return
    if (event.key === 'Escape') {
      event.preventDefault()
      event.stopPropagation()
      dismissed.current = token
      setToken(null)
    } else if (position && results.length && (event.key === 'ArrowDown' || event.key === 'ArrowUp')) {
      event.preventDefault()
      event.stopPropagation()
      const offset = event.key === 'ArrowDown' ? 1 : -1
      setActive(Math.min(results.length - 1, Math.max(0, activeIndex + offset)))
    } else if (position && activeIndex >= 0 && event.key === 'Enter' && !event.ctrlKey && !event.metaKey && !event.shiftKey && !event.altKey) {
      event.preventDefault()
      event.stopPropagation()
      choose(results[activeIndex])
    }
  }

  const visible = open && !!position
  return <>
    <Input.TextArea {...props} ref={textarea} value={value} maxLength={maxLength} disabled={disabled} readOnly={readOnly}
      aria-autocomplete="list" aria-controls={visible ? listId : undefined} aria-expanded={visible}
      aria-activedescendant={visible && activeIndex >= 0 ? `${listId}-${activeIndex}` : undefined}
      onChange={(event) => {
        onChange(event.currentTarget.value)
        if (!(event.nativeEvent as InputEvent).isComposing) detect(event.currentTarget)
      }}
      onClick={(event) => { onClick?.(event); detect(event.currentTarget) }}
      onSelect={(event) => { onSelect?.(event); detect(event.currentTarget) }}
      onKeyDown={(event) => { onKeyDown?.(event); handlePickerKey(event) }}
      onBlur={(event) => {
        onBlur?.(event)
        dismissed.current = null
        setToken(null)
      }}
      onCompositionStart={(event) => {
        composing.current = true
        setToken(null)
        onCompositionStart?.(event)
      }}
      onCompositionEnd={(event) => {
        composing.current = false
        onCompositionEnd?.(event)
        detect(event.currentTarget)
      }} />
    {visible && createPortal(<div className="drama-canvas-mention-picker" style={position}>
      <div className="drama-canvas-mention-heading"><strong>引用画布卡片</strong><span>↑↓ 选择 · 回车插入</span></div>
      <div ref={list} id={listId} role="listbox" aria-label="可引用的画布卡片" className="drama-canvas-mention-list">
        {results.map((option, index) => {
          const { label, icon: Icon } = dramaCanvasReferenceKinds[option.kind]
          return <button key={option.id} id={`${listId}-${index}`} type="button" role="option" aria-selected={index === activeIndex}
            tabIndex={-1} title={option.title} onMouseDown={(event) => event.preventDefault()} onClick={() => choose(option)}>
            <span className={`drama-canvas-mention-thumbnail ${option.kind}`} aria-hidden="true">
              {option.imageUrl ? <img src={option.imageUrl} alt="" loading="lazy" /> : <Icon />}
            </span>
            <strong>{option.title}</strong><small>{option.kind === 'scene' ? '图片' : label}</small>
          </button>
        })}
        {results.length === 0 && <p role="status">{query ? '没有匹配的卡片' : '画布上还没有其他卡片'}</p>}
      </div>
      {filtered.length > maxOptions && <div className="drama-canvas-mention-hint">继续输入名称以筛选更多卡片</div>}
    </div>, document.body)}
  </>
}
