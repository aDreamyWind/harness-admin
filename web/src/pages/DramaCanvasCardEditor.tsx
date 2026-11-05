import { UploadOutlined } from '@ant-design/icons'
import { Button, Form, Input, Modal, Radio, Space, Upload } from 'antd'
import { useId, useState } from 'react'

import type { DramaCanvasCard } from '../api/drama'

const cardLabels = { text: '文本', image: '图片', video: '视频', audio: '音频' }
const mediaFormats = {
  image: { accept: 'image/png,image/jpeg,image/gif,image/webp,.png,.jpg,.jpeg,.gif,.webp', label: 'PNG、JPEG、GIF 或 WebP', maxMiB: 20 },
  video: { accept: 'video/mp4,video/webm,.mp4,.webm', label: 'MP4 或 WebM', maxMiB: 100 },
  audio: { accept: 'audio/mpeg,audio/mp3,audio/wav,audio/x-wav,audio/ogg,application/ogg,.mp3,.wav,.ogg', label: 'MP3、WAV 或 Ogg', maxMiB: 30 },
}

interface CardEditorProps {
  draft: DramaCanvasCard | null
  editing: boolean
  busy?: boolean
  onCancel: () => void
  onSave: (draft: DramaCanvasCard, file?: File) => void
}

export function DramaCanvasCardEditor({ draft, ...props }: CardEditorProps) {
  return draft && <CardEditor key={draft.id} draft={draft} {...props} />
}

function CardEditor({ draft, editing, busy, onCancel, onSave }: Omit<CardEditorProps, 'draft'> & { draft: DramaCanvasCard }) {
  const id = useId()
  const label = cardLabels[draft.kind]
  const format = draft.kind === 'text' ? undefined : mediaFormats[draft.kind]
  const uploaded = !!format && draft.content.startsWith('/api/v1/ai-drama/')
  const [title, setTitle] = useState(draft.title)
  const [content, setContent] = useState(uploaded ? '' : draft.content)
  const [source, setSource] = useState<'link' | 'file'>(uploaded ? 'file' : 'link')
  const [file, setFile] = useState<File>()
  const [error, setError] = useState('')
  const selectingFile = !!format && source === 'file'
  const canSave = selectingFile ? !!file || uploaded : !!content.trim()

  function pickFile(next: File) {
    if (!format) return Upload.LIST_IGNORE
    const accepted = format.accept.split(',')
    const extension = `.${next.name.split('.').pop()?.toLowerCase()}`
    if (!accepted.includes(next.type) && !accepted.includes(extension)) {
      setError(`请选择 ${format.label} 文件。`)
    } else if (next.size === 0 || next.size > format.maxMiB * 1024 * 1024) {
      setError(`请选择非空且不超过 ${format.maxMiB} MB 的文件。`)
    } else {
      setFile(next)
      setError('')
    }
    return Upload.LIST_IGNORE
  }

  function save() {
    if (!canSave || busy) return
    let value = content.trim()
    if (selectingFile) {
      value = file ? '' : draft.content
    } else if (format) {
      const url = URL.canParse(value) ? new URL(value) : undefined
      if (!url || (url.protocol !== 'https:' && url.protocol !== 'http:') || url.username || url.password) {
        setError('请输入有效的 HTTP 或 HTTPS 链接。')
        return
      }
    }
    onSave({ ...draft, title: title.trim() || label, content: value }, selectingFile ? file : undefined)
  }

  return <Modal open title={`${editing ? '编辑' : '添加'}${label}节点`} onOk={save}
    onCancel={() => { if (!busy) onCancel() }} okText="保存" cancelText="取消" confirmLoading={busy}
    closable={!busy} keyboard={!busy} mask={{ closable: !busy }}
    okButtonProps={{ disabled: !canSave || !!error }} cancelButtonProps={{ disabled: busy }}>
    <Form layout="vertical" disabled={busy}>
      <Form.Item label="名称" htmlFor={`${id}-title`}>
        <Input id={`${id}-title`} value={title} maxLength={80} onChange={(event) => setTitle(event.target.value)} />
      </Form.Item>
      {format && <Form.Item label="媒体来源">
        <Radio.Group aria-label="媒体来源" value={source} onChange={(event) => { setSource(event.target.value); setError('') }}>
          <Radio.Button value="link">链接</Radio.Button>
          <Radio.Button value="file">本地文件</Radio.Button>
        </Radio.Group>
      </Form.Item>}
      {selectingFile && format ? <Form.Item label="媒体文件" validateStatus={error ? 'error' : undefined}
        help={error || `支持 ${format.label}，最大 ${format.maxMiB} MB。`}>
        <Space wrap>
          <Upload accept={format.accept} beforeUpload={pickFile} showUploadList={false} disabled={busy}>
            <Button icon={<UploadOutlined />}>{file || uploaded ? '更换文件' : '选择文件'}</Button>
          </Upload>
          {file ? <span style={{ overflowWrap: 'anywhere' }}>{file.name}</span> : uploaded && <span>保留当前文件</span>}
        </Space>
      </Form.Item> : <Form.Item label={format ? `${label}链接` : '文本内容'} htmlFor={`${id}-content`}
        validateStatus={error ? 'error' : undefined} help={error || undefined}>
        {format
          ? <Input id={`${id}-content`} value={content} maxLength={2048} placeholder="https://…"
            onChange={(event) => { setContent(event.target.value); setError('') }} />
          : <Input.TextArea id={`${id}-content`} value={content} rows={5} maxLength={10000}
            onChange={(event) => setContent(event.target.value)} />}
      </Form.Item>}
    </Form>
  </Modal>
}
