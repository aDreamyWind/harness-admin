import { Alert, Button, Empty, Spin } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { dramaSegmentVideoURL, listDramaSegmentVideos, startDramaSegmentVideo } from '../api/drama'

export function DramaSegmentVideoPreview({ projectId, episodeId, runId, segmentId, revision, canGenerate, missingReferences }: {
  projectId: string
  episodeId: string
  runId: string
  segmentId: string
  revision: number
  canGenerate: boolean
  missingReferences: string[]
}) {
  const client = useQueryClient()
  const scope = [projectId, episodeId, runId, segmentId] as const
  const queryKey = ['drama-segment-videos', ...scope]
  const query = useQuery({
    queryKey,
    queryFn: () => listDramaSegmentVideos(...scope),
    refetchInterval: (state) => state.state.data?.[0]?.status === 'running' ? 3000 : false,
  })
  const start = useMutation({
    mutationFn: () => startDramaSegmentVideo(...scope, revision),
    onSuccess: (video) => client.setQueryData(queryKey, [video, ...(query.data ?? []).filter((item) => item.id !== video.id)]),
    onSettled: () => client.invalidateQueries({ queryKey }),
  })
  const latest = query.data?.[0]
  const completed = query.data?.find((video) => video.status === 'succeeded')
  const missing = missingReferences.length > 0
  const running = latest?.status === 'running'
  const label = running ? '生成中' : latest?.status === 'failed' ? '重试生成' : completed ? '重新生成' : '生成视频'

  return <>
    <div className="drama-storyboard-preview-heading">
      <h3>片段预览</h3>
      <Button type="primary" disabled={!canGenerate || missing || running || query.isPending || query.isError} loading={start.isPending}
        onClick={() => start.mutate()}>{label}</Button>
    </div>
    {query.isError && <Alert type="error" showIcon title="读取视频任务失败" action={<Button size="small" onClick={() => void query.refetch()}>重试</Button>} />}
    {missing && !running && <Alert type="info" showIcon title={`请先生成参考图：${missingReferences.join("、")}`} />}
    {!missing && start.isError && <Alert type="error" showIcon title={start.error.message} />}
    {latest?.status === 'failed' && <Alert type="error" showIcon title={latest.error || '片段视频生成失败'} />}
    {running && <Alert type="info" icon={<Spin size="small" />} showIcon title="正在生成片段视频，可以切换片段或稍后回来查看。" />}
    {completed
      ? <video key={completed.id} className="drama-segment-video" aria-label="生成的片段视频" controls playsInline preload="metadata" src={dramaSegmentVideoURL(...scope, completed.id)} />
      : !running && !query.isError && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={query.isPending ? '正在读取视频记录' : '当前片段视频尚未生成'} />}
  </>
}
