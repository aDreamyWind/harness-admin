import { SettingOutlined } from '@ant-design/icons'
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Button, Modal, Select, message } from 'antd'
import { getDramaCanvas, getDramaSettings, listDramaModels, saveDramaSettings } from '../api/drama'

export function useDramaCanvas(projectId: string) {
  return useQuery({ queryKey: ['drama-canvas', projectId], queryFn: () => getDramaCanvas(projectId), staleTime: 1000,
    refetchInterval: (query) => query.state.data?.forms.some((f) => f.imageStatus === 'queued' || f.imageStatus === 'running') ? 3000 : false,
  })
}
export function DramaImageModelSelect() {
  const [messageApi, holder] = message.useMessage()
  const models = useQuery({ queryKey: ['drama-models'], queryFn: listDramaModels })
  const settings = useQuery({ queryKey: ['drama-settings'], queryFn: getDramaSettings })
  return <>{holder}<Select aria-label="图片模型" placeholder="设置图片模型" loading={models.isLoading || settings.isLoading} value={settings.data?.imageModelCode || undefined}
    style={{ minWidth: 180, maxWidth: 300 }} options={models.data?.filter((m) => m.modelType === 'image').map((m) => ({ value: m.code, label: m.name }))}
    onChange={async (value: string) => { try { const current = await getDramaSettings(); await saveDramaSettings({ ...current, imageModelCode: value }); await settings.refetch() } catch (error) { messageApi.error(error instanceof Error ? error.message : '保存图片模型失败') } }}
    notFoundContent={models.error ? '读取模型失败' : '暂无已启用且有权限的图片模型'} /></>
}

export function DramaImageSettingsButton() {
  const [open, setOpen] = useState(false)
  return <><Button icon={<SettingOutlined />} onClick={() => setOpen(true)}>图片模型设置</Button><Modal title="图片模型设置" open={open} onCancel={() => setOpen(false)} footer={<Button type="primary" onClick={() => setOpen(false)}>完成</Button>}><p>当前账号在漫剧中生成图片时使用此模型。</p><DramaImageModelSelect /></Modal></>
}
