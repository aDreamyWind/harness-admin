import { csrfHeaders, request } from './client'

export interface NovelCover {
  url: string
  source: 'upload' | 'ai'
  updatedAt: string
}

export interface NovelCoverPreview {
  token: string
  previewUrl: string
}

export interface NovelBook {
  id: string
  title: string
  chapterCount: number
  finalizedChapterCount: number
  openingPending?: boolean
  updatedAt: string
  cover?: NovelCover
}

export interface NovelBeat {
  title: string
  goal: string
}

export interface NovelOutline {
  synopsis: string
  world: string
  characters: string
  beats: NovelBeat[]
}

export interface NovelOutlineCheck {
  status: 'passed' | 'needs_review' | 'unavailable'
  repaired: boolean
  issues: { chapter: number; message: string; quote?: string }[]
}

export interface NovelChapter {
  body: string
  summary: string
  review: string
  reviewFailed?: boolean
  reviewPassed?: boolean
  authorInstruction?: string
  final: boolean
}

export interface NovelProject extends NovelBook {
  brief: string
  authorGuidance?: string
  storyMode?: 'serial'
  needsTitle?: boolean
  version: number
  modelCode: string
  outline: NovelOutline | null
  outlineCheck?: NovelOutlineCheck
  confirmed: boolean
  chapters: NovelChapter[]
}

export interface NovelCreate {
  title?: string
  suggestTitle?: boolean
  brief: string
  chapterCount?: number
  storyMode?: 'serial'
}

export type NovelAction =
  | { kind: 'generate_outline'; modelCode: string; instruction?: string; outline?: NovelOutline }
  | { kind: 'check_outline'; modelCode: string; outline?: NovelOutline }
  | { kind: 'rename'; title: string }
  | { kind: 'suggest_title'; modelCode: string }
  | { kind: 'save_author_guidance'; authorGuidance: string }
  | { kind: 'save_outline' | 'confirm_outline'; outline: NovelOutline }
  | { kind: 'reopen_outline' }
  | { kind: 'append_chapters'; beats: NovelBeat[] }
  | { kind: 'write'; chapter: number; modelCode: string; instruction: string }
  | { kind: 'review' | 'revise_review'; chapter: number; modelCode: string }
  | { kind: 'reopen_chapter'; chapter: number }
  | { kind: 'save_draft' | 'finalize'; chapter: number; body: string; summary: string }

const base = '/api/v1/ai-novel'

export const listNovels = () => request<NovelBook[]>(base + '/projects')
export const listNovelModels = () => request<{ code: string; name: string }[]>(base + '/models')
export const listNovelCoverModels = () => request<{ code: string; name: string }[]>(base + '/cover-models')
export const getNovel = (id: string) => request<NovelProject>(base + '/projects/' + encodeURIComponent(id))
export const listNovelFinalizedChapters = (id: string) =>
  request<{ number: number; title: string }[]>(`${base}/projects/${encodeURIComponent(id)}/finalized-chapters`)

function post<T>(path: string, data: object, options?: Pick<RequestInit, 'keepalive'>) {
  return request<T>(base + path, {
    method: 'POST',
    ...options,
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify(data),
  })
}

export const refineNovelIdea = (input: { brief: string; instruction?: string; modelCode: string }) =>
  post<{ brief: string }>('/ideas/refine', input)
export const createNovel = (input: NovelCreate) => post<NovelProject>('/projects', input)
export const applyNovelAction = (p: NovelProject, action: NovelAction) =>
  post<NovelProject>('/projects/' + encodeURIComponent(p.id) + '/actions', { ...action, version: p.version })

export const planNovelChapters = (p: NovelProject, input: { count: number; modelCode: string; instruction: string }) =>
  post<{ beats: NovelBeat[] }>(`/projects/${encodeURIComponent(p.id)}/chapter-plan`, { ...input, version: p.version })

export function uploadNovelCover(id: string, file: File) {
  const body = new FormData()
  body.append('file', file)
  return request<NovelCover>(`${base}/projects/${encodeURIComponent(id)}/cover`, {
    method: 'POST', headers: csrfHeaders(), body,
  })
}

export interface NovelCoverGenerationInput {
  modelCode: string
  instruction: string
  styleId?: string
}

export interface NovelCoverGeneration {
  status: 'idle' | 'running' | 'succeeded' | 'failed'
  jobId?: string
  preview?: NovelCoverPreview
  errorMessage?: string
  modelCode?: string
  styleId?: string
  instruction?: string
}

export const getNovelCoverGeneration = (id: string, signal?: AbortSignal) =>
  request<NovelCoverGeneration>(`${base}/projects/${encodeURIComponent(id)}/cover/generation`, { signal })

export const startNovelCoverGeneration = (id: string, input: NovelCoverGenerationInput) =>
  post<NovelCoverGeneration>(`/projects/${encodeURIComponent(id)}/cover/generation`, input, { keepalive: true })

export const applyNovelCover = (id: string, token: string) =>
  post<NovelCover>(`/projects/${encodeURIComponent(id)}/cover/apply`, { token })
