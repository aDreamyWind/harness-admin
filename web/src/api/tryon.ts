import { csrfHeaders, request } from './client'

export type TryOnKind = 'person_2d' | 'person_real' | 'garment' | 'pose'
export type TryOnBox = [number, number, number, number]

export interface TryOnAsset {
  id: string
  kind: TryOnKind
  name: string
  category?: string
  builtin?: boolean
  retired?: boolean
  createdAt: string
}
export interface TryOnCandidate {
  name: string
  category: string
  box: TryOnBox
}
export interface TryOnModel { code: string; name: string }
export interface TryOnModels { detection: TryOnModel[]; image: TryOnModel[]; person: TryOnModel[] }
export interface TryOnInput {
  personId: string
  garmentId: string
  poseId?: string
  modelCode: string
}
export interface TryOnJob extends TryOnInput {
  id: string
  status: 'running' | 'succeeded' | 'failed'
  error?: string
  createdAt: string
}

const base = '/api/v1/ai-tryon'
export const getTryOnModels = () => request<TryOnModels>(base + '/models')
export const getTryOnAssets = () => request<TryOnAsset[]>(base + '/assets')
export const getTryOnJobs = () => request<TryOnJob[]>(base + '/jobs')
export const tryOnAssetURL = (id: string) => base + '/assets/' + encodeURIComponent(id) + '/content'
export const tryOnResultURL = (id: string) => base + '/jobs/' + encodeURIComponent(id) + '/result'

function postJSON<T>(path: string, data: object) {
  return request<T>(base + path, {
    method: 'POST', headers: { 'Content-Type': 'application/json', ...csrfHeaders() }, body: JSON.stringify(data),
  })
}

function postImage<T>(path: string, file: File, fields: Record<string, string>) {
  const data = new FormData()
  data.append('file', file)
  for (const [key, value] of Object.entries(fields)) data.append(key, value)
  return request<T>(base + path, { method: 'POST', headers: csrfHeaders(), body: data })
}

export const startTryOn = (input: TryOnInput) => postJSON<TryOnJob>('/jobs', input)
export const generateTryOnPerson = (input: { kind: TryOnKind; name: string; description: string; modelCode: string }) =>
  postJSON<TryOnAsset>('/people/generate', input)
export const detectTryOnGarments = (file: File, modelCode: string) =>
  postImage<TryOnCandidate[]>('/garments/detect', file, { modelCode })
export const saveTryOnAsset = (file: File, kind: TryOnKind, name: string, category: string, box?: TryOnBox) =>
  postImage<TryOnAsset>('/assets', file, { kind, name, category, ...(box ? { box: JSON.stringify(box) } : {}) })
