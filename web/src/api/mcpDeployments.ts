import { csrfHeaders, request } from './client'
import type { MCPService } from './mcp'

export interface MCPDeploymentVersion {
  id: string
  serviceId: string
  sourceType: 'github' | 'gitee' | 'upload'
  sourceRef: string
  revision: string
  runtimeType: 'node' | 'python'
  entryFile: string
  buildStatus: 'building' | 'succeeded' | 'failed'
  buildLog: string
  errorMessage: string
  createdAt: string
  finishedAt?: string
}

export interface MCPDeploymentResult { service: MCPService; version: MCPDeploymentVersion }
export interface MCPRuntimeStatus { running: boolean; status: string; logs: string; protocolHealthy: boolean; protocolError?: string }
export interface MCPDeploymentCapability { enabled: boolean; sourceTypes: string[]; runtimes: string[] }
export interface MCPSourceInspection { revision: string; entryFile: string; buildCommand: string; hasLockfile: boolean }

export const getMCPDeploymentCapability = () => request<MCPDeploymentCapability>('/api/v1/mcp-deployments/capability')
export const listMCPDeploymentVersions = (id: string) => request<MCPDeploymentVersion[]>(`/api/v1/mcp-deployments/${id}/versions`)
export const getMCPRuntimeStatus = (id: string) => request<MCPRuntimeStatus>(`/api/v1/mcp-deployments/${id}/runtime`)

export const inspectMCPRepository = (repositoryUrl: string, ref?: string) => request<MCPSourceInspection>(
  '/api/v1/mcp-deployments/inspect/repository', {
    method: 'POST', headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify({ repositoryUrl, ref }),
  },
)

export interface MCPDeploymentInput {
  name: string
  sourceType: 'repository' | 'upload'
  repositoryUrl?: string
  ref?: string
  archive?: File
  runtimeType: 'node' | 'python'
  entryFile?: string
}

export function buildMCPDeployment(input: MCPDeploymentInput, serviceId?: string) {
  const base = serviceId ? `/api/v1/mcp-deployments/${serviceId}/versions` : '/api/v1/mcp-deployments/import'
  if (input.sourceType === 'repository') {
    return request<MCPDeploymentResult>(`${base}/repository`, {
      method: 'POST', headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
      body: JSON.stringify({ name: input.name, repositoryUrl: input.repositoryUrl,
        ref: input.ref, runtimeType: input.runtimeType, entryFile: input.entryFile }),
    })
  }
  const form = new FormData()
  form.set('name', input.name)
  form.set('runtimeType', input.runtimeType)
  form.set('entryFile', input.entryFile ?? '')
  if (input.archive) form.set('archive', input.archive)
  return request<MCPDeploymentResult>(`${base}/upload`, { method: 'POST', headers: csrfHeaders(), body: form })
}

export function changeMCPDeployment(id: string, action: 'start' | 'stop' | 'restart') {
  return request<MCPService>(`/api/v1/mcp-deployments/${id}/${action}`, {
    method: 'POST', headers: csrfHeaders(),
  })
}

export function rollbackMCPDeployment(id: string, versionId: string) {
  return request<MCPService>(`/api/v1/mcp-deployments/${id}/rollback`, {
    method: 'POST', headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify({ versionId }),
  })
}

export function updateMCPDeploymentMetadata(id: string, name: string) {
  return request<MCPService>(`/api/v1/mcp-deployments/${id}/metadata`, {
    method: 'PUT', headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify({ name }),
  })
}
