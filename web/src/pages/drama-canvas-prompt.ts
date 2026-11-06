import type { DramaCanvasMentionOption } from './DramaCanvasReferencePicker'

export type CanvasPromptPart = string | { refId: string }
export type CanvasPromptRefChanges = { added: string[]; removed: string[] }
export type PromptSelection = { start: number; end: number }

export function appendPromptPart(parts: CanvasPromptPart[], part: CanvasPromptPart) {
  if (part === '') return
  const last = parts.at(-1)
  if (typeof part === 'string' && typeof last === 'string') parts[parts.length - 1] = last + part
  else parts.push(part)
}

export function canvasPromptRefIds(parts: readonly CanvasPromptPart[]) {
  return new Set(parts.flatMap((part) => typeof part === 'string' ? [] : [part.refId]))
}

export function canvasPromptParts(parts: readonly CanvasPromptPart[], linkedIds: readonly string[]) {
  const allowed = new Set(linkedIds)
  const next: CanvasPromptPart[] = []
  for (const part of parts) {
    if (typeof part === 'string' || allowed.has(part.refId)) appendPromptPart(next, part)
  }
  for (const id of canvasPromptRefIds(next)) allowed.delete(id)
  for (const refId of allowed) next.push({ refId })
  return next
}

export function canvasPromptText(parts: readonly CanvasPromptPart[], options: readonly DramaCanvasMentionOption[]) {
  const names = new Map(options.map(({ id, title }) => [id, title]))
  return parts.map((part) => {
    if (typeof part === 'string') return part
    const title = names.get(part.refId)
    return title ? `「${title}」` : ''
  }).join('')
}

export function samePrompt(a: readonly CanvasPromptPart[], b: readonly CanvasPromptPart[]) {
  return a.length === b.length && a.every((part, i) => {
    const other = b[i]
    return typeof part === 'string' ? part === other : typeof other === 'object' && part.refId === other.refId
  })
}

export function promptPartLength(part: CanvasPromptPart) {
  return typeof part === 'string' ? part.length : 1
}

export function slicePrompt(parts: readonly CanvasPromptPart[], start: number, end: number = Infinity) {
  const result: CanvasPromptPart[] = []
  let offset = 0
  for (const part of parts) {
    const next = offset + promptPartLength(part)
    if (offset >= end) break
    if (next > start) appendPromptPart(result, typeof part === 'string' ? part.slice(Math.max(0, start - offset), end - offset) : part)
    offset = next
  }
  return result
}

export function replacePromptRange(parts: readonly CanvasPromptPart[], { start, end }: PromptSelection, replacement: readonly CanvasPromptPart[]) {
  const next = slicePrompt(parts, 0, start)
  for (const part of [...replacement, ...slicePrompt(parts, end)]) appendPromptPart(next, part)
  return next
}

function readPromptChildren(root: ParentNode, parts: CanvasPromptPart[]) {
  if (root.childNodes.length === 1 && root.firstChild?.nodeName === 'BR') return
  for (const child of root.childNodes) {
    if (child instanceof Text) {
      appendPromptPart(parts, child.data.replaceAll('\u00a0', ' '))
    } else if (child instanceof HTMLElement) {
      const refId = child.dataset.canvasRef
      if (refId) appendPromptPart(parts, { refId })
      else if (child.tagName === 'BR') appendPromptPart(parts, '\n')
      else {
        const last = parts.at(-1)
        if ((child.tagName === 'DIV' || child.tagName === 'P') && last !== undefined && !(typeof last === 'string' && last.endsWith('\n'))) {
          appendPromptPart(parts, '\n')
        }
        readPromptChildren(child, parts)
      }
    }
  }
}

export function readPrompt(root: ParentNode) {
  const parts: CanvasPromptPart[] = []
  readPromptChildren(root, parts)
  return parts
}

export function promptOffset(root: HTMLElement, node: Node, offset: number) {
  const prefix = document.createRange()
  prefix.selectNodeContents(root)
  prefix.setEnd(node, offset)
  return readPrompt(prefix.cloneContents()).reduce((length, part) => length + promptPartLength(part), 0)
}

export function promptSelection(root: HTMLElement): PromptSelection | undefined {
  const selection = window.getSelection()
  if (!selection?.rangeCount) return
  const range = selection.getRangeAt(0)
  if (!root.contains(range.startContainer) || !root.contains(range.endContainer)) return
  return {
    start: promptOffset(root, range.startContainer, range.startOffset),
    end: promptOffset(root, range.endContainer, range.endOffset),
  }
}

function promptPoint(root: HTMLElement, offset: number): [Node, number] {
  for (const [index, child] of Array.from(root.childNodes).entries()) {
    if (child instanceof Text) {
      if (offset <= child.length) return [child, offset]
      offset -= child.length
    } else {
      if (offset === 0) return [root, index]
      offset -= 1
    }
  }
  return [root, root.childNodes.length]
}

export function selectPrompt(root: HTMLElement, { start, end }: PromptSelection) {
  const selection = window.getSelection()
  if (!selection) return
  const range = document.createRange()
  range.setStart(...promptPoint(root, start))
  range.setEnd(...promptPoint(root, end))
  selection.removeAllRanges()
  selection.addRange(range)
}
