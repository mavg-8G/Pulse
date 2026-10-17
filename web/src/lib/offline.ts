// Lectura sin conexión y cambios pendientes en las listas (fase 10).
import { useSyncExternalStore } from 'react'
import type { QueryClient } from '@tanstack/react-query'
import { ApiError, get } from './api'
import { monthOf } from './dates'
import { snapshot, subscribe, type Op } from './outbox'
import type { Recurring, Transaction, TxPage } from './types'
import { currentUser, getCache, putCache } from './vault'

// Solo se guarda lo que hace falta para Inicio, Movimientos recientes y Plan.
const CACHEABLE = [/^\/api\/me$/, /^\/api\/accounts(\?archived=1)?$/, /^\/api\/categories$/, /^\/api\/budgets$/, /^\/api\/budgets\/summary\?month=\d{4}-\d{2}$/, /^\/api\/budgets\/breakdown\?month=\d{4}-\d{2}$/,
  /^\/api\/dashboard\?month=\d{4}-\d{2}$/, /^\/api\/recurring$/]

export function cacheable(path: string): boolean {
  if (CACHEABLE.some((r) => r.test(path))) return true
  if (!path.startsWith('/api/transactions?')) return false
  const p = new URLSearchParams(path.slice('/api/transactions?'.length))
  return !p.get('q') && !Number(p.get('offset') ?? 0) // búsquedas y páginas siguientes no
}

// ---------- estado de conexión ----------
type NetState = { offline: boolean; cachedAt: number | null }
let net: NetState = { offline: typeof navigator !== 'undefined' && navigator.onLine === false, cachedAt: null }
const netListeners = new Set<() => void>()
function setNet(n: NetState) {
  if (n.offline === net.offline && n.cachedAt === net.cachedAt) return
  net = n
  netListeners.forEach((l) => l())
}
if (typeof window !== 'undefined') {
  window.addEventListener('offline', () => setNet({ offline: true, cachedAt: net.cachedAt }))
  window.addEventListener('online', () => setNet({ offline: false, cachedAt: null }))
}
export const useNetwork = () =>
  useSyncExternalStore(
    (l) => {
      netListeners.add(l)
      return () => netListeners.delete(l)
    },
    () => net,
  )

/** GET que guarda la respuesta cifrada en el dispositivo y, sin red, devuelve la última guardada. */
export async function cachedGet<T>(path: string, signal?: AbortSignal): Promise<T> {
  const owner = currentUser()
  try {
    const v = await get<T>(path, signal)
    if (owner !== currentUser()) throw new DOMException('La sesión cambió', 'AbortError')
    if (cacheable(path)) putCache(path, v).catch(() => {})
    if (net.offline) setNet({ offline: false, cachedAt: null })
    return v
  } catch (e) {
    if (owner !== currentUser()) throw new DOMException('La sesión cambió', 'AbortError')
    if (e instanceof ApiError && e.status === 0 && cacheable(path)) {
      const c = await getCache<T>(path).catch(() => null)
      if (c) {
        setNet({ offline: true, cachedAt: Math.min(c.at, net.cachedAt ?? c.at) })
        return c.value
      }
    }
    throw e
  }
}

// ---------- cambios pendientes ----------
export const usePendingOps = () => useSyncExternalStore(subscribe, snapshot)

/** Convierte "AAAA-MM-DDTHH:MM" en la zona de la persona a ISO (UTC). */
export function localInputToISO(local: string, tz: string): string {
  const m = /^(\d{4})-(\d{2})-(\d{2})(?:T(\d{2}):(\d{2}))?/.exec(local)
  if (!m) return local
  const guess = Date.UTC(+m[1], +m[2] - 1, +m[3], +(m[4] ?? 12), +(m[5] ?? 0))
  const offsetAt = (t: number) => {
    const p = Object.fromEntries(
      new Intl.DateTimeFormat('en-US', { timeZone: tz, hourCycle: 'h23', year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
        .formatToParts(new Date(t))
        .map((x) => [x.type, x.value]),
    )
    return Date.UTC(+p.year, +p.month - 1, +p.day, +p.hour, +p.minute) - t
  }
  let t = guess - offsetAt(guess)
  t = guess - offsetAt(t) // segunda pasada: cerca de un cambio de horario
  return new Date(t).toISOString()
}

function applyTxBody(t: Transaction, body: Record<string, unknown> | undefined, tz: string): Transaction {
  const out = { ...t, pending: true }
  if (!body) return out
  for (const k of ['account_id', 'category_id', 'amount_cents', 'kind', 'merchant', 'notes', 'description', 'status'] as const) {
    if (body[k] !== undefined) (out as Record<string, unknown>)[k] = body[k]
  }
  if (typeof body.occurred_at === 'string') out.occurred_at = body.occurred_at.endsWith('Z') || /[+-]\d{2}:\d{2}$/.test(body.occurred_at) ? body.occurred_at : localInputToISO(body.occurred_at, tz)
  return out
}

const signed = (t: Pick<Transaction, 'kind' | 'amount_cents'>) => (t.kind === 'refund' ? -t.amount_cents : t.amount_cents)

/** Muestra en la lista lo hecho sin conexión: gastos nuevos, ediciones y borrados pendientes. */
export function overlayTransactions(page: TxPage, ops: Op[], f: { month?: string; category_id?: string; account_id?: string; q?: string; status?: string }, tz: string): TxPage {
  const txOps = ops.filter((o) => o.kind.startsWith('tx.'))
  if (txOps.length === 0) return page
  let items = [...page.items]
  let total = page.total
  let sum = page.sum_cents
  for (const op of txOps) {
    const i = items.findIndex((t) => t.id === op.entity)
    if (op.kind === 'tx.delete' && op.state === 'pending') {
      if (i >= 0) {
        sum -= signed(items[i])
        items.splice(i, 1)
        total--
      }
    } else if (op.kind === 'tx.update' || (op.kind === 'tx.delete' && op.state === 'conflict')) {
      if (i >= 0) {
        const next = applyTxBody(items[i], op.kind === 'tx.update' ? op.body : undefined, tz)
        sum += signed(next) - signed(items[i])
        items[i] = next
      }
    } else if (op.kind === 'tx.create' && i < 0 && !f.q && !f.status) {
      const t = applyTxBody({ id: op.entity, account_id: '', category_id: '', amount_cents: 0, currency: 'USD', occurred_at: new Date(op.at).toISOString(), kind: 'purchase',
        merchant: '', description: '', notes: '', source: 'manual', status: 'confirmed', version: 0 }, op.body, tz)
      if (f.month && monthOf(new Date(t.occurred_at), tz) !== f.month) continue
      if (f.account_id && t.account_id !== f.account_id) continue
      if (f.category_id && t.category_id !== f.category_id) continue
      items.push(t)
      total++
      sum += signed(t)
    }
  }
  items = items.sort((a, b) => (a.occurred_at < b.occurred_at ? 1 : a.occurred_at > b.occurred_at ? -1 : 0))
  return { ...page, items, total, sum_cents: sum }
}

/** Lo mismo para los gastos fijos. */
export function overlayRecurring(list: Recurring[], ops: Op[]): Recurring[] {
  let out = [...list]
  for (const op of ops) {
    if (!op.kind.startsWith('rec.')) continue
    const i = out.findIndex((r) => r.id === op.entity)
    if (op.kind === 'rec.delete' && op.state === 'pending') out = out.filter((r) => r.id !== op.entity)
    else if (op.kind === 'rec.update' && i >= 0) out[i] = { ...out[i], ...(op.body as Partial<Recurring>), pending: true }
    else if (op.kind === 'rec.create' && i < 0) {
      const b = op.body as Partial<Recurring> & { start_date?: string }
      out.push({ id: op.entity, account_id: b.account_id ?? '', category_id: b.category_id ?? '', amount_cents: b.amount_cents ?? 0, merchant: b.merchant ?? '',
        day: b.day ?? Number(b.start_date?.slice(8, 10) ?? 1), next_date: b.start_date ?? '', version: 0, pending: true })
    }
  }
  return out
}

/** Al entrar con conexión se guarda lo básico (cuentas, categorías, gastos fijos) para poder registrar sin red. */
export async function warmCache(client?: QueryClient) {
  const owner = currentUser()
  const reads = [
    [['me'], '/api/me'], [['accounts', false], '/api/accounts'], [['accounts', true], '/api/accounts?archived=1'],
    [['categories'], '/api/categories'], [['recurring'], '/api/recurring'], [['budgets'], '/api/budgets'],
  ] as const
  for (const [queryKey, path] of reads) {
    if (owner !== currentUser()) return
    if (client) await client.fetchQuery({ queryKey, queryFn: ({ signal }) => cachedGet(path, signal), staleTime: 30_000 }).catch(() => {})
    else await cachedGet(path).catch(() => {})
  }
}
