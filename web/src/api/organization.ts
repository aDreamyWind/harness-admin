import { csrfHeaders, request } from './client'

export interface Organization {
  id: string
  code: string
  name: string
  status: 'active' | 'disabled'
  createdAt: string
  updatedAt: string
}

export interface UpdateOrganizationInput {
  name: string
}

export function getOrganization(): Promise<Organization> {
  return request<Organization>('/api/v1/organization')
}

export function updateOrganization(input: UpdateOrganizationInput): Promise<Organization> {
  return request<Organization>('/api/v1/organization', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify(input),
  })
}
