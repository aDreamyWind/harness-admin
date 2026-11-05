import { RobotOutlined } from '@ant-design/icons'
import { useMutation } from '@tanstack/react-query'
import { Alert, Button, Form, Input, InputNumber, Modal, Space } from 'antd'
import { useState } from 'react'

import { ApiError } from '../api/client'
import { planNovelChapters, type NovelBeat, type NovelProject } from '../api/novel'

export const novelChapterLimit = 500
const maxBatchSize = 30

export function NovelAppendChapters({ project, model, onAppend, onClose, onReload }: {
  project: NovelProject
  model?: { code: string; name: string }
  onAppend: (beats: NovelBeat[]) => Promise<void>
  onClose: () => void
  onReload: () => void
}) {
  const maxCount = Math.min(maxBatchSize, novelChapterLimit - project.chapterCount)
  const serial = project.storyMode === 'serial'
  const [count, setCount] = useState<number | null>(() => Math.min(serial ? 5 : 3, maxCount))
  const [instruction, setInstruction] = useState('')
  const [beats, setBeats] = useState<NovelBeat[]>()
  const save = useMutation({ mutationFn: onAppend })
  const plan = useMutation({
    mutationFn: (input: { count: number; modelCode: string; instruction: string }) => planNovelChapters(project, input),
    onSuccess: (result) => setBeats(result.beats),
  })
  const busy = plan.isPending || save.isPending
  const validCount = count !== null && Number.isInteger(count) && count >= 1 && count <= maxCount
  const complete = validCount && beats?.length === count && beats.every((beat) => beat.title.trim() && beat.goal.trim())
  const failure = plan.error ?? save.error
  const conflict = failure instanceof ApiError && failure.code === 'novel_conflict'

  function generate() {
    if (!validCount || !model || busy || conflict) return
    save.reset()
    plan.mutate({ count, modelCode: model.code, instruction: instruction.trim() })
  }

  function setBeat(index: number, field: keyof NovelBeat, value: string) {
    setBeats((previous) => previous?.map((beat, i) => i === index ? { ...beat, [field]: value } : beat))
  }

  return <Modal title="追加章节" open width={660} footer={null} onCancel={onClose}
    closable={!busy} keyboard={!busy} mask={{ closable: !busy && !beats }}>
    <Form layout="vertical" disabled={busy || conflict}>
      <p className="novel-hint">{serial
        ? `AI 会根据全书主线和已定稿剧情，从第 ${project.chapterCount + 1} 章接着规划。直接点击「AI 规划下一批」即可。`
        : `从第 ${project.chapterCount + 1} 章接着规划。作品最多可分批追加到 ${novelChapterLimit} 章。`}</p>
      <Form.Item label="本批章数" htmlFor="novel-append-count" extra={`本次可追加 1 到 ${maxCount} 章。`}>
        <InputNumber id="novel-append-count" min={1} max={maxCount} precision={0} value={count}
          onChange={setCount} disabled={!!beats || busy || conflict} />
      </Form.Item>
      {model && <Form.Item label="接下来的剧情方向（选填）" htmlFor="novel-append-instruction"
        extra={`AI 会参考已有大纲和定稿摘要，使用 ${model.name} 拟定新章节。`}>
        <Input.TextArea id="novel-append-instruction" rows={2} maxLength={1200} value={instruction}
          onChange={(event) => setInstruction(event.target.value)} placeholder="例如：主角暂时离开故乡，让前面埋下的线索在下一段剧情里重新出现。" />
      </Form.Item>}
      {beats && <div className="novel-append-beats" role="region" aria-label="待追加章节蓝图" aria-busy={plan.isPending}>
        {beats.map((beat, index) => (
          <fieldset className="novel-beat" key={index}>
            <legend>第 {project.chapterCount + index + 1} 章</legend>
            <Form.Item label="标题" htmlFor={`novel-append-title-${index}`}>
              <Input id={`novel-append-title-${index}`} value={beat.title} maxLength={120}
                onChange={(event) => setBeat(index, 'title', event.target.value)} />
            </Form.Item>
            <Form.Item label="本章目标" htmlFor={`novel-append-goal-${index}`}>
              <Input.TextArea id={`novel-append-goal-${index}`} rows={2} value={beat.goal} maxLength={1200}
                onChange={(event) => setBeat(index, 'goal', event.target.value)} />
            </Form.Item>
          </fieldset>
        ))}
      </div>}
    </Form>
    {failure && <Alert type="error" showIcon title={failure.message}
      action={conflict && <Button size="small" onClick={onReload}>关闭并重新加载</Button>} />}
    <Space wrap className="novel-append-actions">
      {model && <Button icon={<RobotOutlined />} type={beats ? 'default' : 'primary'} loading={plan.isPending}
        disabled={!validCount || save.isPending || conflict} onClick={generate}>
        {beats ? '重新生成章节蓝图' : serial ? 'AI 规划下一批' : 'AI 拟定章节蓝图'}
      </Button>}
      {!beats && <Button type={model ? 'default' : 'primary'} disabled={!validCount || busy || conflict} onClick={() => {
        if (validCount) setBeats(Array.from({ length: count }, () => ({ title: '', goal: '' })))
      }}>手动填写</Button>}
      {beats && <Button type="primary" loading={save.isPending} disabled={!complete || plan.isPending || conflict}
        onClick={() => save.mutate(beats)}>保存并追加 {beats.length} 章</Button>}
      <Button disabled={busy} onClick={onClose}>取消</Button>
    </Space>
  </Modal>
}
