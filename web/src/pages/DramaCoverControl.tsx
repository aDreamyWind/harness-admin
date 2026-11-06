import { CheckOutlined, CloseOutlined, LoadingOutlined, PictureOutlined, UploadOutlined, ZoomInOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Alert, Button, Image, Input, Modal, Select, Skeleton, Tabs, Tag, Upload } from 'antd'
import { useEffect, useId, useState } from 'react'

import {
  applyDramaCoverHistory, chooseDramaCover, getDramaCoverGeneration, getDramaSettings, listDramaCoverHistory, listDramaModels,
  startDramaCoverGeneration, uploadDramaCover,
  type DramaCoverGeneration, type DramaProject,
} from '../api/drama'
import { useAuth } from '../auth/AuthContext'
import { CoverHistoryGallery } from './CoverHistoryGallery'
import { DramaProjectPoster } from './DramaProjectPoster'
import { coverFileTypes, useCoverUpload } from './useCoverUpload'

type CoverChoice = 'current' | 'candidate'
type CoverImageStatus = { url: string; ready: boolean }
type CoverChange = { file: File } | { revision: string }

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
    <figcaption>{url ? label : '暂无封面'}</figcaption>
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
  const historyKey = ['drama-cover-history', ...scope, project.id]
  const modelFieldId = useId()
  const instructionFieldId = useId()
  const [open, setOpen] = useState(false)
  const [tab, setTab] = useState('ai')
  const [modelCode, setModelCode] = useState<string>()
  const [instructionDraft, setInstructionDraft] = useState<string>()
  const [imageChecks, setImageChecks] = useState<Partial<Record<CoverChoice, CoverImageStatus>>>({})
  const [previewChoice, setPreviewChoice] = useState<CoverChoice>()
  const [otherPreviewOpen, setOtherPreviewOpen] = useState(false)
  const { picked, fileError, pickFile, clearFile } = useCoverUpload()
  const generation = useQuery({
    queryKey: generationKey,
    queryFn: ({ signal }) => getDramaCoverGeneration(project.id, signal),
    refetchInterval: (query) => query.state.data?.status === 'running' ? 2500 : false,
  })
  const models = useQuery({ queryKey: ['drama-cover-models', ...scope], queryFn: listDramaModels, enabled: open && tab === 'ai', staleTime: 60_000 })
  const settings = useQuery({ queryKey: ['drama-cover-settings', ...scope], queryFn: getDramaSettings, enabled: open && tab === 'ai' })
  const history = useQuery({
    queryKey: historyKey, queryFn: ({ signal }) => listDramaCoverHistory(project.id, signal), enabled: open && tab === 'history',
  })
  const imageModels = models.data?.filter((model) => model.modelType === 'image') ?? []
  const defaultModel = imageModels.find((model) => model.code === settings.data?.imageModelCode)
  const selectedModel = imageModels.find((model) => model.code === modelCode) ?? defaultModel ?? imageModels[0]
  const job = generation.data
  const instruction = instructionDraft ?? job?.instruction ?? ''

  useEffect(() => {
    if (job) onStateChange(job)
  }, [job, onStateChange])
  useEffect(() => {
    if (job?.status === 'succeeded') {
      void queryClient.invalidateQueries({ queryKey: ['drama-cover-history', user?.organizationId, user?.id, project.id] })
    }
  }, [job?.status, job?.jobId, queryClient, user?.organizationId, user?.id, project.id])

  const start = useMutation({
    mutationFn: ({ code, instruction }: { code: string; instruction: string }) => startDramaCoverGeneration(project.id, code, instruction),
    onMutate: () => queryClient.cancelQueries({ queryKey: generationKey }),
    onSuccess: (state) => queryClient.setQueryData(generationKey, state),
    onError: () => { void generation.refetch() },
  })
  const choose = useMutation({
    mutationFn: ({ jobId, choice }: { jobId: string; choice: CoverChoice }) => chooseDramaCover(project.id, jobId, choice),
    onMutate: () => queryClient.cancelQueries({ queryKey: generationKey }),
    onSuccess: (state) => {
      queryClient.setQueryData(generationKey, state)
      void queryClient.invalidateQueries({ queryKey: historyKey })
      closeDialog()
    },
    onError: () => { void generation.refetch() },
  })
  const change = useMutation({
    mutationFn: (input: CoverChange) => 'file' in input
      ? uploadDramaCover(project.id, input.file)
      : applyDramaCoverHistory(project.id, input.revision),
    onMutate: () => queryClient.cancelQueries({ queryKey: generationKey }),
    onSuccess: async (state) => {
      await queryClient.cancelQueries({ queryKey: generationKey })
      queryClient.setQueryData(generationKey, state)
      void queryClient.invalidateQueries({ queryKey: historyKey })
      clearFile()
    },
    onError: () => { void generation.refetch() },
  })
  const saving = choose.isPending || change.isPending
  const running = start.isPending || (job ? job.status === 'running' : project.coverGenerationStatus === 'running')
  const coverUrl = job ? job.coverUrl : project.coverUrl
  const candidateUrl = job?.candidateUrl
  const currentReady = imageChecks.current?.url === coverUrl && imageChecks.current?.ready === true
  const currentFailed = imageChecks.current?.url === coverUrl && imageChecks.current?.ready === false
  const candidateReady = imageChecks.candidate?.url === candidateUrl && imageChecks.candidate?.ready === true
  const candidateFailed = imageChecks.candidate?.url === candidateUrl && imageChecks.candidate?.ready === false
  const previewItems: { choice: CoverChoice; src: string; alt: string }[] = []
  if (coverUrl && currentReady) previewItems.push({ choice: 'current', src: coverUrl, alt: coverLabels.current.alt })
  if (tab === 'ai' && candidateUrl && candidateReady) previewItems.push({ choice: 'candidate', src: candidateUrl, alt: coverLabels.candidate.alt })
  const previewIndex = previewItems.findIndex(({ choice }) => choice === previewChoice)
  const previewOpen = open && previewIndex !== -1
  const dialogBlocked = saving || previewOpen || otherPreviewOpen
  const style = project.style || project.overview?.videoStyle
  const runningModel = imageModels.find((model) => model.code === job?.modelCode)?.name ?? job?.modelCode
  const selectionError = choose.error?.message
  const startError = start.error?.message
  const backgroundError = job?.status === 'failed' ? job.error || '封面生成失败，请重试。' : undefined
  const error = selectionError ?? (running ? undefined : startError ?? backgroundError)

  function closeDialog() {
    setPreviewChoice(undefined)
    setOtherPreviewOpen(false)
    setTab('ai')
    clearFile()
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
    <Modal title="漫剧封面" open={open} width={tab === 'ai' && candidateUrl ? 940 : 820} centered footer={null}
      onCancel={() => { if (!dialogBlocked) closeDialog() }} closable={!saving}
      keyboard={!dialogBlocked} focusable={{ trap: !previewOpen && !otherPreviewOpen }} mask={{ closable: !dialogBlocked }}>
      <div className="drama-cover-dialog">
        <div className="drama-cover-dialog-heading">
          <strong>{project.title}</strong>
          {style && <Tag>{style}</Tag>}
        </div>
        <Tabs activeKey={tab} onChange={(key) => { change.reset(); setTab(key) }} items={[
          { key: 'ai', label: 'AI 生成', disabled: saving && tab !== 'ai', children: <div className="drama-cover-tab">
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
                  <label htmlFor={instructionFieldId}>画面要求（选填）</label>
                  <Input.TextArea id={instructionFieldId} value={instruction} maxLength={500} rows={3}
                    onChange={(event) => setInstructionDraft(event.target.value)} placeholder="例如突出月下的古城，主角用侧脸入画" />
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
                  onClick={() => { if (selectedModel) { choose.reset(); start.mutate({ code: selectedModel.code, instruction: instruction.trim() }) } }}>
                  {coverUrl || job?.status === 'failed' ? '重新生成' : '开始生成'}
                </Button>}
              </>}
            </div>
          </div> },
          { key: 'upload', label: '自己上传', disabled: saving && tab !== 'upload', children: <div className="drama-cover-tab">
            <div className="drama-cover-single">
              {picked ? <figure>
                <div className="drama-cover-preview-tile drama-cover-upload-preview">
                  <Image src={picked.url} alt="待上传漫剧封面" preview={{ onOpenChange: setOtherPreviewOpen }} />
                </div>
                <figcaption>待上传预览</figcaption>
              </figure> : <CoverPreviewCard choice="current" title={project.title} url={coverUrl} ready={currentReady} failed={currentFailed}
                onImageCheck={recordImageCheck} onPreview={setPreviewChoice} />}
              <div className="drama-cover-settings drama-cover-upload-settings">
                <p>支持 JPG、PNG 和 WebP，最大 10 MiB，边长不超过 4096 像素。建议使用 2:3 的竖图。</p>
                <Upload accept={coverFileTypes} beforeUpload={(file) => { change.reset(); return pickFile(file) }} disabled={saving} showUploadList={false}>
                  <Button icon={<UploadOutlined />} disabled={saving}>选择图片</Button>
                </Upload>
                {picked && <p>{picked.file.name}</p>}
                {fileError && <Alert type="error" showIcon title={fileError} />}
                <Button type="primary" loading={change.isPending} disabled={!picked || saving}
                  onClick={() => picked && change.mutate({ file: picked.file })}>上传并设为封面</Button>
              </div>
            </div>
          </div> },
          { key: 'history', label: '历史封面', disabled: saving && tab !== 'history', children: <CoverHistoryGallery
            items={history.data} loading={history.isPending} error={history.isError} disabled={saving}
            applying={change.isPending && change.variables && 'revision' in change.variables ? change.variables.revision : undefined}
            onRetry={() => void history.refetch()} onApply={(revision) => change.mutate({ revision })} onPreviewChange={setOtherPreviewOpen} /> },
        ]} />
        {change.isError && <Alert type="error" showIcon title={change.error.message} />}
        {change.isSuccess && <Alert type="success" showIcon title="封面已更新" />}
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
