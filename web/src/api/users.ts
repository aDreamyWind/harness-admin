import { csrfHeaders, request } from './client'

export interface ManagedUser {
  id: string
  username: string
  email?: string
  displayName: string
  departmentId?: string
  departmentName?: string
  status: 'active' | 'locked' | 'disabled'
  roleIds: string[]
  roleNames: string[]
  lastLoginAt?: string
  createdAt: string
}

export interface UserList {
  items: ManagedUser[]
  total: number
  page: number
  pageSize: number
}

export interface UserListQuery {
  search?: string
  status?: string
  page: number
  pageSize: number
}

export interface CreateUserInput {
  username: string
  email?: string
  displayName: string
  password: string
  departmentId?: string
}

export interface UpdateUserInput {
  displayName: string
  email: string
}

export async function listUsers(query: UserListQuery): Promise<UserList> {
  const search = new URLSearchParams({
    page: String(query.page),
    pageSize: String(query.pageSize),
  })
  if (query.search) search.set('search', query.search)
  if (query.status) search.set('status', query.status)
  return request<UserList>(`/api/v1/users?${search}`)
}

export function createUser(input: CreateUserInput): Promise<ManagedUser> {
  return request<ManagedUser>('/api/v1/users', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify(input),
  })
}

export function updateUser(userId: string, input: UpdateUserInput): Promise<ManagedUser> {
  return request<ManagedUser>(`/api/v1/users/${userId}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify(input),
  })
}

export function changeUserStatus(userId: string, status: 'active' | 'disabled'): Promise<ManagedUser> {
  return request<ManagedUser>(`/api/v1/users/${userId}/status`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify({ status }),
  })
}

export function resetUserPassword(userId: string, password: string): Promise<void> {
  return request(`/api/v1/users/${userId}/password`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify({ password }),
  })
}

export function unlockUser(userId: string): Promise<ManagedUser> {
	return request<ManagedUser>(`/api/v1/users/${userId}/unlock`, {
		method: 'POST',
		headers: csrfHeaders(),
	})
}
