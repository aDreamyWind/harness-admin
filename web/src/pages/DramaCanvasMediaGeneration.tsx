import { CloseOutlined, PictureOutlined, VideoCameraOutlined } from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { Alert, Button, Input, InputNumber, Select } from 'antd'
import { useState } from 'react'

import { listDramaCanvasGenerationModels, type DramaCanvasGenerationInput, type DramaCanvasGenerationJob, type DramaCanvasGenerationKind } from '../api/drama'
import './drama-canvas-media-generation.css'

export type CanvasGenerationReference = { id: string; title: string } & (
  { kind: 'text'; content: string } | { kind: 'image'; url?: string }
)

export function DramaCanvasMediaGeneration({ kind, content, job, references, busy, error, onGenerate, onAdopt, onImport, onClose }: {
  kind: DramaCanvasGenerationKind; content: string; job?: DramaCanvasGenerationJob; references: CanvasGenerationReference[]
  busy?: boolean; error?: string; onGenerate: (input: DramaCanvasGenerationInput) => void
  onAdopt: (url: string) => void; onImport: () => void; onClose: () => void
}) {
  const [modelCode, setModelCode] = useState<string>()
  const [prompt, setPrompt] = useState('')
  const [ratio, setRatio] = useState<NonNullable<DramaCanvasGenerationInput['aspectRatio']>>('16:9')
  const [seconds, setSeconds] = useState<number>()
  const models = useQuery({ queryKey: ['drama-canvas-generation-models'], queryFn: listDramaCanvasGenerationModels, staleTime: 60_000 })
  const available = (models.data ?? []).filter((model) => model.kind === kind)
  const images = references.filter((reference) => reference.kind === 'image')
  const imageCount = images.filter((reference) => reference.url).length
  const compatible = available.filter((item) => imageCount >= item.minReferences && imageCount <= item.maxReferences)
  const model = available.find((item) => item.code === modelCode) ?? compatible.find((item) => item.code === job?.modelCode) ?? compatible[0] ?? available[0]
  const duration = model?.minDuration !== undefined && model.maxDuration !== undefined
    ? Math.min(model.maxDuration, Math.max(model.minDuration, seconds ?? 5)) : undefined
  const running = busy || job?.status === 'running'
  const title = kind === 'image' ? '图片' : '视频'
  const referenceWarning = images.some((reference) => !reference.url) ? '有参考图还未就绪，请先上传图片或等待出图。'
    : model && imageCount < model.minReferences ? `该模型至少需要 ${model.minReferences} 张参考图。`
    : model && imageCount > model.maxReferences ? `该模型最多支持 ${model.maxReferences} 张参考图。` : undefined
  const referenceLimit = model && (model.maxReferences === 0 ? '支持纯文字生成'
    : model.minReferences > 0 ? `需要 ${model.minReferences}–${model.maxReferences} 张参考图` : `最多 ${model.maxReferences} 张参考图`)
  const result = content && job?.resultUrl !== content ? job?.resultUrl : undefined

  return <section className="drama-canvas-media-generation nodrag nopan nowheel" aria-label={`${title}节点生成`} onDoubleClick={(event) => event.stopPropagation()}>
    <header>
      <strong>{kind === 'image' ? <PictureOutlined /> : <VideoCameraOutlined />} {title}生成</strong>
      <Button type="text" size="small" icon={<CloseOutlined />} aria-label="收起生成设置" onClick={onClose} />
    </header>
    <Input.TextArea aria-label={`${title}生成提示词`} value={prompt} maxLength={8000} disabled={running}
      placeholder={references.length ? '补充提示词（选填）' : `描述想生成的${title}`} autoSize={{ minRows: 2, maxRows: 5 }}
      onChange={(event) => setPrompt(event.target.value)} />
    {!!references.length && <ul className="drama-canvas-generation-inputs" aria-label="连入的生成素材">
      {references.map((reference) => <li key={reference.id}>
        {reference.kind === 'image' && reference.url && <img src={reference.url} alt="" loading="lazy" />}
        <span title={reference.kind === 'text' ? reference.content : reference.title}>{reference.title}</span>
        <small>{reference.kind === 'text' ? '提示词' : reference.url ? '参考图' : '待出图'}</small>
      </li>)}
    </ul>}
    <div className="drama-canvas-media-fields">
      <Select size="small" aria-label={`${title}生成模型`} value={model?.code} onChange={setModelCode} disabled={running}
        loading={models.isPending} placeholder="选择模型" options={available.map((item) => ({ value: item.code, label: item.name }))} />
      <Select size="small" aria-label="生成比例" value={ratio} onChange={setRatio} disabled={running}
        options={['16:9', '9:16', '1:1'].map((value) => ({ value, label: value }))} />
      {kind === 'video' && model && <InputNumber size="small" aria-label="视频时长（秒）" disabled={running}
        min={model.minDuration} max={model.maxDuration} precision={0} value={duration} onChange={(value) => setSeconds(value ?? undefined)} suffix="秒" />}
    </div>
    {models.isError && <Alert type="error" title="读取生成模型失败" action={<Button size="small" onClick={() => void models.refetch()}>重试</Button>} />}
    {models.isSuccess && !available.length && <p className="drama-canvas-media-hint">当前账号没有可用于画布的{title}模型。</p>}
    {referenceWarning && <p className="drama-canvas-media-hint">{referenceWarning}</p>}
    {model && !referenceWarning && <p className="drama-canvas-media-hint">{referenceLimit}{kind === 'video' && ` · ${model.minDuration}–${model.maxDuration} 秒`}</p>}
    {job?.status === 'running' && <p className="drama-canvas-media-hint" role="status">正在后台生成{title}，可以继续操作画布。</p>}
    {(error || job?.status === 'failed') && <p className="drama-canvas-media-failure" role="alert">{error || job?.errorMessage || '生成失败，请重试。'}</p>}
    <footer>
      <Button size="small" type="text" onClick={onImport}>导入素材</Button>
      <Button size="small" type="primary" loading={running} disabled={!model || !!referenceWarning || (!prompt.trim() && !references.length)}
        onClick={() => { if (model) onGenerate({ modelCode: model.code, prompt: prompt.trim(), aspectRatio: ratio, ...(kind === 'video' ? { duration } : {}) }) }}>
        {running ? '生成中' : `生成${title}`}
      </Button>
    </footer>
    {result && <div className="drama-canvas-media-result">
      {kind === 'image' ? <img src={result} alt="生成结果" /> : <video src={result} controls playsInline preload="metadata" aria-label="生成结果" />}
      <Button size="small" onClick={() => onAdopt(result)}>应用生成结果</Button>
    </div>}
  </section>
}
