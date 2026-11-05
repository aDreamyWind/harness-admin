import {
  CameraOutlined, HistoryOutlined, PictureOutlined, PlusOutlined, SkinOutlined, UserOutlined,
} from '@ant-design/icons'
import { Alert, Button, Empty, Image, Segmented, Select, Spin, Tabs, Tag } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'
import {
  getTryOnAssets, getTryOnJobs, getTryOnModels, startTryOn, tryOnAssetURL, tryOnResultURL,
  type TryOnAsset, type TryOnInput, type TryOnJob, type TryOnKind,
} from '../api/tryon'
import { TryOnAssetDialog, tryOnCategories, tryOnLabels } from './TryOnAssetDialog'
import './tryon.css'

const assetsKey = ['tryon', 'assets']
const jobsKey = ['tryon', 'jobs']
const modelsKey = ['tryon', 'models']
const emptySelection = { personId: '', garmentId: '', poseId: '' }
const jobLabels = { running: '生成中', succeeded: '已完成', failed: '生成失败' }

function message(error: Error | null) {
  return error?.message ?? '请求失败，请重试。'
}

export function TryOnPage() {
  const client = useQueryClient()
  const assets = useQuery({ queryKey: assetsKey, queryFn: getTryOnAssets })
  const models = useQuery({ queryKey: modelsKey, queryFn: getTryOnModels })
  const jobs = useQuery({
    queryKey: jobsKey, queryFn: getTryOnJobs,
    refetchInterval: (query) => query.state.data?.some((job) => job.status === 'running') ? 2200 : false,
  })
  const [selection, setSelection] = useState(emptySelection)
  const [chosenModel, setChosenModel] = useState<string>()
  const [jobId, setJobId] = useState('')
  const [tab, setTab] = useState('studio')
  const [personKind, setPersonKind] = useState<'person_2d' | 'person_real'>('person_2d')
  const [category, setCategory] = useState<string>()
  const [dialog, setDialog] = useState<TryOnKind>()
  const restored = useRef(false)

  const imageModels = models.data?.image ?? []
  const modelCode = imageModels.find((item) => item.code === chosenModel)?.code ?? imageModels[0]?.code
  const mutation = useMutation({
    mutationFn: startTryOn,
    onSuccess: (job) => {
      setSelection({ personId: job.personId, garmentId: job.garmentId, poseId: job.poseId ?? '' })
      setJobId(job.id)
      client.setQueryData<TryOnJob[]>(jobsKey, (old) => [job, ...(old ?? []).filter((item) => item.id !== job.id)])
      void client.invalidateQueries({ queryKey: jobsKey })
    },
    onError: () => { void client.invalidateQueries({ queryKey: jobsKey }) },
  })

  useEffect(() => {
    if (!jobs.data || restored.current) return
    restored.current = true
    const job = jobs.data[0]
    if (job) {
      setJobId(job.id)
      setSelection({ personId: job.personId, garmentId: job.garmentId, poseId: job.poseId ?? '' })
      setChosenModel(job.modelCode)
    }
  }, [jobs.data])

  const allAssets = assets.data ?? []
  const people = allAssets.filter((a) => a.kind === 'person_2d' || a.kind === 'person_real')
  const garments = allAssets.filter((a) => a.kind === 'garment')
  const poses = allAssets.filter((a) => a.kind === 'pose')
  const person = people.find((a) => a.id === selection.personId)
  const garment = garments.find((a) => a.id === selection.garmentId)
  const pose = poses.find((a) => a.id === selection.poseId)
  const current = jobs.data?.find((item) => item.id === jobId)
  const running = jobs.data?.find((item) => item.status === 'running')

  function select(field: keyof typeof emptySelection, id = '') {
    restored.current = true
    setSelection((old) => ({ ...old, [field]: id }))
    setJobId('')
    mutation.reset()
  }

  function useAsset(asset: TryOnAsset) {
    select(asset.kind === 'garment' ? 'garmentId' : asset.kind === 'pose' ? 'poseId' : 'personId', asset.id)
    setTab('studio')
  }

  function saveAsset(asset: TryOnAsset) {
    client.setQueryData<TryOnAsset[]>(assetsKey, (old) => [asset, ...(old ?? []).filter((item) => item.id !== asset.id)])
    void client.invalidateQueries({ queryKey: assetsKey })
    useAsset(asset)
  }

  function useJob(job: TryOnJob) {
    setSelection({ personId: job.personId, garmentId: job.garmentId, poseId: job.poseId ?? '' })
    setJobId(job.id)
    setChosenModel(job.modelCode)
    mutation.reset()
    setTab('studio')
  }

  const refError = assets.error ?? jobs.error
  const blocked = !person || !garment || !modelCode || Boolean(running) || mutation.isPending
  const submit = () => {
    if (blocked || !person || !garment || !modelCode) return
    const input: TryOnInput = { personId: person.id, garmentId: garment.id, poseId: pose?.id, modelCode }
    mutation.mutate(input)
  }

  return (
    <main className="tryon-page">
      <header className="tryon-heading">
        <div><div className="tryon-kicker"><SkinOutlined /> 虚拟试衣工作室</div>
          <h1>AI 试衣</h1><p>为同一个人物试穿不同衣物，并排看效果。</p></div>
        <Button icon={<HistoryOutlined />} onClick={() => setTab('history')}>试穿记录</Button>
      </header>
      {refError && <Alert type="error" showIcon title={message(refError)}
        action={<Button size="small" onClick={() => { void assets.refetch(); void jobs.refetch() }}>重试</Button>} />}
      {models.isError && <Alert type="error" showIcon title={message(models.error)}
        action={<Button size="small" onClick={() => void models.refetch()}>重新加载模型</Button>} />}
      <Tabs activeKey={tab} onChange={setTab} items={[
        { key: 'studio', label: '试衣台', children:
          <div className="tryon-workspace">
            <section className="tryon-stage" aria-label="原图和试穿效果">
              <header className="tryon-stage-header">
                <div><span>效果对比</span><h2>试穿镜</h2></div>
                {current?.status === 'succeeded' && <Button href={tryOnResultURL(current.id)} download="试穿效果">下载效果图</Button>}
                {current?.status === 'running' && <Tag color="processing">正在生成</Tag>}
              </header>
              <div className="tryon-comparison">
                <figure><figcaption>人物原图</figcaption><div className="tryon-stage-picture">
                  {person ? <Image src={tryOnAssetURL(person.id)} alt={person.name} /> :
                    <div className="tryon-placeholder"><UserOutlined /><span>选择一位人物</span></div>}
                </div></figure>
                <figure><figcaption>试穿效果</figcaption><div className="tryon-stage-picture">
                  {current?.status === 'succeeded' ? <Image src={tryOnResultURL(current.id)} alt="试穿效果图" /> :
                    <div className="tryon-placeholder">
                      {current?.status === 'running' || mutation.isPending ? <Spin /> : <PictureOutlined />}
                      <span>{current?.status === 'running' || mutation.isPending ? '正在生成试穿图' : current?.status === 'failed' ? '这次试穿没有完成' : '效果图会出现在这里'}</span>
                    </div>}
                </div></figure>
              </div>
              {current?.status === 'failed' && <Alert type="error" showIcon title={current.error ?? '请重新生成。'} />}
              {running && running.id !== jobId && <Alert type="info" showIcon title="已有一组试穿正在生成。"
                action={<Button size="small" onClick={() => useJob(running)}>查看任务</Button>} />}
              {mutation.isError && <Alert type="error" showIcon title={message(mutation.error)} />}
            </section>
            <aside className="tryon-controls" aria-label="本次生成的参考图">
              <div className="tryon-control-heading"><h2>生成参考</h2><span>{Number(!!person) + Number(!!garment) + Number(!!pose)} 张参考图</span></div>
              <Reference title="人物" detail="保留身份、体型和画风" asset={person} options={people}
                onSelect={(id) => select('personId', id)} onAdd={() => setDialog(personKind)} />
              <Reference title="衣物" detail="参考款式、颜色和纹理" asset={garment} options={garments}
                onSelect={(id) => select('garmentId', id)} onAdd={() => setDialog('garment')} />
              <Reference title="姿势（可选）" detail={pose ? '只参考动作和朝向' : '默认保留人物原姿势'} asset={pose} options={poses}
                onSelect={(id) => select('poseId', id)} onAdd={() => setDialog('pose')} />
              <label className="tryon-model">图片模型
                <Select value={modelCode} loading={models.isPending} onChange={setChosenModel} placeholder="暂无可用图片模型"
                  options={imageModels.map((item) => ({ label: item.name, value: item.code }))} />
              </label>
              {models.isSuccess && imageModels.length === 0 &&
                <small>请在模型目录启用支持多张参考图的图片模型，并检查调用权限。</small>}
              <Button type="primary" size="large" block icon={<CameraOutlined />} loading={mutation.isPending}
                disabled={blocked} onClick={submit}>{running ? '试穿生成中' : '生成试穿图'}</Button>
            </aside>
          </div> },
        { key: 'people', label: '人物库', children:
          <Library
            controls={<Segmented value={personKind} onChange={setPersonKind} options={[
              { label: '2D 插画', value: 'person_2d' }, { label: '真人 / 写实', value: 'person_real' },
            ]} />}
            assets={people.filter((a) => a.kind === personKind)} addLabel="添加人物" pending={assets.isPending}
            onAdd={() => setDialog(personKind)} onSelect={useAsset} /> },
        { key: 'garments', label: '衣物库', children:
          <Library controls={<Select aria-label="筛选衣物分类" placeholder="全部分类" allowClear value={category}
              onChange={setCategory} style={{ width: 170 }} options={tryOnCategories.map((value) => ({ label: value, value }))} />}
            assets={garments.filter((a) => !category || a.category === category)} addLabel="添加衣物" pending={assets.isPending}
            onAdd={() => setDialog('garment')} onSelect={useAsset} /> },
        { key: 'history', label: '试穿记录', children: <section className="tryon-library">
          {jobs.isPending ? <Spin /> : !jobs.data?.length ? <Empty description="生成试穿图后，记录会出现在这里。" /> :
            <div className="tryon-history">{jobs.data.map((job) => {
              const label = [job.personId, job.garmentId].map((id) => allAssets.find((item) => item.id === id)?.name ?? '素材').join(' / ')
              return <button className="tryon-history-item" type="button" key={job.id} onClick={() => useJob(job)}>
                {job.status === 'succeeded' ? <img src={tryOnResultURL(job.id)} alt="" loading="lazy" /> :
                  <span className="tryon-history-icon"><HistoryOutlined /></span>}
                <span><strong>{label}</strong><small>{new Date(job.createdAt).toLocaleString('zh-CN')}</small></span>
                <Tag color={job.status === 'succeeded' ? 'success' : job.status === 'running' ? 'processing' : 'error'}>{jobLabels[job.status]}</Tag>
              </button>
            })}</div>}
        </section> },
      ]} />
      {dialog && <TryOnAssetDialog kind={dialog} models={models.data} imageModel={modelCode} onClose={() => setDialog(undefined)} onSaved={saveAsset} />}
    </main>
  )
}

function Reference({ title, detail, asset, options, onSelect, onAdd }: {
  title: string; detail: string; asset?: TryOnAsset; options: TryOnAsset[]; onSelect: (id?: string) => void; onAdd: () => void
}) {
  return (
    <div className="tryon-reference">
      <div className="tryon-thumb">{asset ? <Image src={tryOnAssetURL(asset.id)} alt={asset.name} /> : <PictureOutlined />}</div>
      <div className="tryon-reference-info">
        <div><strong>{title}</strong><small>{detail}</small></div>
        <div className="tryon-picker">
          <Select aria-label={`选择${title}`} showSearch optionFilterProp="label" allowClear value={asset?.id}
            onChange={onSelect} placeholder={options.length ? '从素材库选择' : '暂无素材'}
            options={options.map((a) => ({ label: `${a.name} · ${a.category || tryOnLabels[a.kind]}`, value: a.id }))} />
          <Button icon={<PlusOutlined />} aria-label={`添加${title}`} onClick={onAdd} />
        </div>
      </div>
    </div>
  )
}

function Library({ assets, controls, addLabel, pending, onAdd, onSelect }: {
  assets: TryOnAsset[]; controls: React.ReactNode; addLabel: string; pending: boolean; onAdd: () => void; onSelect: (asset: TryOnAsset) => void
}) {
  return (
    <section className="tryon-library">
      <div className="tryon-library-tools">{controls}<Button type="primary" icon={<PlusOutlined />} onClick={onAdd}>{addLabel}</Button></div>
      {pending ? <Spin /> : !assets.length ? <Empty description="这里还没有素材，添加后就可以去试穿。" /> :
        <div className="tryon-library-grid">{assets.map((asset) =>
          <article key={asset.id}>
            <div className="tryon-library-image"><Image src={tryOnAssetURL(asset.id)} alt={asset.name} loading="lazy" /></div>
            <div className="tryon-library-meta"><strong>{asset.name}</strong><small>{asset.category || tryOnLabels[asset.kind]}</small></div>
            <Button block onClick={() => onSelect(asset)}>去试穿</Button>
          </article>,
        )}</div>}
    </section>
  )
}
