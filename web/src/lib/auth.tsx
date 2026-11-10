import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { clearViewContext, setViewOwner } from './viewState'
import { ApiError, get, onUnauthorized } from './api'
import type { AuthState, Me } from './types'
import { clearBrowserPush } from './push'
import { flush, loadOutbox, resetOutbox } from './outbox'
import { warmCache } from './offline'
import { closeVault, currentUser, openVault, wipeVaults } from './vault'

interface AuthInfo {
  state: AuthState | 'loading' | 'offline'
  userId?: string
  email?: string
  methods?: string[]
  /** Sin conexión: se muestra lo guardado en este dispositivo de la última persona que entró. */
  offline?: boolean
  refresh: () => Promise<void>
}

const Ctx = createContext<AuthInfo>({ state: 'loading', refresh: async () => {} })

/** Abre el almacén cifrado de esta persona y su cola de cambios pendientes. */
async function unlock(uid: string, valid: () => boolean): Promise<boolean> {
  const { key } = await get<{ key: string }>('/api/security/vault-key')
  if (!valid()) return false
  if (!(await openVault(uid, key))) return false
  if (!valid()) return false
  await loadOutbox().catch(() => {})
  return true
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const qc = useQueryClient()
  const identity = useRef<string | null>(null)
  const revision = useRef(0)
  const [info, setInfo] = useState<Omit<AuthInfo, 'refresh'>>({ state: 'loading' })
  const refresh = useCallback(async () => {
    const request = ++revision.current
    const valid = () => request === revision.current
    try {
      const readState = () => get<{ state: AuthState; email?: string; methods?: string[] }>('/api/auth/state')
      const r = await readState().catch((e) => {
        // Un solo reintento de lectura ante una interrupción transitoria en línea.
        if (valid() && e instanceof ApiError && e.status === 0 && navigator.onLine !== false) return readState()
        throw e
      })
      if (!valid()) return
      if (r.state === 'full') {
        const me = await get<Me>('/api/me')
        if (!valid()) return
        if (me) {
          if (identity.current !== me.id) { setInfo({ state: 'loading' }); qc.clear(); resetOutbox() }
          identity.current = me.id
          setViewOwner(me.id)
          qc.setQueryData(['me'], me)
          if (await unlock(me.id, valid)) {
            flush().catch(() => {})
            warmCache(qc).catch(() => {})
          }
        } else throw new ApiError(0, 'network', 'No se pudo verificar la sesión.')
      } else {
        identity.current = null
        qc.clear()
        resetOutbox()
        closeVault()
        clearViewContext()
      }
      if (!valid()) return
      setInfo({ ...r, userId: identity.current ?? undefined })
    } catch (e) {
      if (!valid()) return
      // Solo una pestaña ya desbloqueada puede seguir offline. Nunca abrir por un id en disco.
      const uid = e instanceof ApiError && e.status === 0 && identity.current === currentUser() ? currentUser() : null
      if (uid) { setViewOwner(uid); setInfo({ state: 'offline', userId: uid, offline: true }) }
      else { identity.current = null; qc.clear(); resetOutbox(); closeVault(); clearViewContext(); setInfo({ state: 'none', offline: e instanceof ApiError && e.status === 0 }) }
    }
  }, [qc])
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
  useEffect(() => {
    let lastActivity = Date.now()
    const touch = () => { lastActivity = Date.now() }
    const clear = () => {
      revision.current++
      closeVault()
      identity.current = null
      qc.clear()
      resetOutbox()
      clearViewContext()
      setInfo({ state: 'none', offline: navigator.onLine === false })
    }
    const lock = () => {
      if (Date.now() - lastActivity >= 15 * 60_000) clear()
    }
    const check = window.setInterval(lock, 15_000)
    const visible = () => { if (document.visibilityState === 'visible') lock() }
    window.addEventListener('pointerdown', touch)
    window.addEventListener('keydown', touch)
    document.addEventListener('visibilitychange', visible)
    window.addEventListener('pulse-vault-lock', clear)
    const channel = typeof BroadcastChannel !== 'undefined' ? new BroadcastChannel('pulse-security') : null
    if (channel) channel.onmessage = (e) => { if (e.data === 'lock') clear() }
    return () => {
      window.clearInterval(check)
      window.removeEventListener('pointerdown', touch)
      window.removeEventListener('keydown', touch)
      document.removeEventListener('visibilitychange', visible)
      window.removeEventListener('pulse-vault-lock', clear)
      channel?.close()
    }
  }, [qc])
  return <Ctx.Provider value={{ ...info, refresh }}>{children}</Ctx.Provider>
}

export const useAuth = () => useContext(Ctx)

/** Al cerrar sesión se borra todo lo que el navegador pudiera tener: caché, almacén cifrado y cola pendiente. */
export async function wipeLocalData() {
  if (typeof BroadcastChannel !== 'undefined') {
    const channel = new BroadcastChannel('pulse-security')
    channel.postMessage('lock')
    channel.close()
  }
  clearViewContext()
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
