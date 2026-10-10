import { ApiError, platformCSRFHeaders, request } from './client'

export interface PlatformAdmin {
  id: string
  username: string
  displayName: string
}

export interface PlatformLoginInput {
  username: string
  password: string
}

export interface PlatformOrganization {
  id: string
  code: string
  name: string
  status: 'active' | 'disabled'
  adminUsername: string
  userCount: number
  createdAt: string
  updatedAt: string
}

export interface CreatePlatformOrganizationInput {
  code: string
  name: string
  adminUsername: string
  adminDisplayName: string
  adminEmail: string
  adminPassword: string
}

interface PlatformLoginResult {
  admin: PlatformAdmin
  csrfToken: string
  expiresAt: string
}

export async function platformLogin(input: PlatformLoginInput): Promise<PlatformAdmin> {
  const result = await request<PlatformLoginResult>('/api/v1/platform/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(input),
  })
  return result.admin
}

export async function getCurrentPlatformAdmin(): Promise<PlatformAdmin | null> {
  try {
    return await request<PlatformAdmin>('/api/v1/platform/auth/me')
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) return null
    throw error
  }
}

export function platformLogout(): Promise<void> {
  return request('/api/v1/platform/auth/logout', { method: 'POST', headers: platformCSRFHeaders() })
}

export function listPlatformOrganizations(): Promise<PlatformOrganization[]> {
  return request('/api/v1/platform/organizations')
}

export function createPlatformOrganization(input: CreatePlatformOrganizationInput): Promise<PlatformOrganization> {
  return request('/api/v1/platform/organizations', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...platformCSRFHeaders() },
    body: JSON.stringify(input),
  })
}

export function updatePlatformOrganizationStatus(id: string, status: PlatformOrganization['status']): Promise<PlatformOrganization> {
  return request(`/api/v1/platform/organizations/${id}/status`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json', ...platformCSRFHeaders() },
    body: JSON.stringify({ status }),
  })
}
