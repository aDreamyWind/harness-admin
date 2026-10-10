import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'

import {
  getCurrentPlatformAdmin,
  platformLogin,
  platformLogout,
  type PlatformAdmin,
  type PlatformLoginInput,
} from '../api/platform'

interface PlatformAuthContextValue {
  admin: PlatformAdmin | null
  isLoading: boolean
  startupError: string
  login: (input: PlatformLoginInput) => Promise<void>
  logout: () => Promise<void>
}

const PlatformAuthContext = createContext<PlatformAuthContextValue | null>(null)

export function PlatformAuthProvider({ children }: { children: ReactNode }) {
  const [admin, setAdmin] = useState<PlatformAdmin | null>(null)
  const [isLoading, setIsLoading] = useState(true)
  const [startupError, setStartupError] = useState('')

  useEffect(() => {
    let active = true
    getCurrentPlatformAdmin()
      .then((current) => {
        if (!active) return
        setAdmin(current)
        if (current && window.location.pathname !== '/platform') window.history.replaceState(null, '', '/platform')
      })
      .catch((error: unknown) => { if (active) setStartupError(error instanceof Error ? error.message : '无法检查平台登录状态') })
      .finally(() => { if (active) setIsLoading(false) })
    return () => { active = false }
  }, [])

  const value = useMemo<PlatformAuthContextValue>(() => ({
    admin,
    isLoading,
    startupError,
    login: async (input) => {
      setAdmin(await platformLogin(input))
      setStartupError('')
      window.history.replaceState(null, '', '/platform')
    },
    logout: async () => {
      await platformLogout()
      setAdmin(null)
      window.history.replaceState(null, '', '/platform/login')
    },
  }), [admin, isLoading, startupError])

  return <PlatformAuthContext.Provider value={value}>{children}</PlatformAuthContext.Provider>
}

export function usePlatformAuth(): PlatformAuthContextValue {
  const context = useContext(PlatformAuthContext)
  if (!context) throw new Error('usePlatformAuth 必须在 PlatformAuthProvider 内使用')
  return context
}
