import type { Viewport, XYPosition } from '@xyflow/react'

type CanvasSize = { width: number; height: number }
export type CanvasRect = XYPosition & CanvasSize
export type CanvasGeometryNode = CanvasSize & { id: string; group: string; position: XYPosition }

const nodeSizes: Record<string, CanvasSize> = {
  character: { width: 280, height: 560 },
  scene: { width: 400, height: 380 },
  prop: { width: 280, height: 430 },
  text: { width: 400, height: 250 },
  image: { width: 400, height: 270 },
  video: { width: 400, height: 280 },
  audio: { width: 400, height: 120 },
}
const groupOrder = Object.keys(nodeSizes)
const nodeGap = 36
const laneGap = 96

export function canvasNodeSize(group: string, measured?: { width?: number; height?: number }): CanvasSize {
  const fallback = nodeSizes[group] ?? nodeSizes.image
  return { width: measured?.width || fallback.width, height: measured?.height || fallback.height }
}

export function arrangeCanvasNodes(nodes: readonly CanvasGeometryNode[]): Map<string, XYPosition> {
  const groups = new Map(groupOrder.map((group) => [group, [] as CanvasGeometryNode[]]))
  for (const node of nodes) {
    const group = groups.get(node.group)
    if (group) group.push(node)
    else groups.set(node.group, [node])
  }

  const positions = new Map<string, XYPosition>()
  let laneX = 0
  for (const group of groups.values()) {
    if (!group.length) continue
    const columnCount = Math.min(group.length, Math.max(3, Math.ceil(Math.sqrt(group.length))))
    const columnWidths = Array<number>(columnCount).fill(0)
    for (const [index, node] of group.entries()) {
      const column = index % columnCount
      columnWidths[column] = Math.max(columnWidths[column], node.width)
    }

    let rowY = 0
    for (let start = 0; start < group.length; start += columnCount) {
      const row = group.slice(start, start + columnCount)
      let columnX = laneX
      for (const [column, node] of row.entries()) {
        positions.set(node.id, { x: columnX, y: rowY })
        columnX += columnWidths[column] + nodeGap
      }
      rowY += Math.max(...row.map((node) => node.height)) + nodeGap
    }
    laneX += columnWidths.reduce((width, column) => width + column + nodeGap, 0) - nodeGap + laneGap
  }
  return positions
}

function hasSpace(point: XYPosition, size: CanvasSize, occupied: readonly CanvasGeometryNode[]) {
  return occupied.every(({ position, width, height }) =>
    point.x + size.width + nodeGap <= position.x || point.x >= position.x + width + nodeGap ||
    point.y + size.height + nodeGap <= position.y || point.y >= position.y + height + nodeGap,
  )
}

export function findCanvasReferencePosition(target: CanvasGeometryNode, size: CanvasSize, occupied: readonly CanvasGeometryNode[]): XYPosition {
  const x = target.position.x - size.width - laneGap
  const center = target.position.y + (target.height - size.height) / 2
  const nearby = occupied.filter(({ position, width }) => position.x < x + size.width + nodeGap && position.x + width + nodeGap > x)
  const candidates = [center, ...nearby.flatMap(({ position, height }) => [
    position.y - size.height - nodeGap,
    position.y + height + nodeGap,
  ])].sort((a, b) => Math.abs(a - center) - Math.abs(b - center))
  return { x, y: candidates.find((y) => hasSpace({ x, y }, size, nearby)) ?? center }
}

function clamp(value: number, min: number, max: number) {
  return Math.max(min, Math.min(value, max))
}

function distance(point: XYPosition, origin: XYPosition) {
  return (point.x - origin.x) ** 2 + (point.y - origin.y) ** 2
}

export function findCanvasNodePosition(anchor: XYPosition, size: CanvasSize, occupied: readonly CanvasGeometryNode[], visible?: CanvasRect): XYPosition {
  const fitsVisible = visible && visible.width >= size.width && visible.height >= size.height
  const preferred = fitsVisible
    ? { x: clamp(anchor.x, visible.x, visible.x + visible.width - size.width), y: clamp(anchor.y, visible.y, visible.y + visible.height - size.height) }
    : anchor
  if (hasSpace(preferred, size, occupied)) return preferred

  const candidates: XYPosition[] = []
  for (const dx of [-2, -1, 0, 1, 2]) {
    for (const dy of [-2, -1, 0, 1, 2]) {
      candidates.push({ x: preferred.x + dx * (size.width + nodeGap), y: preferred.y + dy * (size.height + nodeGap) })
    }
  }
  const nearby = [...occupied].sort((a, b) => distance(a.position, preferred) - distance(b.position, preferred)).slice(0, 24)
  for (const { position, width, height } of nearby) {
    candidates.push({ x: position.x - size.width - nodeGap, y: preferred.y }, { x: position.x + width + nodeGap, y: preferred.y })
    candidates.push({ x: preferred.x, y: position.y - size.height - nodeGap }, { x: preferred.x, y: position.y + height + nodeGap })
  }

  if (fitsVisible) {
    const inView = candidates.map(({ x, y }) => ({
      x: clamp(x, visible.x, visible.x + visible.width - size.width),
      y: clamp(y, visible.y, visible.y + visible.height - size.height),
    })).sort((a, b) => distance(a, preferred) - distance(b, preferred)).find((point) => hasSpace(point, size, occupied))
    if (inView) return inView
  }
  const free = candidates.sort((a, b) => distance(a, preferred) - distance(b, preferred)).find((point) => hasSpace(point, size, occupied))
  if (free) return free

  const below = occupied.reduce((y, node) => Math.max(y, node.position.y + node.height + nodeGap), preferred.y)
  return { x: preferred.x, y: below }
}

function revealOffset(start: number, size: number, safeStart: number, safeSize: number) {
  if (safeSize <= 0) return 0
  const end = start + size
  const safeEnd = safeStart + safeSize
  if (size > safeSize) {
    if (start > safeStart) return safeStart - start
    if (end < safeEnd) return safeEnd - end
    return 0
  }
  if (start < safeStart) return safeStart - start
  if (end > safeEnd) return safeEnd - end
  return 0
}

export function revealCanvasNode(node: CanvasRect, viewport: Viewport, safeArea: CanvasRect): Viewport {
  const { x, y, zoom } = viewport
  const dx = revealOffset(node.x * zoom + x, node.width * zoom, safeArea.x, safeArea.width)
  const dy = revealOffset(node.y * zoom + y, node.height * zoom, safeArea.y, safeArea.height)
  return dx || dy ? { x: x + dx, y: y + dy, zoom } : viewport
}
