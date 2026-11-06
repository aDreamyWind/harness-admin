import { DeleteOutlined, EllipsisOutlined, PlusOutlined } from '@ant-design/icons'
import { Alert, Button, Dropdown, Empty, Image, Input, Modal, Spin, message } from 'antd'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { createTryOnOutfit, deleteTryOnOutfit, tryOnAssetURL, type TryOnAsset, type TryOnOutfit } from '../api/tryon'
import { tryOnCategories, tryOnLabels } from './TryOnAssetDialog'

export const maxTryOnGarments = 4
export const outfitsKey = ['tryon', 'outfits']
export const personFilters = [
  { value: 'all', label: '全部' },
  { value: 'person_2d', label: tryOnLabels.person_2d },
  { value: 'person_real', label: '写实' },
]
export const garmentFilters = [
  { value: 'all', label: '全部' },
  ...tryOnCategories.map((category) => ({ value: category, label: category })),
  { value: 'outfits', label: '套装' },
]

export function TryOnFilters({ label, options, value, onChange }: {
  label: string; options: { value: string; label: string }[]; value: string; onChange: (value: string) => void
}) {
  return <div className="tryon-filters" role="group" aria-label={label}>
    {options.map((option) => <Button key={option.value} size="small" type={option.value === value ? 'primary' : 'text'}
      aria-pressed={option.value === value} onClick={() => onChange(option.value)}>{option.label}</Button>)}
  </div>
}

export function TryOnDeleteMenu({ name, description, disabled, onDelete }: {
  name: string; description: string; disabled: boolean; onDelete: () => Promise<unknown>
}) {
  const [modal, holder] = Modal.useModal()
  return <>
    {holder}
    <Dropdown trigger={['click']} placement="bottomRight" autoFocus menu={{
      items: [{ key: 'delete', label: '删除', icon: <DeleteOutlined aria-hidden />, danger: true }],
      onClick: () => {
        modal.confirm({
          title: `删除「${name}」？`, content: description, okText: '确认删除', cancelText: '取消',
          okButtonProps: { danger: true }, focusable: { autoFocusButton: 'cancel' }, onOk: onDelete,
        })
      },
    }}>
      <Button type="text" className="tryon-library-more" icon={<EllipsisOutlined />} disabled={disabled}
        title="更多操作" aria-label={`「${name}」的更多操作`} aria-haspopup="menu" />
    </Dropdown>
  </>
}

function AssetImage({ asset, preview = false }: { asset: TryOnAsset; preview?: boolean }) {
  return <Image src={tryOnAssetURL(asset.id)} alt={asset.name} preview={preview} loading="lazy" />
}

export function personCoverClass(asset: TryOnAsset) {
  if (asset.kind !== 'person_2d' && asset.kind !== 'person_real') return ''
  return asset.builtin ? 'tryon-person-cover tryon-person-cover-builtin' : 'tryon-person-cover'
}

function OutfitImages({ outfit, garments }: { outfit: TryOnOutfit; garments: TryOnAsset[] }) {
  return <div className="tryon-outfit-images">
    {outfit.garmentIds.map((id) => {
      const garment = garments.find((asset) => asset.id === id)
      return <div key={id}>{garment ? <AssetImage asset={garment} /> : <small>衣物不可用</small>}</div>
    })}
  </div>
}

export function outfitAvailable(outfit: TryOnOutfit, garments: TryOnAsset[]) {
  return outfit.garmentIds.every((id) => garments.some((asset) => asset.id === id))
}

export type TryOnPickerKind = 'person' | 'garment' | 'pose'

const pickerLabels = {
  person: { title: '选择人物', add: '添加人物', action: '使用此人物' },
  garment: { title: '选择衣物或套装', add: '添加新衣物', action: '加入试衣组合' },
  pose: { title: '选择姿态', add: '上传姿态', action: '使用此姿态' },
}

export function TryOnReferencePicker({ kind, assets, outfits, selected, onPick, onAdd, onClose }: {
  kind: TryOnPickerKind; assets: TryOnAsset[]; outfits: TryOnOutfit[]; selected: string[]
  onPick: (ids: string[]) => void; onAdd: () => void; onClose: () => void
}) {
  const [filter, setFilter] = useState('all')
  const [search, setSearch] = useState('')
  const filters = kind === 'person' ? personFilters : kind === 'garment' ? garmentFilters : []
  const config = pickerLabels[kind]
  const query = search.trim().toLowerCase()
  const matches = (name: string) => !query || name.toLowerCase().includes(query)
  const shown = assets.filter((asset) => matches(asset.name) &&
    (filter === 'all' || (kind === 'person' ? asset.kind === filter : asset.category === filter)))
  const shownOutfits = outfits.filter((outfit) => matches(outfit.name))
  const availableSlots = maxTryOnGarments - selected.length
  return (
    <Modal open title={config.title} width={900} onCancel={onClose}
      footer={<><Button onClick={onAdd} icon={<PlusOutlined />}>{config.add}</Button><Button onClick={onClose}>关闭</Button></>}>
      <div className="tryon-picker-content">
        {filters.length > 0 && <TryOnFilters label={kind === 'person' ? '人物分类' : '衣物分类'} options={filters} value={filter} onChange={setFilter} />}
        <Input aria-label="搜索素材名称" placeholder={filter === 'outfits' ? '搜索套装名称' : '搜索素材名称'}
          allowClear value={search} onChange={(event) => setSearch(event.target.value)} />
        {kind === 'garment' && <small>已选 {selected.length} 件，还可添加 {availableSlots} 件。</small>}
        {filter === 'outfits' ? shownOutfits.length ? (
          <div className="tryon-picker-grid">
            {shownOutfits.map((outfit) => {
              const additions = outfit.garmentIds.filter((id) => !selected.includes(id))
              const available = outfitAvailable(outfit, assets)
              const note = !available ? '含不可用衣物' : !additions.length ? '已加入' :
                additions.length > availableSlots ? '超出剩余件数' : '加入套装'
              return <article key={outfit.id}>
                <OutfitImages outfit={outfit} garments={assets} />
                <strong>{outfit.name}</strong><small>{outfit.garmentIds.length} 件衣物</small>
                <Button size="small" disabled={!available || !additions.length || additions.length > availableSlots}
                  aria-label={`${note}${outfit.name}`} onClick={() => onPick(additions)}>{note}</Button>
              </article>
            })}
          </div>
        ) : <Empty description="没有找到套装。" /> : shown.length ? (
          <div className="tryon-picker-grid">
            {shown.map((asset) => {
              const alreadySelected = selected.includes(asset.id)
              return <article key={asset.id}>
                <div className={`tryon-picker-image ${personCoverClass(asset)}`}><AssetImage asset={asset} preview /></div>
                <strong>{asset.name}</strong><small>{asset.category || tryOnLabels[asset.kind]}</small>
                <Button size="small" aria-label={`${config.action}：${asset.name}`}
                  disabled={alreadySelected || (kind === 'garment' && availableSlots <= 0)}
                  onClick={() => onPick([asset.id])}>{alreadySelected ? '已选择' : config.action}</Button>
              </article>
            })}
          </div>
        ) : <Empty description="没有找到素材。" />}
      </div>
    </Modal>
  )
}

export function TryOnOutfitLibrary({ outfits, garments, loading, error, onRefresh, onUse }: {
  outfits: TryOnOutfit[]; garments: TryOnAsset[]; loading: boolean; error: Error | null; onRefresh: () => void
  onUse: (outfit: TryOnOutfit) => void
}) {
  const client = useQueryClient()
  const [notice, holder] = message.useMessage()
  const [creating, setCreating] = useState(false)
  const [viewing, setViewing] = useState<TryOnOutfit>()
  const create = useMutation({
    mutationFn: createTryOnOutfit,
    onSuccess: (outfit) => {
      client.setQueryData<TryOnOutfit[]>(outfitsKey, (old) => [outfit, ...(old ?? []).filter((item) => item.id !== outfit.id)])
      void client.invalidateQueries({ queryKey: outfitsKey })
      setCreating(false)
      void notice.success('套装已创建')
    },
  })
  const remove = useMutation({
    mutationFn: deleteTryOnOutfit,
    onSuccess: (_, id) => {
      client.setQueryData<TryOnOutfit[]>(outfitsKey, (old) => old?.filter((item) => item.id !== id))
      void client.invalidateQueries({ queryKey: outfitsKey })
      void notice.success('套装已删除')
    },
    onError: (failure) => { void notice.error(failure.message) },
  })
  return <>
    {holder}
    <div className="tryon-outfit-toolbar">
      <span>把常用的 2–4 件衣物存成套装。</span>
      <Button type="primary" icon={<PlusOutlined />} disabled={garments.length < 2} onClick={() => {
        create.reset()
        setCreating(true)
      }}>创建套装</Button>
    </div>
    {error && <Alert type="error" showIcon title={error.message}
      action={<Button size="small" onClick={onRefresh}>重试</Button>} />}
    {loading ? <Spin /> : outfits.length ? (
      <div className="tryon-outfit-grid">
        {outfits.map((outfit) => <article key={outfit.id}>
          <OutfitImages outfit={outfit} garments={garments} />
          <div className="tryon-library-card-details">
            <div className="tryon-library-meta"><strong>{outfit.name}</strong><small>{outfit.garmentIds.length} 件衣物</small></div>
            <TryOnDeleteMenu name={outfit.name} description="只删除套装，单品仍在衣物库。" disabled={remove.isPending}
              onDelete={() => remove.mutateAsync(outfit.id).catch(() => undefined)} />
          </div>
          <div className="tryon-library-actions">
            <Button size="small" onClick={() => setViewing(outfit)}>查看</Button>
            <Button size="small" disabled={!outfitAvailable(outfit, garments)} onClick={() => onUse(outfit)}>使用套装</Button>
          </div>
        </article>)}
      </div>
    ) : !error && <Empty description={garments.length < 2 ? '先添加至少两件衣物，再创建套装。' : '还没有套装。'} />}
    {creating && <OutfitCreator garments={garments} saving={create.isPending} error={create.error}
      onSave={(input) => create.mutate(input)} onClose={() => setCreating(false)} />}
    {viewing && <Modal open title={viewing.name} width={720} onCancel={() => setViewing(undefined)}
      footer={<><Button onClick={() => setViewing(undefined)}>关闭</Button>
        <Button type="primary" disabled={!outfitAvailable(viewing, garments)} onClick={() => {
          onUse(viewing)
          setViewing(undefined)
        }}>使用套装</Button></>}>
      <div className="tryon-picker-grid">
        {viewing.garmentIds.map((id) => {
          const asset = garments.find((item) => item.id === id)
          return <article key={id}>
            {asset ? <><div className="tryon-picker-image"><AssetImage asset={asset} preview /></div><strong>{asset.name}</strong>
              <small>{asset.category}</small></> : <small>衣物不可用</small>}
          </article>
        })}
      </div>
    </Modal>}
  </>
}

function OutfitCreator({ garments, saving, error, onSave, onClose }: {
  garments: TryOnAsset[]; saving: boolean; error: Error | null
  onSave: (input: { name: string; garmentIds: string[] }) => void; onClose: () => void
}) {
  const [name, setName] = useState('')
  const [selected, setSelected] = useState<string[]>([])
  const [search, setSearch] = useState('')
  const selectedGarments = selected.filter((id) => garments.some((asset) => asset.id === id))
  const shown = garments.filter((asset) => asset.name.toLowerCase().includes(search.trim().toLowerCase()))
  return (
    <Modal open title="创建套装" width={820} onCancel={onClose} maskClosable={!saving} closable={!saving} keyboard={!saving}
      footer={<><Button onClick={onClose} disabled={saving}>取消</Button>
        <Button type="primary" loading={saving} disabled={!name.trim() || selectedGarments.length < 2}
          onClick={() => onSave({ name: name.trim(), garmentIds: selectedGarments })}>保存套装</Button></>}>
      <div className="tryon-picker-content">
        {error && <Alert type="error" showIcon title={error.message} />}
        <label className="tryon-model">套装名称<Input value={name} maxLength={80} disabled={saving}
          placeholder="给套装起个名字" onChange={(event) => setName(event.target.value)} /></label>
        <div className="tryon-outfit-toolbar"><strong>已选 {selectedGarments.length} / {maxTryOnGarments} 件</strong>
          <small>从衣物库选 2–4 件</small></div>
        <Input aria-label="搜索衣物" allowClear placeholder="搜索衣物" value={search} onChange={(event) => setSearch(event.target.value)} />
        <div className="tryon-picker-grid" role="group" aria-label="选择套装单品">
          {shown.map((asset) => {
            const checked = selectedGarments.includes(asset.id)
            return <label className="tryon-outfit-choice" key={asset.id} data-checked={checked}>
              <div className="tryon-picker-image"><AssetImage asset={asset} /></div>
              <span><input type="checkbox" checked={checked} disabled={saving || (!checked && selectedGarments.length >= maxTryOnGarments)}
                aria-label={asset.name} onChange={(event) => setSelected(event.target.checked
                  ? [...selectedGarments, asset.id] : selectedGarments.filter((id) => id !== asset.id))} /> {asset.name}</span>
              <small>{asset.category}</small>
            </label>
          })}
        </div>
        {!shown.length && <Empty description="没有找到衣物。" />}
      </div>
    </Modal>
  )
}
