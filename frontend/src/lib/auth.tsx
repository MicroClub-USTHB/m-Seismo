import { createContext, use, useCallback, useMemo, useState, type ReactNode } from 'react'
import { api, setAuthToken, type Owner, type SessionResponse } from './api'

const STORAGE_KEY = 'm-seismo.session'

interface StoredSession {
  owner: Owner
  token: string
}

function readStoredSession(): StoredSession | null {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    return raw ? (JSON.parse(raw) as StoredSession) : null
  } catch {
    return null
  }
}

interface AuthContextValue {
  owner: Owner | null
  isAuthenticated: boolean
  login: (email: string, password: string) => Promise<void>
  logout: () => void
}

const AuthContext = createContext<AuthContextValue | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [owner, setOwner] = useState<Owner | null>(() => {
    const stored = readStoredSession()
    if (stored) setAuthToken(stored.token)
    return stored?.owner ?? null
  })

  const login = useCallback(async (email: string, password: string) => {
    const session = await api.post<SessionResponse>('/auth/login', { email, password })
    setAuthToken(session.token.access_token)
    localStorage.setItem(STORAGE_KEY, JSON.stringify({ owner: session.owner, token: session.token.access_token }))
    setOwner(session.owner)
  }, [])

  const logout = useCallback(() => {
    setAuthToken(null)
    localStorage.removeItem(STORAGE_KEY)
    setOwner(null)
  }, [])

  const value = useMemo(
    () => ({ owner, isAuthenticated: owner !== null, login, logout }),
    [owner, login, logout],
  )

  return <AuthContext value={value}>{children}</AuthContext>
}

export function useAuth(): AuthContextValue {
  const ctx = use(AuthContext)
  if (!ctx) throw new Error('useAuth must be used within AuthProvider')
  return ctx
}

/** For route beforeLoad guards, which run before React renders: reads the
 * same storage AuthProvider does, without needing the context. */
export function getStoredOwner(): Owner | null {
  const stored = readStoredSession()
  if (stored) setAuthToken(stored.token)
  return stored?.owner ?? null
}
