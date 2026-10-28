import { csrfHeaders, request } from './client'

export interface DramaModel { code: string; name: string; modelType: string }
export interface DramaSettings {
  textModelCode: string
  imageModelCode: string
  videoModelCode: string
  updatedAt?: string
}
export interface DramaAsset { name: string; description: string }
export interface DramaEpisode { title: string; synopsis: string }
export interface DramaOverview {
  videoStyle?: string
  aspectRatio?: string
  protagonist?: string
  genre?: string
  synopsis?: string
  background?: string
  setting?: string
}
export interface DramaProject {
  id: string
  title: string
  sourceName: string
  sourceLength: number
  sourceMode: 'upload' | 'generate'
  style: string
  aspectRatio: '9:16' | '16:9'
  modelCode: string
  status: 'queued' | 'running' | 'ready' | 'failed'
  phase: string
  error?: string
  overview: DramaOverview
  characters: DramaAsset[]
  scenes: DramaAsset[]
  props: DramaAsset[]
  episodes: DramaEpisode[]
  createdAt: string
  updatedAt: string
}
export interface CreateDramaInput {
  title: string
  sourceName: string
  sourceText: string
  sourceMode: 'upload' | 'generate'
  style: string
  aspectRatio: '9:16' | '16:9'
}

export const listDramaModels = () => request<DramaModel[]>('/api/v1/ai-drama/models')
export const getDramaSettings = () => request<DramaSettings>('/api/v1/ai-drama/settings')
export const saveDramaSettings = (input: DramaSettings) => request<DramaSettings>('/api/v1/ai-drama/settings', {
  method: 'PUT', headers: { 'Content-Type': 'application/json', ...csrfHeaders() }, body: JSON.stringify(input),
})
export const listDramaProjects = () => request<DramaProject[]>('/api/v1/ai-drama/projects')
export const getDramaProject = (id: string) => request<DramaProject>(`/api/v1/ai-drama/projects/${id}`)
export const getDramaSource = (id: string) => request<{ sourceText: string }>(`/api/v1/ai-drama/projects/${id}/source`)
export const retryDramaProject = (id: string) => request<DramaProject>(`/api/v1/ai-drama/projects/${id}/retry`, {
  method: 'POST', headers: csrfHeaders(),
})
export const reanalyzeDramaOverview = (id: string) => request<DramaProject>(`/api/v1/ai-drama/projects/${id}/reanalyze-overview`, {
  method: 'POST', headers: csrfHeaders(),
})
export const createDramaProject = (input: CreateDramaInput) => request<DramaProject>('/api/v1/ai-drama/projects', {
  method: 'POST', headers: { 'Content-Type': 'application/json', ...csrfHeaders() }, body: JSON.stringify(input),
})
export const updateDramaOverview = (id: string, overview: DramaOverview) => request<DramaProject>(`/api/v1/ai-drama/projects/${id}/overview`, {
  method: 'PUT', headers: { 'Content-Type': 'application/json', ...csrfHeaders() }, body: JSON.stringify(overview),
})

export interface DramaSourceRange {
  startOffset: number
  endOffset: number
  totalLength: number
  text: string
}
export type DramaBatchMode = 'beginning' | 'previous' | 'manual'
export interface DramaBatchSelection {
  mode: DramaBatchMode
  previousBatchId?: string
  startOffset?: number
  endOffset?: number
}
export interface DramaBatchSuggestion { startOffset: number; endOffset: number; boundary: 'episode' | 'chapter' | 'paragraph' | 'limit' | 'end' }
export interface DramaBatch {
  id: string
  projectId: string
  number: number
  startOffset: number
  endOffset: number
  selectionMode: DramaBatchMode
  previousBatchId?: string
  boundary: DramaBatchSuggestion['boundary'] | 'manual'
  status: 'pending' | 'stale' | 'ready'
  revision: number
  createdAt: string
  updatedAt: string
}
export interface DramaBatchRangeChange { startOffset: number; endOffset: number; revision: number }

export const getDramaSourceRange = (id: string, start: number, limit = 12000) =>
  request<DramaSourceRange>(`/api/v1/ai-drama/projects/${id}/source/range?start=${start}&limit=${limit}`)
export const listDramaBatches = (id: string) => request<DramaBatch[]>(`/api/v1/ai-drama/projects/${id}/batches`)
export const suggestDramaBatch = (id: string, input: Omit<DramaBatchSelection, 'endOffset'>) => {
  const query = new URLSearchParams({ mode: input.mode })
  if (input.previousBatchId) query.set('previousBatchId', input.previousBatchId)
  if (input.startOffset !== undefined) query.set('startOffset', String(input.startOffset))
  return request<DramaBatchSuggestion>(`/api/v1/ai-drama/projects/${id}/batches/suggest?${query}`)
}
export const createDramaBatch = (id: string, input: DramaBatchSelection) => request<DramaBatch>(`/api/v1/ai-drama/projects/${id}/batches`, {
  method: 'POST', headers: { 'Content-Type': 'application/json', ...csrfHeaders() }, body: JSON.stringify(input),
})
export const updateDramaBatchRange = (id: string, batchId: string, input: DramaBatchRangeChange) => request<DramaBatch>(`/api/v1/ai-drama/projects/${id}/batches/${batchId}/range`, {
  method: 'PATCH', headers: { 'Content-Type': 'application/json', ...csrfHeaders() }, body: JSON.stringify(input),
})
