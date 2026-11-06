import { useReactFlow, useViewport, type XYPosition } from '@xyflow/react'
import { useLayoutEffect, useRef, useState, type ReactNode, type RefObject } from 'react'

import { revealCanvasNode } from './drama-canvas-geometry'

const gap = 16
const canvasTop = 76
const canvasLeft = 64
const panelWidth = 336

export function DramaCanvasFloatingPanel({ node, shell, children }: {
  node: { position: XYPosition; width: number; height: number }
  shell: RefObject<HTMLDivElement | null>
  children: ReactNode
}) {
  const flow = useReactFlow()
  const view = useViewport()
  const panel = useRef<HTMLDivElement>(null)
  const revealed = useRef(false)
  const [bounds, setBounds] = useState({ width: 0, height: 0 })
  const [panelHeight, setPanelHeight] = useState(0)
  const { width, height } = bounds
  const compact = width < 720
  const maxHeight = compact ? Math.floor(height * 0.46) : height - canvasTop - gap
  const nodeLeft = node.position.x * view.zoom + view.x
  const nodeTop = node.position.y * view.zoom + view.y
  const nodeRight = nodeLeft + node.width * view.zoom
  const onLeft = width - gap - nodeRight < panelWidth + gap && nodeLeft - canvasLeft > width - gap - nodeRight

  useLayoutEffect(() => {
    const element = shell.current
    if (!element) return
    const measure = () => setBounds({ width: element.clientWidth, height: element.clientHeight })
    const observer = new ResizeObserver(measure)
    observer.observe(element)
    measure()
    return () => observer.disconnect()
  }, [shell])

  useLayoutEffect(() => {
    const element = panel.current
    if (!width || !element) return
    const measure = () => setPanelHeight(element.offsetHeight)
    const observer = new ResizeObserver(measure)
    observer.observe(element)
    measure()
    return () => observer.disconnect()
  }, [width])

  useLayoutEffect(() => {
    if (!width || !height || revealed.current) return
    revealed.current = true
    const current = flow.getViewport()
    const room = width - canvasLeft - (compact ? gap : panelWidth + gap * 2)
    const visibleHeight = Math.max(80, compact ? height - maxHeight - canvasTop - gap * 2 : height - canvasTop - gap)
    const zoom = Math.max(0.05, Math.min(current.zoom, room / node.width, visibleHeight / node.height))
    const sameOrigin = { x: current.x + node.position.x * (current.zoom - zoom), y: current.y + node.position.y * (current.zoom - zoom), zoom }
    const target = revealCanvasNode({ ...node.position, width: node.width, height: node.height }, sameOrigin, {
      x: !compact && onLeft ? canvasLeft + panelWidth + gap : canvasLeft,
      y: canvasTop,
      width: room,
      height: visibleHeight,
    })
    if (target.x !== current.x || target.y !== current.y || target.zoom !== current.zoom) void flow.setViewport(target)
  }, [width, height, compact, maxHeight, flow, node, onLeft])

  if (!width) return null
  const left = Math.max(gap, Math.min(width - panelWidth - gap, onLeft ? nodeLeft - panelWidth - gap : nodeRight + gap))
  const top = Math.max(canvasTop, Math.min(height - gap - panelHeight, nodeTop))
  return <div ref={panel} className="drama-canvas-floating-panel" style={compact
    ? { left: gap, right: gap, bottom: gap, maxHeight }
    : { width: panelWidth, left, top, maxHeight }}>
    {children}
  </div>
}
