import { csrfHeaders, request } from './client'

export interface GatewayAPIKey {
  id: string
  name: string
  keyPrefix: string
  status: 'active' | 'revoked'
  expiresAt?: string
  lastUsedAt?: string
  requestsPerMinute: number
  monthlyTokenLimit: number
  monthlyTokenUsed: number
  allowedModelCodes: string[]
  createdAt: string
  revokedAt?: string
}

export interface CreatedGatewayAPIKey extends GatewayAPIKey {
  secret: string
}

export interface GatewayCall {
  id: string
  apiKeyId?: string
  apiKeyName?: string
  authType: 'api_key' | 'user'
  userId?: string
  userName?: string
  departmentId?: string
  requestId: string
  modelCode: string
  upstreamModelId: string
  streaming: boolean
  result: 'success' | 'failed'
  httpStatus: number
  latencyMs: number
  promptTokens: number
  completionTokens: number
  totalTokens: number
  usageReported: boolean
  quotaExceeded: boolean
  errorCode: string
  sourceIp: string
  createdAt: string
}

export type UsageGroupBy = 'department' | 'user' | 'model'

export interface GatewayUsageGroup {
  id: string
  name: string
  callCount: number
  knownTokens: number
  unknownUsageCalls: number
  overQuotaCalls: number
}

export interface GatewayUsagePage {
  month: string
  groupBy: UsageGroupBy
  callCount: number
  knownTokens: number
  unknownUsageCalls: number
  overQuotaCalls: number
  items: GatewayUsageGroup[]
  total: number
  page: number
  pageSize: number
}

export interface GatewayUsageQuery {
  month: string
  groupBy: UsageGroupBy
  page: number
  pageSize: number
}

export interface CreateGatewayAPIKeyInput {
  name: string
  expiresAt?: string
  requestsPerMinute: number
  monthlyTokenLimit: number
  allowedModelCodes: string[]
}

export interface GatewayCallPage {
  items: GatewayCall[]
  total: number
  page: number
  pageSize: number
}

export interface GatewayCallQuery {
  page: number
  pageSize: number
  apiKeyId?: string
  modelCode?: string
  result?: GatewayCall['result']
  startedAt?: string
  endedAt?: string
}

export const listGatewayAPIKeys = () => request<GatewayAPIKey[]>('/api/v1/model-gateway/api-keys')
export function listGatewayUsage(query: GatewayUsageQuery) {
  const params = new URLSearchParams({ month: query.month, groupBy: query.groupBy, page: String(query.page), pageSize: String(query.pageSize) })
  return request<GatewayUsagePage>(`/api/v1/model-gateway/usage?${params}`)
}
export function listGatewayCalls(query: GatewayCallQuery) {
  const params = new URLSearchParams({ page: String(query.page), pageSize: String(query.pageSize) })
  if (query.apiKeyId) params.set('apiKeyId', query.apiKeyId)
  if (query.modelCode) params.set('modelCode', query.modelCode)
  if (query.result) params.set('result', query.result)
  if (query.startedAt) params.set('startedAt', query.startedAt)
  if (query.endedAt) params.set('endedAt', query.endedAt)
  return request<GatewayCallPage>(`/api/v1/model-gateway/calls?${params}`)
}

export function createGatewayAPIKey(input: CreateGatewayAPIKeyInput) {
  return request<CreatedGatewayAPIKey>('/api/v1/model-gateway/api-keys', {
    method: 'POST', headers: { 'Content-Type': 'application/json', ...csrfHeaders() }, body: JSON.stringify(input),
  })
}

export function updateGatewayAPIKey(id: string, input: CreateGatewayAPIKeyInput) {
  return request<GatewayAPIKey>(`/api/v1/model-gateway/api-keys/${id}`, {
    method: 'PUT', headers: { 'Content-Type': 'application/json', ...csrfHeaders() }, body: JSON.stringify(input),
  })
}

export function revokeGatewayAPIKey(id: string) {
  return request<GatewayAPIKey>(`/api/v1/model-gateway/api-keys/${id}/revoke`, {
    method: 'PATCH', headers: csrfHeaders(),
  })
}

export function revealGatewayAPIKeySecret(id: string) {
  return request<{ secret: string }>(`/api/v1/model-gateway/api-keys/${id}/secret`)
}
