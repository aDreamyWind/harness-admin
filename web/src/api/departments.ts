import { csrfHeaders, request } from './client'

export interface Department {
  id: string
  parentId?: string
  code: string
  name: string
  sortOrder: number
  status: 'active' | 'disabled'
  userCount: number
  createdAt: string
}

export interface DepartmentInput {
  parentId?: string
  code?: string
  name: string
  sortOrder: number
}

export function listDepartments(): Promise<Department[]> {
  return request<Department[]>('/api/v1/departments')
}

export function createDepartment(input: DepartmentInput): Promise<Department> {
  return request<Department>('/api/v1/departments', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify(input),
  })
}

export function updateDepartment(departmentId: string, input: DepartmentInput): Promise<Department> {
  return request<Department>(`/api/v1/departments/${departmentId}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify(input),
  })
}

export function changeDepartmentStatus(departmentId: string, status: Department['status']): Promise<Department> {
  return request<Department>(`/api/v1/departments/${departmentId}/status`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify({ status }),
  })
}

export function assignUserDepartment(userId: string, departmentId?: string): Promise<{ userId: string; departmentId: string }> {
  return request(`/api/v1/users/${userId}/department`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify({ departmentId: departmentId ?? '' }),
  })
}
