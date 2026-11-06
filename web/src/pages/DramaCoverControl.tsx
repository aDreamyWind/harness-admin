import { CheckOutlined, CloseOutlined, LoadingOutlined, PictureOutlined, ZoomInOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Alert, Button, Image, Modal, Select, Skeleton, Tag } from 'antd'
import { useEffect, useId, useState } from 'react'

import {
  chooseDramaCover, getDramaCoverGeneration, getDramaSettings, listDramaModels, startDramaCoverGeneration,
  type DramaCoverGeneration, type DramaProject,
} from '../api/drama'
import { useAuth } from '../auth/AuthContext'
import { DramaProjectPoster } from './DramaProjectPoster'

type CoverChoice = 'current' | 'candidate'
type CoverImageStatus = { url: string; ready: boolean }

const coverLabels = {
  current: { label: '当前封面', alt: '当前漫剧封面' },
  candidate: { label: '新封面', alt: '新漫剧封面' },
}

function CoverPreviewCard({ choice, title, url, ready, failed, onImageCheck, onPreview }: {
  choice: CoverChoice; title: string; url?: string; ready: boolean; failed: boolean
  onImageCheck: (choice: CoverChoice, url: string, ready: boolean) => void
  onPreview: (choice: CoverChoice) => void
}) {
  const { label, alt } = coverLabels[choice]
  return <figure
    onLoadCapture={(event) => { if (url && event.target instanceof HTMLImageElement) onImageCheck(choice, url, true) }}
    onErrorCapture={(event) => { if (url && event.target instanceof HTMLImageElement) onImageCheck(choice, url, false) }}>
    {url ? <button type="button" className="drama-cover-preview-tile" aria-label={`预览${label}`} disabled={!ready}
      onClick={(event) => { event.currentTarget.focus({ preventScroll: true }); onPreview(choice) }}>
      <DramaProjectPoster key={url} title={title} url={url} alt={alt} />
      {!failed && <span className="drama-cover-preview-mark" aria-hidden="true"><ZoomInOutlined /> 放大</span>}
    </button> : <div className="drama-cover-preview-tile"><DramaProjectPoster title={title} /></div>}
    <figcaption>{url ? label : '尚未生成封面'}</figcaption>
  </figure>
}

export function DramaCoverControl({ project, disabled, onStateChange }: {
  project: DramaProject
  disabled?: boolean
  onStateChange: (state: DramaCoverGeneration) => void
}) {
  const { user } = useAuth()
  const queryClient = useQueryClient()
  const scope = [user?.organizationId, user?.id]
  const generationKey = ['drama-cover-generation', ...scope, project.id]
  const modelFieldId = useId()
  const [open, setOpen] = useState(false)
  const [modelCode, setModelCode] = useState<string>()
  const [imageChecks, setImageChecks] = useState<Partial<Record<CoverChoice, CoverImageStatus>>>({})
  const [previewChoice, setPreviewChoice] = useState<CoverChoice>()
  const generation = useQuery({
    queryKey: generationKey,
    queryFn: ({ signal }) => getDramaCoverGeneration(project.id, signal),
    refetchInterval: (query) => query.state.data?.status === 'running' ? 2500 : false,
  })
  const models = useQuery({ queryKey: ['drama-cover-models', ...scope], queryFn: listDramaModels, enabled: open, staleTime: 60_000 })
  const settings = useQuery({ queryKey: ['drama-cover-settings', ...scope], queryFn: getDramaSettings, enabled: open })
  const imageModels = models.data?.filter((model) => model.modelType === 'image') ?? []
  const defaultModel = imageModels.find((model) => model.code === settings.data?.imageModelCode)
  const selectedModel = imageModels.find((model) => model.code === modelCode) ?? defaultModel ?? imageModels[0]
  const job = generation.data

  useEffect(() => {
    if (job) onStateChange(job)
  }, [job, onStateChange])

  const start = useMutation({
    mutationFn: (code: string) => startDramaCoverGeneration(project.id, code),
    onMutate: () => queryClient.cancelQueries({ queryKey: generationKey }),
    onSuccess: (state) => queryClient.setQueryData(generationKey, state),
    onError: () => { void generation.refetch() },
  })
  const choose = useMutation({
    mutationFn: ({ jobId, choice }: { jobId: string; choice: CoverChoice }) => chooseDramaCover(project.id, jobId, choice),
    onMutate: () => queryClient.cancelQueries({ queryKey: generationKey }),
    onSuccess: (state) => {
      queryClient.setQueryData(generationKey, state)
      closeDialog()
    },
    onError: () => { void generation.refetch() },
  })
  const running = start.isPending || (job ? job.status === 'running' : project.coverGenerationStatus === 'running')
  const coverUrl = job ? job.coverUrl : project.coverUrl
  const candidateUrl = job?.candidateUrl
  const currentReady = imageChecks.current?.url === coverUrl && imageChecks.current?.ready === true
  const currentFailed = imageChecks.current?.url === coverUrl && imageChecks.current?.ready === false
  const candidateReady = imageChecks.candidate?.url === candidateUrl && imageChecks.candidate?.ready === true
  const candidateFailed = imageChecks.candidate?.url === candidateUrl && imageChecks.candidate?.ready === false
  const previewItems: { choice: CoverChoice; src: string; alt: string }[] = []
  if (coverUrl && currentReady) previewItems.push({ choice: 'current', src: coverUrl, alt: coverLabels.current.alt })
  if (candidateUrl && candidateReady) previewItems.push({ choice: 'candidate', src: candidateUrl, alt: coverLabels.candidate.alt })
  const previewIndex = previewItems.findIndex(({ choice }) => choice === previewChoice)
  const previewOpen = open && previewIndex !== -1
  const style = project.style || project.overview?.videoStyle
  const runningModel = imageModels.find((model) => model.code === job?.modelCode)?.name ?? job?.modelCode
  const selectionError = choose.error?.message
  const startError = start.error?.message
  const backgroundError = job?.status === 'failed' ? job.error || '封面生成失败，请重试。' : undefined
  const error = selectionError ?? (running ? undefined : startError ?? backgroundError)

  function closeDialog() {
    setPreviewChoice(undefined)
    setOpen(false)
  }

  function recordImageCheck(choice: CoverChoice, url: string, ready: boolean) {
    setImageChecks((checks) => checks[choice]?.url === url && checks[choice]?.ready === ready
      ? checks : { ...checks, [choice]: { url, ready } })
  }

  function submitChoice(choice: CoverChoice) {
    if (!job?.jobId || !candidateUrl || choose.isPending || (choice === 'candidate' && !candidateReady)) return
    choose.mutate({ jobId: job.jobId, choice })
  }

  return <>
    <Button
      className="drama-cover-trigger"
      icon={running ? <LoadingOutlined aria-hidden="true" /> : <PictureOutlined aria-hidden="true" />}
      disabled={disabled}
      onClick={() => setOpen(true)}
    >{running ? '封面生成中' : candidateUrl ? '选择漫剧封面' : '生成漫剧封面'}</Button>
    <Modal title="漫剧封面" open={open} width={candidateUrl ? 940 : 820} centered footer={null}
      onCancel={() => { if (!choose.isPending && !previewOpen) closeDialog() }} closable={!choose.isPending}
      keyboard={!choose.isPending && !previewOpen} focusable={{ trap: !previewOpen }}
      mask={{ closable: !choose.isPending && !previewOpen }}>
      <div className="drama-cover-dialog">
        <div className="drama-cover-dialog-heading">
          <strong>{project.title}</strong>
          {style && <Tag>{style}</Tag>}
        </div>
        <div className={candidateUrl ? 'drama-cover-comparison' : 'drama-cover-single'}>
          <CoverPreviewCard choice="current" title={project.title} url={coverUrl} ready={currentReady} failed={currentFailed}
            onImageCheck={recordImageCheck} onPreview={setPreviewChoice} />
          {candidateUrl ? <CoverPreviewCard choice="candidate" title={project.title} url={candidateUrl} ready={candidateReady} failed={candidateFailed}
            onImageCheck={recordImageCheck} onPreview={setPreviewChoice} /> : <div className="drama-cover-settings">
            <p>根据故事内容和当前漫剧风格制作竖版封面，成图包含剧名。</p>
            {running ? <div className="drama-cover-running" role="status">
              <LoadingOutlined aria-hidden="true" />
              <div><strong>正在后台生成封面</strong>
                <span>{runningModel ? `使用 ${runningModel} · ` : ''}关闭窗口后会继续生成。</span>
              </div>
            </div> : <>
              <label htmlFor={modelFieldId}>图片模型</label>
              <Select id={modelFieldId} aria-label="图片模型" value={selectedModel?.code}
                onChange={setModelCode} loading={models.isFetching || settings.isLoading} disabled={settings.isLoading} placeholder="选择图片模型"
                options={imageModels.map((model) => ({ value: model.code, label: model.name }))}
                notFoundContent="暂无可用的图片模型" />
              <p className="drama-cover-helper">{coverUrl ? '新封面生成后，可与当前封面对比再选用。' : '首张封面完成后会自动用于项目卡片。'}</p>
            </>}
          </div>}
        </div>
        {generation.isPending && !running && <Skeleton paragraph={{ rows: 1 }} title={false} active />}
        {generation.isError && <Alert type="warning" showIcon title="暂时无法读取封面状态"
          action={<Button size="small" onClick={() => void generation.refetch()}>重试</Button>} />}
        {models.isError && !running && !candidateUrl && <Alert type="warning" showIcon title="读取图片模型失败"
          action={<Button size="small" onClick={() => void models.refetch()}>重试</Button>} />}
        {candidateFailed && <Alert type="warning" showIcon title="新封面加载失败，请保留当前封面后再试。" />}
        {error && <Alert type="error" showIcon title={error} />}
        <div className="drama-cover-actions">
          {candidateUrl ? <>
            <Button disabled={choose.isPending} onClick={() => submitChoice('current')}>保留当前封面</Button>
            <Button type="primary" icon={<CheckOutlined aria-hidden="true" />} loading={choose.isPending && choose.variables?.choice === 'candidate'}
              disabled={!candidateReady || choose.isPending} onClick={() => submitChoice('candidate')}>采用新封面</Button>
          </> : <>
            <Button onClick={closeDialog}>关闭</Button>
            {!running && <Button type="primary" icon={<PictureOutlined aria-hidden="true" />}
              disabled={!selectedModel || !job || generation.isError || settings.isLoading}
              onClick={() => { if (selectedModel) { choose.reset(); start.mutate(selectedModel.code) } }}>
              {coverUrl || job?.status === 'failed' ? '重新生成' : '开始生成'}
            </Button>}
          </>}
        </div>
        <Image.PreviewGroup items={previewItems.map(({ src, alt }) => ({ src, alt }))}
          classNames={{ popup: { root: 'drama-cover-lightbox' } }}
          preview={{
            open: previewOpen,
            current: Math.max(previewIndex, 0),
            alt: previewItems[previewIndex]?.alt,
            closeIcon: <CloseOutlined aria-label="关闭大图预览" />,
            onOpenChange: (visible) => { if (!visible) setPreviewChoice(undefined) },
            onChange: (index) => setPreviewChoice(previewItems[index]?.choice),
            countRender: (index, total) => `${coverLabels[previewItems[index - 1]?.choice ?? 'current'].label} · ${index} / ${total}`,
          }} />
      </div>
    </Modal>
  </>
}
