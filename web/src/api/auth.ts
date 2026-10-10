import { ApiError, csrfHeaders, request } from './client'

export { ApiError } from './client'

export interface AuthUser {
  id: string
  organizationId: string
  organization: string
  organizationCode: string
  username: string
  email?: string
  displayName: string
}

export interface LoginInput {
  organizationCode: string
  username: string
  password: string
}

export interface UserSession {
  id: string
  userAgent: string
  sourceIp?: string
  expiresAt: string
  lastSeenAt: string
  createdAt: string
  current: boolean
}

export interface ChangePasswordInput {
  currentPassword: string
  newPassword: string
}

interface LoginResult {
  user: AuthUser
  csrfToken: string
  expiresAt: string
}

export async function login(input: LoginInput): Promise<AuthUser> {
  const result = await request<LoginResult>('/api/v1/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(input),
  })
  return result.user
}

export async function getCurrentUser(): Promise<AuthUser | null> {
  try {
    return await request<AuthUser>('/api/v1/auth/me')
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      return null
    }
    throw error
  }
}

export async function logout(): Promise<void> {
  await request<{ loggedOut: boolean }>('/api/v1/auth/logout', {
    method: 'POST',
    headers: csrfHeaders(),
  })
}

export function changePassword(input: ChangePasswordInput): Promise<{ changed: boolean; revokedSessions: number }> {
  return request('/api/v1/auth/password', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify(input),
  })
}

export function listSessions(): Promise<UserSession[]> {
  return request<UserSession[]>('/api/v1/auth/sessions')
}

export function revokeSession(sessionId: string): Promise<void> {
  return request(`/api/v1/auth/sessions/${sessionId}`, {
    method: 'DELETE',
    headers: csrfHeaders(),
  })
}

export function revokeOtherSessions(): Promise<{ revokedSessions: number }> {
  return request('/api/v1/auth/sessions/revoke-others', {
    method: 'POST',
    headers: csrfHeaders(),
  })
}
