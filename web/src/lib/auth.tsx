import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react'
import { ApiError, get, onUnauthorized } from './api'
import type { AuthState, Me } from './types'
import { clearBrowserPush } from './push'
import { flush, loadOutbox, resetOutbox } from './outbox'
import { warmCache } from './offline'
import { lastUser, openVault, rememberUser, wipeVaults } from './vault'

interface AuthInfo {
  state: AuthState | 'loading'
  email?: string
  methods?: string[]
  /** Sin conexión: se muestra lo guardado en este dispositivo de la última persona que entró. */
  offline?: boolean
  refresh: () => Promise<void>
}

const Ctx = createContext<AuthInfo>({ state: 'loading', refresh: async () => {} })

/** Abre el almacén cifrado de esta persona y su cola de cambios pendientes. */
async function unlock(uid: string): Promise<boolean> {
  if (!(await openVault(uid))) return false
  await loadOutbox().catch(() => {})
  return true
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [info, setInfo] = useState<Omit<AuthInfo, 'refresh'>>({ state: 'loading' })
  const refresh = useCallback(async () => {
    try {
      const r = await get<{ state: AuthState; email?: string; methods?: string[] }>('/api/auth/state')
      if (r.state === 'full') {
        const me = await get<Me>('/api/me').catch(() => null)
        if (me) {
          rememberUser(me.id)
          if (await unlock(me.id)) {
            flush().catch(() => {})
            warmCache().catch(() => {})
          }
        }
      }
      setInfo(r)
    } catch (e) {
      const uid = e instanceof ApiError && e.status === 0 ? lastUser() : null
      if (uid && (await unlock(uid))) setInfo({ state: 'full', offline: true })
      else setInfo({ state: 'none' })
    }
  }, [])
  useEffect(() => {
    refresh()
    onUnauthorized(() => refresh())
    const back = () => {
      if (info.offline) refresh()
    }
    window.addEventListener('online', back)
    return () => {
      onUnauthorized(null)
      window.removeEventListener('online', back)
    }
  }, [refresh, info.offline])
  return <Ctx.Provider value={{ ...info, refresh }}>{children}</Ctx.Provider>
}

export const useAuth = () => useContext(Ctx)

/** Al cerrar sesión se borra todo lo que el navegador pudiera tener: caché, almacén cifrado y cola pendiente. */
export async function wipeLocalData() {
  await clearBrowserPush().catch(() => {})
  resetOutbox()
  try {
    await wipeVaults()
    if ('caches' in window) {
      const keys = await caches.keys()
      await Promise.all(keys.map((k) => caches.delete(k)))
    }
    sessionStorage.clear()
  } catch {
    /* nada que limpiar */
  }
}
