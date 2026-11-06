import { Alert, Button, Modal, Spin, Tag, message } from 'antd'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import {
  retryTryOnGarmentJob, saveTryOnGarmentJob, tryOnGarmentResultURL, type TryOnAsset, type TryOnGarmentJob,
} from '../api/tryon'

export const garmentJobsKey = ['tryon', 'garment-jobs']

const statuses = {
  queued: { label: '排队中', color: 'default' },
  running: { label: '生成中', color: 'processing' },
  succeeded: { label: '待确认', color: 'success' },
  failed: { label: '生成失败', color: 'error' },
  saved: { label: '已入库', color: 'default' },
}

export function TryOnGarmentJobs({ jobs, loading, error, onRefresh, onSaved }: {
  jobs: TryOnGarmentJob[]; loading: boolean; error: Error | null; onRefresh: () => void; onSaved: (asset: TryOnAsset) => void
}) {
  const client = useQueryClient()
  const [messages, holder] = message.useMessage()
  const [viewing, setViewing] = useState<TryOnGarmentJob>()
  const retry = useMutation({
    mutationFn: retryTryOnGarmentJob,
    onSuccess: async (job) => {
      await client.cancelQueries({ queryKey: garmentJobsKey })
      client.setQueryData<TryOnGarmentJob[]>(garmentJobsKey, (old) =>
        old?.map((item) => item.id === job.id ? job : item))
      void client.invalidateQueries({ queryKey: garmentJobsKey })
      void messages.success('已重新提交生成')
    },
    onError: (error) => { void messages.error(error.message) },
  })
  const save = useMutation({
    mutationFn: saveTryOnGarmentJob,
    onSuccess: async (asset, id) => {
      await client.cancelQueries({ queryKey: garmentJobsKey })
      client.setQueryData<TryOnGarmentJob[]>(garmentJobsKey, (old) => old?.filter((job) => job.id !== id))
      void client.invalidateQueries({ queryKey: garmentJobsKey })
      onSaved(asset)
      setViewing(undefined)
      void messages.success('衣物已入库')
    },
    onError: (error) => { void messages.error(error.message) },
  })

  return (
    <section className="tryon-library tryon-garment-jobs" aria-label="衣物生成任务">
      {holder}
      <header className="tryon-control-heading">
        <div>
          <h2>衣物生成任务</h2>
          <span>后台生成完整单品图，确认后加入衣物库。</span>
        </div>
        <Button size="small" onClick={onRefresh}>刷新任务</Button>
      </header>
      {error && <Alert type="error" showIcon title={error.message} />}
      {loading ? <Spin /> : jobs.length ? (
        <ul className="tryon-garment-job-list">
          {jobs.map((job) => <li key={job.id}>
            <div className="tryon-garment-job-info">
              <strong>{job.name}</strong>
              <small>{job.category}</small>
              {job.status === 'failed' && job.error && <small className="tryon-garment-job-error">{job.error}</small>}
            </div>
            <div className="tryon-garment-job-actions">
              <Tag color={statuses[job.status].color}>{statuses[job.status].label}</Tag>
              {job.status === 'failed' && <Button size="small" disabled={retry.isPending}
                loading={retry.isPending && retry.variables === job.id} onClick={() => retry.mutate(job.id)}>重试</Button>}
              {job.status === 'succeeded' && <Button size="small" type="primary" onClick={() => setViewing(job)}>查看并确认</Button>}
            </div>
          </li>)}
        </ul>
      ) : !error && <small className="tryon-garment-job-empty">暂无待处理任务。</small>}
      {viewing && <GarmentResultDialog key={viewing.id} job={viewing} saving={save.isPending}
        onSave={() => save.mutate(viewing.id)} onClose={() => setViewing(undefined)} />}
    </section>
  )
}

function GarmentResultDialog({ job, saving, onSave, onClose }: {
  job: TryOnGarmentJob; saving: boolean; onSave: () => void; onClose: () => void
}) {
  const [status, setStatus] = useState<'loading' | 'loaded' | 'error'>('loading')
  return (
    <Modal open title={`确认「${job.name}」`} onCancel={onClose} maskClosable={!saving} closable={!saving} keyboard={!saving}
      footer={[
        <Button key="back" disabled={saving} onClick={onClose}>返回</Button>,
        <Button key="save" type="primary" disabled={status !== 'loaded'} loading={saving} onClick={onSave}>确认入库</Button>,
      ]}>
      <div className="tryon-garment-result">
        <p>{job.category} · 遮挡处由 AI 补全，请确认款式和细节。</p>
        {status === 'loading' && <Spin />}
        {status === 'error' && <Alert type="error" showIcon title="无法加载生成图，请关闭后重新打开。" />}
        {status !== 'error' && <img src={tryOnGarmentResultURL(job.id)} alt={`${job.name}的生成图`}
          onLoad={() => setStatus('loaded')} onError={() => setStatus('error')} />}
      </div>
    </Modal>
  )
}
