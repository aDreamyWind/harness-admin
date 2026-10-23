import { csrfHeaders, request } from './client'

export type ProviderKind = 'openai_compatible' | 'deepseek' | 'aliyun_bailian' | 'kimi' | 'ollama' | 'siliconflow' | 'claude' | 'gemini'
export type ModelType = 'chat' | 'image' | 'video' | 'audio' | 'embedding' | 'rerank' | 'unknown'

export interface ModelProvider {
  id: string
  code: string
  name: string
  providerKind: ProviderKind
  baseUrl: string
  modelListUrl: string
  status: 'active' | 'disabled'
  requestTimeoutMs: number
  streamUsageEnabled: boolean
  credentialConfigured: boolean
  lastProbeStatus: 'unknown' | 'success' | 'failed'
  lastProbeAt?: string
  createdAt: string
  updatedAt: string
}

export interface ModelCatalogEntry {
  id: string
  providerConnectionId: string
  providerName: string
  code: string
  upstreamModelId: string
  name: string
  modelType: ModelType
  capabilities: string[]
  contextWindowTokens: number
  maxOutputTokens: number
  status: 'active' | 'disabled'
  createdAt: string
  updatedAt: string
}

export interface ProviderInput {
  code?: string
  name: string
  providerKind?: ProviderKind
  baseUrl: string
  modelListUrl?: string
  apiKey?: string
  requestTimeoutMs: number
  streamUsageEnabled: boolean
}

export interface ModelInput {
  code?: string
  providerConnectionId: string
  upstreamModelId: string
  name: string
  modelType: ModelType
  capabilities: string[]
  contextWindowTokens: number
  maxOutputTokens: number
}

export interface ProbeResult {
  status: 'success'
  latencyMs: number
  checkedAt: string
}

export interface DiscoveredModel {
  upstreamModelId: string
  name: string
  modelType: ModelType
  capabilities: string[]
  contextWindowTokens: number
  maxOutputTokens: number
}

export const listModelProviders = () => request<ModelProvider[]>('/api/v1/model-providers')
export const listModels = () => request<ModelCatalogEntry[]>('/api/v1/models')

export function createModelProvider(input: ProviderInput) {
  return request<ModelProvider>('/api/v1/model-providers', {
    method: 'POST', headers: { 'Content-Type': 'application/json', ...csrfHeaders() }, body: JSON.stringify(input),
  })
}

export function updateModelProvider(id: string, input: ProviderInput) {
  return request<ModelProvider>(`/api/v1/model-providers/${id}`, {
    method: 'PUT', headers: { 'Content-Type': 'application/json', ...csrfHeaders() }, body: JSON.stringify(input),
  })
}

export function changeModelProviderStatus(id: string, status: ModelProvider['status']) {
  return request<ModelProvider>(`/api/v1/model-providers/${id}/status`, {
    method: 'PATCH', headers: { 'Content-Type': 'application/json', ...csrfHeaders() }, body: JSON.stringify({ status }),
  })
}

export function probeModelProvider(id: string) {
  return request<ProbeResult>(`/api/v1/model-providers/${id}/probe`, { method: 'POST', headers: csrfHeaders() })
}

export function discoverProviderModels(id: string) {
  return request<DiscoveredModel[]>(`/api/v1/model-providers/${id}/discover-models`, { method: 'POST', headers: csrfHeaders() })
}

export function importProviderModels(id: string, upstreamModelIds: string[], activateImportedModels: boolean) {
  return request<{ imported: number; skipped: number; activated: number }>(`/api/v1/model-providers/${id}/import-models`, {
    method: 'POST', headers: { 'Content-Type': 'application/json', ...csrfHeaders() }, body: JSON.stringify({ upstreamModelIds, activateImportedModels }),
  })
}

export function revealModelProviderCredential(id: string) {
  return request<{ secret: string }>(`/api/v1/model-providers/${id}/credential`)
}

export function createModel(input: ModelInput) {
  return request<ModelCatalogEntry>('/api/v1/models', {
    method: 'POST', headers: { 'Content-Type': 'application/json', ...csrfHeaders() }, body: JSON.stringify(input),
  })
}

export function updateModel(id: string, input: ModelInput) {
  return request<ModelCatalogEntry>(`/api/v1/models/${id}`, {
    method: 'PUT', headers: { 'Content-Type': 'application/json', ...csrfHeaders() }, body: JSON.stringify(input),
  })
}

export function changeModelStatus(id: string, status: ModelCatalogEntry['status']) {
  return request<ModelCatalogEntry>(`/api/v1/models/${id}/status`, {
    method: 'PATCH', headers: { 'Content-Type': 'application/json', ...csrfHeaders() }, body: JSON.stringify({ status }),
  })
}
