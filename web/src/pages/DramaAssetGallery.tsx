import { PictureOutlined, EyeOutlined, GiftOutlined, NodeIndexOutlined, UserOutlined } from '@ant-design/icons'
import { Alert, Button, Modal, Tooltip, message } from 'antd'
import { useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'

import { generateDramaImage, type DramaAsset, type DramaAssetKind } from '../api/drama'
import { useDramaCanvas } from './DramaImageTools'

type GalleryAsset = DramaAsset & { stateLabel?: string; assetId?: string }

const kindNames: Record<DramaAssetKind, string> = { character: '角色', scene: '场景', prop: '道具' }
const kindIcons = { character: <UserOutlined />, scene: <PictureOutlined />, prop: <GiftOutlined /> }

// 资产关联保持不变；图片及派生形象从项目画布读取。
export function DramaAssetGallery({ assets, kind, projectId, compact = false }: { assets: GalleryAsset[]; kind: DramaAssetKind; projectId: string; compact?: boolean }) {
  const [preview, setPreview] = useState<GalleryAsset | null>(null)
  const canvas = useDramaCanvas(projectId)
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const [messageApi, holder] = message.useMessage()
  const [starting, setStarting] = useState<string | null>(null)
  const formFor = (asset: GalleryAsset) => canvas.data?.forms.find((f) => f.assetId === (asset.assetId || asset.id) && !f.parentId)
  const countFor = (asset: GalleryAsset) => canvas.data?.forms.filter((f) => f.assetId === (asset.assetId || asset.id)).length || 1
  function enterCanvas(asset: GalleryAsset) { const params = new URLSearchParams(searchParams); params.set('canvas', '1'); params.set('focus', asset.assetId || asset.id || ''); navigate(`/ai-apps/drama/projects/${projectId}?${params}`) }
  async function generate(asset: GalleryAsset) { const form = formFor(asset); if (!form) return; setStarting(form.id); try { await generateDramaImage(projectId, form.id); await canvas.refetch(); messageApi.success('图片生成任务已开始') } catch (error) { messageApi.error(error instanceof Error ? error.message : '生成失败') } finally { setStarting(null) } }
  const previewForm = preview && formFor(preview)
  return <>
    {holder}
    {canvas.error && <Alert type="error" showIcon title="读取资产图片失败" action={<Button onClick={() => void canvas.refetch()}>重试</Button>} />}
    <div className={`drama-asset-gallery ${kind}${compact ? ' compact' : ''}`}>
      {assets.map((asset, index) => <article className="drama-asset-tile" key={asset.id || `${asset.name}-${index}`} aria-label={`${kindNames[kind]}：${asset.name}`}>
        <div className="drama-asset-cover">
          {formFor(asset)?.imageUrl ? <img className="drama-asset-image" src={formFor(asset)?.imageUrl} alt={asset.name} loading="lazy" /> : <div className="drama-asset-placeholder" aria-hidden="true">{kindIcons[kind]}<strong>{asset.name}</strong>{(formFor(asset)?.imageStatus === 'running' || formFor(asset)?.imageStatus === 'queued') && <small>正在生成…</small>}</div>}
          <span className="drama-asset-form-count">共 {countFor(asset)} 个形象</span>
          <span className="drama-asset-kind-label">{kindNames[kind]}</span>
          <div className="drama-asset-cover-overlay">
            <div className="drama-asset-cover-actions">
              <Button icon={<EyeOutlined />} aria-label={`预览${asset.name}`} onClick={() => setPreview(asset)}>预览</Button>
              <Button icon={<NodeIndexOutlined />} onClick={() => enterCanvas(asset)}>进入画布</Button>
            </div>
            <p>{asset.description || asset.appearanceDescription || '暂无描述'}</p>
          </div>
        </div>
        {formFor(asset)?.imageUrl && <h4 title={asset.name}>{asset.name}</h4>}
        {asset.stateLabel && <p className="drama-asset-state-label" title={asset.stateLabel}>{asset.stateLabel}</p>}
        {!compact && <>{!formFor(asset)?.imageUrl && <div className="drama-asset-tile-buttons">
          <Button type="primary" icon={<PictureOutlined />} loading={starting === formFor(asset)?.id || formFor(asset)?.imageStatus === 'queued' || formFor(asset)?.imageStatus === 'running'} disabled={!formFor(asset)} onClick={() => void generate(asset)}>{starting === formFor(asset)?.id || formFor(asset)?.imageStatus === 'queued' || formFor(asset)?.imageStatus === 'running' ? '生成中' : '生成图片'}</Button>
        </div>}{formFor(asset)?.imageError && <Tooltip title={formFor(asset)?.imageError}><p className="drama-asset-image-error">生成失败：{formFor(asset)?.imageError}</p></Tooltip>}</>}

      </article>)}
    </div>
    <Modal title={preview ? `${preview.name} · ${kindNames[kind]}` : '资产预览'} open={preview !== null} onCancel={() => setPreview(null)} footer={<Button onClick={() => setPreview(null)}>关闭</Button>} width={760}>
      {preview && <div className={`drama-asset-preview ${kind}`}>
        <div className="drama-asset-preview-cover">{previewForm?.imageUrl ? <img className="drama-asset-image" src={previewForm.imageUrl} alt={preview.name} /> : <div className="drama-asset-placeholder" aria-hidden="true">{kindIcons[kind]}<span>暂无图片</span></div>}</div>
        <div className="drama-asset-preview-copy">
          <div><h4>资产描述</h4><p>{preview.description || '暂无描述'}</p></div>
          {kind === 'character' && <><div><h4>外貌</h4><p>{preview.appearanceDescription || '暂无外貌描述'}</p></div><div><h4>音色</h4><p>{preview.voiceDescription || '暂无音色描述'}</p></div></>}
          {preview.stateLabel && <div><h4>本集状态</h4><p>{preview.stateLabel}</p></div>}
        </div>
      </div>}
    </Modal>
  </>
}
