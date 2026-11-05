import { BookOutlined, PlusOutlined } from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { Alert, Button, Empty, Radio, Select, Skeleton } from 'antd'
import { useEffect, useId } from 'react'
import { Link } from 'react-router-dom'

import { listNovelFinalizedChapters, listNovels, type NovelBook } from '../api/novel'
import { useAuth } from '../auth/AuthContext'
import './novel-drama-source.css'

export interface NovelDramaSelection {
  projectId: string
  title?: string
  startChapter?: number
  endChapter?: number
}

function chapterSelection(book: NovelBook, previous?: NovelDramaSelection): NovelDramaSelection {
  if (book.finalizedChapterCount < 1) return { projectId: book.id, title: book.title }
  const startChapter = previous?.startChapter ?? 1
  const endChapter = previous?.endChapter ?? book.finalizedChapterCount
  if (startChapter >= 1 && startChapter <= endChapter && endChapter <= book.finalizedChapterCount) {
    return { projectId: book.id, title: book.title, startChapter, endChapter }
  }
  return { projectId: book.id, title: book.title, startChapter: 1, endChapter: book.finalizedChapterCount }
}

export function NovelDramaSource({ value, onChange, onCreateNovel, disabled }: {
  value: NovelDramaSelection | undefined
  onChange: (value: NovelDramaSelection | undefined) => void
  onCreateNovel: () => void
  disabled?: boolean
}) {
  const id = useId()
  const { user } = useAuth()
  const books = useQuery({ queryKey: ['novel-books', user?.organizationId, user?.id], queryFn: listNovels })
  const selectedBook = books.data?.find((book) => book.id === value?.projectId)
  const selectedBookId = selectedBook?.id
  const chapters = useQuery({
    queryKey: ['novel-finalized-chapters', user?.organizationId, user?.id, selectedBookId],
    queryFn: () => listNovelFinalizedChapters(selectedBookId ?? ''),
    enabled: !!selectedBook?.finalizedChapterCount,
  })
  const selected = selectedBook && chapterSelection(selectedBook, value)

  useEffect(() => {
    if (!value || !books.isSuccess || books.isFetching) return
    if (!selectedBook) {
      onChange(undefined)
      return
    }
    const resolved = chapterSelection(selectedBook, value)
    if (resolved.title !== value.title || resolved.startChapter !== value.startChapter || resolved.endChapter !== value.endChapter) {
      onChange(resolved)
    }
  }, [books.isFetching, books.isSuccess, onChange, selectedBook, value])

  if (books.isPending || (books.isFetching && !books.data?.length)) {
    return <Skeleton active aria-label="正在读取我的小说" paragraph={{ rows: 2 }} />
  }
  if (books.isError) return <Alert type="error" showIcon title={books.error.message || '读取小说列表失败'}
    action={<Button size="small" onClick={() => void books.refetch()}>重试</Button>} />
  if (!books.data.length) return <Empty className="novel-drama-empty" image={Empty.PRESENTED_IMAGE_SIMPLE} description="你还没有小说作品">
    <Button type="primary" icon={<PlusOutlined />} disabled={disabled} onClick={onCreateNovel}>新建小说</Button>
  </Empty>

  const chapterTitles = new Map(chapters.data?.map(({ number, title }) => [number, title]))
  const chapterOptions = Array.from({ length: selectedBook?.finalizedChapterCount ?? 0 }, (_, index) => {
    const number = index + 1
    const title = chapterTitles.get(number)
    return { label: title ? `第 ${number} 章 · ${title}` : `第 ${number} 章`, value: number }
  })
  const start = selected?.startChapter
  const end = selected?.endChapter

  return <div className="novel-drama-source">
    <Radio.Group className="novel-drama-books" name={`${id}-book`} role="radiogroup" aria-label="选择小说"
      value={value?.projectId ?? ''} disabled={disabled} onChange={(event) => {
        const book = books.data.find((item) => item.id === event.target.value)
        if (book) onChange(chapterSelection(book))
      }}>
      {books.data.map((book) => <Radio key={book.id} className="novel-drama-book" value={book.id}>
        <span className="novel-drama-cover" aria-hidden="true">
          {book.cover ? <img alt="" loading="lazy" src={book.cover.url} /> : <BookOutlined />}
        </span>
        <span className="novel-drama-book-info">
          <strong title={book.title}>{book.title}</strong>
          <small>{book.openingPending ? '待规划开篇' : `已定稿 ${book.finalizedChapterCount} / ${book.chapterCount} 章`}</small>
        </span>
      </Radio>)}
    </Radio.Group>
    {!selected && <p className="novel-drama-source-note">选择一本小说，再指定需要改编的章节。</p>}
    {selected && (start === undefined || end === undefined) && <div className="novel-drama-selection-details">
      <p className="novel-drama-source-note" role="status">定稿至少一章后即可解析。</p>
      {!disabled && <Link to={`/ai-apps/novel/${encodeURIComponent(selected.projectId)}?from=drama`}>前往作品</Link>}
    </div>}
    {selected && start !== undefined && end !== undefined && <fieldset className="novel-drama-range">
      <legend>选用章节</legend>
      <div className="novel-drama-selection-details novel-drama-range-fields">
        <div>
          <label htmlFor={`${id}-start`}>起始章</label>
          <Select id={`${id}-start`} size="small" value={start} options={chapterOptions} disabled={disabled} loading={chapters.isPending}
            showSearch={{ optionFilterProp: 'label' }} onChange={(number: number) => {
              onChange({ ...selected, startChapter: number, endChapter: Math.max(end, number) })
            }} />
        </div>
        <div>
          <label htmlFor={`${id}-end`}>结束章</label>
          <Select id={`${id}-end`} size="small" value={end} options={chapterOptions.filter((chapter) => chapter.value >= start)}
            disabled={disabled} loading={chapters.isPending} showSearch={{ optionFilterProp: 'label' }}
            onChange={(number: number) => onChange({ ...selected, endChapter: number })} />
        </div>
        <p className="novel-drama-source-note" aria-live="polite">共选用 {end - start + 1} 章已定稿正文</p>
      </div>
      {chapters.isError && <Alert type="warning" showIcon title="读取章节标题失败，可以继续按章号选择"
        action={<Button size="small" onClick={() => void chapters.refetch()}>重试</Button>} />}
    </fieldset>}
  </div>
}
