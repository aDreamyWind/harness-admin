import { CloseOutlined, PlusOutlined, ThunderboltOutlined } from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { useViewport } from '@xyflow/react'
import { Alert, Button, InputNumber, Select, Tooltip } from 'antd'
import { useRef, type ReactNode } from 'react'

import { dramaVideoResolutions, listDramaCanvasGenerationModels, listDramaStyles, type DramaCanvasGenerationInput, type DramaCanvasGenerationJob, type DramaCanvasGenerationKind, type DramaCanvasImageResolution, type DramaVideoResolution } from '../api/drama'
import { DramaCanvasPromptEditor, canvasPromptParts, canvasPromptText, type CanvasPromptPart, type CanvasPromptRefChanges, type DramaCanvasPromptHandle } from './DramaCanvasPromptEditor'
import type { DramaCanvasMentionOption } from './DramaCanvasReferencePicker'
import { DramaStylePicker } from './DramaStylePicker'
import type { CanvasGeometryNode } from './drama-canvas-geometry'
import './drama-canvas-media-generation.css'

export type CanvasGenerationReference = { id: string; title: string } & (
  { kind: 'text'; content: string } | { kind: 'image'; url?: string }
)
type CanvasRatio = NonNullable<DramaCanvasGenerationInput['aspectRatio']>
type CanvasStyle = { id: string; name: string }
const ratios: CanvasRatio[] = ['16:9', '9:16', '1:1']
export type CanvasMediaDraft = {
  prompt: CanvasPromptPart[]; ratio: CanvasRatio; modelCode?: string; seconds?: number; style?: CanvasStyle
  resolution?: DramaCanvasImageResolution; videoResolution?: DramaVideoResolution
}

export function DramaCanvasComposerAnchor({ node, above, children }: { node: CanvasGeometryNode; above?: boolean; children: ReactNode }) {
  const { x, y, zoom } = useViewport()
  return <div className={above ? 'drama-canvas-replace-anchor' : 'drama-canvas-composer-anchor'} style={{
    left: x + (node.position.x + node.width / 2) * zoom,
    top: y + (node.position.y + (above ? 0 : node.height)) * zoom + (above ? -14 : 14),
  }}>{children}</div>
}

export function DramaCanvasComposerReferences({ references }: { references: CanvasGenerationReference[] }) {
  const images = references.flatMap((ref) => ref.kind === 'image' && ref.url ? [{ ...ref, url: ref.url }] : [])
  return images.length > 0 && <ol className="drama-canvas-reference-strip" aria-label="参考图">
    {images.map((ref, index) => <li key={ref.id} title={ref.title}>
      <img src={ref.url} alt={ref.title} loading="lazy" /><span aria-hidden="true">{index + 1}</span>
    </li>)}
  </ol>
}

export function DramaCanvasComposerSources({ disabled, uploading, onUpload, onMention }: {
  disabled?: boolean; uploading?: boolean; onUpload: () => void; onMention: () => void
}) {
  return <div className="drama-canvas-composer-sources">
    <Tooltip title="上传参考图并新建连线卡片"><Button size="small" shape="circle" aria-label="上传参考图" icon={<PlusOutlined />} loading={uploading} disabled={disabled} onClick={onUpload} /></Tooltip>
    <Tooltip title="引用画布卡片"><Button size="small" shape="circle" aria-label="引用画布卡片" disabled={disabled} onClick={onMention}>@</Button></Tooltip>
  </div>
}

export function DramaCanvasMediaGeneration({ kind, content, draft, defaultStyleId, onDraftChange, job, references, mentionOptions, onReferenceChange, busy, saving, uploading, error, onGenerate, onAdopt, onUploadReference, onClose }: {
  kind: DramaCanvasGenerationKind; content: string; job?: DramaCanvasGenerationJob; references: CanvasGenerationReference[]
  draft: CanvasMediaDraft; defaultStyleId?: string; onDraftChange: (patch: Partial<CanvasMediaDraft>) => void
  mentionOptions: DramaCanvasMentionOption[]; onReferenceChange: (changes: CanvasPromptRefChanges) => boolean
  busy?: boolean; saving?: boolean; uploading?: boolean; error?: string; onGenerate: (input: DramaCanvasGenerationInput) => void
  onAdopt: (url: string) => void; onUploadReference: () => void; onClose: () => void
}) {
  const { modelCode, ratio, seconds } = draft
  const mention = useRef<DramaCanvasPromptHandle>(null)
  const linkedIds = references.map((reference) => reference.id)
  const prompt = canvasPromptParts(draft.prompt, linkedIds)
  const promptText = prompt.some((part) => typeof part === 'string' && part.trim()) ? canvasPromptText(prompt, mentionOptions).trim() : ''
  const styles = useQuery({ queryKey: ['drama-styles'], queryFn: listDramaStyles, staleTime: 60_000 })
  const style = draft.style ?? styles.data?.find((item) => item.category === '2D' && item.id === defaultStyleId)
  const models = useQuery({ queryKey: ['drama-canvas-generation-models'], queryFn: listDramaCanvasGenerationModels, staleTime: 60_000 })
  const available = (models.data ?? []).filter((model) => model.kind === kind)
  const images = references.filter((reference) => reference.kind === 'image')
  const imageCount = images.filter((reference) => reference.url).length
  const compatible = available.filter((item) => imageCount >= item.minReferences && imageCount <= item.maxReferences)
  const model = available.find((item) => item.code === modelCode) ?? compatible.find((item) => item.code === job?.modelCode) ?? compatible[0] ?? available[0]
  const resolutions: readonly DramaCanvasImageResolution[] = model?.kind === 'image' && model.resolutions?.length ? model.resolutions : ['1K']
  const resolution = draft.resolution && resolutions.includes(draft.resolution) ? draft.resolution : resolutions[0]
  const videoResolutions = dramaVideoResolutions(model)
  const videoResolution = draft.videoResolution && videoResolutions.includes(draft.videoResolution) ? draft.videoResolution : '720P'
  const sizes = ratios.flatMap((aspectRatio) => resolutions.map((quality) => ({
    value: `${aspectRatio}|${quality}`, label: `${aspectRatio} · ${quality}`, aspectRatio, quality,
  })))
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

  return <section className="drama-canvas-media-generation" aria-label={`${title}节点生成`}>
    <Button className="drama-canvas-composer-close" type="text" size="small" icon={<CloseOutlined />} aria-label="收起生成设置" onClick={onClose} />
    <div className="drama-canvas-composer-prompt">
      <DramaCanvasComposerReferences references={references} />
      <DramaCanvasPromptEditor ariaLabel={`${title}生成提示词`} value={prompt} maxLength={8000} disabled={running}
        placeholder={`描述想生成的${title}，输入 @ 引用素材`} editorRef={mention} linkedIds={linkedIds}
        options={mentionOptions} onChange={(value, changes) => {
          if (!onReferenceChange(changes)) return false
          onDraftChange({ prompt: value })
        }} />
    </div>
    <footer className="drama-canvas-composer-toolbar">
      <DramaCanvasComposerSources disabled={running || saving} uploading={uploading} onUpload={onUploadReference} onMention={() => mention.current?.open()} />
      <DramaStylePicker value={style?.id} valueLabel={style?.name} onChange={({ id, name: label }) => onDraftChange({ style: { id, name: label } })} disabled={running} />
      <Select className="drama-canvas-composer-model" size="small" aria-label={`${title}生成模型`} value={model?.code} onChange={(code) => {
        const nextModel = available.find((item) => item.code === code)
        onDraftChange({ modelCode: code, videoResolution: kind === 'video' && dramaVideoResolutions(nextModel).includes(videoResolution) ? videoResolution : undefined })
      }} disabled={running}
        loading={models.isPending} placeholder="选择模型" popupMatchSelectWidth={false} options={available.map((item) => ({ value: item.code, label: item.name }))} />
      {kind === 'image' ? <Select className="drama-canvas-composer-size" size="small" aria-label="图片尺寸" value={`${ratio}|${resolution}`} disabled={running}
        options={sizes} onChange={(_, option) => { if (option && !Array.isArray(option)) onDraftChange({ ratio: option.aspectRatio, resolution: option.quality }) }} />
        : <div className="drama-canvas-composer-video-size">
          <Select className="drama-canvas-composer-ratio" size="small" aria-label="生成比例" value={ratio} onChange={(value) => onDraftChange({ ratio: value })} disabled={running}
            options={ratios.map((value) => ({ value, label: value }))} />
          {duration !== undefined && <InputNumber size="small" aria-label="视频时长（秒）" disabled={running}
            min={model?.minDuration} max={model?.maxDuration} precision={0} value={duration} onChange={(value) => onDraftChange({ seconds: value ?? undefined })} suffix="秒" />}
          <Select className="drama-canvas-composer-video-resolution" size="small" aria-label="视频清晰度" value={videoResolution}
            disabled={running || videoResolutions.length === 1} options={videoResolutions.map((value) => ({ value, label: value }))}
            onChange={(value) => onDraftChange({ videoResolution: value })} />
        </div>}
      <Button className="drama-canvas-composer-generate" size="small" type="primary" icon={<ThunderboltOutlined />} loading={running}
        disabled={saving || uploading || !model || !!referenceWarning || (!promptText && !references.length)}
        onClick={() => { if (model) onGenerate({
          modelCode: model.code, prompt: promptText, aspectRatio: ratio, styleId: style?.id || undefined,
          ...(kind === 'video' ? { duration, resolution: videoResolution } : { resolution }),
        }) }}>{running ? '生成中' : `生成${title}`}</Button>
    </footer>
    {models.isError && <Alert type="error" title="读取生成模型失败" action={<Button size="small" onClick={() => void models.refetch()}>重试</Button>} />}
    {models.isSuccess && !available.length && <p className="drama-canvas-media-hint">当前账号没有可用于画布的{title}模型。</p>}
    {referenceWarning && <p className="drama-canvas-media-hint">{referenceWarning}</p>}
    {model && !referenceWarning && <p className="drama-canvas-media-hint">{referenceLimit}{kind === 'video' && ` · ${model.minDuration}–${model.maxDuration} 秒`}</p>}
    {job?.status === 'running' && <p className="drama-canvas-media-hint" role="status">正在后台生成{title}，可以继续操作画布。</p>}
    {(error || job?.status === 'failed') && <p className="drama-canvas-media-failure" role="alert">{error || job?.errorMessage || '生成失败，请重试。'}</p>}
    {result && <div className="drama-canvas-media-result">
      {kind === 'image' ? <img src={result} alt="生成结果" /> : <video src={result} controls playsInline preload="metadata" aria-label="生成结果" />}
      <Button size="small" onClick={() => onAdopt(result)}>应用生成结果</Button>
    </div>}
  </section>
}
