import { AimOutlined, CompressOutlined, DragOutlined, LayoutOutlined, MinusOutlined, PlusOutlined, RedoOutlined, SearchOutlined, SelectOutlined, UndoOutlined } from '@ant-design/icons'
import { NodeToolbar, Position, useReactFlow, useViewport, type XYPosition } from '@xyflow/react'
import { Button, Dropdown, Empty, Input, Popover, Tooltip } from 'antd'
import { useState, type ReactNode } from 'react'

export type DramaCanvasTool = 'select' | 'pan'
export type CanvasSearchItem = { id: string; name: string; category: string }

function ToolButton({ label, hint, icon, active, disabled, placement = 'top', releasePointerFocus = false, onClick }: {
  label: string; hint?: string; icon: ReactNode; active?: boolean; disabled?: boolean
  placement?: 'top' | 'right'; releasePointerFocus?: boolean; onClick: () => void
}) {
  return <Tooltip title={hint ?? label} placement={placement} styles={{ root: { pointerEvents: 'none' } }}>
    <Button type="text" icon={icon} aria-label={label} aria-pressed={active}
      disabled={disabled} onClick={(event) => {
        onClick()
        if (releasePointerFocus && event.detail > 0) event.currentTarget.blur()
      }} />
  </Tooltip>
}

function NodeSearch({ items, onFocusNode }: { items: CanvasSearchItem[]; onFocusNode: (id: string) => void }) {
  const [keyword, setKeyword] = useState('')
  const [cursor, setCursor] = useState(0)
  const query = keyword.trim().toLocaleLowerCase()
  const matches = items.filter(({ name, category }) => `${name} ${category}`.toLocaleLowerCase().includes(query))
  const current = Math.min(cursor, Math.max(0, matches.length - 1))
  return <div className="drama-canvas-search">
    <Input autoFocus aria-label="按名称查找节点" placeholder="按名称或类型查找" value={keyword}
      onChange={(event) => { setKeyword(event.target.value); setCursor(0) }}
      onKeyDown={(event) => {
        if (!matches.length || event.nativeEvent.isComposing || event.keyCode === 229) return
        if (event.key === 'Enter') { event.preventDefault(); onFocusNode(matches[current].id) }
        if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
          event.preventDefault()
          const next = (current + (event.key === 'ArrowDown' ? 1 : matches.length - 1)) % matches.length
          setCursor(next)
          document.getElementById(`drama-canvas-search-${matches[next].id}`)?.scrollIntoView({ block: 'nearest' })
        }
      }} />
    <div className="drama-canvas-search-results">
      {matches.length ? matches.map((item, index) => <button key={item.id} id={`drama-canvas-search-${item.id}`} type="button"
        className={current === index ? 'active' : ''} onClick={() => onFocusNode(item.id)}>
        <span>{item.name}</span><small>{item.category}</small>
      </button>) : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="没有匹配的节点" />}
    </div>
  </div>
}

export function DramaCanvasNodeToolbar({ nodes, children }: { nodes: { id: string; position: XYPosition }[]; children: ReactNode }) {
  const viewport = useViewport()
  const top = Math.min(...nodes.map((node) => node.position.y * viewport.zoom + viewport.y))
  return <NodeToolbar nodeId={nodes.map((node) => node.id)} isVisible offset={8}
    position={top < 116 ? Position.Bottom : Position.Top}>{children}</NodeToolbar>
}

export function DramaCanvasToolDock({ tool, onTool, canUndo, canRedo, onUndo, onRedo, items, searchOpen, onSearchOpen, onFocusNode }: {
  tool: DramaCanvasTool; onTool: (tool: DramaCanvasTool) => void; canUndo: boolean; canRedo: boolean
  onUndo: () => void; onRedo: () => void; items: CanvasSearchItem[]; searchOpen: boolean
  onSearchOpen: (open: boolean) => void; onFocusNode: (id: string) => void
}) {
  return <div className="drama-canvas-tool-dock" role="toolbar" aria-label="画布工具">
    <ToolButton label="选择工具" hint="选择工具 · V" placement="right" releasePointerFocus icon={<SelectOutlined />} active={tool === 'select'} onClick={() => onTool('select')} />
    <ToolButton label="抓手工具" hint="抓手工具 · H，或按住空格" placement="right" releasePointerFocus icon={<DragOutlined />} active={tool === 'pan'} onClick={() => onTool('pan')} />
    <hr />
    <ToolButton label="撤销" hint="撤销 · Ctrl / ⌘ + Z" placement="right" icon={<UndoOutlined />} disabled={!canUndo} onClick={onUndo} />
    <ToolButton label="重做" hint="重做 · Ctrl / ⌘ + Shift + Z" placement="right" icon={<RedoOutlined />} disabled={!canRedo} onClick={onRedo} />
    <hr />
    <Popover title="查找节点" trigger="click" placement="rightTop" open={searchOpen} onOpenChange={onSearchOpen} destroyOnHidden
      content={searchOpen && <NodeSearch items={items} onFocusNode={onFocusNode} />}>
      <Button type="text" icon={<SearchOutlined />} aria-label="查找节点" aria-expanded={searchOpen} />
    </Popover>
  </div>
}

export function DramaCanvasViewTools({ hasNodes, hasSelection, minimap, onMinimap, onFit, onFocusSelection }: {
  hasNodes: boolean; hasSelection: boolean; minimap: boolean; onMinimap: () => void; onFit: () => void; onFocusSelection: () => void
}) {
  const { zoom } = useViewport()
  const flow = useReactFlow()
  return <div className="drama-canvas-view-tools" role="toolbar" aria-label="画布视图">
    <ToolButton label="小地图" icon={<LayoutOutlined />} active={minimap} onClick={onMinimap} />
    <ToolButton label="定位所选" icon={<AimOutlined />} disabled={!hasSelection} onClick={onFocusSelection} />
    <ToolButton label="适应画布" icon={<CompressOutlined />} disabled={!hasNodes} onClick={onFit} />
    <ToolButton label="缩小" icon={<MinusOutlined />} disabled={zoom <= 0.05} onClick={() => void flow.zoomOut({ duration: 150 })} />
    <Dropdown trigger={['click']} placement="top" menu={{
      items: [25, 50, 75, 100, 150, 200].map((value) => ({ key: String(value), label: `${value}%` })),
      onClick: ({ key }) => void flow.zoomTo(Number(key) / 100, { duration: 180 }),
    }}><Button type="text" aria-label="缩放比例" className="drama-canvas-zoom">{Math.round(zoom * 100)}%</Button></Dropdown>
    <ToolButton label="放大" icon={<PlusOutlined />} disabled={zoom >= 2} onClick={() => void flow.zoomIn({ duration: 150 })} />
  </div>
}
