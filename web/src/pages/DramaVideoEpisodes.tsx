import { PictureOutlined, ReloadOutlined, UserOutlined, VideoCameraOutlined, GiftOutlined } from '@ant-design/icons'
import { Alert, Button, Empty, Select, Skeleton, Tag } from 'antd'
import { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'

import { listDramaBatches, listDramaVideoEpisodes, type DramaVideoEpisode } from '../api/drama'

export function DramaVideoEpisodes({ projectId }: { projectId: string }) {
  const [searchParams, setSearchParams] = useSearchParams()
  const [episodes, setEpisodes] = useState<DramaVideoEpisode[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [refresh, setRefresh] = useState(0)
  useEffect(() => {
    let active = true
    let timer: number | undefined
    let requestNumber = 0
    async function load(silent = false) {
      const request = ++requestNumber
      window.clearTimeout(timer)
      if (!silent) setLoading(true)
      try {
        // 先读取任务状态，再读取剧集，避免完成瞬间先读到空列表却提前停止轮询。
        const batches = await listDramaBatches(projectId)
        const items = await listDramaVideoEpisodes(projectId)
        if (!active || request !== requestNumber) return
        setEpisodes(items); setError('')
        if (batches.some((batch) => batch.status === 'running')) timer = window.setTimeout(() => void load(true), 5000)
      } catch (failure) {
        if (active && request === requestNumber) setError(failure instanceof Error ? failure.message : '读取视频分集失败')
      } finally { if (active && request === requestNumber) setLoading(false) }
    }
    void load()
    const onFocus = () => { void load(true) }
    window.addEventListener('focus', onFocus)
    return () => { active = false; window.clearTimeout(timer); window.removeEventListener('focus', onFocus) }
  }, [projectId, refresh])

  function openEpisode(id: string) {
    setSearchParams((current) => { const next = new URLSearchParams(current); next.set('section', 'videoEpisodes'); next.set('episode', id); return next })
  }
  function showScripts() {
    setSearchParams((current) => { const next = new URLSearchParams(current); next.set('section', 'episodes'); next.delete('episode'); return next })
  }
  const batches = Array.from(new Map(episodes.map((episode) => [episode.batch.id, episode.batch])).values())
  const batchFilter = searchParams.get('videoBatch') || ''
  const visible = batchFilter ? episodes.filter((episode) => episode.batch.id === batchFilter) : episodes
  if (loading) return <Skeleton active />
  if (error) return <Alert type="error" showIcon title="读取视频分集失败" description={error} action={<Button onClick={() => setRefresh((value) => value + 1)}>重试</Button>} />
  return <section aria-label="视频分集">
    <div className="drama-video-list-toolbar">
      <Select aria-label="按来源批次筛选视频分集" value={batchFilter} options={[{ value: '', label: '全部批次' }, ...batches.map((batch) => ({ value: batch.id, label: `第 ${batch.number} 批 · ${batch.name}` }))]} onChange={(value) => setSearchParams((current) => { const next = new URLSearchParams(current); if (value) next.set('videoBatch', value); else next.delete('videoBatch'); return next })} />
      <Button type="text" icon={<ReloadOutlined />} onClick={() => setRefresh((value) => value + 1)}>刷新</Button>
    </div>
    {!episodes.length ? <div className="drama-empty"><Empty description="暂无可制作的分集剧本"><Button type="primary" onClick={showScripts}>前往剧本分集</Button></Empty></div> : !visible.length ? <div className="drama-empty"><Empty description="此批次暂无分集剧本" /></div> : <div className="drama-video-episode-grid">
      {visible.map((episode) => <article className="drama-video-episode-card" key={episode.scriptEpisodeId}>
        <button type="button" className="drama-video-episode-cover" aria-label={`进入第 ${episode.number} 集：${episode.title}`} onClick={() => openEpisode(episode.scriptEpisodeId)}>
          <span className="drama-video-episode-number">{episode.number}</span><VideoCameraOutlined /><span>待拆分</span>
        </button>
        <div className="drama-video-episode-copy">
          <h3 title={episode.title}>第{episode.number}集：{episode.title}</h3>
          <span className="drama-video-batch-name" title={`第 ${episode.batch.number} 批 · 批内第 ${episode.batchEpisodeNumber} 集`}>{episode.batch.name}</span>
          <div className="drama-video-asset-counts"><span><UserOutlined />角色 {episode.characterCount}</span><span><PictureOutlined />场景 {episode.sceneCount}</span><span><GiftOutlined />道具 {episode.propCount}</span></div>
          {episode.scriptNeedsReview && <Tag color="warning">来源剧本需核对</Tag>}
          <Button type="primary" block onClick={() => openEpisode(episode.scriptEpisodeId)}>进入制作</Button>
        </div>
      </article>)}
    </div>}
  </section>
}
