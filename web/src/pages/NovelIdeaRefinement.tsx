import { RobotOutlined } from '@ant-design/icons'
import { useMutation } from '@tanstack/react-query'
import { Alert, Button, Input } from 'antd'
import { useState } from 'react'

import { refineNovelIdea } from '../api/novel'

export function NovelIdeaRefinement({ brief, model, disabled, onApply }: {
  brief: string
  model?: { code: string; name: string }
  disabled: boolean
  onApply: (brief: string) => void
}) {
  const [expanded, setExpanded] = useState(false)
  const [instruction, setInstruction] = useState('')
  const refine = useMutation({ mutationFn: refineNovelIdea })
  const source = brief.trim()
  const instructionText = instruction.trim()
  const sameInput = refine.variables?.brief === source && refine.variables.instruction === instructionText
    && refine.variables.modelCode === model?.code
  const suggestion = sameInput ? refine.data?.brief : undefined
  const pending = refine.isPending
  const noChange = suggestion === source

  return (
    <div className="novel-idea-refinement">
      <Button icon={<RobotOutlined />} disabled={disabled || pending} aria-expanded={expanded}
        onClick={() => setExpanded((value) => !value)}>
        {expanded ? '收起灵感优化' : 'AI 优化灵感'}
      </Button>
      {expanded && (
        <div className="novel-ai-panel">
          <label htmlFor="novel-idea-instruction">优化方案（选填）</label>
          <Input.TextArea id="novel-idea-instruction" rows={2} value={instruction} maxLength={1200} disabled={disabled || pending}
            onChange={(event) => setInstruction(event.target.value)}
            placeholder="例如：把设定改成民国悬疑，加强主角的两难，保留那把钥匙。" />
          <p className="novel-hint">{model ? `使用 ${model.name}。可以先看结果，再决定是否采用。` : '需要可用的文本模型。'}</p>
          <Button loading={pending} disabled={disabled || !source || !model}
            onClick={() => { if (model) refine.mutate({ brief: source, modelCode: model.code, instruction: instructionText }) }}>
            生成优化建议
          </Button>
          {sameInput && refine.error && <Alert type="error" title={refine.error.message} />}
          {!sameInput && refine.isSuccess && <small className="novel-hint">输入已变动，可以根据现在的内容重新生成建议。</small>}
          {suggestion && (
            <>
              <label htmlFor="novel-idea-suggestion">优化结果</label>
              <Input.TextArea id="novel-idea-suggestion" value={suggestion} readOnly autoSize={{ minRows: 3, maxRows: 7 }} />
              <Button type="primary" disabled={disabled || pending || noChange} onClick={() => {
                onApply(suggestion)
                refine.reset()
                setExpanded(false)
              }}>采用优化结果</Button>
              {noChange && <small className="novel-hint">这次返回的内容与原灵感相同，可以补充具体的优化方案再试一次。</small>}
            </>
          )}
        </div>
      )}
    </div>
  )
}
