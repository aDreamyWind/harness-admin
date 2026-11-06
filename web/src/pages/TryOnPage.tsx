import {
  CameraOutlined, CloseOutlined, PictureOutlined, PlusOutlined, UserOutlined,
} from '@ant-design/icons'
import { Alert, Button, Empty, Image, Select, Spin, Tabs, Tag, message } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useRef, useState } from 'react'
import {
  deleteTryOnAsset, getTryOnAssets, getTryOnGarmentJobs, getTryOnJobs, getTryOnModels, getTryOnOutfits,
  startTryOn, tryOnAssetURL, tryOnResultURL,
  type TryOnAsset, type TryOnGarmentJob, type TryOnInput, type TryOnJob, type TryOnKind,
} from '../api/tryon'
import { formatDateTime } from '../utils/dateTime'
import { TryOnAssetDialog, tryOnLabels } from './TryOnAssetDialog'
import { garmentJobsKey, TryOnGarmentJobs } from './TryOnGarmentJobs'
import {
  garmentFilters, maxTryOnGarments, outfitsKey, personCoverClass, personFilters,
  TryOnDeleteMenu, TryOnFilters, TryOnOutfitLibrary, TryOnReferencePicker,
  type TryOnPickerKind,
} from './TryOnWardrobe'
import './tryon.css'

const assetsKey = ['tryon', 'assets']
const jobsKey = ['tryon', 'jobs']
const modelsKey = ['tryon', 'models']
type Selection = { personId: string; garmentIds: string[]; poseId: string }
const emptySelection: Selection = { personId: '', garmentIds: [], poseId: '' }
const jobStatuses = {
  queued: { label: '排队中', color: 'default' },
  running: { label: '生成中', color: 'processing' },
  succeeded: { label: '已完成', color: 'success' },
  failed: { label: '生成失败', color: 'error' },
}

function errorMessage(error: Error | null) {
  return error?.message ?? '请求失败，请重试。'
}

function selectable(asset: TryOnAsset) {
  return !asset.retired && !asset.deleted
}

function activeJob(job: TryOnJob) {
  return job.status === 'queued' || job.status === 'running'
}

function jobSelection(job: TryOnJob): Selection {
  return {
    personId: job.personId,
    garmentIds: job.garmentIds?.length ? job.garmentIds : job.garmentId ? [job.garmentId] : [],
    poseId: job.poseId ?? '',
  }
}

export function TryOnPage() {
  const client = useQueryClient()
  const [messageApi, messageContext] = message.useMessage()
  const assets = useQuery({ queryKey: assetsKey, queryFn: getTryOnAssets })
  const models = useQuery({ queryKey: modelsKey, queryFn: getTryOnModels })
  const outfits = useQuery({ queryKey: outfitsKey, queryFn: getTryOnOutfits })
  const jobs = useQuery({
    queryKey: jobsKey, queryFn: getTryOnJobs,
    refetchInterval: (query) => query.state.data?.some(activeJob) ? 2200 : false,
  })
  const garmentJobs = useQuery({
    queryKey: garmentJobsKey, queryFn: getTryOnGarmentJobs,
    refetchInterval: (query) => query.state.data?.some((job) => job.status === 'queued' || job.status === 'running') ? 2200 : false,
  })
  const [selection, setSelection] = useState(emptySelection)
  const [viewingJobId, setViewingJobId] = useState('')
  const [submittedJobId, setSubmittedJobId] = useState('')
  const [chosenModel, setChosenModel] = useState<string>()
  const [tab, setTab] = useState('studio')
  const [personFilter, setPersonFilter] = useState('all')
  const [garmentFilter, setGarmentFilter] = useState('all')
  const [dialog, setDialog] = useState<TryOnKind>()
  const [picker, setPicker] = useState<TryOnPickerKind>()
  const [hasGarmentDraft, setHasGarmentDraft] = useState(false)
  const stage = useRef<HTMLElement>(null)

  const imageModels = models.data?.image ?? []
  const modelCode = imageModels.find((item) => item.code === chosenModel)?.code ?? imageModels[0]?.code
  const mutation = useMutation({
    mutationFn: startTryOn,
    onSuccess: async (job) => {
      await client.cancelQueries({ queryKey: jobsKey })
      setSubmittedJobId(job.id)
      client.setQueryData<TryOnJob[]>(jobsKey, (old) => [job, ...(old ?? []).filter((item) => item.id !== job.id)])
      void client.invalidateQueries({ queryKey: jobsKey })
      void messageApi.success('试穿任务已提交，可以继续搭配')
    },
    onError: () => { void client.invalidateQueries({ queryKey: jobsKey }) },
  })

  const allAssets = assets.data ?? []
  const byId = new Map(allAssets.map((asset) => [asset.id, asset]))
  const availableAssets = allAssets.filter(selectable)
  const people = availableAssets.filter((asset) => asset.kind === 'person_2d' || asset.kind === 'person_real')
  const garments = availableAssets.filter((asset) => asset.kind === 'garment')
  const poses = availableAssets.filter((asset) => asset.kind === 'pose')
  const garmentTasks = (garmentJobs.data ?? []).filter((job) => job.status !== 'saved')
  const tryOnJobs = jobs.data ?? []
  const pendingJobs = tryOnJobs.filter(activeJob)
  const studioJobs = [...pendingJobs, ...tryOnJobs.filter((job) => !activeJob(job))]
  const outfitItems = outfits.data ?? []
  const person = byId.get(selection.personId)
  const pose = byId.get(selection.poseId)
  const selectedGarments = selection.garmentIds.map((id) => ({ id, asset: byId.get(id) }))
  const viewedJob = tryOnJobs.find((job) => job.id === viewingJobId)
  const submittedJob = tryOnJobs.find((job) => job.id === submittedJobId)
  const recentSelection = submittedJob && jobSelection(submittedJob)
  const matchingSubmittedJob = recentSelection?.personId === selection.personId &&
    recentSelection.poseId === selection.poseId && recentSelection.garmentIds.join() === selection.garmentIds.join() &&
    submittedJob?.modelCode === modelCode ? submittedJob : undefined
  const current = viewedJob ?? matchingSubmittedJob
  const comparisonPerson = viewedJob ? byId.get(viewedJob.personId) : person
  const emptyStage = !comparisonPerson && !current

  const removal = useMutation({
    mutationFn: (asset: TryOnAsset) => deleteTryOnAsset(asset.id),
    onSuccess: async (_, asset) => {
      await client.cancelQueries({ queryKey: assetsKey })
      client.setQueryData<TryOnAsset[]>(assetsKey, (old) =>
        old?.map((item) => item.id === asset.id ? { ...item, deleted: true } : item))
      setSelection((old) => ({
        personId: old.personId === asset.id ? '' : old.personId,
        garmentIds: old.garmentIds.filter((id) => id !== asset.id),
        poseId: old.poseId === asset.id ? '' : old.poseId,
      }))
      void client.invalidateQueries({ queryKey: assetsKey })
      void client.invalidateQueries({ queryKey: outfitsKey })
      void messageApi.success('已从素材库移除')
    },
    onError: (error) => { void messageApi.error(errorMessage(error)) },
  })

  const deleteControls = {
    deleting: removal.isPending || mutation.isPending,
    onDelete: async (asset: TryOnAsset) => {
      try {
        await removal.mutateAsync(asset)
      } catch {
        // 删除错误已由 mutation 显示，素材继续保留在库中。
      }
    },
  }

  function updateSelection(update: (old: Selection) => Selection) {
    setSelection(update)
    setViewingJobId('')
    if (!mutation.isPending) mutation.reset()
  }

  function appendGarments(ids: string[]) {
    const additions = ids.filter((id) => !selection.garmentIds.includes(id))
    if (selection.garmentIds.length + additions.length > maxTryOnGarments) {
      void messageApi.warning('一次最多试穿 4 件衣物，请先移除部分衣物')
      return false
    }
    if (additions.length) updateSelection((old) => ({ ...old, garmentIds: [...old.garmentIds, ...additions] }))
    return true
  }

  function useAsset(asset: TryOnAsset) {
    if (asset.kind === 'garment') {
      if (!appendGarments([asset.id])) return
    } else {
      const field = asset.kind === 'pose' ? 'poseId' : 'personId'
      updateSelection((old) => ({ ...old, [field]: asset.id }))
    }
    setTab('studio')
  }

  function cacheAsset(asset: TryOnAsset) {
    client.setQueryData<TryOnAsset[]>(assetsKey, (old) => [asset, ...(old ?? []).filter((item) => item.id !== asset.id)])
    void client.invalidateQueries({ queryKey: assetsKey })
  }

  function saveAsset(asset: TryOnAsset) {
    cacheAsset(asset)
    useAsset(asset)
  }

  function garmentsQueued(queued: TryOnGarmentJob[]) {
    setHasGarmentDraft(true)
    void messageApi.success(`已提交 ${queued.length} 件衣物，可在衣物库查看进度`)
    void client.cancelQueries({ queryKey: garmentJobsKey }).then(() => {
      client.setQueryData<TryOnGarmentJob[]>(garmentJobsKey, (old) => [
        ...queued, ...(old ?? []).filter((item) => !queued.some((job) => job.id === item.id)),
      ])
      void client.invalidateQueries({ queryKey: garmentJobsKey })
    })
  }

  function showJob(job: TryOnJob) {
    setViewingJobId(job.id)
    setTab('studio')
    requestAnimationFrame(() => stage.current?.scrollIntoView({ block: 'start' }))
  }

  function canUseReferences(input: Selection) {
    return input.garmentIds.length > 0 && [input.personId, ...input.garmentIds, ...(input.poseId ? [input.poseId] : [])]
      .every((id) => {
        const asset = byId.get(id)
        return asset && selectable(asset)
      })
  }

  function submit() {
    if (!modelCode || mutation.isPending || !canUseReferences(selection)) return
    setViewingJobId('')
    const input: TryOnInput = { ...selection, poseId: selection.poseId || undefined, modelCode }
    mutation.mutate(input)
  }

  function retryJob(job: TryOnJob) {
    const references = jobSelection(job)
    if (!imageModels.some((model) => model.code === job.modelCode) || !canUseReferences(references)) {
      void messageApi.warning('原参考素材或图片模型不可用，请在试衣台重新选择')
      return
    }
    if (!mutation.isPending) mutation.mutate({ ...references, poseId: references.poseId || undefined, modelCode: job.modelCode })
  }

  function chooseReference(ids: string[]) {
    if (picker === 'garment') {
      appendGarments(ids)
      return
    }
    const field = picker === 'pose' ? 'poseId' : 'personId'
    updateSelection((old) => ({ ...old, [field]: ids[0] }))
    setPicker(undefined)
  }

  function openAssetDialog() {
    setDialog(picker === 'person' ? (person?.kind === 'person_real' ? 'person_real' : 'person_2d') : picker)
    setPicker(undefined)
  }

  function comboLabel(job: TryOnJob) {
    const reference = jobSelection(job)
    const label = (id: string) => byId.get(id)?.name ?? '素材'
    return [label(reference.personId), reference.garmentIds.map(label).join(' + '), reference.poseId && label(reference.poseId)]
      .filter(Boolean).join(' / ')
  }

  const refError = assets.error ?? jobs.error
  const isSubmitting = mutation.isPending
  const unavailableReference = assets.isSuccess && Boolean((selection.personId && !people.some((asset) => asset.id === selection.personId)) ||
    selectedGarments.some(({ asset }) => !asset || !selectable(asset)) || (selection.poseId && !poses.some((asset) => asset.id === selection.poseId)))
  const personLibraryKind = personFilter === 'person_real' ? 'person_real' : 'person_2d'

  return (
    <main className="tryon-page" aria-label="AI 试衣">
      {messageContext}
      {refError && <Alert type="error" showIcon title={errorMessage(refError)}
        action={<Button size="small" onClick={() => { void assets.refetch(); void jobs.refetch() }}>重试</Button>} />}
      {models.isError && <Alert type="error" showIcon title={errorMessage(models.error)}
        action={<Button size="small" onClick={() => void models.refetch()}>重新加载模型</Button>} />}
      <Tabs activeKey={tab} onChange={setTab} items={[
        { key: 'studio', label: '试衣台', children: <>
          <div className="tryon-workspace">
            <section ref={stage} className="tryon-stage" aria-label="原图和试穿效果">
              <header className="tryon-stage-header">
                <div><span>{viewedJob ? '试穿记录' : '当前搭配'}</span><h2>试穿镜</h2></div>
                <div className="tryon-stage-actions">
                  {viewedJob && <Button size="small" onClick={() => setViewingJobId('')}>回到当前搭配</Button>}
                  {current?.status === 'succeeded' && <Button size="small" href={tryOnResultURL(current.id)} download="试穿效果">下载效果图</Button>}
                  {current && current.status !== 'succeeded' && <Tag color={jobStatuses[current.status].color}>{jobStatuses[current.status].label}</Tag>}
                </div>
              </header>
              {viewedJob && <small className="tryon-viewing-label">{comboLabel(viewedJob)}</small>}
              {emptyStage ? <button type="button" className="tryon-stage-picture tryon-empty-stage" aria-label="选择人物"
                onClick={() => setPicker('person')}>
                <span className="tryon-placeholder"><UserOutlined aria-hidden /><span>点击选择人物</span></span>
              </button> : <div className="tryon-comparison">
                <figure><figcaption>人物原图</figcaption><div className="tryon-stage-picture">
                  {comparisonPerson ? <Image src={tryOnAssetURL(comparisonPerson.id)} alt={comparisonPerson.name} /> :
                    <div className="tryon-placeholder"><UserOutlined /><span>选择一位人物</span></div>}
                </div></figure>
                <figure><figcaption>试穿效果</figcaption><div className="tryon-stage-picture">
                  {current?.status === 'succeeded' ? <Image src={tryOnResultURL(current.id)} alt="试穿效果图" /> :
                    <div className="tryon-placeholder">
                      {current && activeJob(current) ? <Spin /> : <PictureOutlined />}
                      <span>{current ? jobStatuses[current.status].label : '效果图会出现在这里'}</span>
                    </div>}
                </div></figure>
              </div>}
              {current?.status === 'failed' && <Alert type="error" showIcon title={current.error || '这次试穿没有完成，请重试。'} />}
              {mutation.isError && <Alert type="error" showIcon title={errorMessage(mutation.error)} />}
            </section>
            <aside className="tryon-controls" aria-label="本次生成的参考图">
              <div className="tryon-control-heading"><h2>试穿搭配</h2><span>{Number(!!person) + selectedGarments.length + Number(!!pose)} 张参考图</span></div>
              <Reference title="人物" asset={person} onPick={() => setPicker('person')} />
              <section className="tryon-garment-strip" aria-label="试穿衣物">
                <div className="tryon-garment-strip-title"><strong>衣物</strong><small>{selectedGarments.length} / {maxTryOnGarments} 件</small></div>
                <div className="tryon-garment-slots" data-empty={selectedGarments.length === 0}>
                  {selectedGarments.map(({ id, asset }) => <figure key={id} className="tryon-garment-slot">
                    <div className="tryon-slot-image">
                      {asset && !asset.deleted ? <Image src={tryOnAssetURL(id)} alt={asset.name} /> : <PictureOutlined />}
                      <Button className="tryon-remove-garment" size="small" shape="circle" icon={<CloseOutlined />}
                        aria-label={`移除${asset?.name ?? '衣物'}`} onClick={() => updateSelection((old) => ({
                          ...old, garmentIds: old.garmentIds.filter((item) => item !== id),
                        }))} />
                    </div>
                    <figcaption title={asset?.name}>{asset?.name ?? '衣物不可用'}</figcaption>
                  </figure>)}
                  {selectedGarments.length < maxTryOnGarments && <button type="button" className="tryon-add-slot" aria-label="添加试穿衣物"
                    onClick={() => setPicker('garment')}><PlusOutlined /><span>加衣物</span></button>}
                </div>
              </section>
              <Reference title="姿态（可选）" asset={pose} onPick={() => setPicker('pose')}
                onClear={selection.poseId ? () => updateSelection((old) => ({ ...old, poseId: '' })) : undefined} />
              {unavailableReference && <Alert type="warning" showIcon title="部分参考素材已不可用，请重新选择。" />}
              <div className="tryon-control-footer">
                <label className="tryon-model">图片模型
                  <Select value={modelCode} loading={models.isPending} onChange={setChosenModel} placeholder="暂无可用图片模型"
                    options={imageModels.map((item) => ({ label: item.name, value: item.code }))} />
                </label>
                {models.isSuccess && imageModels.length === 0 &&
                  <small>请在模型目录启用支持多张参考图的图片模型。</small>}
                <Button type="primary" size="large" block icon={<CameraOutlined />} loading={isSubmitting}
                  disabled={isSubmitting || !modelCode || !canUseReferences(selection)} onClick={submit}>生成试穿图</Button>
              </div>
            </aside>
          </div>
          <section className="tryon-library tryon-task-panel" aria-label="试穿任务">
            <header className="tryon-control-heading"><div><h2>试穿任务</h2>
              <span>进行中的任务优先显示</span></div>
              <div className="tryon-stage-actions">
                {pendingJobs.length > 0 && <Tag color="processing">{pendingJobs.length} 个进行中</Tag>}
                {tryOnJobs.length > 0 && <Button type="link" onClick={() => setTab('history')}>查看全部</Button>}
              </div>
            </header>
            <TryOnTaskCards strip jobs={studioJobs} assets={byId} pending={jobs.isPending} label={comboLabel}
              onView={showJob} onRetry={retryJob} retrying={isSubmitting} />
          </section>
        </> },
        { key: 'people', label: '人物库', children:
          <Library controls={<TryOnFilters label="人物分类" options={personFilters} value={personFilter} onChange={setPersonFilter} />}
            assets={people.filter((asset) => personFilter === 'all' || asset.kind === personFilter)}
            addLabel="添加人物" pending={assets.isPending}
            onAdd={() => setDialog(personLibraryKind)} onSelect={useAsset} {...deleteControls} /> },
        { key: 'garments', label: <span>衣物库{garmentTasks.length > 0 &&
          <span className="tryon-garment-count" aria-hidden>{garmentTasks.length}</span>}</span>, children: <>
          <TryOnGarmentJobs jobs={garmentTasks} loading={garmentJobs.isPending} error={garmentJobs.error}
            onRefresh={() => { void garmentJobs.refetch() }} onSaved={cacheAsset} />
          {garmentFilter === 'outfits' ? <section className="tryon-library">
            <div className="tryon-library-tools"><TryOnFilters label="衣物分类" options={garmentFilters} value={garmentFilter} onChange={setGarmentFilter} /></div>
            <TryOnOutfitLibrary outfits={outfitItems} garments={garments} loading={outfits.isPending} error={outfits.error}
              onRefresh={() => { void outfits.refetch() }} onUse={(outfit) => {
                updateSelection((old) => ({ ...old, garmentIds: outfit.garmentIds }))
                setTab('studio')
              }} />
          </section> : <Library controls={<TryOnFilters label="衣物分类" options={garmentFilters} value={garmentFilter} onChange={setGarmentFilter} />}
            assets={garments.filter((asset) => garmentFilter === 'all' || asset.category === garmentFilter)}
            addLabel={hasGarmentDraft ? '继续添加衣物' : '添加衣物'} pending={assets.isPending}
            onAdd={() => setDialog('garment')} onSelect={useAsset} {...deleteControls} />}
        </> },
        { key: 'poses', label: '姿态库', children:
          <Library controls={<span>选择全身动作，作为试穿时的姿态参考。</span>} assets={poses} addLabel="上传姿态"
            pending={assets.isPending} useLabel="使用此姿态" onAdd={() => setDialog('pose')} onSelect={useAsset} {...deleteControls} /> },
        { key: 'history', label: '试穿记录', children: <section className="tryon-library">
          <TryOnTaskCards jobs={tryOnJobs} assets={byId} pending={jobs.isPending} label={comboLabel}
            onView={showJob} onRetry={retryJob} retrying={isSubmitting} />
        </section> },
      ]} />
      {picker && <TryOnReferencePicker kind={picker} assets={picker === 'garment' ? garments : picker === 'person' ? people : poses}
        outfits={outfitItems} selected={picker === 'garment' ? selection.garmentIds : [picker === 'person' ? selection.personId : selection.poseId]}
        onPick={chooseReference} onAdd={openAssetDialog} onClose={() => setPicker(undefined)} />}
      <TryOnAssetDialog kind="garment" open={dialog === 'garment'} models={models.data} imageModel={modelCode}
        onClose={() => setDialog(undefined)} onSaved={saveAsset} onQueued={garmentsQueued} />
      {dialog && dialog !== 'garment' && <TryOnAssetDialog kind={dialog} models={models.data} imageModel={modelCode}
        onClose={() => setDialog(undefined)} onSaved={saveAsset} />}
    </main>
  )
}

function Reference({ title, asset, onPick, onClear }: {
  title: string; asset?: TryOnAsset; onPick: () => void; onClear?: () => void
}) {
  return (
    <div className="tryon-reference" data-empty={!asset}>
      <div className="tryon-thumb">{asset && !asset.deleted ? <Image src={tryOnAssetURL(asset.id)} alt={asset.name} /> : <PictureOutlined />}</div>
      <div className="tryon-reference-info">
        <div><strong>{title}</strong><small>{asset?.name ?? '未选择'}</small></div>
        <div className="tryon-reference-actions">
          <Button size="small" onClick={onPick} aria-label={`选择${title}`}>{asset ? '更换' : '选择'}</Button>
          {onClear && <Button size="small" type="text" onClick={onClear}>清空</Button>}
        </div>
      </div>
    </div>
  )
}

function TryOnTaskCards({ jobs, assets, pending, label, onView, onRetry, retrying, strip = false }: {
  jobs: TryOnJob[]; assets: ReadonlyMap<string, TryOnAsset>; pending: boolean
  label: (job: TryOnJob) => string; onView: (job: TryOnJob) => void
  onRetry: (job: TryOnJob) => void; retrying: boolean; strip?: boolean
}) {
  if (pending) return <Spin />
  if (!jobs.length) return <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="生成试穿图后，任务会出现在这里。" />
  return <div className={strip ? 'tryon-task-grid tryon-task-strip' : 'tryon-task-grid'}
    tabIndex={strip ? 0 : undefined} role={strip ? 'group' : undefined} aria-label={strip ? '可横向滚动的试穿任务' : undefined}>
    {jobs.map((job) => {
      const combination = label(job)
      const cover = job.status === 'succeeded' ? tryOnResultURL(job.id)
        : assets.has(job.personId) ? tryOnAssetURL(job.personId) : undefined
      return <article key={job.id} className="tryon-task-card">
        <button className="tryon-task-preview" type="button" onClick={() => onView(job)}>
          <span className="tryon-task-cover">
            {cover ? <img src={cover} alt="" loading="lazy" /> : <PictureOutlined />}
            <Tag className="tryon-task-badge" color={jobStatuses[job.status].color}>{jobStatuses[job.status].label}</Tag>
            {job.status === 'running' && <span className="tryon-task-progress" aria-hidden><Spin size="small" /></span>}
          </span>
          <span className="tryon-task-info">
            <strong title={combination}>{combination}</strong>
            {activeJob(job) && <span className="tryon-task-garments" aria-hidden>
              {jobSelection(job).garmentIds.filter((id) => assets.has(id)).map((id) =>
                <img key={id} src={tryOnAssetURL(id)} alt="" loading="lazy" />)}
            </span>}
            <time dateTime={job.createdAt}>{formatDateTime(job.createdAt)}</time>
          </span>
        </button>
        {job.status === 'failed' && <div className="tryon-task-retry">
          {job.error && <small title={job.error}>{job.error}</small>}
          <Button size="small" disabled={retrying} onClick={() => onRetry(job)}>重新生成</Button>
        </div>}
      </article>
    })}
  </div>
}

function Library({ assets, controls, addLabel, useLabel = '去试穿', pending, onAdd, onSelect, deleting, onDelete }: {
  assets: TryOnAsset[]; controls: React.ReactNode; addLabel: string; useLabel?: string; pending: boolean; onAdd: () => void
  onSelect: (asset: TryOnAsset) => void; deleting: boolean; onDelete: (asset: TryOnAsset) => Promise<void>
}) {
  return (
    <section className="tryon-library">
      <div className="tryon-library-tools">{controls}<Button type="primary" icon={<PlusOutlined />} onClick={onAdd}>{addLabel}</Button></div>
      {pending ? <Spin /> : !assets.length ? <Empty description="这里还没有素材，添加后就可以去试穿。" /> :
        <div className="tryon-library-grid">{assets.map((asset) =>
          <article key={asset.id}>
            <div className={`tryon-library-image ${personCoverClass(asset)}`}>
              <Image src={tryOnAssetURL(asset.id)} alt={asset.name} loading="lazy" />
            </div>
            <div className="tryon-library-card-details">
              <div className="tryon-library-meta"><strong>{asset.name}</strong><small>{asset.builtin ? '内置 · ' : ''}{asset.category || tryOnLabels[asset.kind]}</small></div>
              {!asset.builtin && <TryOnDeleteMenu name={asset.name} disabled={deleting}
                description="删除后将从素材库移除，相关试穿记录仍可查看。" onDelete={() => onDelete(asset)} />}
            </div>
            <div className="tryon-library-actions">
              <Button size="small" onClick={() => onSelect(asset)}>{useLabel}</Button>
            </div>
          </article>,
        )}</div>}
    </section>
  )
}
