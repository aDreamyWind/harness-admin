import { useState } from 'react'
import './drama-cover.css'

export function DramaProjectPoster({ title, url, alt = '', lazy = false }: {
  title: string; url?: string; alt?: string; lazy?: boolean
}) {
  const [failedUrl, setFailedUrl] = useState<string>()
  return <span className="drama-project-poster">
    {url && url !== failedUrl
      ? <img src={url} alt={alt} loading={lazy ? 'lazy' : undefined} onError={() => setFailedUrl(url)} />
      : <span className="drama-project-poster-placeholder" aria-hidden="true">
        <span className="drama-project-poster-eyebrow">AI 漫剧</span>
        <span className="drama-project-poster-title">{title}</span>
        <span className="drama-project-poster-caption">{url ? '封面加载失败' : '待生成封面'}</span>
      </span>}
  </span>
}
