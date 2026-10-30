import { CheckOutlined, DownOutlined } from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { Alert, Button, Empty, Modal, Skeleton } from 'antd'
import { useState } from 'react'
import { listDramaStyles, type DramaStyleOption } from '../api/drama'
import './drama-style-picker.css'

export function DramaStylePicker({ value, onChange, disabled = false }: { value?: string; onChange: (style: DramaStyleOption) => void; disabled?: boolean }) {
  const [open, setOpen] = useState(false)
  const [category, setCategory] = useState('全部')
  const query = useQuery({ queryKey: ['drama-styles'], queryFn: listDramaStyles, staleTime: 60000 })
  const selected = query.data?.find((style) => style.id === value || style.name === value)
  const categories = ['全部', ...new Set(query.data?.map((style) => style.category) || [])]
  const visible = query.data?.filter((style) => category === '全部' || style.category === category) || []
  const preview = (style: DramaStyleOption) => ({ backgroundImage: `url(${JSON.stringify(style.previewUrl)})`, backgroundPosition: `${style.previewX * 50}% ${style.previewY * 50}%` })
  return <><button type="button" className="drama-style-picker" aria-label={`选择风格：${selected?.name || '请选择'}`} disabled={disabled} onClick={() => setOpen(true)}>
    {selected && <span className="drama-style-picker-preview" style={preview(selected)} />}<span>{selected?.name || (query.isLoading ? '加载风格…' : '选择风格')}</span><DownOutlined />
  </button><Modal className="drama-visual-style-modal" title="风格库" open={open} onCancel={() => setOpen(false)} footer={null} width={780} destroyOnHidden>
    {query.isLoading ? <Skeleton active /> : query.error ? <Alert type="error" showIcon title="读取风格库失败" description={query.error.message} action={<Button onClick={() => void query.refetch()}>重试</Button>} /> : <>
      <div className="drama-visual-style-tabs" role="tablist" aria-label="风格分类">{categories.map((item) => <button type="button" role="tab" aria-selected={item === category} key={item} className={item === category ? 'active' : ''} onClick={() => setCategory(item)}>{item}</button>)}</div>
      {visible.length ? <div className="drama-visual-style-grid">{visible.map((style) => <button type="button" key={style.id} aria-pressed={selected?.id === style.id} className={`drama-visual-style-card${selected?.id === style.id ? ' selected' : ''}`} style={preview(style)} onClick={() => { onChange(style); setOpen(false) }}><span>{style.name}</span>{selected?.id === style.id && <CheckOutlined />}</button>)}</div> : <Empty description="暂无可选风格" />}
    </>}
  </Modal></>
}
