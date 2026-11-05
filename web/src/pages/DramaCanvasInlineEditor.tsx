import { UploadOutlined } from '@ant-design/icons'
import { Button, Input } from 'antd'
import { useId, useRef, useState, type FormEvent, type ReactNode } from 'react'

import type { DramaAssetKind, DramaCanvasCardKind } from '../api/drama'
import './drama-canvas-inline-editor.css'

const labels = { character: '角色', scene: '场景', prop: '道具', text: '文本', image: '图片', video: '视频', audio: '音频' }
const formats = {
  image: { accept: 'image/png,image/jpeg,image/gif,image/webp,.png,.jpg,.jpeg,.gif,.webp', label: 'PNG、JPEG、GIF 或 WebP', maxMiB: 20 },
  video: { accept: 'video/mp4,video/webm,.mp4,.webm', label: 'MP4 或 WebM', maxMiB: 100 },
  audio: { accept: 'audio/mpeg,audio/mp3,audio/wav,audio/x-wav,audio/ogg,application/ogg,.mp3,.wav,.ogg', label: 'MP3、WAV 或 Ogg', maxMiB: 30 },
}

export type DramaCanvasInlineInput = { title: string; content: string; file?: File }

interface DramaCanvasInlineEditorProps {
  kind: DramaAssetKind | DramaCanvasCardKind
  title: string
  content: string
  saving?: boolean
  error?: string
  isNew?: boolean
  children?: ReactNode
  onSave: (input: DramaCanvasInlineInput) => void
  onCancel: () => void
}

export function DramaCanvasInlineEditor({ kind, title: initialTitle, content: initialContent, saving, error, isNew, children, onSave, onCancel }: DramaCanvasInlineEditorProps) {
  const id = useId()
  const fileInput = useRef<HTMLInputElement>(null)
  const media = kind === 'image' || kind === 'video' || kind === 'audio' ? formats[kind] : undefined
  const asset = kind === 'character' || kind === 'scene' || kind === 'prop'
  const existingFile = media && initialContent.startsWith('/api/v1/ai-drama/') ? initialContent : ''
  const [title, setTitle] = useState(initialTitle)
  const [content, setContent] = useState(existingFile ? '' : initialContent)
  const [file, setFile] = useState<File>()
  const [validationError, setValidationError] = useState('')
  const canSave = (!asset || Boolean(title.trim())) && Boolean(content.trim() || file || existingFile || kind === 'image' || kind === 'video')
  const label = labels[kind]

  function selectFile(next?: File) {
    if (!next || !media) return
    const accepted = media.accept.split(',')
    const extension = `.${next.name.split('.').pop()?.toLowerCase()}`
    if (!accepted.includes(next.type) && !accepted.includes(extension)) {
      setValidationError(`请选择 ${media.label} 文件。`)
      return
    }
    if (!next.size || next.size > media.maxMiB * 1024 * 1024) {
      setValidationError(`请选择非空且不超过 ${media.maxMiB} MB 的文件。`)
      return
    }
    setValidationError('')
    setFile(next)
  }

  function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    event.stopPropagation()
    if (saving || !canSave || validationError) return
    const value = content.trim()
    if (media && !file && value) {
      const url = URL.canParse(value) ? new URL(value) : undefined
      if (!url || (url.protocol !== 'http:' && url.protocol !== 'https:') || url.username || url.password) {
        setValidationError('请输入有效的 HTTP 或 HTTPS 链接。')
        return
      }
    }
    onSave({ title: title.trim() || label, content: file ? '' : value || existingFile, file })
  }

  return <form className="drama-canvas-inline-editor nodrag nopan nowheel" aria-label={`${isNew ? '新建' : '编辑'}${label}节点`}
    aria-busy={saving || undefined} onSubmit={save} onDoubleClick={(event) => event.stopPropagation()}
    onKeyDown={(event) => {
      if (event.nativeEvent.isComposing || event.keyCode === 229) return
      if (event.key === 'Escape') {
        event.stopPropagation()
        if (!saving) onCancel()
      } else if (event.key === 'Enter' && (event.metaKey || event.ctrlKey)) {
        event.preventDefault()
        event.stopPropagation()
        event.currentTarget.requestSubmit()
      }
    }}>
    <label className="drama-canvas-inline-field" htmlFor={`${id}-title`}>
      <span>名称{!asset && <small>选填</small>}</span>
      <Input id={`${id}-title`} size="small" autoFocus={asset} value={title} maxLength={80} disabled={saving}
        placeholder={asset ? `输入${label}名称` : `默认名称为${label}`} onChange={(event) => setTitle(event.target.value)} />
    </label>
    {media ? <>
      {file ? <div className="drama-canvas-inline-file">
        <span title={file.name}>{file.name}</span>
        <Button size="small" type="text" disabled={saving} onClick={() => { setFile(undefined); setValidationError('') }}>改用链接</Button>
      </div> : <label className="drama-canvas-inline-field" htmlFor={`${id}-content`}>
        <span>{label}链接</span>
        <Input id={`${id}-content`} size="small" autoFocus value={content} maxLength={2048} disabled={saving}
          placeholder={existingFile ? '留空则保留当前文件' : 'https://…'}
          onChange={(event) => { setContent(event.target.value); setValidationError('') }} />
      </label>}
      <div className="drama-canvas-inline-upload">
        <input ref={fileInput} type="file" accept={media.accept} hidden disabled={saving} onChange={(event) => {
          selectFile(event.target.files?.[0])
          event.target.value = ''
        }} />
        <Button size="small" icon={<UploadOutlined />} disabled={saving} onClick={() => fileInput.current?.click()}>
          {file || existingFile ? '更换文件' : '选择本地文件'}
        </Button>
        <small>{media.label} · {media.maxMiB} MB 以内</small>
      </div>
    </> : <label className="drama-canvas-inline-field" htmlFor={`${id}-content`}>
      <span>{asset ? '形象描述' : '文本内容'}</span>
      <Input.TextArea id={`${id}-content`} size="small" autoFocus={!asset} value={content} maxLength={10000} disabled={saving}
        autoSize={{ minRows: 3, maxRows: 6 }} placeholder={asset ? `描述${label}的外观和特点` : '输入要放在画布上的文本'}
        onChange={(event) => setContent(event.target.value)} />
    </label>}
    {children}
    {(validationError || error) && <div className="drama-canvas-inline-error" role="alert">{validationError || error}</div>}
    <div className="drama-canvas-inline-actions">
      <Button size="small" type="text" disabled={saving} onClick={onCancel}>取消</Button>
      <Button size="small" type="primary" htmlType="submit" loading={saving} disabled={!canSave || Boolean(validationError)}>保存</Button>
    </div>
  </form>
}
