import { csrfHeaders, request, requestBlob } from './client'

export type TryOnKind = 'person_2d' | 'person_real' | 'garment' | 'pose'
export type TryOnBox = [number, number, number, number]

export interface TryOnAsset {
  id: string
  kind: TryOnKind
  name: string
  category?: string
  builtin?: boolean
  retired?: boolean
  deleted?: boolean
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
  garmentIds: string[]
  poseId?: string
  modelCode: string
}
export interface TryOnJob extends Omit<TryOnInput, 'garmentIds'> {
  id: string
  garmentId?: string
  garmentIds?: string[]
  status: 'queued' | 'running' | 'succeeded' | 'failed'
  error?: string
  createdAt: string
}
export interface TryOnOutfit {
  id: string
  name: string
  garmentIds: string[]
  createdAt: string
}
export interface TryOnGarmentItem {
  name: string
  category: string
  box?: TryOnBox
}
export interface TryOnGarmentJob {
  id: string
  name: string
  category: string
  modelCode: string
  status: 'queued' | 'running' | 'succeeded' | 'failed' | 'saved'
  error?: string
  createdAt: string
}

const base = '/api/v1/ai-tryon'
export const getTryOnModels = () => request<TryOnModels>(base + '/models')
export const getTryOnAssets = () => request<TryOnAsset[]>(base + '/assets')
export const getTryOnJobs = () => request<TryOnJob[]>(base + '/jobs')
export const getTryOnOutfits = () => request<TryOnOutfit[]>(base + '/outfits')
export const deleteTryOnOutfit = (id: string) =>
  request<{ deleted: true }>(base + '/outfits/' + encodeURIComponent(id), { method: 'DELETE', headers: csrfHeaders() })
export const deleteTryOnAsset = (id: string) =>
  request<{ deleted: true }>(base + '/assets/' + encodeURIComponent(id), { method: 'DELETE', headers: csrfHeaders() })
export const tryOnAssetURL = (id: string) => base + '/assets/' + encodeURIComponent(id) + '/content'
export const tryOnResultURL = (id: string) => base + '/jobs/' + encodeURIComponent(id) + '/result'
const garmentJobPath = (id: string) => '/garment-jobs/' + encodeURIComponent(id)
export const tryOnGarmentResultURL = (id: string) => base + garmentJobPath(id) + '/result'
export const getTryOnGarmentJobs = () => request<TryOnGarmentJob[]>(base + '/garment-jobs')
export const saveTryOnGarmentJob = (id: string) =>
  request<TryOnAsset>(base + garmentJobPath(id) + '/save', { method: 'POST', headers: csrfHeaders() })
export const retryTryOnGarmentJob = (id: string) =>
  request<TryOnGarmentJob>(base + garmentJobPath(id) + '/retry', { method: 'POST', headers: csrfHeaders() })

function postJSON<T>(path: string, data: object) {
  return request<T>(base + path, {
    method: 'POST', headers: { 'Content-Type': 'application/json', ...csrfHeaders() }, body: JSON.stringify(data),
  })
}

function imageRequest(file: File, fields: Record<string, string>): RequestInit {
  const data = new FormData()
  data.append('file', file)
  for (const [key, value] of Object.entries(fields)) data.append(key, value)
  return { method: 'POST', headers: csrfHeaders(), body: data }
}

export const startTryOn = (input: TryOnInput) => postJSON<TryOnJob>('/jobs', input)
export const createTryOnOutfit = (input: { name: string; garmentIds: string[] }) =>
  postJSON<TryOnOutfit>('/outfits', input)
export const generateTryOnPerson = (input: { kind: TryOnKind; name: string; description: string; modelCode: string }) =>
  postJSON<TryOnAsset>('/people/generate', input)
export const detectTryOnGarments = (file: File, modelCode: string) =>
  request<TryOnCandidate[]>(base + '/garments/detect', imageRequest(file, { modelCode }))
export const queueTryOnGarments = (file: File, modelCode: string, items: TryOnGarmentItem[]) =>
  request<TryOnGarmentJob[]>(base + '/garment-jobs', imageRequest(file, { modelCode, items: JSON.stringify(items) }))

export async function previewTryOnAsset(file: File, input: {
  kind: TryOnKind
  modelCode: string
  name?: string
  category?: string
  box?: TryOnBox
}): Promise<File> {
  const { box, ...fields } = input
  const image = await requestBlob(base + '/assets/preview', 'image/png, image/jpeg', '生成素材失败，请重试。',
    imageRequest(file, { ...fields, ...(box ? { box: JSON.stringify(box) } : {}) }))
  if (!image.size || !['image/png', 'image/jpeg'].includes(image.type)) {
    throw new Error('生成服务未返回可用的图片，请重试。')
  }
  return new File([image], image.type === 'image/png' ? 'generated.png' : 'generated.jpg', { type: image.type })
}

export const saveTryOnAsset = (file: File, kind: TryOnKind, name: string, category: string) =>
  request<TryOnAsset>(base + '/assets', imageRequest(file, { kind, name, category }))
