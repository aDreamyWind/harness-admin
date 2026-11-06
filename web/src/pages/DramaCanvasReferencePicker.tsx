import { AudioOutlined, FileTextOutlined, GiftOutlined, PictureOutlined, SearchOutlined, UserOutlined, VideoCameraOutlined } from '@ant-design/icons'
import { Input } from 'antd'
import type { CSSProperties, FocusEventHandler, KeyboardEvent, Ref } from 'react'

import type { DramaAssetKind, DramaCanvasCardKind } from '../api/drama'
import './drama-canvas-reference-picker.css'

export type DramaCanvasMentionKind = DramaAssetKind | DramaCanvasCardKind
export type DramaCanvasMentionOption = { id: string; title: string; kind: DramaCanvasMentionKind; imageUrl?: string }
export type DramaCanvasReferenceCategory = DramaCanvasMentionKind | 'all'

export const dramaCanvasReferenceKinds = {
  character: { label: '角色', icon: UserOutlined },
  scene: { label: '场景', icon: PictureOutlined },
  prop: { label: '道具', icon: GiftOutlined },
  image: { label: '图片', icon: PictureOutlined },
  text: { label: '文本', icon: FileTextOutlined },
  video: { label: '视频', icon: VideoCameraOutlined },
  audio: { label: '音频', icon: AudioOutlined },
} satisfies Record<DramaCanvasMentionKind, { label: string; icon: typeof PictureOutlined }>

const categories: DramaCanvasMentionKind[] = ['character', 'scene', 'prop', 'image', 'text', 'video', 'audio']
const primaryCategories: DramaCanvasMentionKind[] = ['character', 'scene', 'prop', 'image']

function ReferenceImage({ option }: { option: DramaCanvasMentionOption }) {
  const Icon = dramaCanvasReferenceKinds[option.kind].icon
  return option.imageUrl ? <img src={option.imageUrl} alt="" loading="lazy" /> : <Icon aria-hidden="true" />
}

export function DramaCanvasReferencePicker({ options, results, linkedIds, category, search, activeIndex, listId, style,
  focusSearch, pickerRef, listRef, onChoose, onCategory, onSearch, onKeyDown, onBlur }: {
  options: DramaCanvasMentionOption[]
  results: DramaCanvasMentionOption[]
  linkedIds: ReadonlySet<string>
  category: DramaCanvasReferenceCategory
  search: string
  activeIndex: number
  listId: string
  style: CSSProperties
  focusSearch: boolean
  pickerRef: Ref<HTMLDivElement>
  listRef: Ref<HTMLDivElement>
  onChoose: (option: DramaCanvasMentionOption) => void
  onCategory: (category: DramaCanvasReferenceCategory) => void
  onSearch: (search: string) => void
  onKeyDown: (event: KeyboardEvent<HTMLDivElement>) => void
  onBlur: FocusEventHandler<HTMLDivElement>
}) {
  const counts = new Map<DramaCanvasMentionKind, number>()
  for (const option of options) counts.set(option.kind, (counts.get(option.kind) ?? 0) + 1)
  const tabs = categories.filter((kind) => primaryCategories.includes(kind) || counts.has(kind))
  const linked = results.filter((option) => linkedIds.has(option.id))
  const unlinked = results.filter((option) => !linkedIds.has(option.id))

  function renderGroup(label: string, items: DramaCanvasMentionOption[], start: number) {
    if (!items.length) return null
    return <div className="drama-canvas-reference-group" role="group" aria-label={label}>
      <strong>{label}</strong>
      <div className="drama-canvas-reference-grid">
        {items.map((option, index) => <button key={option.id} id={`${listId}-${start + index}`} type="button" role="option"
          aria-selected={start + index === activeIndex} tabIndex={-1} title={option.title}
          onMouseDown={(event) => event.preventDefault()} onClick={() => onChoose(option)}>
          <span className="drama-canvas-reference-art"><ReferenceImage option={option} /></span>
          <span className="drama-canvas-reference-name">{option.title}</span>
        </button>)}
      </div>
    </div>
  }

  return <div className="drama-canvas-mention-picker drama-canvas-reference-picker" ref={pickerRef} style={style} onKeyDown={onKeyDown} onBlur={onBlur}>
    <div className="drama-canvas-reference-tabs" role="group" aria-label="引用分类">
      <button type="button" aria-pressed={category === 'all'} onClick={() => onCategory('all')}>全部 <span>{options.length}</span></button>
      {tabs.map((kind) => <button type="button" key={kind} aria-pressed={category === kind} onClick={() => onCategory(kind)}>
        {dramaCanvasReferenceKinds[kind].label} <span>{counts.get(kind) ?? 0}</span>
      </button>)}
    </div>
    <Input prefix={<SearchOutlined />} className="drama-canvas-reference-search" size="small" autoFocus={focusSearch}
      placeholder="搜索卡片名称" aria-label="搜索引用卡片" aria-controls={listId} aria-autocomplete="list"
      aria-activedescendant={activeIndex >= 0 ? `${listId}-${activeIndex}` : undefined}
      value={search} onChange={(event) => onSearch(event.target.value)} allowClear />
    <div ref={listRef} id={listId} role="listbox" aria-label="可引用的画布卡片" className="drama-canvas-reference-list">
      {renderGroup('已引用', linked, 0)}
      {renderGroup('未引用', unlinked, linked.length)}
      {!results.length && <p role="status">{search.trim() ? '没有匹配的卡片' : '这一类还没有可引用的卡片'}</p>}
    </div>
  </div>
}
