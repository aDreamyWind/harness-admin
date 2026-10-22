import { csrfHeaders, request } from './client'

export interface MCPService {
  id: string
  code: string
  name: string
  description: string
  transport: 'streamable_http' | 'sse' | 'stdio'
  upstreamUrl: string
  parameters: MCPParameterDefinition[]
  credentialsConfigured: boolean
  status: 'active' | 'disabled'
  sourceType: 'remote' | 'github' | 'gitee' | 'upload'
  runtimeType?: 'node' | 'python'
  entryFile?: string
  deploymentStatus?: 'building' | 'failed' | 'stopped' | 'running'
  deploymentError?: string
  currentRevision?: string
  createdAt: string
  updatedAt: string
}

export interface MCPTool {
  name: string
  description: string
  inputSchema: Record<string, unknown>
  discoveredAt: string
}

export interface MCPServiceInput {
  name: string
  description?: string
  transport: MCPService['transport']
  upstreamUrl: string
  parameters: MCPParameterDefinition[]
}

export interface MCPParameterDefinition {
  location: 'header' | 'query' | 'path'
  name: string
  label?: string
  description?: string
  required: boolean
}

export interface MCPParameterValue {
  location: MCPParameterDefinition['location']
  name: string
  value: string
}

export interface MCPUpstreamCredentials {
  parameters: MCPParameterValue[]
}

export const listMCPServices = () => request<MCPService[]>('/api/v1/mcp-services')

export const revealMCPServiceCredentials = (id: string) => request<MCPUpstreamCredentials>(`/api/v1/mcp-services/${id}/credentials`)

export function updateMCPServiceCredentials(id: string, parameters: MCPParameterValue[], definitions: MCPParameterDefinition[]) {
  return request<MCPService>(`/api/v1/mcp-services/${id}/credentials`, {
    method: 'PUT', headers: { 'Content-Type': 'application/json', ...csrfHeaders() }, body: JSON.stringify({ parameters, definitions }),
  })
}

export function createMCPService(input: MCPServiceInput) {
  return request<MCPService>('/api/v1/mcp-services', {
    method: 'POST', headers: { 'Content-Type': 'application/json', ...csrfHeaders() }, body: JSON.stringify(input),
  })
}

export function updateMCPService(id: string, input: MCPServiceInput) {
  return request<MCPService>(`/api/v1/mcp-services/${id}`, {
    method: 'PUT', headers: { 'Content-Type': 'application/json', ...csrfHeaders() }, body: JSON.stringify(input),
  })
}

export function changeMCPServiceStatus(id: string, status: MCPService['status']) {
  return request<MCPService>(`/api/v1/mcp-services/${id}/status`, {
    method: 'PATCH', headers: { 'Content-Type': 'application/json', ...csrfHeaders() }, body: JSON.stringify({ status }),
  })
}

export const listMCPTools = (id: string) => request<MCPTool[]>(`/api/v1/mcp-services/${id}/tools`)

export function discoverMCPTools(id: string, connectionId?: string) {
  return request<MCPTool[]>(`/api/v1/mcp-services/${id}/discover`, {
    method: 'POST', headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify({ connectionId }),
  })
}

export function testMCPTool(id: string, name: string, argumentsValue: Record<string, unknown>, connectionId?: string) {
  return request<unknown>(`/api/v1/mcp-services/${id}/test`, {
    method: 'POST', headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify({ name, arguments: argumentsValue, connectionId }),
  })
}
