import { ArrowLeftOutlined, FileTextOutlined } from '@ant-design/icons'
import { Alert, Button, Drawer, Empty, Skeleton, Tooltip } from 'antd'
import { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'

import { ApiError } from '../api/client'
import { getDramaVideoEpisodeWorkspace, type DramaProject, type DramaVideoEpisodeWorkspace } from '../api/drama'
import { DramaAssetGallery } from './DramaAssetGallery'
import { DramaStoryboardWorkspace } from './DramaStoryboardWorkspace'

const assetGroups = [{ field: 'characters', kind: 'character', label: '角色' }, { field: 'scenes', kind: 'scene', label: '场景' }, { field: 'props', kind: 'prop', label: '道具' }] as const

export function DramaVideoEpisodeStudio({ project, episodeId }: { project: DramaProject; episodeId: string }) {
  const [, setSearchParams] = useSearchParams()
  const [workspace, setWorkspace] = useState<DramaVideoEpisodeWorkspace | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [refresh, setRefresh] = useState(0)
  const [scriptOpen, setScriptOpen] = useState(false)
  useEffect(() => {
    let active = true
    setLoading(true); setWorkspace(null); setError(''); setScriptOpen(false)
    void getDramaVideoEpisodeWorkspace(project.id, episodeId).then((item) => { if (active) setWorkspace(item) })
      .catch((failure) => { if (active) setError(failure instanceof ApiError && failure.status === 404 ? '这集剧本不存在或已重新解析，请返回视频分集查看。' : failure instanceof Error ? failure.message : '读取单集制作工作区失败') })
      .finally(() => { if (active) setLoading(false) })
    return () => { active = false }
  }, [project.id, episodeId, refresh])
  function goBack() {
    setSearchParams((current) => { const next = new URLSearchParams(current); next.set('section', 'videoEpisodes'); next.delete('episode'); next.delete('storyboard'); next.delete('segment'); return next })
  }
  function showSourceBatch() {
    if (!workspace) return
    setSearchParams((current) => { const next = new URLSearchParams(current); next.set('section', 'episodes'); next.set('batch', workspace.episode.batch.id); next.set('scriptEpisode', episodeId); next.delete('episode'); next.delete('storyboard'); next.delete('segment'); return next })
  }
  const episode = workspace?.episode
  return <div className="drama-video-studio">
    <header className="drama-video-studio-header">
      <div className="drama-video-studio-identity"><Button type="text" icon={<ArrowLeftOutlined />} onClick={goBack} aria-label="返回视频分集" /><div><h1>{episode ? `第${episode.number}集：${episode.title}` : '单集制作'}</h1>{episode && <span>{episode.batch.name} · 批内第 {episode.batchEpisodeNumber} 集</span>}</div></div>
      <div className="drama-video-studio-actions"><span className="drama-video-studio-format">{project.overview.videoStyle || project.style} · {project.overview.aspectRatio || project.aspectRatio}</span><Button icon={<FileTextOutlined />} disabled={!workspace} onClick={() => setScriptOpen(true)}>本集剧本</Button><Tooltip title="生成视频片段后可合成"><span><Button disabled>合成</Button></span></Tooltip></div>
    </header>
    {loading ? <div className="drama-video-studio-loading"><Skeleton active /></div> : error ? <div className="drama-video-studio-loading"><Alert type="error" showIcon title="读取单集失败" description={error} action={<Button onClick={() => setRefresh((value) => value + 1)}>重试</Button>} /></div> : workspace && <div className="drama-video-studio-body">
      <aside className="drama-video-studio-assets" aria-label="本集资产库">
        <div className="drama-video-assets-heading"><h2>本集资产库</h2><Button type="link" size="small" onClick={showSourceBatch}>来源剧本</Button></div>
        {assetGroups.map((group) => <details key={group.kind} open className="drama-video-assets-group"><summary>{group.label}（{workspace[group.field].length}）</summary>
          {workspace[group.field].length ? <DramaAssetGallery compact assets={workspace[group.field]} kind={group.kind} projectId={project.id} /> : <p className="drama-video-asset-empty">本集未关联{group.label}</p>}
        </details>)}
      </aside>
      <main className="drama-video-storyboard" aria-label="本集分镜制作区域">
        {episode?.scriptNeedsReview && <Alert type="warning" showIcon title="来源剧本需要核对" description="来源批次已调整或正在重新解析，请先核对本集剧本。" action={<Button onClick={showSourceBatch}>查看来源</Button>} />}
        <DramaStoryboardWorkspace key={episodeId} projectId={project.id} episodeId={episodeId} needsReview={!!episode?.scriptNeedsReview} />
      </main>
    </div>}
    <Drawer title={episode ? `第${episode.number}集：${episode.title}` : '本集剧本'} open={scriptOpen} onClose={() => setScriptOpen(false)} size={760}>
      {workspace ? <><p className="drama-video-script-synopsis">{episode?.synopsis}</p><pre className="drama-source-text">{workspace.scriptText}</pre></> : <Empty description="暂无剧本" />}
    </Drawer>
  </div>
}
