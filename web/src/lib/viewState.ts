import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { currentUser } from './vault'

const monthRE = /^(?!0000)\d{4}-(0[1-9]|1[0-2])$/
const idRE = /^[A-Za-z0-9_-]{1,64}$/
const searches = new Map<string, string>() // Texto sensible: solo memoria, nunca URL/sessionStorage.
const positions = new Map<string, number>()
const prefix = 'pulse.view.'
let sessionOwner: string | null = null
const owner = () => sessionOwner ?? currentUser() ?? 'session'
const keyFor = (view: string) => `${prefix}${owner()}.${view}`

function stored(view: string): URLSearchParams {
  try { return new URLSearchParams(sessionStorage.getItem(keyFor(view)) ?? '') } catch { return new URLSearchParams() }
}
function save(view: string, p: URLSearchParams) {
  try { sessionStorage.setItem(keyFor(view), p.toString()) } catch { /* preferencia opcional */ }
}
export function clearViewContext() {
  sessionOwner = null
  searches.clear()
  positions.clear()
  try {
    Object.keys(sessionStorage).filter((k) => k.startsWith(prefix) || k === 'pulse.reportes').forEach((k) => sessionStorage.removeItem(k))
  } catch { /* almacenamiento bloqueado */ }
}
export function setViewOwner(uid: string) {
  try {
    if (sessionStorage.getItem('pulse.view.owner') !== uid) clearViewContext()
    sessionStorage.setItem('pulse.view.owner', uid)
  } catch { if (sessionOwner !== uid) clearViewContext() }
  sessionOwner = uid
}

export interface MovementView { month: string; category: string; account: string; limit: number }
export function movementView(p: URLSearchParams, current: string): MovementView {
  const limit = Number(p.get('limit') ?? 50)
  return {
    month: monthRE.test(p.get('month') ?? '') ? p.get('month')! : current,
    category: idRE.test(p.get('category') ?? '') ? p.get('category')! : '',
    account: idRE.test(p.get('account') ?? '') ? p.get('account')! : '',
    limit: Number.isInteger(limit) && limit >= 50 && limit <= 5000 ? limit : 50,
  }
}
function movementParams(v: MovementView) {
  const p = new URLSearchParams({ month: v.month })
  if (v.category) p.set('category', v.category)
  if (v.account) p.set('account', v.account)
  if (v.limit > 50) p.set('limit', String(v.limit))
  return p
}

export function useMovementView(current: string) {
  const [params, setParams] = useSearchParams()
  const explicit = ['month', 'category', 'account', 'limit'].some((k) => params.has(k))
  const value = movementView(explicit ? params : stored('movements'), current)
  const canonical = movementParams(value).toString()
  const [q, setQ] = useState(() => searches.get(keyFor('search')) ?? '')
  const update = (patch: Partial<MovementView>, push = false) => {
    const next = movementParams({ ...value, ...patch })
    save('movements', next)
    setParams(next, { replace: !push })
  }
  useEffect(() => {
    save('movements', new URLSearchParams(canonical))
    // No consumir el parámetro de apertura de AppShell antes de que se procese.
    if (!params.has('nuevo') && params.toString() !== canonical) setParams(canonical, { replace: true })
  }, [canonical, params, setParams])
  return { ...value, q, update, setQ: (text: string) => { searches.set(keyFor('search'), text); setQ(text); if (value.limit !== 50) update({ limit: 50 }) } }
}

export function useViewMonth(view: string, current: string): [string, (month: string) => void] {
  const [params, setParams] = useSearchParams()
  const candidate = params.get('month') ?? stored(view).get('month') ?? current
  const month = monthRE.test(candidate) ? candidate : current
  useEffect(() => {
    save(view, new URLSearchParams({ month }))
    if (!params.has('nuevo') && params.get('month') !== month) setParams({ month }, { replace: true })
  }, [month, params, setParams, view])
  return [month, (next) => setParams({ month: next })]
}

/** Restaura solo cuando existen las filas; el listener captura la posición antes de salir de la vista. */
export function useViewScroll(view: string, context: string, ready: boolean) {
  const key = `${keyFor(view)}|${context}`
  const restored = useRef('')
  useLayoutEffect(() => {
    if (!ready || restored.current === key) return
    restored.current = key
    const y = positions.get(key) ?? 0
    const previous = document.documentElement.style.scrollBehavior
    document.documentElement.style.scrollBehavior = 'auto'
    window.scrollTo({ top: y, behavior: 'instant' })
    // En móvil el navegador puede ajustar el scroll al retirar el foco de la vista anterior.
    const frame = requestAnimationFrame(() => window.scrollTo({ top: y, behavior: 'instant' }))
    document.documentElement.style.scrollBehavior = previous
    return () => cancelAnimationFrame(frame)
  }, [key, ready])
  useLayoutEffect(() => {
    const record = () => { if (restored.current === key) positions.set(key, window.scrollY) }
    window.addEventListener('scroll', record, { passive: true })
    document.addEventListener('click', record, true)
    window.addEventListener('popstate', record)
    return () => {
      window.removeEventListener('scroll', record)
      document.removeEventListener('click', record, true)
      window.removeEventListener('popstate', record)
    }
  }, [key])
}

export function useDebounced<T>(value: T, delay = 250): T {
  const [settled, setSettled] = useState(value)
  useEffect(() => { const t = setTimeout(() => setSettled(value), delay); return () => clearTimeout(t) }, [value, delay])
  return settled
}
