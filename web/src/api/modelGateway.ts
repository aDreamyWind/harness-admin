import { csrfHeaders, request } from './client'

export interface GatewayAPIKey {
  id: string
  name: string
  keyPrefix: string
  status: 'active' | 'revoked'
  expiresAt?: string
  lastUsedAt?: string
  createdAt: string
  revokedAt?: string
}

export interface CreatedGatewayAPIKey extends GatewayAPIKey {
  secret: string
}

export interface GatewayCall {
  id: string
  apiKeyId: string
  apiKeyName: string
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
  errorCode: string
  sourceIp: string
  createdAt: string
}

export interface CreateGatewayAPIKeyInput {
  name: string
  expiresAt?: string
}

export const listGatewayAPIKeys = () => request<GatewayAPIKey[]>('/api/v1/model-gateway/api-keys')
export const listGatewayCalls = () => request<GatewayCall[]>('/api/v1/model-gateway/calls?limit=100')

export function createGatewayAPIKey(input: CreateGatewayAPIKeyInput) {
  return request<CreatedGatewayAPIKey>('/api/v1/model-gateway/api-keys', {
    method: 'POST', headers: { 'Content-Type': 'application/json', ...csrfHeaders() }, body: JSON.stringify(input),
  })
}

export function revokeGatewayAPIKey(id: string) {
  return request<GatewayAPIKey>(`/api/v1/model-gateway/api-keys/${id}/revoke`, {
    method: 'PATCH', headers: csrfHeaders(),
  })
}
