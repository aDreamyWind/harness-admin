import {
  AudioOutlined, FileTextOutlined, GiftOutlined, PictureOutlined, UploadOutlined, UserOutlined, VideoCameraOutlined,
} from '@ant-design/icons'
import { useEffect, useRef, type KeyboardEvent, type ReactNode } from 'react'
import type { DramaAssetKind, DramaCanvasCardKind } from '../api/drama'
import './drama-canvas-create-menu.css'

export type CanvasCreateKind = DramaAssetKind | DramaCanvasCardKind | 'upload'

type Choice = { kind: CanvasCreateKind; label: string; icon: ReactNode }

const assets: Choice[] = [
  { kind: 'character', label: '角色', icon: <UserOutlined /> },
  { kind: 'scene', label: '场景', icon: <PictureOutlined /> },
  { kind: 'prop', label: '道具', icon: <GiftOutlined /> },
]
const materials: Choice[] = [
  { kind: 'text', label: '文本', icon: <FileTextOutlined /> },
  { kind: 'image', label: '图片', icon: <PictureOutlined /> },
  { kind: 'video', label: '视频', icon: <VideoCameraOutlined /> },
  { kind: 'audio', label: '音频', icon: <AudioOutlined /> },
]

export function DramaCanvasCreateMenu({ connected = false, autoFocus = true, onChoose, onClose }: {
  connected?: boolean
  autoFocus?: boolean
  onChoose: (kind: CanvasCreateKind) => void
  onClose?: () => void
}) {
  const panel = useRef<HTMLDivElement>(null)
  const title = connected ? '创建节点并连线' : '添加节点'

  useEffect(() => {
    if (autoFocus) panel.current?.querySelector('button')?.focus({ preventScroll: true })
  }, [autoFocus])

  function handleKey(event: KeyboardEvent<HTMLDivElement>) {
    if (event.key === 'Escape' && onClose) {
      event.preventDefault()
      event.stopPropagation()
      onClose()
      return
    }
    if (!['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown', 'Home', 'End'].includes(event.key)) return
    const rows = Array.from(panel.current?.querySelectorAll<HTMLElement>('[data-create-row]') ?? [], (row) => Array.from(row.querySelectorAll('button')))
    const row = rows.findIndex((items) => items.some((button) => button === event.target))
    if (row < 0) return
    const column = rows[row].findIndex((button) => button === event.target)
    const rowIndex = event.key === 'ArrowUp' ? (row + rows.length - 1) % rows.length
      : event.key === 'ArrowDown' ? (row + 1) % rows.length : row
    const items = rows[rowIndex]
    let next = items[Math.min(column, items.length - 1)]
    if (event.key === 'ArrowLeft') next = items[(column + items.length - 1) % items.length]
    if (event.key === 'ArrowRight') next = items[(column + 1) % items.length]
    if (event.key === 'Home') next = rows[0][0]
    if (event.key === 'End') next = rows.at(-1)?.at(-1) ?? next
    event.preventDefault()
    event.stopPropagation()
    next.focus({ preventScroll: true })
  }

  const groups = [
    { label: '项目资产', items: assets },
    { label: '素材节点', items: materials },
  ]

  return <div className="drama-canvas-create-menu" ref={panel} role="group" aria-label={title} onKeyDown={handleKey}>
    <div className="drama-canvas-create-heading">{title}</div>
    {groups.map(({ label, items }) => <div className="drama-canvas-create-group" key={label} role="group" aria-label={label}>
      <div className="drama-canvas-create-label" aria-hidden="true">{label}</div>
      <div className="drama-canvas-create-row" data-create-row>
        {items.map(({ kind, label, icon }) => <button type="button" key={kind} className="drama-canvas-create-choice"
          data-kind={kind} onClick={() => onChoose(kind)}>
          <span className="drama-canvas-create-icon" aria-hidden="true">{icon}</span>
          <span>{label}</span>
        </button>)}
      </div>
    </div>)}
    {!connected && <div className="drama-canvas-create-footer" data-create-row>
      <button className="drama-canvas-create-upload" type="button" onClick={() => onChoose('upload')}>
        <UploadOutlined aria-hidden="true" />上传本地素材
      </button>
    </div>}
  </div>
}
