import { SyncOutlined } from '@ant-design/icons'
import { Button, Input, message, Space } from 'antd'
import { useState } from 'react'

import type { NovelAction, NovelProject } from '../api/novel'

type TitleAction = Extract<NovelAction, { kind: 'rename' | 'suggest_title' }>

export function NovelTitleEditor({ title, modelCode, busy, suggesting, dirty, run }: {
  title: string
  modelCode?: string
  busy: boolean
  suggesting: boolean
  dirty: boolean
  run: (action: TitleAction) => Promise<NovelProject>
}) {
  const [draft, setDraft] = useState<string>()
  const [undo, setUndo] = useState<{ before: string; after: string }>()
  const [messageApi, messageContext] = message.useMessage()
  const blocked = busy || dirty

  async function rename(next: string) {
    if (blocked || !next.trim()) return
    if (next.trim() === title) {
      setDraft(undefined)
      return
    }
    try {
      await run({ kind: 'rename', title: next.trim() })
      setDraft(undefined)
      setUndo(undefined)
    } catch {
      // 工作台统一显示保存失败，保留输入以便重试。
    }
  }

  async function suggest() {
    if (blocked || !modelCode) return
    try {
      const updated = await run({ kind: 'suggest_title', modelCode })
      if (updated.title === title) {
        void messageApi.info('AI 推荐的书名和当前相同')
      } else {
        setUndo({ before: title, after: updated.title })
      }
    } catch {
      // 工作台统一显示生成失败。
    }
  }

  return <div className="novel-title-row">
    {messageContext}
    {draft !== undefined ? <Space wrap size={6}>
      <Input className="novel-title-input" aria-label="书名" value={draft} autoFocus maxLength={120} disabled={busy}
        onChange={(event) => setDraft(event.target.value)}
        onPressEnter={(event) => { if (!event.nativeEvent.isComposing) void rename(draft) }}
        onKeyDown={(event) => { if (event.key === 'Escape' && !busy) setDraft(undefined) }} />
      <Button size="small" type="primary" disabled={blocked || !draft.trim()} onClick={() => void rename(draft)}>保存书名</Button>
      <Button size="small" type="text" disabled={busy} onClick={() => setDraft(undefined)}>取消</Button>
    </Space> : <>
      <h1>
        <button type="button" className="novel-title-text" title={dirty ? '请先保存正在编辑的内容' : '双击修改书名'}
          disabled={blocked} onDoubleClick={() => setDraft(title)}
          onClick={(event) => { if (event.detail === 0) setDraft(title) }}>{title}</button>
      </h1>
      <Button type="text" size="small" className="novel-title-suggest" data-suggesting={suggesting || undefined}
        icon={<SyncOutlined spin={suggesting} />} aria-label="AI 拟新书名"
        title={dirty ? '请先保存正在编辑的内容' : modelCode ? 'AI 拟新书名' : '暂无可用文本模型'}
        disabled={blocked || !modelCode} onClick={() => void suggest()} />
      {undo?.after === title && <Button type="link" size="small" disabled={blocked}
        onClick={() => void rename(undo.before)}>撤销改名</Button>}
    </>}
  </div>
}
