import { CheckOutlined, ZoomInOutlined } from '@ant-design/icons'
import { Alert, Button, Empty, Image, Skeleton } from 'antd'

import type { CoverHistoryItem } from '../api/cover'
import './cover-history.css'

const dateLabel = new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' })

export function CoverHistoryGallery({ items, loading, error, disabled, applying, onRetry, onApply, onPreviewChange }: {
  items?: CoverHistoryItem[]
  loading: boolean
  error: boolean
  disabled?: boolean
  applying?: string
  onRetry: () => void
  onApply: (revision: string) => void
  onPreviewChange: (open: boolean) => void
}) {
  if (loading) return <Skeleton active title={false} paragraph={{ rows: 3 }} />
  if (error) return <Alert type="warning" showIcon title="暂时无法读取历史封面"
    action={<Button size="small" onClick={onRetry}>重试</Button>} />
  if (!items?.length) return <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="生成或上传的封面会保存在这里" />
  return <Image.PreviewGroup classNames={{ popup: { root: 'cover-history-lightbox' } }}
    preview={{ onOpenChange: onPreviewChange }}>
    <ul className="cover-history-grid">
      {items.map((item) => {
        const source = item.source === 'ai' ? 'AI 生成' : '自己上传'
        const date = dateLabel.format(new Date(item.createdAt))
        return <li key={item.revision} className="cover-history-card">
          <div className="cover-history-art">
            <Image src={item.url} alt={`${source}的封面，${date}`} loading="lazy"
              preview={{ cover: <ZoomInOutlined aria-label="放大预览" /> }} />
          </div>
          <div className="cover-history-info">
            <span>{source}</span>
            <time dateTime={item.createdAt}>{date}</time>
            {item.current ? <span className="cover-history-current"><CheckOutlined /> 当前封面</span>
              : <Button size="small" block loading={applying === item.revision} disabled={disabled || !!applying}
                onClick={() => onApply(item.revision)}>设为封面</Button>}
          </div>
        </li>
      })}
    </ul>
  </Image.PreviewGroup>
}
