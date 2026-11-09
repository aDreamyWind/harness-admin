import { ApartmentOutlined, BookOutlined, LinkOutlined, PushpinOutlined, TeamOutlined, WarningOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Alert, Button, Empty, Input, Pagination, Select, Spin, Tabs } from 'antd'
import { useMemo, useState } from 'react'

import { backfillNovelStoryMemory, getNovelStoryMemory, resolveNovelMemoryConflict,
  type NovelMemoryConflict, type NovelMemoryFact, type NovelMemoryKind } from '../api/novel'
import { useAuth } from '../auth/AuthContext'
import './novel-story-memory.css'

const pageSize = 12
const kindLabels: Record<NovelMemoryKind, string> = { character: '人物', place: '地点', clue: '伏笔' }
const fieldLabels: Record<string, string> = {
  alias: '别名', birth_name: '本名', birthplace: '出生地', relationship: '关系', location: '所在地点',
  region: '归属', holder: '持有者', status: '状态', detail: '资料',
}
const kinds = [
  { key: 'character', icon: <TeamOutlined /> },
  { key: 'place', icon: <PushpinOutlined /> },
  { key: 'clue', icon: <LinkOutlined /> },
] as const

type OpenChapter = (chapter: number) => void
type MemoryChange = { kind: 'backfill' } | { kind: 'resolve'; conflictId: string; choice: 0 | 1 }

function factField(fact: NovelMemoryFact) {
  return fieldLabels[fact.field] ?? '资料'
}

function FactSource({ fact, openChapter, disabled }: { fact: NovelMemoryFact; openChapter: OpenChapter; disabled: boolean }) {
  return <div className="novel-memory-evidence">
    <Button type="link" size="small" disabled={disabled} onClick={() => openChapter(fact.chapter)}>第 {fact.chapter} 章</Button>
    <q>{fact.quote}</q>
  </div>
}

function FactList({ facts, kind, openChapter, disabled }: {
  facts: NovelMemoryFact[]; kind: NovelMemoryKind; openChapter: OpenChapter; disabled: boolean
}) {
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(1)
  const groups = useMemo(() => {
    const query = search.trim().toLowerCase()
    const grouped = new Map<string, NovelMemoryFact[]>()
    for (const fact of facts) {
      if (fact.kind !== kind || (query && ![fact.name, factField(fact), fact.value, fact.related, fact.quote]
        .some((part) => part?.toLowerCase().includes(query)))) continue
      const items = grouped.get(fact.name)
      if (items) items.push(fact)
      else grouped.set(fact.name, [fact])
    }
    return Array.from(grouped)
  }, [facts, kind, search])
  const currentPage = Math.min(page, Math.max(1, Math.ceil(groups.length / pageSize)))

  return <div className="novel-memory-stack">
    <Input aria-label={`搜索${kindLabels[kind]}`} placeholder={kind === 'character' ? '搜索姓名、别名或原文' : `搜索${kindLabels[kind]}或原文`}
      value={search} allowClear onChange={(event) => { setSearch(event.target.value); setPage(1) }} />
    {groups.length === 0 ? <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={search.trim()
      ? `没有匹配的${kindLabels[kind]}` : `已整理的章节中还没有${kindLabels[kind]}资料`} /> :
      <ul className="novel-memory-fact-list">
        {groups.slice((currentPage - 1) * pageSize, currentPage * pageSize).map(([name, items]) => <li key={name}>
          <h4>{name}</h4>
          <ul className="novel-memory-fact-rows">
            {items.map((fact, index) => <li key={`${fact.chapter}:${index}`}>
              <span className="novel-memory-field">{factField(fact)}</span>
              <div><span>{fact.value}</span>
                <FactSource fact={fact} openChapter={openChapter} disabled={disabled} /></div>
            </li>)}
          </ul>
        </li>)}
      </ul>}
    {groups.length > pageSize && <Pagination aria-label={`${kindLabels[kind]}分页`} size="small" current={currentPage}
      total={groups.length} pageSize={pageSize} showSizeChanger={false} onChange={setPage} />}
  </div>
}

function MemoryConflicts({ conflicts, pending, openChapter, sourceDisabled, resolve }: {
  conflicts: NovelMemoryConflict[]; pending: boolean; openChapter: OpenChapter; sourceDisabled: boolean
  resolve: (conflictId: string, choice: 0 | 1) => void
}) {
  if (!conflicts.length) return <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="目前没有需要确认的冲突" />
  return <div className="novel-memory-stack">
    <p className="novel-hint">比较两条原文，选择后续章节要沿用的事实。原有章节会保留。</p>
    {conflicts.map((conflict) => <section className="novel-memory-conflict" key={conflict.id}>
      <h4>{factField(conflict.options[0])}待确认</h4>
      <p>{conflict.reason}</p>
      <div className="novel-memory-conflict-options">
        {([0, 1] as const).map((choice) => {
          const fact = conflict.options[choice]
          return <div key={choice}>
            <strong>{fact.name}</strong>
            <span>{factField(fact)} · {fact.value}</span>
            <FactSource fact={fact} openChapter={openChapter} disabled={sourceDisabled} />
            <Button size="small" disabled={pending} aria-label={`沿用第 ${fact.chapter} 章中${fact.name}的${factField(fact)} ${fact.value}`}
              onClick={() => resolve(conflict.id, choice)}>沿用这条</Button>
          </div>
        })}
      </div>
    </section>)}
  </div>
}

function MemoryRelations({ facts, openChapter, disabled }: {
  facts: NovelMemoryFact[]; openChapter: OpenChapter; disabled: boolean
}) {
  const connections = useMemo(() => {
    const result = new Map<string, NovelMemoryFact[]>()
    for (const fact of facts) {
      if (!fact.related || fact.related === fact.name) continue
      for (const name of [fact.name, fact.related]) {
        const edges = result.get(name)
        if (edges) edges.push(fact)
        else result.set(name, [fact])
      }
    }
    return result
  }, [facts])
  const [selection, setSelection] = useState('')
  const [page, setPage] = useState(1)
  const focus = connections.has(selection) ? selection : connections.keys().next().value
  if (!focus) return <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="已整理的原文中还没有明确的关联" />
  const edges = connections.get(focus) ?? []
  const currentPage = Math.min(page, Math.max(1, Math.ceil(edges.length / pageSize)))

  function select(name: string) {
    setSelection(name)
    setPage(1)
  }

  return <div className="novel-memory-stack">
    <div className="novel-memory-graph-controls">
      <Select aria-label="选择关系图中心" showSearch optionFilterProp="label" value={focus} onChange={select}
        options={Array.from(connections.keys(), (name) => ({ value: name, label: name }))} />
      <small className="novel-hint">点选相邻名字可查看它的关联。每条连线都可核对原文。</small>
    </div>
    <div className="novel-memory-graph" role="group" aria-label={`${focus}的关系图`}>
      <strong className="novel-memory-graph-center">{focus}</strong>
      <ul>
        {edges.slice((currentPage - 1) * pageSize, currentPage * pageSize).map((fact, index) => {
          const target = focus === fact.name ? fact.related : fact.name
          return target && <li key={`${fact.chapter}:${index}`}>
            <div className="novel-memory-graph-edge">
              <Button size="small" autoInsertSpace={false} onClick={() => select(target)}>{target}</Button>
              <span><b>{factField(fact)}</b> {fact.value}</span>
            </div>
            <FactSource fact={fact} openChapter={openChapter} disabled={disabled} />
          </li>
        })}
      </ul>
    </div>
    {edges.length > pageSize && <Pagination aria-label="关系图分页" size="small" current={currentPage} total={edges.length}
      pageSize={pageSize} showSizeChanger={false} onChange={setPage} />}
  </div>
}

export function NovelStoryMemory({ projectId, openChapter, sourceDisabled }: {
  projectId: string; openChapter: OpenChapter; sourceDisabled: boolean
}) {
  const { user } = useAuth()
  const cache = useQueryClient()
  const key = ['novel-story-memory', user?.organizationId, user?.id, projectId] as const
  const change = useMutation({
    mutationFn: (input: MemoryChange) => input.kind === 'backfill' ? backfillNovelStoryMemory(projectId)
      : resolveNovelMemoryConflict(projectId, input.conflictId, input.choice),
    onMutate: () => cache.cancelQueries({ queryKey: key, exact: true }),
    onSuccess: async (memory) => {
      await cache.cancelQueries({ queryKey: key, exact: true })
      cache.setQueryData(key, memory)
    },
    onSettled: () => { void cache.invalidateQueries({ queryKey: key, exact: true }) },
  })
  const memory = useQuery({
    queryKey: key,
    queryFn: ({ signal }) => getNovelStoryMemory(projectId, signal),
    refetchInterval: (query) => !change.isPending && (query.state.data?.pending ?? 0) > 0 ? 3000 : false,
  })
  const data = memory.data
  const facts = data?.facts ?? []
  const conflicts = data?.conflicts ?? []
  const canBackfill = !!data && (data.missing > 0 || data.failed > 0)

  return <section className="novel-memory" aria-labelledby="novel-memory-title">
    <div className="novel-memory-header">
      <div>
        <h2 id="novel-memory-title"><BookOutlined aria-hidden="true" /> 故事档案</h2>
        <p>从定稿正文提取人物、地点和伏笔，供后续章节引用。</p>
      </div>
      {canBackfill && <Button size="small" loading={change.isPending && change.variables?.kind === 'backfill'}
        disabled={change.isPending} onClick={() => change.mutate({ kind: 'backfill' })}>
        {data.missing > 0 ? '补录历史章节' : '重试失败章节'}
      </Button>}
    </div>
    {memory.isPending && <Spin size="small" aria-label="正在读取故事档案" />}
    {memory.isError && <Alert type="warning" showIcon title="暂时无法读取故事档案"
      action={<Button size="small" onClick={() => void memory.refetch()}>重新读取</Button>} />}
    {change.isError && <Alert type="error" showIcon title={change.error.message || '故事档案更新失败，请重试'} />}
    {data && <>
      <div className="novel-memory-progress" role="status">
        <span>已整理 <b>{data.completed}</b> 章</span>
        {data.pending > 0 && <span>后台整理中 <b>{data.pending}</b> 章</span>}
        {data.missing > 0 && <span>待补录 <b>{data.missing}</b> 章</span>}
        {data.failed > 0 && <span className="novel-memory-disputed">整理失败 <b>{data.failed}</b> 章</span>}
      </div>
      {data.completed === 0 && !facts.length && !conflicts.length ? <p className="novel-hint">{data.pending > 0
        ? '正在后台读取定稿章节，完成后这里会显示带原文的资料。'
        : canBackfill ? '补录后可以查看旧章节里的人物、地点和伏笔。' : '第一章定稿后，故事档案会在后台开始整理。'}</p> :
        <Tabs size="small" tabBarGutter={16} items={[
          ...kinds.map(({ key, icon }) => ({ key, icon, label: kindLabels[key], children: <FactList kind={key} facts={facts}
            openChapter={openChapter} disabled={sourceDisabled} /> })),
          { key: 'graph', icon: <ApartmentOutlined />, label: '关系图', children: <MemoryRelations facts={facts}
            openChapter={openChapter} disabled={sourceDisabled} /> },
          { key: 'conflicts', icon: <WarningOutlined />, label: `待确认${conflicts.length ? ` ${conflicts.length}` : ''}`,
            children: <MemoryConflicts conflicts={conflicts} pending={change.isPending} openChapter={openChapter} sourceDisabled={sourceDisabled}
              resolve={(conflictId, choice) => change.mutate({ kind: 'resolve', conflictId, choice })} /> },
        ]} />}
    </>}
  </section>
}
