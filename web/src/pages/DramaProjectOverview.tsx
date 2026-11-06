import {
  CheckCircleOutlined, EditOutlined, EnvironmentOutlined, FileTextOutlined, LoadingOutlined,
  ProfileOutlined, SlidersOutlined, TagOutlined, UserOutlined,
} from '@ant-design/icons'
import { Button, Input, Select, message } from 'antd'
import { useId, useState } from 'react'

import {
  updateDramaOverview, updateDramaVisualSettings, type DramaCoverGeneration,
  type DramaProject, type DramaStoryOverview, type DramaStyleOption,
} from '../api/drama'
import { DramaCoverControl } from './DramaCoverControl'
import { DramaStylePicker } from './DramaStylePicker'

type AspectRatio = DramaProject['aspectRatio']
type VisualChange = { style: DramaStyleOption } | { ratio: AspectRatio }
export type DramaOverviewChange = Partial<Pick<DramaProject, 'style' | 'styleId' | 'aspectRatio' | 'overview' | 'updatedAt'>>

const ratioOptions: { value: AspectRatio; label: string }[] = [
  { value: '9:16', label: '竖屏 9:16' }, { value: '16:9', label: '横屏 16:9' },
]
const storyFacts = [
  { key: 'protagonist', label: '主角', icon: <UserOutlined aria-hidden="true" /> },
  { key: 'genre', label: '故事类型', icon: <TagOutlined aria-hidden="true" /> },
] as const
const storySections = {
  synopsis: { label: '故事梗概', icon: <FileTextOutlined aria-hidden="true" /> },
  background: { label: '故事背景', icon: <EnvironmentOutlined aria-hidden="true" /> },
  setting: { label: '故事设定', icon: <ProfileOutlined aria-hidden="true" /> },
}

export function selectedAspectRatio(project: DramaProject): AspectRatio {
  const ratio = project.overview?.aspectRatio
  return ratio === '9:16' || ratio === '16:9' ? ratio : project.aspectRatio
}

function storyFields({ protagonist = '', genre = '', synopsis = '', background = '', setting = '' }: DramaStoryOverview): Required<DramaStoryOverview> {
  return { protagonist, genre, synopsis, background, setting }
}

export function DramaProjectOverview({ project, reanalyzing, onReanalyze, onChange, onCoverChange }: {
  project: DramaProject
  reanalyzing: boolean
  onReanalyze: () => void
  onChange: (change: DramaOverviewChange) => void
  onCoverChange: (state: DramaCoverGeneration) => void
}) {
  const [messageApi, holder] = message.useMessage()
  const fieldId = useId()
  const [draft, setDraft] = useState<Required<DramaStoryOverview>>()
  const [savingStory, setSavingStory] = useState(false)
  const [pendingStyle, setPendingStyle] = useState<DramaStyleOption>()
  const [pendingRatio, setPendingRatio] = useState<AspectRatio>()
  const [visualStatus, setVisualStatus] = useState<'saved' | 'failed'>()
  const visualSaving = !!(pendingStyle || pendingRatio)
  const story = storyFields(project.overview ?? {})
  const ratio = selectedAspectRatio(project)
  const storyChanged = !!draft && Object.keys(story).some((key) => draft[key as keyof DramaStoryOverview] !== story[key as keyof DramaStoryOverview])

  async function saveVisual(change: VisualChange) {
    const isStyle = 'style' in change
    if (isStyle) {
      if (pendingStyle || change.style.id === project.styleId) return
      setPendingStyle(change.style)
    } else {
      if (pendingRatio || change.ratio === ratio) return
      setPendingRatio(change.ratio)
    }
    setVisualStatus(undefined)
    try {
      const saved = await updateDramaVisualSettings(project.id, isStyle ? { styleId: change.style.id } : { aspectRatio: change.ratio })
      const { styleId, style, aspectRatio, updatedAt } = saved
      onChange({
        ...(style !== undefined ? { styleId, style } : {}),
        ...(aspectRatio !== undefined ? { aspectRatio } : {}),
        overview: {
          ...(style !== undefined ? { styleId, videoStyle: style } : {}),
          ...(aspectRatio !== undefined ? { aspectRatio } : {}),
        },
        updatedAt,
      })
      setVisualStatus('saved')
    } catch (error) {
      setVisualStatus('failed')
      const label = isStyle ? '视频风格' : '画面比例'
      void messageApi.error(error instanceof Error && error.message ? `${label}保存失败。${error.message}` : `${label}保存失败，请重新选择。`)
    } finally {
      if (isStyle) setPendingStyle(undefined)
      else setPendingRatio(undefined)
    }
  }

  async function saveStory() {
    if (!draft || !storyChanged || savingStory) return
    setSavingStory(true)
    try {
      const saved = await updateDramaOverview(project.id, draft)
      onChange({ overview: storyFields(saved.overview ?? {}), updatedAt: saved.updatedAt })
      setDraft(undefined)
      void messageApi.success('故事概览已保存')
    } catch (error) {
      void messageApi.error(error instanceof Error ? error.message : '故事概览保存失败，请重试')
    } finally {
      setSavingStory(false)
    }
  }

  function changeStory(key: keyof DramaStoryOverview, value: string) {
    setDraft((current) => current ? { ...current, [key]: value } : current)
  }

  function renderStorySection(key: keyof typeof storySections) {
    const { label, icon } = storySections[key]
    const headingId = `${fieldId}-${key}`
    const content = story[key].trim()
    return <section className={`drama-story-section drama-story-${key}`} aria-labelledby={headingId}>
      <h3 id={headingId}>{icon}{label}</h3>
      {draft ? <Input.TextArea aria-labelledby={headingId} autoSize={{ minRows: key === 'synopsis' ? 6 : 4, maxRows: 14 }}
        readOnly={savingStory} value={draft[key]} onChange={(event) => changeStory(key, event.target.value)} />
        : <p className={`drama-story-copy${content ? '' : ' is-empty'}`}>{content || '等待解析结果'}</p>}
    </section>
  }

  return <section className="drama-overview" aria-label="剧本概览内容">
    {holder}
    <div className="drama-overview-toolbar">
      <div className="drama-overview-controls">
        <div className="drama-visual-title"><SlidersOutlined aria-hidden="true" />画面设置</div>
        <div className="drama-visual-field">
          <label htmlFor={`${fieldId}-style`}>视频风格</label>
          <DramaStylePicker id={`${fieldId}-style`} value={pendingStyle?.id ?? (project.styleId || project.style)}
            valueLabel={pendingStyle?.name ?? project.style} disabled={!!pendingStyle} onChange={(style) => void saveVisual({ style })} />
        </div>
        <div className="drama-visual-field">
          <label htmlFor={`${fieldId}-ratio`}>画面比例</label>
          <Select id={`${fieldId}-ratio`} aria-label="画面比例" value={pendingRatio ?? ratio} options={ratioOptions}
            loading={!!pendingRatio} disabled={!!pendingRatio} onChange={(value) => void saveVisual({ ratio: value })} />
        </div>
        <span className="drama-visual-status" data-state={visualSaving ? 'saving' : visualStatus} role="status">
          {visualSaving ? <><LoadingOutlined aria-hidden="true" />正在保存…</> : visualStatus === 'saved' ? <><CheckCircleOutlined aria-hidden="true" />已自动保存</>
            : visualStatus === 'failed' ? '请重新选择' : '选择后自动保存'}
        </span>
      </div>
      <div className="drama-overview-actions">
        <DramaCoverControl project={project} disabled={!!draft || visualSaving} onStateChange={onCoverChange} />
        <Button loading={reanalyzing} disabled={!!draft || project.status === 'queued' || project.status === 'running' || (project.status === 'failed' && !story.protagonist)}
          onClick={onReanalyze}>AI重新解析</Button>
      </div>
    </div>
    <article className="drama-story-card" aria-labelledby={`${fieldId}-heading`}>
      <header className="drama-story-heading">
        <h2 id={`${fieldId}-heading`}>剧本概览</h2>
        <div className="drama-story-actions">
          {draft ? <>
            <Button disabled={savingStory} onClick={() => setDraft(undefined)}>取消</Button>
            <Button type="primary" loading={savingStory} disabled={!storyChanged} onClick={() => void saveStory()}>保存修改</Button>
          </> : <Button icon={<EditOutlined />} onClick={() => setDraft(story)}>编辑概览</Button>}
        </div>
      </header>
      <div className="drama-story-layout">
        <div className="drama-story-main">
          <dl className="drama-story-facts">
            {storyFacts.map(({ key, label, icon }) => <div key={key}>
              <dt>{icon}{draft ? <label htmlFor={`${fieldId}-${key}`}>{label}</label> : label}</dt>
              <dd>{draft ? <Input id={`${fieldId}-${key}`} readOnly={savingStory} value={draft[key]}
                onChange={(event) => changeStory(key, event.target.value)} /> : story[key].trim() || '待解析'}</dd>
            </div>)}
          </dl>
          {renderStorySection('synopsis')}
        </div>
        <div className="drama-story-reference">
          {renderStorySection('background')}
          {renderStorySection('setting')}
        </div>
      </div>
    </article>
  </section>
}
