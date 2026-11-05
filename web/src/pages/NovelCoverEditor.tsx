import { PictureOutlined, RobotOutlined, UploadOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Alert, Button, Form, Image, Input, Modal, Select, Space, Tabs, Upload } from 'antd'
import { useEffect, useState } from 'react'

import { ApiError } from '../api/client'
import { applyNovelCover, generateNovelCover, listNovelCoverModels, uploadNovelCover, type NovelBook, type NovelCoverGenerationInput, type NovelCoverPreview, type NovelProject } from '../api/novel'
import { useAuth } from '../auth/AuthContext'
import { DramaStylePicker } from './DramaStylePicker'
import './novel-cover-editor.css'

const maxCoverBytes = 10 * 1024 * 1024
const coverFileTypes = 'image/jpeg,image/png,image/webp'
type CoverChange = { file: File } | { token: string }
type CoverCandidate = NovelCoverPreview & { applyError?: string }

export function NovelCoverEditor({ book, onClose }: { book: NovelBook; onClose: () => void }) {
  const { user } = useAuth()
  const cache = useQueryClient()
  const scope = [user?.organizationId, user?.id]
  const [tab, setTab] = useState('ai')
  const [model, setModel] = useState<string>()
  const [styleId, setStyleId] = useState<string>()
  const [instruction, setInstruction] = useState('')
  const [candidate, setCandidate] = useState<CoverCandidate>()
  const [candidateImage, setCandidateImage] = useState<{ token: string; status: 'loaded' | 'failed' }>()
  const [picked, setPicked] = useState<{ file: File; url: string }>()
  const [fileError, setFileError] = useState('')
  const [discardOpen, setDiscardOpen] = useState(false)
  const models = useQuery({ queryKey: ['novel-cover-models', ...scope], queryFn: listNovelCoverModels, enabled: tab === 'ai' })
  const availableModels = models.data ?? []
  const activeModel = availableModels.find((item) => item.code === model) ?? availableModels[0]
  const generation = useMutation({
    mutationFn: (input: NovelCoverGenerationInput) => generateNovelCover(book.id, input),
    onSuccess: setCandidate,
  })
  const change = useMutation({
    mutationFn: (input: CoverChange) => 'file' in input
      ? uploadNovelCover(book.id, input.file)
      : applyNovelCover(book.id, input.token),
    onSuccess: async (cover) => {
      const listKey = ['novel-books', ...scope]
      const projectKey = ['novel-project', ...scope, book.id]
      await Promise.all([
        cache.cancelQueries({ queryKey: listKey }),
        cache.cancelQueries({ queryKey: projectKey }),
      ])
      cache.setQueryData<NovelBook[]>(listKey, (books) => books?.map((item) => item.id === book.id ? { ...item, cover } : item))
      cache.setQueryData<NovelProject>(projectKey, (project) => project && { ...project, cover })
      setCandidate(undefined)
      setPicked(undefined)
    },
    onError: (error, input) => {
      if (!(error instanceof ApiError)) return
      if ('token' in input && ['novel_cover_preview_expired', 'novel_cover_conflict'].includes(error.code)) {
        setCandidate((previous) => previous?.token === input.token ? { ...previous, applyError: error.message } : previous)
      }
      if (error.status === 409) {
        void cache.invalidateQueries({ queryKey: ['novel-books', ...scope] })
        void cache.invalidateQueries({ queryKey: ['novel-project', ...scope, book.id] })
      }
    },
  })
  const busy = generation.isPending || change.isPending
  const imageStatus = candidateImage?.token === candidate?.token ? candidateImage?.status : undefined

  useEffect(() => () => { if (picked) URL.revokeObjectURL(picked.url) }, [picked])

  function pickFile(file: File) {
    change.reset()
    setPicked(undefined)
    if (file.size === 0 || file.size > maxCoverBytes) {
      setFileError('请选择不超过 10 MB 的图片。')
      return Upload.LIST_IGNORE
    }
    if (file.type && !coverFileTypes.split(',').includes(file.type)) {
      setFileError('请选择 JPG、PNG 或 WebP 图片。')
      return Upload.LIST_IGNORE
    }
    setFileError('')
    setPicked({ file, url: URL.createObjectURL(file) })
    return Upload.LIST_IGNORE
  }

  function generatePreview() {
    if (!activeModel || busy) return
    change.reset()
    generation.mutate({ modelCode: activeModel.code, instruction: instruction.trim(), styleId })
  }

  function applyPreview() {
    if (!candidate || candidate.applyError || imageStatus !== 'loaded' || busy) return
    generation.reset()
    change.mutate({ token: candidate.token })
  }

  function closeEditor() {
    if (busy) return
    if (candidate) setDiscardOpen(true)
    else onClose()
  }

  const preview = (tab === 'upload' ? picked?.url : undefined) ?? book.cover?.url
  const shownCandidate = tab === 'ai' ? candidate : undefined
  const imageError = imageStatus === 'failed' ? '候选封面无法加载，请重新生成。' : undefined
  const error = (tab === 'ai' ? generation.error?.message ?? candidate?.applyError ?? imageError : undefined) ?? change.error?.message

  return <>
    <Modal title={`设置封面 · ${book.title}`} open footer={null} onCancel={closeEditor} width={shownCandidate ? 820 : 620}
      closable={!busy} keyboard={!busy} mask={{ closable: !busy && !candidate }}>
      <div className={`novel-cover-editor${shownCandidate ? ' novel-cover-editor--comparison' : ''}`}>
        <div className="novel-cover-previews">
          <figure className="novel-cover-preview-item">
            <div className="novel-cover-preview">
              {preview ? <img src={preview} alt={tab === 'upload' && picked ? '待上传封面预览' : '当前封面'} />
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
                preview={imageStatus === 'loaded'}
                onError={() => setCandidateImage({ token: shownCandidate.token, status: 'failed' })} />
            </div>
            <figcaption>候选封面</figcaption>
          </figure>}
        </div>
        <Tabs activeKey={tab} onChange={(key) => {
          change.reset()
          generation.reset()
          setCandidateImage(undefined)
          setTab(key)
        }} items={[
          { key: 'ai', label: 'AI 生成', disabled: busy && tab !== 'ai', children: <div className="novel-cover-fields">
            <p>AI 会参考已保存的故事灵感和大纲。点「设为封面」后才会替换当前封面。</p>
            <Form layout="vertical" disabled={busy}>
              <Form.Item label="图片模型" htmlFor="novel-cover-model">
                <Select id="novel-cover-model" value={activeModel?.code} onChange={setModel}
                  placeholder="暂无可用图片模型" loading={models.isPending}
                  options={availableModels.map((item) => ({ value: item.code, label: item.name }))} />
              </Form.Item>
              <Form.Item label="画面风格（选填）" htmlFor="novel-cover-style">
                <div className="novel-cover-style">
                  <DramaStylePicker id="novel-cover-style" value={styleId} valueLabel={styleId ? undefined : '不限风格'}
                    onChange={(style) => setStyleId(style.id)} disabled={busy} />
                  {styleId && <Button type="link" size="small" aria-label="清除画面风格" onClick={() => setStyleId(undefined)}>清除</Button>}
                </div>
              </Form.Item>
              <Form.Item label={candidate ? '画面要求与修改意见（选填）' : '画面要求（选填）'} htmlFor="novel-cover-instruction">
                <Input.TextArea id="novel-cover-instruction" rows={candidate ? 3 : 2} maxLength={1200} value={instruction}
                  onChange={(event) => setInstruction(event.target.value)}
                  placeholder={candidate ? '补充想调整的地方，例如把雨夜改成清晨' : '例如突出雨夜里的孤舟'} />
              </Form.Item>
            </Form>
            {models.isError && <Alert type="error" title={models.error.message}
              action={<Button size="small" onClick={() => void models.refetch()}>重试</Button>} />}
            {models.isSuccess && !activeModel && <Alert type="info" title="当前没有可用的图片模型，可以切换到自己上传。" />}
            <Space wrap>
              <Button type={candidate ? 'default' : 'primary'} icon={<RobotOutlined />} loading={generation.isPending}
                disabled={!activeModel || change.isPending} onClick={generatePreview}>
                {generation.isPending ? '正在生成预览' : candidate ? '重新生成' : '生成预览'}
              </Button>
              {candidate && <Button type="primary" loading={change.isPending}
                disabled={generation.isPending || imageStatus !== 'loaded' || Boolean(candidate.applyError)}
                onClick={applyPreview}>设为封面</Button>}
            </Space>
          </div> },
          { key: 'upload', label: '自己上传', disabled: busy && tab !== 'upload', children: <div className="novel-cover-fields">
            <p>支持 JPG、PNG 和 WebP，最大 10 MB，边长不超过 4096 像素。建议使用 2:3 的竖图。</p>
            <Upload accept={coverFileTypes} beforeUpload={pickFile} disabled={busy} showUploadList={false}>
              <Button icon={<UploadOutlined />} disabled={busy}>选择图片</Button>
            </Upload>
            {picked && <p className="novel-cover-filename">{picked.file.name}</p>}
            {fileError && <Alert type="error" title={fileError} />}
            <Button type="primary" loading={change.isPending} disabled={!picked}
              onClick={() => picked && change.mutate({ file: picked.file })}>上传并设为封面</Button>
          </div> },
        ]} />
      </div>
      {error && <Alert type="error" showIcon title={error} />}
      {change.isSuccess && <Alert type="success" showIcon title="封面已更新" />}
    </Modal>
    <Modal title="放弃候选封面？" open={discardOpen} onCancel={() => setDiscardOpen(false)} onOk={onClose}
      okText="放弃预览" cancelText="继续编辑" okButtonProps={{ danger: true }}>
      <p>候选封面还未应用。关闭后需要重新生成预览。</p>
    </Modal>
  </>
}
