import { Alert, Button, Empty, Modal, Select, Skeleton, Spin, Tag } from 'antd'
import { useId, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { dramaSegmentVideoURL, dramaVideoResolutions, getDramaSettings, listDramaCanvasGenerationModels, listDramaSegmentVideoCalls, listDramaSegmentVideos, type DramaSegmentVideo, type DramaVideoResolution, startDramaSegmentVideo } from '../api/drama'

export function DramaSegmentVideoPreview({ projectId, episodeId, runId, segmentId, revision, segmentVersion, canGenerate, missingReferences }: {
  projectId: string
  episodeId: string
  runId: string
  segmentId: string
  revision: number
  segmentVersion?: number
  canGenerate: boolean
  missingReferences: string[]
}) {
  const client = useQueryClient()
  const resolutionId = useId()
  const scope = [projectId, episodeId, runId, segmentId] as const
  const queryKey = ['drama-segment-videos', ...scope]
  const query = useQuery({
    queryKey,
    queryFn: () => listDramaSegmentVideos(...scope),
    refetchInterval: (state) => state.state.data?.some((video) => video.status === 'running') ? 3000 : false,
  })
  const start = useMutation({
    mutationFn: (resolution: DramaVideoResolution) => startDramaSegmentVideo(...scope, revision, resolution),
    onSuccess: (video) => client.setQueryData(queryKey, [video, ...(query.data ?? []).filter((item) => item.id !== video.id)]),
    onSettled: () => client.invalidateQueries({ queryKey }),
  })
  const settings = useQuery({ queryKey: ['drama-settings'], queryFn: getDramaSettings })
  const models = useQuery({ queryKey: ['drama-canvas-generation-models'], queryFn: listDramaCanvasGenerationModels, staleTime: 60_000 })
  const videoModel = models.data?.find((model) => model.kind === 'video' && model.maxReferences > 0 && model.code === settings.data?.videoModelCode)
  const resolutions = dramaVideoResolutions(videoModel)
  const [quality, setQuality] = useState<{ modelCode: string; resolution: DramaVideoResolution }>()
  const resolution = quality && quality.modelCode === videoModel?.code && resolutions.includes(quality.resolution) ? quality.resolution : '720P'
  const [dismissedFailureId, setDismissedFailureId] = useState<string>()
  const [inspecting, setInspecting] = useState<DramaSegmentVideo | null>(null)
  const calls = useQuery({
    queryKey: ['drama-segment-video-calls', ...scope, inspecting?.id],
    queryFn: () => listDramaSegmentVideoCalls(...scope, inspecting!.id),
    enabled: inspecting !== null,
  })
  const videos = query.data ?? []
  const latest = videos.find((video) => video.segmentVersion === segmentVersion)
  const visibleVideos = videos.filter((video) => video.status !== 'failed')
  const failure = latest?.status === 'failed' && latest.id !== dismissedFailureId && !start.isPending && !start.isError ? latest : undefined
  const completed = videos.some((video) => video.segmentVersion === segmentVersion && video.status === 'succeeded')
  const missing = missingReferences.length > 0
  const running = videos.some((video) => video.status === 'running')
  const label = running || start.isPending ? '生成中' : failure || start.isError ? '重试生成' : completed ? '重新生成' : '生成视频'

  return <>
    <div className="drama-storyboard-preview-heading">
      <h3>片段预览</h3>
      <div className="drama-storyboard-video-actions">
        <div className="drama-storyboard-video-resolution">
          <label htmlFor={resolutionId}>清晰度</label>
          <Select id={resolutionId} value={resolution} options={resolutions.map((value) => ({ value, label: value }))}
            disabled={!canGenerate || running || start.isPending || resolutions.length === 1}
            onChange={(value) => { if (videoModel) setQuality({ modelCode: videoModel.code, resolution: value }) }} />
        </div>
        <Button type="primary" disabled={!canGenerate || missing || running || query.isPending || query.isError} loading={start.isPending}
          onClick={() => {
            if (latest?.status === 'failed') setDismissedFailureId(latest.id)
            start.mutate(resolution)
          }}>{label}</Button>
      </div>
    </div>
    {query.isError && <Alert type="error" showIcon title="读取视频任务失败" action={<Button size="small" onClick={() => void query.refetch()}>重试</Button>} />}
    {missing && !running && <Alert type="info" showIcon title={`请先生成参考图：${missingReferences.join("、")}`} />}
    {!missing && start.isError && <Alert type="error" showIcon title={start.error.message} />}
    {failure && <Alert type="error" showIcon title="本次视频生成失败" description={failure.error}
      action={<Button size="small" type="link" onClick={() => setInspecting(failure)}>查看调用记录</Button>} />}
    {start.isPending && !running && <Alert type="info" icon={<Spin size="small" />} showIcon title="正在提交视频生成任务" />}
    <Modal open={inspecting !== null} title="片段视频调用记录" footer={null} width={680} onCancel={() => setInspecting(null)}>
      {inspecting && <p>任务编号 <code>{inspecting.id}</code></p>}
      {calls.isPending && <Skeleton active paragraph={{ rows: 2 }} />}
      {calls.isError && <Alert type="error" showIcon title="读取调用记录失败"
        action={<Button size="small" onClick={() => void calls.refetch()}>重试</Button>} />}
      {calls.isSuccess && (calls.data.length ? <ol className="drama-video-call-list">
        {calls.data.map((call, index) => <li key={`${call.createdAt}/${index}`}>
          <div><strong>{call.operation === 'video' ? '提交生成任务' : '查询生成结果'}</strong>
            <Tag color={call.result === 'failed' ? 'error' : 'success'}>{call.result === 'failed' ? '失败' : '成功'}</Tag></div>
          <div className="drama-video-call-meta">{new Date(call.createdAt).toLocaleString()} · {call.model} · HTTP {call.httpStatus}</div>
          {call.code && <code>{call.code}</code>}
          {call.message && <p>{call.message}</p>}
        </li>)}
      </ol> : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={inspecting?.error || '该任务暂无可关联的模型调用记录'} />)}
    </Modal>
    {visibleVideos.length > 0 && <ol className="drama-video-call-list" aria-label="视频生成记录">
      {visibleVideos.map((video) => <li key={video.id}>
        <div>
          {(visibleVideos.length > 1 || video.segmentVersion !== segmentVersion) &&
            <Tag color={video.segmentVersion === segmentVersion ? 'blue' : 'default'}>
              使用片段版本 {video.segmentVersion}
            </Tag>}
          <span className="drama-video-call-meta">{video.duration} 秒 · {new Date(video.createdAt).toLocaleString()}</span>
        </div>
        {video.status === 'running' && <Alert type="info" icon={<Spin size="small" />} showIcon title="正在生成片段视频" />}
        {video.status === 'succeeded' && <video className="drama-segment-video" aria-label={`片段版本 ${video.segmentVersion} 的视频`} controls playsInline preload="metadata" src={dramaSegmentVideoURL(...scope, video.id)} />}
      </li>)}
    </ol>}
    {!visibleVideos.length && !failure && !start.isPending && !start.isError && !query.isError && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={query.isPending ? '正在读取视频记录' : '当前片段视频尚未生成'} />}
  </>
}
