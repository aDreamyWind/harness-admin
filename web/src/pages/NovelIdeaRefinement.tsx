import { HistoryOutlined, RobotOutlined } from '@ant-design/icons'
import { useMutation } from '@tanstack/react-query'
import { Alert, Button, Input, Select } from 'antd'
import { useEffect, useId, useRef, useState } from 'react'

import { formatDateTime } from '../utils/dateTime'
import { refineNovelIdea } from '../api/novel'
import { findRepeatedIdeaSuggestion, ideaSuggestionExcerpts, listNovelIdeaHistory, matchingIdeaSuggestions, novelIdeaHistoryPrefix, persistNovelIdeaHistoryEntry, type NovelIdeaHistoryEntry } from './novelIdeaHistory'

export function NovelIdeaRefinement({ brief, model, disabled, historyScope, onApply }: {
  brief: string
  model?: { code: string; name: string }
  disabled: boolean
  historyScope: string
  onApply: (brief: string) => void
}) {
  const panelID = useId()
  const [panel, setPanel] = useState<'refine' | 'history'>()
  const [instruction, setInstruction] = useState('')
  const [saved, setSaved] = useState(() => listNovelIdeaHistory(historyScope))
  const [temporary, setTemporary] = useState<NovelIdeaHistoryEntry[]>([])
  const [historyID, setHistoryID] = useState<string>()
  const activeRefine = useRef<AbortController | null>(null)
  const history = [...temporary, ...saved.filter((entry) => !temporary.some((item) => item.id === entry.id))]
    .sort((first, second) => second.savedAt - first.savedAt)
  const selected = history.find((entry) => entry.id === historyID) ?? history[0]
  const selectedResult = selected?.result

  useEffect(() => {
    if (!historyScope) return
    const prefix = novelIdeaHistoryPrefix(historyScope)
    function sync(event: StorageEvent) {
      if (event.key === null || event.key.startsWith(prefix)) setSaved(listNovelIdeaHistory(historyScope))
    }
    window.addEventListener('storage', sync)
    return () => window.removeEventListener('storage', sync)
  }, [historyScope])

  useEffect(() => () => activeRefine.current?.abort(), [historyScope])

  function remember(entry: NovelIdeaHistoryEntry) {
    if (persistNovelIdeaHistoryEntry(historyScope, entry)) {
      setSaved(listNovelIdeaHistory(historyScope))
      setTemporary((current) => current.filter((item) => item.id !== entry.id))
    } else {
      setTemporary((current) => [entry, ...current.filter((item) => item.id !== entry.id)])
    }
  }

  const refine = useMutation({
    mutationFn: async ({ entry, modelCode, signal }: {
      entry: NovelIdeaHistoryEntry; modelCode: string; signal: AbortSignal
    }) => {
      const source = entry.source.trim()
      const related = () => historyScope && !entry.instruction
        ? matchingIdeaSuggestions([...history, ...listNovelIdeaHistory(historyScope)], source)
        : []
      const generate = (previous: string[]) => {
        const excerpts = ideaSuggestionExcerpts(previous)
        return refineNovelIdea({ brief: source, modelCode, instruction: entry.instruction,
          previousSuggestions: excerpts.length ? excerpts : undefined }, signal)
      }

      const first = await generate(related())
      const previous = related()
      const repeated = findRepeatedIdeaSuggestion(previous, source, first)
      if (!repeated) return first

      // 把撞名放到片段开头，即使它在原版的 500 字以外也能传给模型。
      const evidence = repeated.name ? `旧版已用名称「${repeated.name}」\n${repeated.suggestion}` : repeated.suggestion
      const retry = await generate([evidence, ...previous.filter((item) => item !== repeated.suggestion)])
      if (findRepeatedIdeaSuggestion(related(), source, retry)) {
        throw new Error('这次仍与相近的历史版本重复，请再生成一次')
      }
      return retry
    },
    onSuccess: (data, { entry }) => remember({ ...entry, result: data.brief }),
  })
  const source = brief.trim()
  const instructionText = instruction.trim()
  const sameInput = refine.variables?.entry.source.trim() === source && refine.variables.entry.instruction === instructionText
    && refine.variables.modelCode === model?.code
  const suggestion = sameInput ? refine.data?.brief : undefined
  const pending = refine.isPending
  const noChange = suggestion === source

  function generate() {
    if (!model || !source || pending || disabled) return
    const entry = { ...ideaVersion(brief), modelName: model.name, instruction: instructionText }
    const controller = new AbortController()
    activeRefine.current = controller
    remember(entry)
    setHistoryID(entry.id)
    refine.mutate({ entry, modelCode: model.code, signal: controller.signal })
  }

  function apply(value: string) {
    if (brief.trim() && !history.some((entry) => entry.source === brief || entry.result === brief)) {
      remember(ideaVersion(brief))
    }
    onApply(value)
    refine.reset()
    setPanel(undefined)
  }

  return (
    <div className="novel-idea-refinement" data-open={panel !== undefined}>
      <div className="novel-idea-tabs" role="group" aria-label="故事灵感工具">
        <Button type="text" className="novel-idea-tab" id={`${panelID}-refine-tab`} icon={<RobotOutlined aria-hidden />}
          disabled={disabled || pending} aria-expanded={panel === 'refine'} aria-controls={panel === 'refine' ? `${panelID}-refine` : undefined}
          onClick={() => setPanel((value) => value === 'refine' ? undefined : 'refine')}>
          AI 优化灵感
        </Button>
        <Button type="text" className="novel-idea-tab" id={`${panelID}-history-tab`} icon={<HistoryOutlined aria-hidden />}
          disabled={disabled || pending} aria-expanded={panel === 'history'} aria-controls={panel === 'history' ? `${panelID}-history` : undefined}
          onClick={() => setPanel((value) => value === 'history' ? undefined : 'history')}>
          灵感历史
        </Button>
      </div>
      {panel === 'refine' && (
        <div className="novel-ai-panel" id={`${panelID}-refine`} role="region" aria-labelledby={`${panelID}-refine-tab`}>
          <label htmlFor="novel-idea-instruction">优化方案（选填）</label>
          <Input.TextArea id="novel-idea-instruction" rows={2} value={instruction} maxLength={1200} disabled={disabled || pending}
            onChange={(event) => setInstruction(event.target.value)}
            placeholder="例如：把设定改成民国悬疑，加强主角的两难，保留那把钥匙。" />
          {!model && <p className="novel-hint">需要可用的文本模型。</p>}
          <Button loading={pending} disabled={disabled || !source || !model} onClick={generate}>
            生成优化建议
          </Button>
          {sameInput && refine.error && <Alert type="error" title={refine.error.message} />}
          {!sameInput && refine.isSuccess && <small className="novel-hint">输入已变动，可以根据现在的内容重新生成建议。</small>}
          {suggestion && (
            <>
              <label htmlFor="novel-idea-suggestion">优化结果</label>
              <Input.TextArea id="novel-idea-suggestion" value={suggestion} readOnly autoSize={{ minRows: 3, maxRows: 7 }} />
              <Button type="primary" disabled={disabled || pending || noChange} onClick={() => apply(suggestion)}>采用优化结果</Button>
              {noChange && <small className="novel-hint">这次返回的内容与原灵感相同，可以补充具体的优化方案再试一次。</small>}
            </>
          )}
        </div>
      )}
      {panel === 'history' && (
        <div className="novel-ai-panel" id={`${panelID}-history`} role="region" aria-labelledby={`${panelID}-history-tab`}>
          <p className="novel-hint">灵感历史保存在当前浏览器。点击下方的恢复按钮，会把所选内容填回上方输入框，并保留你当前填写的内容。</p>
          {selected ? <>
            <label htmlFor="novel-idea-history">查看版本</label>
            <Select id="novel-idea-history" value={selected.id} onChange={setHistoryID} style={{ width: '100%' }}
              options={history.map((entry) => ({ value: entry.id, label: historyLabel(entry) }))} />
            {selected.instruction && <p className="novel-hint">优化方案：{selected.instruction}</p>}
            <label htmlFor="novel-idea-history-source">{selected.modelName ? '原始灵感' : '恢复前的内容'}</label>
            <Input.TextArea id="novel-idea-history-source" readOnly value={selected.source} autoSize={{ minRows: 2, maxRows: 5 }} />
            <Button disabled={disabled || brief === selected.source} onClick={() => apply(selected.source)}>
              {selected.modelName ? '恢复原始灵感' : '恢复这版灵感'}
            </Button>
            {selectedResult !== undefined ? <>
              <label htmlFor="novel-idea-history-result">优化结果</label>
              <Input.TextArea id="novel-idea-history-result" readOnly value={selectedResult} autoSize={{ minRows: 3, maxRows: 6 }} />
              <Button disabled={disabled || brief === selectedResult} onClick={() => apply(selectedResult)}>恢复优化结果</Button>
            </> : selected.modelName && <p className="novel-hint">这次没有优化结果，原始灵感已保留。</p>}
          </> : <p className="novel-hint">还没有灵感历史。生成优化建议后，可以在这里查看原文和结果。</p>}
        </div>
      )}
      {temporary.length > 0 && <p className="novel-hint" role="status">浏览器未能保存部分版本，请在关闭弹窗前复制需要保留的内容。</p>}
    </div>
  )
}

function ideaVersion(source: string): NovelIdeaHistoryEntry {
  const savedAt = Date.now()
  const id = window.crypto.randomUUID?.() ?? `${savedAt}-${Math.random().toString(36).slice(2)}`
  return { id, savedAt, source }
}

function historyLabel(entry: NovelIdeaHistoryEntry) {
  const date = formatDateTime(entry.savedAt)
  const preview = Array.from(entry.source.trim().replace(/\s+/g, ' ')).slice(0, 18).join('')
  return `${date} · ${entry.modelName ?? '恢复前备份'} · ${preview}`
}
