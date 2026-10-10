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

const csrfCookieName = 'harness_admin_csrf'
const platformCSRFCookieName = 'harness_platform_csrf'

export async function request<T>(path: string, init?: RequestInit): Promise<T> {
  let response: Response
  try {
    response = await fetch(path, {
      credentials: 'same-origin',
      ...init,
      headers: {
        Accept: 'application/json',
        ...init?.headers,
      },
    })
  } catch {
    throw new ApiError('无法连接控制面服务，请检查服务是否已启动', 0, 'network_error')
  }

  const body = (await response.json().catch(() => ({}))) as ApiResponse<T>
  if (!response.ok) {
    throw new ApiError(
      body.error?.message ?? '请求处理失败',
      response.status,
      body.error?.code ?? 'unknown_error',
      body.error?.requestId,
    )
  }
  if (body.data === undefined) {
    throw new ApiError('服务返回了无效数据', response.status, 'invalid_response')
  }
  return body.data
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
    throw new ApiError('登录安全凭据已失效，请刷新页面后重试', 403, 'missing_csrf')
  }
  return { 'X-CSRF-Token': token }
}

export function platformCSRFHeaders(): Record<string, string> {
  const token = getCookie(platformCSRFCookieName)
  if (!token) {
    throw new ApiError('平台登录安全凭据已失效，请刷新页面后重试', 403, 'missing_csrf')
  }
  return { 'X-CSRF-Token': token }
}
