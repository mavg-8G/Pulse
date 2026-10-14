import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react'
import { get, onUnauthorized } from './api'
import type { AuthState } from './types'
import { clearBrowserPush } from './push'

interface AuthInfo {
  state: AuthState | 'loading'
  email?: string
  methods?: string[]
  refresh: () => Promise<void>
}

const Ctx = createContext<AuthInfo>({ state: 'loading', refresh: async () => {} })

export function AuthProvider({ children }: { children: ReactNode }) {
  const [info, setInfo] = useState<Omit<AuthInfo, 'refresh'>>({ state: 'loading' })
  const refresh = useCallback(async () => {
    try {
      const r = await get<{ state: AuthState; email?: string; methods?: string[] }>('/api/auth/state')
      setInfo(r)
    } catch {
      setInfo({ state: 'none' })
    }
  }, [])
  useEffect(() => {
    refresh()
    onUnauthorized(() => refresh())
    return () => onUnauthorized(null)
  }, [refresh])
  return <Ctx.Provider value={{ ...info, refresh }}>{children}</Ctx.Provider>
}

export const useAuth = () => useContext(Ctx)

/** Al cerrar sesión se borra todo lo que el navegador pudiera tener en caché. */
export async function wipeLocalData() {
  await clearBrowserPush().catch(() => {})
  try {
    if ('caches' in window) {
      const keys = await caches.keys()
      await Promise.all(keys.map((k) => caches.delete(k)))
    }
    if ('indexedDB' in window && 'databases' in indexedDB) {
      const dbs = await indexedDB.databases()
      dbs.forEach((d) => d.name && indexedDB.deleteDatabase(d.name))
    }
    sessionStorage.clear()
  } catch {
    /* nada que limpiar */
  }
}
