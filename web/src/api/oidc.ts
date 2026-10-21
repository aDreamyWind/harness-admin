import { csrfHeaders, request } from './client'

export type OIDCClientStatus = 'active' | 'disabled'
export type OIDCScope = 'openid' | 'profile' | 'email' | 'model.invoke' | 'mcp.invoke' | 'offline_access'
export type OIDCApplicationType = 'web' | 'desktop'
export type OIDCTokenEndpointAuthMethod = 'client_secret_basic' | 'none'

export interface OIDCClient {
  id: string
  name: string
  clientId: string
  clientSecretPrefix: string
  applicationType: OIDCApplicationType
  tokenEndpointAuthMethod: OIDCTokenEndpointAuthMethod
  redirectUris: string[]
  allowedScopes: OIDCScope[]
  status: OIDCClientStatus
  createdAt: string
  updatedAt: string
}

export interface CreatedOIDCClient extends OIDCClient {
  clientSecret?: string
}

export interface OIDCClientInput {
  name: string
  applicationType: OIDCApplicationType
  redirectUris: string[]
  allowedScopes: OIDCScope[]
}

export interface OIDCAuthorizationRequest {
  clientName: string
  clientId: string
  applicationType: OIDCApplicationType
  redirectUri: string
  redirectHost: string
  scopes: OIDCScope[]
}

export interface AuthorizedApplication {
  id: string
  clientId: string
  clientName: string
  applicationType: OIDCApplicationType
  scopes: OIDCScope[]
  grantedAt: string
  updatedAt: string
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

export function revealOIDCClientSecret(id: string) {
  return request<{ secret: string }>(`/api/v1/oidc/clients/${id}/secret`)
}

export function getOIDCAuthorizationRequest(search: string) {
  return request<OIDCAuthorizationRequest>(`/api/v1/oauth2/consent${normalizeSearch(search)}`)
}

export function decideOIDCAuthorization(search: string, approved: boolean) {
  return request<{ redirectUri: string }>(`/api/v1/oauth2/consent${normalizeSearch(search)}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify({ approved }),
  })
}

export const listAuthorizedApplications = () => request<AuthorizedApplication[]>('/api/v1/account/authorized-applications')

export function revokeAuthorizedApplication(id: string) {
  return request<{ revoked: boolean }>(`/api/v1/account/authorized-applications/${id}`, {
    method: 'DELETE', headers: csrfHeaders(),
  })
}

function normalizeSearch(search: string): string {
  if (!search) return ''
  return search.startsWith('?') ? search : `?${search}`
}
