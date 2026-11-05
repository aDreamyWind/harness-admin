import { DownOutlined } from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { Alert, Button, Empty, Modal, Skeleton } from 'antd'
import { useState } from 'react'
import { listDramaStyles, type DramaStyleOption } from '../api/drama'
import './drama-style-picker.css'

function stylePreview(style: DramaStyleOption) {
  return {
    backgroundImage: `url(${JSON.stringify(style.previewUrl)})`,
    backgroundPosition: `${style.previewX * 50}% ${style.previewY * 50}%`,
  }
}

export function DramaStylePicker({ value, valueLabel, onChange, disabled = false }: {
  value?: string
  valueLabel?: string
  onChange: (style: DramaStyleOption) => void
  disabled?: boolean
}) {
  const [open, setOpen] = useState(false)
  const query = useQuery({ queryKey: ['drama-styles'], queryFn: listDramaStyles, staleTime: 60000 })
  const styles = query.data?.filter((style) => style.category === '2D') ?? []
  const selected = styles.find((style) => style.id === value || style.name === value)
  const label = selected?.name || valueLabel || (query.isLoading ? '加载风格…' : value || '选择风格')

  return <>
    <button type="button" className="drama-style-picker" aria-label={`选择风格（${label}）`} aria-haspopup="dialog"
      disabled={disabled} onClick={() => setOpen(true)}>
      {selected && <span className="drama-style-picker-preview" style={stylePreview(selected)} aria-hidden="true" />}
      <span className="drama-style-picker-label">{label}</span>
      <DownOutlined aria-hidden="true" />
    </button>
    <Modal title="2D 风格库" open={open} onCancel={() => setOpen(false)} footer={null} width={780} destroyOnHidden>
      {query.isLoading ? <Skeleton active /> : query.error ? (
        <Alert type="error" showIcon title="读取风格库失败" description={query.error.message}
          action={<Button onClick={() => void query.refetch()}>重试</Button>} />
      ) : styles.length > 0 ? (
        <div className="drama-visual-style-grid">
          {styles.map((style) => <button type="button" key={style.id} aria-pressed={selected?.id === style.id}
            className="drama-visual-style-card" onClick={() => { onChange(style); setOpen(false) }}>
            <span className="drama-visual-style-thumbnail" style={stylePreview(style)} aria-hidden="true" />
            <span className="drama-visual-style-name">{style.name}</span>
          </button>)}
        </div>
      ) : <Empty description="暂无可选的 2D 风格" />}
    </Modal>
  </>
}
