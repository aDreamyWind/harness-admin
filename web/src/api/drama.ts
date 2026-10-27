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
export const createDramaProject = (input: CreateDramaInput) => request<DramaProject>('/api/v1/ai-drama/projects', {
  method: 'POST', headers: { 'Content-Type': 'application/json', ...csrfHeaders() }, body: JSON.stringify(input),
})
export const updateDramaOverview = (id: string, overview: DramaOverview) => request<DramaProject>(`/api/v1/ai-drama/projects/${id}/overview`, {
  method: 'PUT', headers: { 'Content-Type': 'application/json', ...csrfHeaders() }, body: JSON.stringify(overview),
})
