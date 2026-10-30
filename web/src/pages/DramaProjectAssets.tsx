import { NodeIndexOutlined } from '@ant-design/icons'
import { Button, Empty, Select } from 'antd'
import { useSearchParams } from 'react-router-dom'
import type { DramaBatch, DramaProject } from '../api/drama'
import { DramaAssetGallery } from './DramaAssetGallery'
import { DramaImageSettingsButton } from './DramaImageTools'

export function DramaProjectAssets({ project, batches }: { project: DramaProject; batches: DramaBatch[] }) {
  const [params, setParams] = useSearchParams()
  const field = params.get('asset') === 'scenes' ? 'scenes' : params.get('asset') === 'props' ? 'props' : 'characters'
  const source = params.get('sourceBatch') || undefined
  const usage = params.get('usedBatch') || undefined
  const groups = ([{ key: 'characters', label: '角色', kind: 'character' }, { key: 'scenes', label: '场景', kind: 'scene' }, { key: 'props', label: '道具', kind: 'prop' }] as const).map((group) => ({ ...group, assets: project[group.key].filter((asset) => (!source || asset.sourceBatch?.id === source) && (!usage || asset.usedInBatches?.some((batch) => batch.id === usage))) }))
  const selected = groups.find((group) => group.key === field)!
  const options = batches.map((batch) => ({ value: batch.id, label: batch.name }))
  function change(key: string, value?: string) { setParams((current) => { const next = new URLSearchParams(current); if (value) next.set(key, value); else next.delete(key); return next }, { replace: true }) }
  function enterCanvas() { setParams((current) => { const next = new URLSearchParams(current); next.set('canvas', '1'); next.delete('focus'); next.delete('selected'); return next }) }
  return <section aria-label="角色、场景、道具">
    <div className="drama-asset-toolbar">
      <div className="drama-asset-tabs" role="tablist" aria-label="资产类型">{groups.map((group) => <button type="button" role="tab" key={group.key} aria-selected={field === group.key} className={field === group.key ? 'active' : ''} onClick={() => change('asset', group.key === 'characters' ? undefined : group.key)}>{group.label}<span>{group.assets.length}</span></button>)}</div>
      <div className="drama-asset-toolbar-actions">
        <Select aria-label="来源批次筛选" placeholder="来源批次" allowClear showSearch optionFilterProp="label" value={source} options={options} onChange={(value) => change('sourceBatch', value)} notFoundContent="暂无批次" />
        <Select aria-label="应用批次筛选" placeholder="应用批次" allowClear showSearch optionFilterProp="label" value={usage} options={options} onChange={(value) => change('usedBatch', value)} notFoundContent="暂无批次" />
        <DramaImageSettingsButton /><Button icon={<NodeIndexOutlined />} onClick={enterCanvas}>进入画布</Button>
      </div>
    </div>
    <div role="tabpanel" aria-label={selected.label}>{selected.assets.length ? <DramaAssetGallery projectId={project.id} kind={selected.kind} assets={selected.assets} /> : <div className="drama-empty"><Empty description={source || usage ? '没有符合筛选条件的资产' : '剧本解析完成后，内容会出现在这里'} />{(source || usage) && <Button onClick={() => { setParams((current) => { const next = new URLSearchParams(current); next.delete('sourceBatch'); next.delete('usedBatch'); return next }, { replace: true }) }}>清除筛选</Button>}</div>}</div>
  </section>
}
