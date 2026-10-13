import { csrfHeaders, request } from './client'

export type OIDCClientStatus = 'active' | 'disabled'
export type OIDCScope = 'openid' | 'profile' | 'email' | 'model.invoke' | 'offline_access'

export interface OIDCClient {
  id: string
  name: string
  clientId: string
  clientSecretPrefix: string
  redirectUris: string[]
  allowedScopes: OIDCScope[]
  status: OIDCClientStatus
  createdAt: string
  updatedAt: string
}

export interface CreatedOIDCClient extends OIDCClient {
  clientSecret: string
}

export interface OIDCClientInput {
  name: string
  redirectUris: string[]
  allowedScopes: OIDCScope[]
}

export const listOIDCClients = () => request<OIDCClient[]>('/api/v1/oidc/clients')

export function createOIDCClient(input: OIDCClientInput) {
  return request<CreatedOIDCClient>('/api/v1/oidc/clients', {
    method: 'POST', headers: { 'Content-Type': 'application/json', ...csrfHeaders() }, body: JSON.stringify(input),
  })
}

export function updateOIDCClient(id: string, input: OIDCClientInput) {
  return request<OIDCClient>(`/api/v1/oidc/clients/${id}`, {
    method: 'PUT', headers: { 'Content-Type': 'application/json', ...csrfHeaders() }, body: JSON.stringify(input),
  })
}

export function changeOIDCClientStatus(id: string, status: OIDCClientStatus) {
  return request<OIDCClient>(`/api/v1/oidc/clients/${id}/status`, {
    method: 'PATCH', headers: { 'Content-Type': 'application/json', ...csrfHeaders() }, body: JSON.stringify({ status }),
  })
}

export function rotateOIDCClientSecret(id: string) {
  return request<CreatedOIDCClient>(`/api/v1/oidc/clients/${id}/rotate-secret`, {
    method: 'POST', headers: csrfHeaders(),
  })
}
