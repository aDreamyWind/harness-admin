import { CheckOutlined, CloseOutlined } from '@ant-design/icons'
import { Button, Input, message } from 'antd'
import { useCallback, useRef, useState } from 'react'

export function DramaProjectTitle({ title, onRename, compact = false }: {
  title: string
  onRename: (title: string) => Promise<void>
  compact?: boolean
}) {
  const [draft, setDraft] = useState<string>()
  const [saving, setSaving] = useState(false)
  const pending = useRef(false)
  const restoreTitleFocus = useRef(false)
  const [messageApi, holder] = message.useMessage()
  const value = draft?.trim() ?? ''
  const controlSize = compact ? 'small' : 'middle'
  const titleButton = useCallback((button: HTMLButtonElement | null) => {
    if (button && restoreTitleFocus.current) {
      restoreTitleFocus.current = false
      button.focus()
    }
  }, [])

  function closeEditor() {
    restoreTitleFocus.current = true
    setDraft(undefined)
  }

  async function save() {
    if (!value || pending.current) return
    if (value === title) {
      closeEditor()
      return
    }
    pending.current = true
    setSaving(true)
    try {
      await onRename(value)
      closeEditor()
    } catch (error) {
      void messageApi.error(error instanceof Error ? error.message : '保存漫剧标题失败，请重试')
    } finally {
      pending.current = false
      setSaving(false)
    }
  }

  return <>
    {holder}
    {draft === undefined ? (
      <button ref={titleButton} type="button" className="drama-project-title-text" title="双击修改标题"
        onDoubleClick={() => setDraft(title)} onClick={(event) => { if (event.detail === 0) setDraft(title) }}>{title}</button>
    ) : (
      <span className="drama-project-title-form">
        <Input aria-label="漫剧标题" autoFocus size={controlSize} value={draft} maxLength={120}
          readOnly={saving} onFocus={(event) => event.currentTarget.select()} onChange={(event) => setDraft(event.target.value)}
          onPressEnter={(event) => { if (!event.nativeEvent.isComposing && event.keyCode !== 229) void save() }}
          onKeyDown={(event) => {
            if (event.key !== 'Escape' || event.nativeEvent.isComposing || event.keyCode === 229) return
            event.stopPropagation()
            if (!pending.current) closeEditor()
          }} />
        <Button type="primary" size={controlSize} aria-label="保存标题" title={compact ? '保存标题' : undefined}
          icon={compact ? <CheckOutlined /> : undefined} disabled={!value} loading={saving} onClick={() => void save()}>{compact ? null : '保存标题'}</Button>
        <Button type="text" size={controlSize} aria-label="取消" title={compact ? '取消' : undefined}
          icon={compact ? <CloseOutlined /> : undefined} disabled={saving} onClick={closeEditor}>{compact ? null : '取消'}</Button>
      </span>
    )}
  </>
}
