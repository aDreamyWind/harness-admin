import { useQueryClient } from '@tanstack/react-query'
import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'

import {
  getCurrentUser,
  login as requestLogin,
  logout as requestLogout,
  updateProfile as requestUpdateProfile,
  type AuthUser,
  type LoginInput,
  type UpdateProfileInput,
} from '../api/auth'
import { onSessionExpired } from '../api/client'

interface AuthContextValue {
  user: AuthUser | null
  isLoading: boolean
  startupError: string
  hasPermission: (permissionCode: string) => boolean
  refreshUser: () => Promise<void>
  login: (input: LoginInput) => Promise<void>
  logout: () => Promise<void>
  updateProfile: (input: UpdateProfileInput) => Promise<void>
}

const AuthContext = createContext<AuthContextValue | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient()
  const [user, setUser] = useState<AuthUser | null>(null)
  const [isLoading, setIsLoading] = useState(true)
  const [startupError, setStartupError] = useState('')

  useEffect(() => onSessionExpired((scope) => {
    if (scope !== 'organization' || !user) return
    queryClient.clear()
    setUser(null)
    setStartupError('登录状态已失效，请重新登录')
  }), [queryClient, user])

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
      hasPermission: (permissionCode) => (user?.permissionCodes ?? []).includes(permissionCode),
      refreshUser: async () => {
        const currentUser = await getCurrentUser()
        setUser(currentUser)
      },
      login: async (input) => {
        const authenticatedUser = await requestLogin(input)
        queryClient.clear()
        setUser(authenticatedUser)
        setStartupError('')
      },
      logout: async () => {
        await requestLogout()
        queryClient.clear()
        setUser(null)
        setStartupError('')
      },
      updateProfile: async (input) => {
        const updatedUser = await requestUpdateProfile(input)
        setUser(updatedUser)
      },
    }),
    [isLoading, queryClient, startupError, user],
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
