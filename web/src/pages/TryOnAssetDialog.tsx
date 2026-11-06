import { AimOutlined, CloudUploadOutlined, ScanOutlined } from '@ant-design/icons'
import { Alert, Button, Checkbox, Input, Modal, Segmented, Select, Space } from 'antd'
import { useEffect, useEffectEvent, useRef, useState, type PointerEvent } from 'react'
import {
  detectTryOnGarments, generateTryOnPerson, previewTryOnAsset, queueTryOnGarments, saveTryOnAsset,
  type TryOnAsset, type TryOnBox, type TryOnCandidate, type TryOnGarmentItem, type TryOnGarmentJob, type TryOnKind, type TryOnModels,
} from '../api/tryon'

export const tryOnCategories = ['上衣', '外套', '裤子', '半裙', '连衣裙', '鞋履', '包袋', '首饰', '其他配饰']
export const tryOnLabels: Record<TryOnKind, string> = {
  person_2d: '动漫', person_real: '真人 / 写实', garment: '衣物', pose: '姿态',
}
const maxGarmentsPerBatch = 8

async function normalizePhoto(file: File) {
  if (!['image/jpeg', 'image/png', 'image/webp'].includes(file.type) || file.size > 25 * 1024 * 1024) {
    throw new Error('请选择 25 MB 以内的 JPG、PNG 或 WebP 图片。')
  }
  // 统一像素方向，确保浏览器画出的框和服务端裁剪位置一致。
  let bitmap: ImageBitmap
  try {
    bitmap = await createImageBitmap(file, { imageOrientation: 'from-image' })
  } catch {
    throw new Error('无法打开这张图片，请重新选择。')
  }
  try {
    const scale = Math.min(1, 1800 / Math.max(bitmap.width, bitmap.height))
    const canvas = document.createElement('canvas')
    canvas.width = Math.max(1, Math.round(bitmap.width * scale))
    canvas.height = Math.max(1, Math.round(bitmap.height * scale))
    const ctx = canvas.getContext('2d')
    if (!ctx) throw new Error('浏览器无法处理这张图片。')
    ctx.drawImage(bitmap, 0, 0, canvas.width, canvas.height)
    const toBlob = (format: string) => new Promise<Blob>((resolve, reject) => {
      canvas.toBlob((blob) => blob ? resolve(blob) : reject(new Error('图片转换失败。')), format, 0.94)
    })
    let result = await toBlob(file.type)
    if (result.size > 6 * 1024 * 1024) result = await toBlob('image/webp')
    if (result.size > 6 * 1024 * 1024) throw new Error('处理后的图片超过 6 MB，请换一张较小的图片。')
    return new File([result], file.name, { type: result.type })
  } finally {
    bitmap.close()
  }
}

function errorText(error: unknown) {
  return error instanceof Error ? error.message : '请求失败，请重试。'
}

function boxStyle([left, top, right, bottom]: TryOnBox) {
  return { left: left * 100 + '%', top: top * 100 + '%', width: (right - left) * 100 + '%', height: (bottom - top) * 100 + '%' }
}

function usePhotoURL(file?: File) {
  const [url, setURL] = useState('')
  useEffect(() => {
    if (!file) {
      setURL('')
      return
    }
    const url = URL.createObjectURL(file)
    setURL(url)
    return () => URL.revokeObjectURL(url)
  }, [file])
  return url
}

const referenceGeneration: Record<TryOnKind, { action: string; guide: string; review: string }> = {
  garment: {
    action: '提交后台生成',
    guide: '识别并勾选衣物，或手动指定单品。生成完成后在衣物库查看并确认入库。',
    review: '',
  },
  pose: {
    action: '生成通用姿态图',
    guide: '提取动作和视角，生成统一人台图，可用于不同人物。',
    review: '请确认动作、身体朝向和视角。',
  },
  person_2d: {
    action: '生成动漫角色',
    guide: '生成适合试衣的全身动漫角色。动漫参考图尽量保留原画风，照片会转成动漫风格。',
    review: '请确认脸型、发型和画风。',
  },
  person_real: {
    action: '生成写实人物图',
    guide: '保留人物特征，生成适合试衣的写实全身人物图。',
    review: '请确认人物外貌和体型。',
  },
}

export function TryOnAssetDialog({ kind: initialKind, open = true, models, imageModel, onClose, onSaved, onQueued }: {
  kind: TryOnKind; open?: boolean; models?: TryOnModels; imageModel?: string
  onClose: () => void; onSaved: (asset: TryOnAsset) => void; onQueued?: (jobs: TryOnGarmentJob[]) => void
}) {
  const [kind, setKind] = useState(initialKind)
  const [mode, setMode] = useState<'upload' | 'generate'>('upload')
  const [file, setFile] = useState<File>()
  const [generated, setGenerated] = useState<{ file: File; modelCode: string }>()
  const [name, setName] = useState('')
  const [category, setCategory] = useState<string>()
  const [description, setDescription] = useState('')
  const [chosenDetectionModel, setDetectionModel] = useState<string>()
  const [chosenPersonModel, setPersonModel] = useState<string>()
  const [chosenReferenceModel, setReferenceModel] = useState<string>()
  const detectionModel = chosenDetectionModel ?? models?.detection[0]?.code
  const personModel = chosenPersonModel ?? models?.person.find((item) => item.code === imageModel)?.code ?? models?.person[0]?.code
  const referenceModel = models?.image.find((item) => item.code === chosenReferenceModel)?.code
    ?? models?.image.find((item) => item.code === imageModel)?.code
    ?? models?.image[0]?.code
  const result = generated?.modelCode === referenceModel ? generated?.file : undefined
  const preview = usePhotoURL(file)
  const resultPreview = usePhotoURL(result)
  const [candidates, setCandidates] = useState<TryOnCandidate[]>([])
  const [garmentMode, setGarmentMode] = useState<'recognized' | 'manual'>('manual')
  const [selected, setSelected] = useState<number[]>([])
  const [submitted, setSubmitted] = useState<number[]>([])
  const [manualSubmitted, setManualSubmitted] = useState(false)
  const [crop, setCrop] = useState<TryOnBox>()
  const [draft, setDraft] = useState<TryOnBox>()
  const [error, setError] = useState('')
  const [hint, setHint] = useState('')
  const [working, setWorking] = useState<'upload' | 'detect' | 'queue' | 'generate' | 'save'>()
  const busy = working !== undefined
  const person = kind === 'person_2d' || kind === 'person_real'
  const garment = kind === 'garment'
  const canClose = garment || !busy
  const recognizing = garment && garmentMode === 'recognized'
  const pointerStart = useRef<{ x: number; y: number; id: number } | undefined>(undefined)
  const input = useRef<HTMLInputElement>(null)

  function clearResult() {
    setGenerated(undefined)
    setError('')
  }

  async function pick(source?: File, pasted = false) {
    if (!source || busy) return
    setError('')
    setWorking('upload')
    try {
      const photo = await normalizePhoto(source)
      clearResult()
      setFile(photo)
      const suggestedName = pasted ? '粘贴图片' : source.name.replace(/\.[^.]+$/, '').slice(0, 80)
      setName((current) => !garment && current.trim() ? current : suggestedName)
      setCategory(undefined)
      setCandidates([])
      setGarmentMode('manual')
      setSelected([])
      setSubmitted([])
      setManualSubmitted(false)
      setCrop(undefined)
      setDraft(undefined)
      pointerStart.current = undefined
      setHint('')
    } catch (error) {
      setError(errorText(error))
    } finally {
      setWorking(undefined)
    }
  }

  const pasteImage = useEffectEvent((event: ClipboardEvent) => {
    const clipboard = event.clipboardData
    if (!clipboard || event.defaultPrevented || busy) return
    const item = Array.from(clipboard.items).find((entry) => entry.kind === 'file' && entry.type.startsWith('image/'))
    const image = item?.getAsFile() ?? Array.from(clipboard.files).find((file) => file.type.startsWith('image/'))
    if (!image) return
    event.preventDefault()
    void pick(image, true)
  })

  useEffect(() => {
    if (!open || mode !== 'upload') return
    const onPaste = (event: ClipboardEvent) => pasteImage(event)
    document.addEventListener('paste', onPaste)
    return () => document.removeEventListener('paste', onPaste)
  }, [open, mode])

  async function detect() {
    if (!file || !detectionModel) return
    setWorking('detect')
    setError('')
    setHint('')
    try {
      const items = await detectTryOnGarments(file, detectionModel)
      setCandidates(items)
      if (items.length) {
        setGarmentMode('recognized')
        setSelected(items.length === 1 ? [0] : [])
        setHint('勾选要生成的衣物。提交一批后，还能继续选剩下的候选。')
      } else {
        setGarmentMode('manual')
        setSelected([])
        setHint('没有找到清晰的衣物候选，可以直接在照片上拖动框选。')
      }
    } catch (error) {
      setError(errorText(error) + ' 你仍可以手动框选。')
    } finally {
      setWorking(undefined)
    }
  }

  function draw(e: PointerEvent<HTMLDivElement>, phase: 'start' | 'move' | 'end') {
    if (!garment || recognizing || busy) return
    const rect = e.currentTarget.getBoundingClientRect()
    const x = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width))
    const y = Math.max(0, Math.min(1, (e.clientY - rect.top) / rect.height))
    if (phase === 'start') {
      if (e.button !== 0) return
      e.currentTarget.setPointerCapture(e.pointerId)
      pointerStart.current = { x, y, id: e.pointerId }
      setDraft([x, y, x, y])
      return
    }
    const start = pointerStart.current
    if (!start || start.id !== e.pointerId) return
    const next: TryOnBox = [Math.min(start.x, x), Math.min(start.y, y), Math.max(start.x, x), Math.max(start.y, y)]
    if (phase === 'move') {
      setDraft(next)
    } else {
      pointerStart.current = undefined
      setDraft(undefined)
      if (next[2] - next[0] >= .008 && next[3] - next[1] >= .008) {
        setCrop(next)
        setManualSubmitted(false)
      }
    }
  }

  const availableCandidates = candidates.flatMap((_, index) => submitted.includes(index) ? [] : [index])
  const garmentItems: TryOnGarmentItem[] = recognizing ? selected.map((index) => candidates[index])
    : name.trim() && category && !manualSubmitted ? [{ name: name.trim(), category, box: crop }] : []
  const canQueueGarments = file && referenceModel && garmentItems.length > 0
  async function queueGarments() {
    if (!file || !referenceModel || !garmentItems.length || busy) return
    setWorking('queue')
    setError('')
    try {
      const jobs = await queueTryOnGarments(file, referenceModel, garmentItems)
      if (recognizing) {
        setSubmitted((previous) => [...previous, ...selected])
        setSelected([])
      } else {
        setManualSubmitted(true)
      }
      setHint(`已提交 ${jobs.length} 件衣物，可到衣物库查看生成任务。`)
      onQueued?.(jobs)
    } catch (error) {
      setError(errorText(error))
    } finally {
      setWorking(undefined)
    }
  }

  const canGenerateReference = file && referenceModel
  async function generateReference() {
    if (!canGenerateReference || !file || !referenceModel || busy) return
    setWorking('generate')
    setError('')
    try {
      const image = await previewTryOnAsset(file, { kind, modelCode: referenceModel })
      setGenerated({ file: image, modelCode: referenceModel })
    } catch (error) {
      setError(errorText(error))
    } finally {
      setWorking(undefined)
    }
  }

  const ready = name.trim() && (mode === 'generate' ? personModel && description.trim() : result)
  async function save() {
    if (!ready || busy) return
    setWorking('save')
    setError('')
    try {
      if (mode === 'generate' && personModel) {
        onSaved(await generateTryOnPerson({ kind, name, description, modelCode: personModel }))
        onClose()
      } else if (result) {
        const asset = await saveTryOnAsset(result, kind, name, '')
        onSaved(asset)
        onClose()
      }
    } catch (error) {
      setError(errorText(error))
    } finally {
      setWorking(undefined)
    }
  }

  const selection = draft ?? crop
  const awaitingPreview = mode === 'upload' && !result
  const instructions = referenceGeneration[kind]
  return (
    <Modal title={person ? '添加人物' : `添加${tryOnLabels[kind]}`} width={mode === 'upload' ? 820 : 540}
      open={open} onCancel={onClose} maskClosable={canClose} closable={canClose} keyboard={canClose}
      footer={[
        <Button key="back" onClick={onClose} disabled={!canClose}>返回</Button>,
        mode === 'upload' && result &&
          <Button key="regenerate" disabled={busy} loading={working === 'generate'} onClick={() => void generateReference()}>重新生成</Button>,
        garment
          ? <Button key="queue" type="primary" disabled={busy || !canQueueGarments} loading={working === 'queue'} onClick={() => void queueGarments()}>
            {garmentItems.length ? `提交 ${garmentItems.length} 件后台生成` : '提交后台生成'}
          </Button>
          : <Button key="primary" type="primary" onClick={() => awaitingPreview ? void generateReference() : void save()}
            disabled={busy || !(awaitingPreview ? canGenerateReference : ready)}
            loading={working === 'save' || (awaitingPreview && working === 'generate')}>
            {mode === 'generate' ? `${instructions.action}并入库` : awaitingPreview ? instructions.action : '确认保存生成图'}
          </Button>,
      ]}>
      <div className="tryon-dialog">
        {error && <Alert type="error" showIcon title={error} />}
        {person && <Space wrap>
          <Segmented<TryOnKind> value={kind} disabled={busy} onChange={(value) => { clearResult(); setKind(value) }} options={[
            { label: tryOnLabels.person_2d, value: 'person_2d' }, { label: tryOnLabels.person_real, value: 'person_real' },
          ]} />
          <Segmented<'upload' | 'generate'> value={mode} disabled={busy} onChange={(value) => { clearResult(); setMode(value) }} options={[
            { label: '参考图生成', value: 'upload' }, { label: '文字生成人物', value: 'generate' },
          ]} />
        </Space>}
        {mode === 'upload' ? <>
          <small>{instructions.guide}</small>
          <input ref={input} type="file" accept="image/jpeg,image/png,image/webp" hidden
            onChange={(e) => { void pick(e.currentTarget.files?.[0]); e.currentTarget.value = '' }} />
          {preview ? <div className="tryon-asset-previews">
            <figure className="tryon-upload-preview">
              <figcaption>参考原图</figcaption>
              <div className="tryon-crop" data-crop={garment && !recognizing && !busy} onPointerDown={(e) => draw(e, 'start')}
                onPointerMove={(e) => draw(e, 'move')} onPointerUp={(e) => draw(e, 'end')}
                onPointerCancel={() => { pointerStart.current = undefined; setDraft(undefined) }}>
                <img src={preview} alt="参考原图" draggable={false} />
                {recognizing ? candidates.map((item, index) => (
                  <span key={index} aria-hidden className={`tryon-crop-box ${submitted.includes(index) ? 'is-submitted' : selected.includes(index) ? 'is-selected' : 'is-secondary'}`}
                    style={boxStyle(item.box)}><span>{index + 1}</span></span>
                )) : selection && <span className="tryon-crop-box" style={boxStyle(selection)} />}
              </div>
              <Button size="small" disabled={busy} onClick={() => input.current?.click()}>更换图片</Button>
              <small>也可直接粘贴图片来更换</small>
            </figure>
            {result && resultPreview && <figure className="tryon-upload-preview tryon-generated-preview">
              <figcaption>生成结果</figcaption>
              <div className="tryon-crop"><img src={resultPreview} alt="生成的素材图" /></div>
              <small>{instructions.review}</small>
            </figure>}
          </div> :
            <button className="tryon-drop" type="button" disabled={busy} onClick={() => input.current?.click()}
              onDragOver={(e) => e.preventDefault()} onDrop={(e) => { e.preventDefault(); void pick(e.dataTransfer.files[0]) }}>
              <CloudUploadOutlined /><strong>{working === 'upload' ? '正在读取图片' : '点击、拖入或粘贴图片'}</strong>
              <span>{kind === 'person_2d' ? '推荐清晰、遮挡少的单人动漫图或照片' : garment ? '支持 JPG、PNG 和 WebP' : '推荐单人、全身清晰、遮挡少的照片或插画'}</span>
            </button>}
          {garment && file && <>
            <Space wrap>
              <Select aria-label="衣物识别模型" placeholder="无可用视觉模型" value={detectionModel} disabled={busy}
                style={{ minWidth: 176 }} onChange={setDetectionModel}
                options={models?.detection.map((item) => ({ label: item.name, value: item.code }))} />
              <Button icon={<ScanOutlined />} loading={working === 'detect'} disabled={!detectionModel || busy || submitted.length > 0}
                onClick={() => void detect()}>识别衣物</Button>
              {!recognizing && crop && <Button disabled={busy} onClick={() => {
                setCrop(undefined)
                setManualSubmitted(false)
              }}>参考整张图</Button>}
            </Space>
            {candidates.length > 0 && <Segmented value={garmentMode} disabled={busy} onChange={setGarmentMode}
              options={[{ label: '识别候选', value: 'recognized' }, { label: '手动指定', value: 'manual' }]} />}
            {hint && <small role="status">{hint}</small>}
            {recognizing ? <div className="tryon-garment-candidates">
              <Space wrap>
                <strong>已选 {selected.length} 件</strong>
                <Button type="link" size="small" disabled={busy || !availableCandidates.length}
                  onClick={() => setSelected(availableCandidates.slice(0, maxGarmentsPerBatch))}>
                  {availableCandidates.length > maxGarmentsPerBatch ? '选择前 8 件' : '全选'}
                </Button>
                <Button type="link" size="small" disabled={busy || !selected.length} onClick={() => setSelected([])}>清空选择</Button>
              </Space>
              <Checkbox.Group value={selected} onChange={setSelected} className="tryon-garment-checkboxes">
                {candidates.map((item, index) => <Checkbox key={index} value={index}
                  disabled={busy || submitted.includes(index) || (selected.length >= maxGarmentsPerBatch && !selected.includes(index))}>
                  {index + 1}. {item.name} · {item.category}{submitted.includes(index) && ' · 已提交'}
                </Checkbox>)}
              </Checkbox.Group>
            </div> : <small><AimOutlined /> 单品图可直接提交，穿搭照可以拖动框选目标衣物。</small>}
          </>}
          {file && <label>参考图生成模型<Select placeholder="暂无可用参考图生成模型" value={referenceModel} disabled={busy}
            options={models?.image.map((item) => ({ label: item.name, value: item.code }))}
            onChange={(value) => { clearResult(); setReferenceModel(value) }} /></label>}
          {working === 'generate' && <Alert type="info" showIcon title="正在生成素材图，请保留此窗口。" />}
        </> : <>
          <label>图片模型<Select placeholder="请选择支持文生图的模型" value={personModel} disabled={busy}
            options={models?.person.map((item) => ({ label: item.name, value: item.code }))} onChange={setPersonModel} /></label>
          <label>人物描述<Input.TextArea value={description} disabled={busy} maxLength={1000} rows={3}
            placeholder={kind === 'person_2d' ? '例如：银色层次短发、灰眼、窄脸的青年，清爽的日系赛璐璐画风'
              : '例如：黑色齐肩卷发、鹅蛋脸的女性，体型匀称，自然棚拍质感'}
            onChange={(e) => setDescription(e.target.value)} /></label>
          {working === 'save' && <Alert type="info" title="正在生成并保存人物，请保留此窗口。" />}
        </>}
        {!recognizing && <div className="tryon-fields">
          <label>名称<Input value={name} maxLength={80} disabled={busy} onChange={(e) => {
            setName(e.target.value)
            setManualSubmitted(false)
          }}
            placeholder="方便下次找到这张图" /></label>
          {garment && <label>分类<Select value={category} disabled={busy} onChange={(value) => {
            setCategory(value)
            setManualSubmitted(false)
          }}
            placeholder="请选择品类" options={tryOnCategories.map((item) => ({ label: item, value: item }))} /></label>}
        </div>}
      </div>
    </Modal>
  )
}
