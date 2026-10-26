interface ApiErrorBody {
  code: string
  message: string
  requestId?: string
}

interface ApiResponse<T> {
  data?: T
  error?: ApiErrorBody
}

export class ApiError extends Error {
  constructor(
    message: string,
    public readonly status: number,
    public readonly code: string,
    public readonly requestId?: string,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

export type SessionScope = 'organization' | 'platform'

const sessionExpiredListeners = new Set<(scope: SessionScope) => void>()

export function onSessionExpired(listener: (scope: SessionScope) => void): () => void {
  sessionExpiredListeners.add(listener)
  return () => { sessionExpiredListeners.delete(listener) }
}

function notifySessionExpired(path: string, status: number) {
  if (status !== 401 || !path.startsWith('/api/v1/')) return
  const pathname = path.split('?')[0]
  if (pathname === '/api/v1/auth/login' || pathname === '/api/v1/platform/auth/login') return
  const scope: SessionScope = pathname.startsWith('/api/v1/platform/') ? 'platform' : 'organization'
  sessionExpiredListeners.forEach((listener) => listener(scope))
}

const csrfCookieName = 'harness_admin_csrf'
const platformCSRFCookieName = 'harness_platform_csrf'

async function fetchApi(path: string, init?: RequestInit, accept = 'application/json'): Promise<Response> {
  let response: Response
  try {
    response = await fetch(path, {
      credentials: 'same-origin',
      ...init,
      headers: {
        Accept: accept,
        ...init?.headers,
      },
    })
  } catch {
    throw new ApiError('无法连接控制面服务，请检查服务是否已启动', 0, 'network_error')
  }
  return response
}

async function responseError(path: string, response: Response, fallbackMessage: string): Promise<ApiError> {
  const body = (await response.json().catch(() => ({}))) as ApiResponse<unknown>
  notifySessionExpired(path, response.status)
  return new ApiError(body.error?.message ?? fallbackMessage, response.status,
    body.error?.code ?? 'unknown_error', body.error?.requestId)
}

export async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetchApi(path, init)
  if (!response.ok) throw await responseError(path, response, '请求处理失败')
  const body = (await response.json().catch(() => ({}))) as ApiResponse<T>
  if (body.data === undefined) {
    throw new ApiError('服务返回了无效数据', response.status, 'invalid_response')
  }
  return body.data
}

export async function requestBlob(path: string, accept: string, fallbackMessage: string): Promise<Blob> {
  const response = await fetchApi(path, undefined, accept)
  if (!response.ok) throw await responseError(path, response, fallbackMessage)
  return response.blob()
}

function getCookie(name: string): string {
  const prefix = `${encodeURIComponent(name)}=`
  const item = document.cookie
    .split(';')
    .map((part) => part.trim())
    .find((part) => part.startsWith(prefix))
  return item ? decodeURIComponent(item.slice(prefix.length)) : ''
}

export function csrfHeaders(): Record<string, string> {
  const token = getCookie(csrfCookieName)
  if (!token) {
    notifySessionExpired('/api/v1/auth/me', 401)
    throw new ApiError('登录状态已失效，请重新登录', 401, 'unauthorized')
  }
  return { 'X-CSRF-Token': token }
}

export function platformCSRFHeaders(): Record<string, string> {
  const token = getCookie(platformCSRFCookieName)
  if (!token) {
    notifySessionExpired('/api/v1/platform/auth/me', 401)
    throw new ApiError('平台登录状态已失效，请重新登录', 401, 'unauthorized')
  }
  return { 'X-CSRF-Token': token }
}
