import { PictureOutlined, EyeOutlined, GiftOutlined, NodeIndexOutlined, UserOutlined } from '@ant-design/icons'
import { Button, Modal, Tag, Tooltip } from 'antd'
import { useState } from 'react'
import { Link } from 'react-router-dom'

import type { DramaAsset, DramaAssetKind } from '../api/drama'

type GalleryAsset = DramaAsset & { stateLabel?: string }

const kindNames: Record<DramaAssetKind, string> = { character: '角色', scene: '场景', prop: '道具' }
const kindIcons = { character: <UserOutlined />, scene: <PictureOutlined />, prop: <GiftOutlined /> }

function BatchTags({ asset, projectId }: { asset: DramaAsset; projectId: string }) {
  return <div className="drama-asset-batch-tags">
    <div><span>来源</span>{asset.sourceBatch ? <Tag color="blue" title={`第 ${asset.sourceBatch.number} 批`}><Link to={`/ai-apps/drama/projects/${projectId}?section=episodes&batch=${asset.sourceBatch.id}`}>{asset.sourceBatch.name}</Link></Tag> : <small>未记录来源</small>}</div>
    <div><span>应用</span>{asset.usedInBatches?.length ? asset.usedInBatches.map((batch) => <Tag key={batch.id} title={`第 ${batch.number} 批`}><Link to={`/ai-apps/drama/projects/${projectId}?section=episodes&batch=${batch.id}`}>{batch.name}</Link></Tag>) : <small>暂无应用</small>}</div>
  </div>
}

// 当前尚未生成资产图片，封面使用明确的空状态；形象数量由后续画布功能提供。
export function DramaAssetGallery({ assets, kind, projectId, compact = false }: { assets: GalleryAsset[]; kind: DramaAssetKind; projectId: string; compact?: boolean }) {
  const [preview, setPreview] = useState<GalleryAsset | null>(null)
  return <>
    <div className={`drama-asset-gallery ${kind}${compact ? ' compact' : ''}`}>
      {assets.map((asset, index) => <article className="drama-asset-tile" key={asset.id || `${asset.name}-${index}`} aria-label={`${kindNames[kind]}：${asset.name}`}>
        <div className="drama-asset-cover">
          <div className="drama-asset-placeholder" aria-hidden="true">{kindIcons[kind]}<span>暂无图片</span></div>
          <span className="drama-asset-kind-label">{kindNames[kind]}</span>
          <div className="drama-asset-cover-overlay">
            <div className="drama-asset-cover-actions">
              <Button icon={<EyeOutlined />} aria-label={`预览${asset.name}`} onClick={() => setPreview(asset)}>预览</Button>
              {!compact && <Tooltip title="自由画布尚未开放"><span tabIndex={0} aria-label="去画布编辑，自由画布尚未开放"><Button disabled icon={<NodeIndexOutlined />}>去画布编辑</Button></span></Tooltip>}
            </div>
            <p>{asset.description || asset.appearanceDescription || '暂无描述'}</p>
          </div>
        </div>
        <h4 title={asset.name}>{asset.name}</h4>
        {asset.stateLabel && <p className="drama-asset-state-label" title={asset.stateLabel}>{asset.stateLabel}</p>}
        {!compact && <BatchTags asset={asset} projectId={projectId} />}
      </article>)}
    </div>
    <Modal title={preview ? `${preview.name} · ${kindNames[kind]}` : '资产预览'} open={preview !== null} onCancel={() => setPreview(null)} footer={<Button onClick={() => setPreview(null)}>关闭</Button>} width={760}>
      {preview && <div className={`drama-asset-preview ${kind}`}>
        <div className="drama-asset-preview-cover"><div className="drama-asset-placeholder" aria-hidden="true">{kindIcons[kind]}<span>暂无图片</span></div></div>
        <div className="drama-asset-preview-copy">
          <div><h4>资产描述</h4><p>{preview.description || '暂无描述'}</p></div>
          {kind === 'character' && <><div><h4>外貌</h4><p>{preview.appearanceDescription || '暂无外貌描述'}</p></div><div><h4>音色</h4><p>{preview.voiceDescription || '暂无音色描述'}</p></div></>}
          {preview.stateLabel && <div><h4>本集状态</h4><p>{preview.stateLabel}</p></div>}
          {!compact && <BatchTags asset={preview} projectId={projectId} />}
        </div>
      </div>}
    </Modal>
  </>
}
