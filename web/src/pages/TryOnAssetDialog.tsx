import { AimOutlined, CloudUploadOutlined, ScanOutlined } from '@ant-design/icons'
import { Alert, Button, Input, Modal, Segmented, Select, Space } from 'antd'
import { useEffect, useRef, useState, type PointerEvent } from 'react'
import {
  detectTryOnGarments, generateTryOnPerson, saveTryOnAsset,
  type TryOnAsset, type TryOnBox, type TryOnCandidate, type TryOnKind, type TryOnModels,
} from '../api/tryon'

export const tryOnCategories = ['上衣', '外套', '裤子', '半裙', '连衣裙', '鞋履', '包袋', '首饰', '其他配饰']
export const tryOnLabels: Record<TryOnKind, string> = {
  person_2d: '2D 插画', person_real: '真人 / 写实', garment: '衣物', pose: '姿态',
}

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

export function TryOnAssetDialog({ kind: initialKind, models, imageModel, onClose, onSaved }: {
  kind: TryOnKind; models?: TryOnModels; imageModel?: string; onClose: () => void; onSaved: (asset: TryOnAsset) => void
}) {
  const [kind, setKind] = useState(initialKind)
  const [mode, setMode] = useState<'upload' | 'generate'>('upload')
  const [file, setFile] = useState<File>()
  const [preview, setPreview] = useState('')
  const [name, setName] = useState('')
  const [category, setCategory] = useState<string>()
  const [description, setDescription] = useState('')
  const [chosenDetectionModel, setDetectionModel] = useState<string>()
  const [chosenPersonModel, setPersonModel] = useState<string>()
  const detectionModel = chosenDetectionModel ?? models?.detection[0]?.code
  const personModel = chosenPersonModel ?? models?.person.find((item) => item.code === imageModel)?.code ?? models?.person[0]?.code
  const [candidates, setCandidates] = useState<TryOnCandidate[]>([])
  const [active, setActive] = useState<number>()
  const [crop, setCrop] = useState<TryOnBox>()
  const [draft, setDraft] = useState<TryOnBox>()
  const [error, setError] = useState('')
  const [hint, setHint] = useState('')
  const [working, setWorking] = useState<'upload' | 'detect' | 'save'>()
  const busy = working !== undefined
  const person = kind === 'person_2d' || kind === 'person_real'
  const pointerStart = useRef<{ x: number; y: number; id: number } | undefined>(undefined)
  const input = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (!file) return
    const url = URL.createObjectURL(file)
    setPreview(url)
    return () => URL.revokeObjectURL(url)
  }, [file])

  async function pick(source?: File) {
    if (!source || busy) return
    setError('')
    setWorking('upload')
    try {
      setFile(await normalizePhoto(source))
      if (!name.trim()) setName(source.name.replace(/\.[^.]+$/, '').slice(0, 80))
      setCandidates([])
      setActive(undefined)
      setCrop(undefined)
      setHint('')
    } catch (error) {
      setError(errorText(error))
    } finally {
      setWorking(undefined)
    }
  }

  function choose(candidate: TryOnCandidate, index: number) {
    setActive(index)
    setCrop(candidate.box)
    setName(candidate.name)
    setCategory(candidate.category)
  }

  async function detect() {
    if (!file || !detectionModel) return
    setWorking('detect')
    setError('')
    setHint('')
    try {
      const items = await detectTryOnGarments(file, detectionModel)
      setCandidates(items)
      if (items.length) {
        choose(items[0], 0)
        setHint('选择一个候选，可拖动重画选框。保存一件后可继续保存其他候选。')
      } else {
        setHint('没有找到清晰的衣物候选，可以直接在照片上拖动框选。')
      }
    } catch (error) {
      setError(errorText(error) + ' 你仍可以手动框选。')
    } finally {
      setWorking(undefined)
    }
  }

  function draw(e: PointerEvent<HTMLDivElement>, phase: 'start' | 'move' | 'end') {
    if (kind !== 'garment' || busy) return
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
      if (next[2] - next[0] >= .008 && next[3] - next[1] >= .008) setCrop(next)
    }
  }

  async function save() {
    setWorking('save')
    setError('')
    try {
      if (mode === 'generate' && personModel) {
        onSaved(await generateTryOnPerson({ kind, name, description, modelCode: personModel }))
        onClose()
      } else if (file) {
        const asset = await saveTryOnAsset(file, kind, name, category ?? '', crop)
        onSaved(asset)
        if (kind === 'garment' && candidates.length) {
          const next = candidates.filter((_, i) => i !== active)
          setCandidates(next)
          if (next.length) choose(next[0], 0)
          else { setActive(undefined); setCrop(undefined) }
          setHint(`已保存「${asset.name}」。${next.length ? '可继续确认下一件。' : '也可以继续框选，或返回试衣台。'}`)
        } else {
          onClose()
        }
      }
    } catch (error) {
      setError(errorText(error))
    } finally {
      setWorking(undefined)
    }
  }

  const ready = name.trim() && (mode === 'generate' ? personModel && description.trim() : file && (kind !== 'garment' || category))
  const selection = draft ?? crop
  return (
    <Modal title={person ? '添加人物' : `添加${tryOnLabels[kind]}`} width={kind === 'garment' ? 820 : 540}
      open onCancel={onClose} maskClosable={!busy} closable={!busy} keyboard={!busy}
      footer={[
        <Button key="back" onClick={onClose} disabled={busy}>返回</Button>,
        <Button key="save" type="primary" onClick={() => void save()} disabled={!ready || busy} loading={working === 'save'}>
          {mode === 'generate' ? '生成人物并入库' : kind === 'garment' ? '确认并保存衣物' : '保存到素材库'}
        </Button>,
      ]}>
      <div className="tryon-dialog">
        {error && <Alert type="error" showIcon title={error} />}
        {person && <Space wrap>
          <Segmented<TryOnKind> value={kind} disabled={busy} onChange={setKind} options={[
            { label: '2D 插画', value: 'person_2d' }, { label: '真人 / 写实', value: 'person_real' },
          ]} />
          <Segmented<'upload' | 'generate'> value={mode} disabled={busy} onChange={setMode} options={[
            { label: '上传图片', value: 'upload' }, { label: 'AI 生成人物', value: 'generate' },
          ]} />
        </Space>}
        {mode === 'upload' ? <>
          <input ref={input} type="file" accept="image/jpeg,image/png,image/webp" hidden
            onChange={(e) => { void pick(e.currentTarget.files?.[0]); e.currentTarget.value = '' }} />
          {preview ? <div className="tryon-upload-preview">
            <div className="tryon-crop" data-crop={kind === 'garment'} onPointerDown={(e) => draw(e, 'start')}
              onPointerMove={(e) => draw(e, 'move')} onPointerUp={(e) => draw(e, 'end')}
              onPointerCancel={() => { pointerStart.current = undefined; setDraft(undefined) }}>
              <img src={preview} alt="待保存素材预览" draggable={false} />
              {!draft && candidates.map((item, i) => i !== active &&
                <span key={i} className="tryon-crop-box is-secondary" style={boxStyle(item.box)} />)}
              {selection && <span className="tryon-crop-box" style={boxStyle(selection)} />}
            </div>
            <Button size="small" disabled={busy} onClick={() => input.current?.click()}>更换图片</Button>
          </div> :
            <button className="tryon-drop" type="button" disabled={busy} onClick={() => input.current?.click()}
              onDragOver={(e) => e.preventDefault()} onDrop={(e) => { e.preventDefault(); void pick(e.dataTransfer.files[0]) }}>
              <CloudUploadOutlined /><strong>{working === 'upload' ? '正在读取图片' : '点击或拖入图片'}</strong>
              <span>{person || kind === 'pose' ? '推荐单人、全身清晰、遮挡少的照片或插画' : '支持 JPG、PNG 和 WebP'}</span>
            </button>}
          {kind === 'garment' && file && <>
            <Space wrap>
              <Select aria-label="衣物识别模型" placeholder="无可用视觉模型" value={detectionModel} disabled={busy}
                style={{ minWidth: 176 }} onChange={setDetectionModel}
                options={models?.detection.map((item) => ({ label: item.name, value: item.code }))} />
              <Button icon={<ScanOutlined />} loading={working === 'detect'} disabled={!detectionModel || busy}
                onClick={() => void detect()}>识别衣物</Button>
              {crop && <Button disabled={busy} onClick={() => { setCrop(undefined); setActive(undefined) }}>使用整张图</Button>}
            </Space>
            <small><AimOutlined /> 在图片上拖动可手动框选。{hint || '单品图也可以直接保存。'}</small>
            {candidates.length > 0 && <Space wrap>{candidates.map((item, i) =>
              <Button key={i} disabled={busy} type={active === i ? 'primary' : 'default'} onClick={() => choose(item, i)}>{item.name} · {item.category}</Button>,
            )}</Space>}
          </>}
        </> : <>
          <label>图片模型<Select placeholder="请选择支持文生图的模型" value={personModel} disabled={busy}
            options={models?.person.map((item) => ({ label: item.name, value: item.code }))} onChange={setPersonModel} /></label>
          <label>人物描述<Input.TextArea value={description} disabled={busy} maxLength={1000} rows={3}
            placeholder="例如：短发女性，体型匀称，复古漫画线条和低饱和配色" onChange={(e) => setDescription(e.target.value)} /></label>
          {working === 'save' && <Alert type="info" title="正在生成并保存人物，请保留此窗口。" />}
        </>}
        <div className="tryon-fields">
          <label>名称<Input value={name} maxLength={80} disabled={busy} onChange={(e) => setName(e.target.value)} placeholder="方便下次找到这张图" /></label>
          {kind === 'garment' && <label>分类<Select value={category} disabled={busy} onChange={setCategory}
            placeholder="请选择品类" options={tryOnCategories.map((item) => ({ label: item, value: item }))} /></label>}
        </div>
      </div>
    </Modal>
  )
}
