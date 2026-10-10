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
