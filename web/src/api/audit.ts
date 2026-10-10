import { request } from './client'

export interface AuditEvent {
  id: string
  actorId?: string
  actorUsername?: string
  actorDisplayName?: string
  requestId?: string
  action: string
  resourceType: string
  resourceId?: string
  result: 'success' | 'denied' | 'failed'
  sourceIp?: string
  detail: Record<string, unknown>
  createdAt: string
}

export interface AuditEventList {
  items: AuditEvent[]
  total: number
  page: number
  pageSize: number
}

export interface AuditEventListQuery {
  search?: string
  action?: string
  result?: string
  startedAt?: string
  endedAt?: string
  page: number
  pageSize: number
}

export function listAuditEvents(query: AuditEventListQuery): Promise<AuditEventList> {
  const search = new URLSearchParams({ page: String(query.page), pageSize: String(query.pageSize) })
  if (query.search) search.set('search', query.search)
  if (query.action) search.set('action', query.action)
  if (query.result) search.set('result', query.result)
  if (query.startedAt) search.set('startedAt', query.startedAt)
  if (query.endedAt) search.set('endedAt', query.endedAt)
  return request<AuditEventList>(`/api/v1/audit-events?${search}`)
}

export function getAuditEvent(id: string): Promise<AuditEvent> {
  return request<AuditEvent>(`/api/v1/audit-events/${id}`)
}
