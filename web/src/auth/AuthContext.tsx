import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'

import {
  getCurrentUser,
  login as requestLogin,
  logout as requestLogout,
  type AuthUser,
  type LoginInput,
} from '../api/auth'

interface AuthContextValue {
  user: AuthUser | null
  isLoading: boolean
  startupError: string
  login: (input: LoginInput) => Promise<void>
  logout: () => Promise<void>
}

const AuthContext = createContext<AuthContextValue | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<AuthUser | null>(null)
  const [isLoading, setIsLoading] = useState(true)
  const [startupError, setStartupError] = useState('')

  useEffect(() => {
    let active = true
    getCurrentUser()
      .then((currentUser) => {
        if (active) {
          setUser(currentUser)
          setStartupError('')
        }
      })
      .catch((error: unknown) => {
        if (active) {
          setStartupError(error instanceof Error ? error.message : '无法检查登录状态')
        }
      })
      .finally(() => {
        if (active) {
          setIsLoading(false)
        }
      })
    return () => {
      active = false
    }
  }, [])

  const value = useMemo<AuthContextValue>(
    () => ({
      user,
      isLoading,
      startupError,
      login: async (input) => {
        const authenticatedUser = await requestLogin(input)
        setUser(authenticatedUser)
        setStartupError('')
      },
      logout: async () => {
        await requestLogout()
        setUser(null)
      },
    }),
    [isLoading, startupError, user],
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth(): AuthContextValue {
  const context = useContext(AuthContext)
  if (!context) {
    throw new Error('useAuth 必须在 AuthProvider 内使用')
  }
  return context
}
