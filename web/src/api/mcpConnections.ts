import { csrfHeaders, request } from './client'
import type { MCPParameterValue } from './mcp'

export interface MCPConnection {
  id: string
  serviceId: string
  serviceName: string
  code: string
  gatewayUrl: string
  name: string
  purpose: 'harness' | 'developer'
  authMode: 'oidc' | 'bearer' | 'none'
  applicationName?: string
  status: 'active' | 'disabled'
  expiresAt?: string
  keyPrefix?: string
  upstreamAuthConfigured: boolean
  upstreamHeaderConfigured: boolean
  upstreamQueryConfigured: boolean
  upstreamConfigured: boolean
  requestsPerMinute: number
  departmentIds: string[]
  userIds: string[]
  createdAt: string
  updatedAt: string
}

export interface CreatedMCPConnection extends MCPConnection {
  secret?: string
}

export interface MCPConnectionInput {
  serviceId: string
  name: string
  purpose: MCPConnection['purpose']
  authMode: MCPConnection['authMode']
  applicationName?: string
  expiresAt?: string
  upstreamParameters?: MCPParameterValue[]
  requestsPerMinute?: number
  departmentIds: string[]
  userIds: string[]
}

export interface MCPUsage {
  connectionId: string
  toolName: string
  userId?: string
  userName?: string
  departmentId?: string
  departmentName?: string
  calls: number
  failures: number
  averageMs: number
}

export interface MCPConnectionHealth {
  connectionId: string
  status: 'healthy' | 'unavailable' | 'inactive'
  message: string
  toolCount: number
  checkedAt: string
}

export const listMCPConnections = () => request<MCPConnection[]>('/api/v1/mcp-connections')
export const checkMCPConnectionHealth = (id: string) => request<MCPConnectionHealth>(`/api/v1/mcp-connections/${id}/health`)
export const listMCPUsage = () => request<MCPUsage[]>('/api/v1/mcp-connections/usage')

export function createMCPConnection(input: MCPConnectionInput) {
  return request<CreatedMCPConnection>('/api/v1/mcp-connections', {
    method: 'POST', headers: { 'Content-Type': 'application/json', ...csrfHeaders() }, body: JSON.stringify(input),
  })
}

export function changeMCPConnectionStatus(id: string, status: MCPConnection['status']) {
  return request<MCPConnection>(`/api/v1/mcp-connections/${id}/status`, {
    method: 'PATCH', headers: { 'Content-Type': 'application/json', ...csrfHeaders() }, body: JSON.stringify({ status }),
  })
}

export function replaceMCPConnectionGrants(id: string, departmentIds: string[], userIds: string[]) {
  return request<MCPConnection>(`/api/v1/mcp-connections/${id}/grants`, {
    method: 'PUT', headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify({ departmentIds, userIds }),
  })
}

export function revealMCPConnectionSecret(id: string) {
  return request<{ secret: string }>(`/api/v1/mcp-connections/${id}/secret`)
}

export function rotateMCPConnectionSecret(id: string) {
  return request<{ secret: string }>(`/api/v1/mcp-connections/${id}/rotate-secret`, {
    method: 'POST', headers: csrfHeaders(),
  })
}

export function revealMCPUpstreamAuthorization(id: string) {
  return request<{ authorization: string }>(`/api/v1/mcp-connections/${id}/upstream-authorization`)
}

export interface MCPUpstreamCredentials {
  parameters: MCPParameterValue[]
  authorization?: string
  headerName?: string
  headerValue?: string
  queryName?: string
  queryValue?: string
}

export function revealMCPUpstreamCredentials(id: string) {
  return request<MCPUpstreamCredentials>(`/api/v1/mcp-connections/${id}/upstream-credentials`)
}

export function updateMCPUpstreamCredentials(id: string, credentials: MCPUpstreamCredentials) {
  return request<MCPConnection>(`/api/v1/mcp-connections/${id}/upstream-credentials`, {
    method: 'PUT', headers: { 'Content-Type': 'application/json', ...csrfHeaders() }, body: JSON.stringify(credentials),
  })
}
