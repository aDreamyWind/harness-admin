import { csrfHeaders, request } from './client'

export type PolicySubjectType = 'department' | 'user'
export type ModelScopeMode = 'inherit' | 'all' | 'selected' | 'none'
export type MonthlyTokenMode = 'shared' | 'per_user'

export interface ModelPolicy {
  subjectType: PolicySubjectType
  subjectId: string
  subjectName: string
  scopeMode: ModelScopeMode
  allowedModelCodes: string[] | null
  requestsPerMinute: number
  monthlyTokenLimit: number
  monthlyTokenMode: MonthlyTokenMode
  monthlyTokenUsed: number
  createdAt: string
  updatedAt: string
}

export interface SaveModelPolicyInput {
  scopeMode: ModelScopeMode
  allowedModelCodes: string[]
  requestsPerMinute: number
  monthlyTokenLimit: number
  monthlyTokenMode: MonthlyTokenMode
}

export function saveDepartmentModelPolicies(subjectIds: string[], input: SaveModelPolicyInput) {
  return request<ModelPolicy[]>('/api/v1/model-policies/departments', {
    method: 'PUT', headers: { 'Content-Type': 'application/json', ...csrfHeaders() },
    body: JSON.stringify({ ...input, subjectIds }),
  })
}

export const listModelPolicies = () => request<ModelPolicy[]>('/api/v1/model-policies')

export function saveModelPolicy(subjectType: PolicySubjectType, subjectId: string, input: SaveModelPolicyInput) {
  return request<ModelPolicy>(`/api/v1/model-policies/${subjectType}/${subjectId}`, {
    method: 'PUT', headers: { 'Content-Type': 'application/json', ...csrfHeaders() }, body: JSON.stringify(input),
  })
}

export function deleteModelPolicy(subjectType: PolicySubjectType, subjectId: string) {
  return request<{ deleted: boolean }>(`/api/v1/model-policies/${subjectType}/${subjectId}`, {
    method: 'DELETE', headers: csrfHeaders(),
  })
}
