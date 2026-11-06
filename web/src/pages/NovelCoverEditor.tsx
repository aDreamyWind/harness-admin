import { LoadingOutlined, PictureOutlined, RobotOutlined, UploadOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Alert, Button, Form, Image, Input, Modal, Select, Space, Tabs, Upload } from 'antd'
import { useEffect, useState } from 'react'

import { ApiError } from '../api/client'
import { applyNovelCover, applyNovelCoverHistory, getNovelCoverGeneration, listNovelCoverHistory, listNovelCoverModels, startNovelCoverGeneration, uploadNovelCover,
  type NovelBook, type NovelCoverGeneration, type NovelCoverGenerationInput, type NovelProject } from '../api/novel'
import { useAuth } from '../auth/AuthContext'
import { CoverHistoryGallery } from './CoverHistoryGallery'
import { DramaStylePicker } from './DramaStylePicker'
import { coverFileTypes, useCoverUpload } from './useCoverUpload'
import './novel-cover-editor.css'

type CoverChange = { file: File } | { token: string } | { revision: string }

export function NovelCoverEditor({ book, onClose }: { book: NovelBook; onClose: () => void }) {
  const { user } = useAuth()
  const cache = useQueryClient()
  const scope = [user?.organizationId, user?.id]
  const generationKey = ['novel-cover-generation', ...scope, book.id]
  const historyKey = ['novel-cover-history', ...scope, book.id]
  const [tab, setTab] = useState('ai')
  const [draft, setDraft] = useState<Partial<NovelCoverGenerationInput>>({})
  const [rejectedToken, setRejectedToken] = useState<string>()
  const [candidateImage, setCandidateImage] = useState<{ token: string; status: 'loaded' | 'failed' }>()
  const [previewOpen, setPreviewOpen] = useState(false)
  const { picked, fileError, pickFile, clearFile } = useCoverUpload()
  const models = useQuery({ queryKey: ['novel-cover-models', ...scope], queryFn: listNovelCoverModels, enabled: tab === 'ai' })
  const history = useQuery({
    queryKey: historyKey, queryFn: ({ signal }) => listNovelCoverHistory(book.id, signal), enabled: tab === 'history',
  })
  const coverGeneration = useQuery({
    queryKey: generationKey,
    queryFn: ({ signal }) => getNovelCoverGeneration(book.id, signal),
    refetchInterval: (query) => query.state.data?.status === 'running' ? 2500 : false,
    refetchOnMount: 'always',
    refetchOnWindowFocus: 'always',
  })
  const job = coverGeneration.data
  const candidate = job?.preview
  const model = draft.modelCode ?? job?.modelCode
  const styleId = (draft.styleId ?? job?.styleId) || undefined
  const instruction = draft.instruction ?? job?.instruction ?? ''
  const availableModels = models.data ?? []
  const activeModel = availableModels.find((item) => item.code === model) ?? availableModels[0]
  const generation = useMutation({
    mutationFn: (input: NovelCoverGenerationInput) => startNovelCoverGeneration(book.id, input),
    onSuccess: async (current) => {
      await cache.cancelQueries({ queryKey: generationKey })
      cache.setQueryData(generationKey, current)
      void cache.invalidateQueries({ queryKey: generationKey })
    },
    onError: () => { void cache.invalidateQueries({ queryKey: generationKey }) },
  })
  const change = useMutation({
    mutationFn: (input: CoverChange) => {
      if ('file' in input) return uploadNovelCover(book.id, input.file)
      if ('revision' in input) return applyNovelCoverHistory(book.id, input.revision)
      return applyNovelCover(book.id, input.token)
    },
    onSuccess: async (cover) => {
      const listKey = ['novel-books', ...scope]
      const projectKey = ['novel-project', ...scope, book.id]
      await Promise.all([
        cache.cancelQueries({ queryKey: listKey }),
        cache.cancelQueries({ queryKey: projectKey }),
        cache.cancelQueries({ queryKey: generationKey }),
      ])
      cache.setQueryData<NovelBook[]>(listKey, (books) => books?.map((item) => item.id === book.id ? { ...item, cover } : item))
      cache.setQueryData<NovelProject>(projectKey, (project) => project && { ...project, cover })
      cache.setQueryData<NovelCoverGeneration>(generationKey, (current) => current && { ...current, preview: undefined })
      void cache.invalidateQueries({ queryKey: generationKey })
      void cache.invalidateQueries({ queryKey: historyKey })
      clearFile()
    },
    onError: (error, input) => {
      if (!(error instanceof ApiError)) return
      if ('token' in input && ['novel_cover_preview_expired', 'novel_cover_conflict'].includes(error.code)) {
        setRejectedToken(input.token)
        void cache.invalidateQueries({ queryKey: generationKey })
      }
      if (error.status === 409) {
        void cache.invalidateQueries({ queryKey: ['novel-books', ...scope] })
        void cache.invalidateQueries({ queryKey: ['novel-project', ...scope, book.id] })
      }
    },
  })
  const generating = generation.isPending || job?.status === 'running'
  const busy = generating || change.isPending
  const imageStatus = candidateImage?.token === candidate?.token ? candidateImage?.status : undefined

  useEffect(() => {
    if (job?.status === 'succeeded') {
      void cache.invalidateQueries({ queryKey: ['novel-cover-history', user?.organizationId, user?.id, book.id] })
    }
  }, [job?.status, job?.jobId, cache, user?.organizationId, user?.id, book.id])

  function generatePreview() {
    if (!activeModel || !job || busy) return
    change.reset()
    generation.mutate({ modelCode: activeModel.code, instruction: instruction.trim(), styleId })
  }

  function applyPreview() {
    if (!candidate || candidate.token === rejectedToken || imageStatus !== 'loaded' || busy) return
    generation.reset()
    change.mutate({ token: candidate.token })
  }

  const preview = (tab === 'upload' ? picked?.url : undefined) ?? book.cover?.url
  const shownCandidate = tab === 'ai' ? candidate : undefined
  const imageError = imageStatus === 'failed' ? '候选封面无法加载，请重新生成。' : undefined
  const error = change.error?.message ?? (tab === 'ai' ? (generating ? undefined : generation.error?.message) ?? imageError : undefined)

  return (
    <Modal title={`设置封面 · ${book.title}`} open centered footer={null} width={shownCandidate ? 940 : 820}
      onCancel={() => { if (!change.isPending && !previewOpen) onClose() }} closable={!change.isPending}
      keyboard={!change.isPending && !previewOpen} focusable={{ trap: !previewOpen }} mask={{ closable: !change.isPending && !previewOpen }}>
      <div className={`novel-cover-editor${shownCandidate ? ' novel-cover-editor--comparison' : ''}${tab === 'history' ? ' novel-cover-editor--history' : ''}`}>
        {tab !== 'history' && <div className="novel-cover-previews">
          <figure className="novel-cover-preview-item">
            <div className="novel-cover-preview">
              {preview ? <Image src={preview} alt={tab === 'upload' && picked ? '待上传封面预览' : '当前封面'}
                preview={{ onOpenChange: setPreviewOpen }} />
                : <span><PictureOutlined aria-hidden="true" /> 暂无封面</span>}
            </div>
            {shownCandidate && <figcaption>当前封面</figcaption>}
          </figure>
          {shownCandidate && <figure className="novel-cover-preview-item novel-cover-preview-item--candidate">
            <div className="novel-cover-preview"
              onLoadCapture={(event) => {
                if (event.target instanceof HTMLImageElement && event.currentTarget.contains(event.target)) {
                  setCandidateImage({ token: shownCandidate.token, status: 'loaded' })
                }
              }}>
              <Image key={shownCandidate.token} src={shownCandidate.previewUrl} alt="候选封面预览"
                preview={imageStatus === 'loaded' ? { onOpenChange: setPreviewOpen } : false}
                onError={() => setCandidateImage({ token: shownCandidate.token, status: 'failed' })} />
            </div>
            <figcaption>{generating || job?.status === 'failed' ? '上一次预览' : '候选封面'}</figcaption>
          </figure>}
        </div>}
        <Tabs activeKey={tab} onChange={(key) => {
          change.reset()
          setCandidateImage(undefined)
          setTab(key)
        }} items={[
          { key: 'ai', label: 'AI 生成', disabled: change.isPending && tab !== 'ai', children: <div className="novel-cover-fields">
            <p>AI 会参考已保存的故事灵感和大纲。点「设为封面」后才会替换当前封面。</p>
            {generating && <Alert type="info" showIcon icon={<LoadingOutlined />}
              title={job?.status === 'running' ? '正在后台生成封面预览' : '正在提交生成任务'}
              description={job?.status === 'running' ? '可以关闭弹窗或离开页面，回来后打开「设置封面」查看结果。' : undefined} />}
            {coverGeneration.isPending && !generating && <Alert type="info" title="正在读取封面预览" />}
            {coverGeneration.isError && <Alert type="warning" title="暂时无法读取封面生成状态"
              action={<Button size="small" onClick={() => void coverGeneration.refetch()}>重试</Button>} />}
            {!generating && job?.status === 'failed' && <Alert type="error" showIcon
              title={job.errorMessage || '封面预览生成失败，请重试。'} />}
            <Form layout="vertical" disabled={busy}>
              <Form.Item label="图片模型" htmlFor="novel-cover-model">
                <Select id="novel-cover-model" value={activeModel?.code}
                  onChange={(modelCode: string) => setDraft((current) => ({ ...current, modelCode }))}
                  placeholder="暂无可用图片模型" loading={models.isPending}
                  options={availableModels.map((item) => ({ value: item.code, label: item.name }))} />
              </Form.Item>
              <Form.Item label="画面风格（选填）" htmlFor="novel-cover-style">
                <div className="novel-cover-style">
                  <DramaStylePicker id="novel-cover-style" value={styleId} valueLabel={styleId ? undefined : '不限风格'}
                    onChange={(style) => setDraft((current) => ({ ...current, styleId: style.id }))} disabled={busy} />
                  {styleId && <Button type="link" size="small" aria-label="清除画面风格"
                    onClick={() => setDraft((current) => ({ ...current, styleId: '' }))}>清除</Button>}
                </div>
              </Form.Item>
              <Form.Item label={candidate ? '画面要求与修改意见（选填）' : '画面要求（选填）'} htmlFor="novel-cover-instruction">
                <Input.TextArea id="novel-cover-instruction" rows={candidate ? 3 : 2} maxLength={1200} value={instruction}
                  onChange={(event) => {
                    const text = event.target.value
                    setDraft((current) => ({ ...current, instruction: text }))
                  }}
                  placeholder={candidate ? '补充想调整的地方，例如把雨夜改成清晨' : '例如突出雨夜里的孤舟'} />
              </Form.Item>
            </Form>
            {models.isError && <Alert type="error" title={models.error.message}
              action={<Button size="small" onClick={() => void models.refetch()}>重试</Button>} />}
            {models.isSuccess && !activeModel && <Alert type="info" title="当前没有可用的图片模型，可以切换到自己上传。" />}
            <Space wrap>
              <Button type={candidate ? 'default' : 'primary'} icon={<RobotOutlined />} loading={generating}
                disabled={!activeModel || !job || busy} onClick={generatePreview}>
                {generating ? '正在生成预览' : candidate || job?.status === 'failed' ? '重新生成' : '生成预览'}
              </Button>
              {candidate && <Button type="primary" loading={change.isPending}
                disabled={busy || imageStatus !== 'loaded' || candidate.token === rejectedToken}
                onClick={applyPreview}>设为封面</Button>}
            </Space>
          </div> },
          { key: 'upload', label: '自己上传', disabled: change.isPending && tab !== 'upload', children: <div className="novel-cover-fields">
            <p>支持 JPG、PNG 和 WebP，最大 10 MiB，边长不超过 4096 像素。建议使用 2:3 的竖图。</p>
            <Upload accept={coverFileTypes} beforeUpload={(file) => { change.reset(); return pickFile(file) }} disabled={change.isPending} showUploadList={false}>
              <Button icon={<UploadOutlined />} disabled={change.isPending}>选择图片</Button>
            </Upload>
            {picked && <p className="novel-cover-filename">{picked.file.name}</p>}
            {fileError && <Alert type="error" title={fileError} />}
            {generating && <Alert type="info" title="封面预览正在生成，请等待完成后再替换当前封面。" />}
            <Button type="primary" loading={change.isPending} disabled={!picked || generating}
              onClick={() => picked && change.mutate({ file: picked.file })}>上传并设为封面</Button>
          </div> },
          { key: 'history', label: '历史封面', disabled: change.isPending && tab !== 'history', children: <CoverHistoryGallery
            items={history.data} loading={history.isPending} error={history.isError} disabled={busy}
            applying={change.isPending && change.variables && 'revision' in change.variables ? change.variables.revision : undefined}
            onRetry={() => void history.refetch()} onApply={(revision) => change.mutate({ revision })} onPreviewChange={setPreviewOpen} /> },
        ]} />
      </div>
      {error && <Alert type="error" showIcon title={error} />}
      {change.isSuccess && <Alert type="success" showIcon title="封面已更新" />}
    </Modal>
  )
}
