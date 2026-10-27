export interface ModuleStatus {
  key: string
  name: string
  status: 'building' | 'planned' | 'ready'
}

export interface SystemBootstrap {
  name: string
  environment: string
  phase: string
  services: Record<string, boolean>
  modules: ModuleStatus[]
}

interface ApiResponse<T> {
  data: T
}

export async function getSystemBootstrap(): Promise<SystemBootstrap> {
  const response = await fetch('/api/v1/system/bootstrap')
  if (!response.ok) {
    throw new Error('无法连接控制面服务')
  }
  const body = (await response.json()) as ApiResponse<SystemBootstrap>
  return body.data
}

export async function getSystemReadiness(): Promise<boolean> {
  try {
    const response = await fetch('/readyz', { credentials: 'same-origin' })
    return response.ok
  } catch {
    return false
  }
}

