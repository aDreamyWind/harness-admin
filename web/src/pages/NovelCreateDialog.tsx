import { BookOutlined, EditOutlined, RobotOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Alert, Button, Form, Input, InputNumber, Modal, Radio, Select } from 'antd'
import { useState } from 'react'
import { useNavigate } from 'react-router-dom'

import { createNovel, listNovelModels, type NovelCreate } from '../api/novel'
import { useAuth } from '../auth/AuthContext'
import { NovelIdeaRefinement } from './NovelIdeaRefinement'
import './novel.css'

const { TextArea } = Input
const defaultChapterCount = 3
type CreationMode = 'ai' | 'serial' | 'author'

export function NovelCreateDialog({ onClose, source }: { onClose: () => void; source?: 'drama' }) {
  const navigate = useNavigate()
  const { user } = useAuth()
  const cache = useQueryClient()
  const listKey = ['novel-books', user?.organizationId, user?.id]
  const ideaHistoryScope = user?.organizationId && user.id ? JSON.stringify([user.organizationId, user.id]) : ''
  const [mode, setMode] = useState<CreationMode>('ai')
  const [selectedModel, setSelectedModel] = useState('')
  const [createForm] = Form.useForm<NovelCreate>()
  const brief = Form.useWatch('brief', createForm) ?? ''
  const aiMode = mode !== 'author'
  const serial = mode === 'serial'
  const models = useQuery({
    queryKey: ['novel-models', user?.organizationId, user?.id],
    queryFn: listNovelModels,
  })
  const availableModels = models.data ?? []
  const defaultModel = availableModels.find((model) => model.code === selectedModel) ?? availableModels[0]
  const create = useMutation({
    mutationFn: ({ input }: { input: NovelCreate; outlineModel?: string }) => createNovel(input),
    onSuccess: (project, { outlineModel }) => {
      void cache.invalidateQueries({ queryKey: listKey })
      navigate({
        pathname: '/ai-apps/novel/' + project.id,
        search: source === 'drama' ? '?from=drama' : '',
      }, { state: { outlineModel } })
    },
  })

  function submitCreate(values: NovelCreate) {
    if (create.isPending) return
    if (aiMode) {
      if (!defaultModel) return
      create.mutate({
        input: {
          title: values.title, brief: values.brief, suggestTitle: true,
          ...(serial ? { storyMode: 'serial' } : { chapterCount: values.chapterCount }),
        },
        outlineModel: defaultModel.code,
      })
      return
    }
    create.mutate({ input: values })
  }

  return (
    <Modal className="novel-create-modal" title="新建小说" open footer={null} closable={!create.isPending}
      mask={{ closable: !create.isPending }} onCancel={() => { if (!create.isPending) onClose() }}>
      <Radio.Group className="novel-mode-options" aria-label="大纲规划方式" value={mode}
        onChange={(event) => setMode(event.target.value)} disabled={create.isPending}>
        <Radio className="novel-mode-option" value="ai">
          <span><RobotOutlined /> AI 帮我规划</span>
          <small>给一句灵感，AI 拟书名、设定和各章大纲。</small>
        </Radio>
        <Radio className="novel-mode-option" value="serial">
          <span><BookOutlined /> 长篇连载</span>
          <small>只填灵感，AI 先定全书主线，再按开篇剧情建议章数。</small>
        </Radio>
        <Radio className="novel-mode-option" value="author">
          <span><EditOutlined /> 我自己规划</span>
          <small>从空白大纲开始，自己决定人物和剧情。</small>
        </Radio>
      </Radio.Group>
      <Form<NovelCreate> form={createForm} layout="vertical"
        onFinish={submitCreate} disabled={create.isPending} clearOnDestroy>
        {!aiMode && <Form.Item label="作品名称" name="title" rules={[{ required: true, whitespace: true, message: '请输入作品名称' }]}>
          <Input maxLength={120} />
        </Form.Item>}
        <Form.Item label={aiMode ? '故事灵感' : '故事构想'} name="brief"
          rules={[{ required: true, whitespace: true, message: '请写下你的故事想法' }]}>
          <TextArea rows={aiMode ? 4 : 5} maxLength={4000} showCount placeholder={aiMode
            ? '例如：一个只负责归还遗失物的夜班店员，收到了一把来自明天的钥匙。'
            : '谁想做什么？什么阻止了他？也可以写下题材、视角和写作要求。'} />
        </Form.Item>
        <NovelIdeaRefinement key={ideaHistoryScope} historyScope={ideaHistoryScope} brief={brief} model={defaultModel} disabled={create.isPending}
          onApply={(value) => createForm.setFieldValue('brief', value)} />
        <div className={aiMode ? 'novel-create-settings' : undefined}>
          {aiMode ? <details>
            <summary>生成选项{defaultModel && <small>{serial
              ? ' · 首批章数由 AI 建议'
              : ` · 默认 ${defaultChapterCount} 章，可换模型或调整章数`}</small>}</summary>
            <div className="novel-create-settings-fields">
              <Form.Item label="作品名称（选填）" name="title">
                <Input maxLength={120} placeholder="留空由 AI 拟定" />
              </Form.Item>
              {!serial && <Form.Item label="首批章数" name="chapterCount" initialValue={defaultChapterCount}
                extra="确认大纲后可以继续分批追加。" rules={[{ required: true, message: '请输入 1 到 30 之间的章数' }]}>
                <InputNumber min={1} max={30} precision={0} />
              </Form.Item>}
              <Form.Item label="文本模型" htmlFor="novel-create-model">
                <Select id="novel-create-model" value={defaultModel?.code} onChange={setSelectedModel}
                  loading={models.isPending} placeholder="暂无可用模型"
                  options={availableModels.map((model) => ({ value: model.code, label: model.name }))} />
              </Form.Item>
            </div>
          </details> : (
            <Form.Item label="首批章数" name="chapterCount" initialValue={defaultChapterCount}
              extra="确认大纲后可以继续分批追加。" rules={[{ required: true, message: '请输入 1 到 30 之间的章数' }]}>
              <InputNumber min={1} max={30} precision={0} />
            </Form.Item>
          )}
        </div>
        {aiMode && models.isError && <Alert type="error" title={models.error.message || '操作失败，请稍后重试'}
          action={<Button size="small" onClick={() => void models.refetch()}>重试</Button>} />}
        {aiMode && models.isSuccess && !defaultModel && <Alert type="info" title="当前没有可用的文本模型，可以切换为自己规划，或请管理员配置模型。" />}
        {create.error && <Alert type="error" title={create.error.message || '操作失败，请稍后重试'} />}
        <div className="novel-create-footer">
          {aiMode && defaultModel && <span>{serial
            ? '首批只规划开篇，后续可让 AI 根据已有剧情接着规划。'
            : '先规划首批章节，后续可分批追加。'} 使用 {defaultModel.name} 生成。</span>}
          <Button type="primary" htmlType="submit" loading={create.isPending} disabled={aiMode && !defaultModel}>
            {serial ? '规划长篇开篇' : aiMode ? '生成小说大纲' : '创建作品'}
          </Button>
        </div>
      </Form>
    </Modal>
  )
}
