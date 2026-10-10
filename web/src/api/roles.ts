import { csrfHeaders, request } from './client'

export interface Permission {
  id: string
  code: string
  name: string
  resource: string
  action: string
  description: string
}

export interface Role {
  id: string
  code: string
  name: string
  description: string
  builtIn: boolean
  userCount: number
  permissionCodes: string[]
  createdAt: string
}

export interface RoleInput {
  code?: string
  name: string
  description: string
  permissionCodes: string[]
}

export function listRoles(): Promise<Role[]> {
  return request<Role[]>('/api/v1/roles')
}

export function listPermissions(): Promise<Permission[]> {
  return request<Permission[]>('/api/v1/permissions')
}

export function createRole(input: RoleInput): Promise<Role> {
  return request<Role>('/api/v1/roles', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify(input),
  })
}

export function updateRole(roleId: string, input: RoleInput): Promise<Role> {
  return request<Role>(`/api/v1/roles/${roleId}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify(input),
  })
}

export function assignUserRoles(userId: string, roleIds: string[]): Promise<{ userId: string; roleIds: string[] }> {
  return request(`/api/v1/users/${userId}/roles`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify({ roleIds }),
  })
}
