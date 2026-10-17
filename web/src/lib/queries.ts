import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { get } from './api'
import { cachedGet, overlayRecurring, overlayTransactions, usePendingOps } from './offline'
import type { Account, Budget, BudgetBreakdown, Category, Dashboard, Me, Recurring, Summary, TxPage } from './types'

// Las consultas de Inicio, Movimientos y Plan usan cachedGet: sin conexión muestran lo último que se vio (cifrado en el dispositivo).
export const useMe = () => useQuery({ queryKey: ['me'], queryFn: () => cachedGet<Me>('/api/me') })
export const useAccounts = (archived = false) =>
  useQuery({ queryKey: ['accounts', archived], queryFn: () => cachedGet<Account[]>(`/api/accounts${archived ? '?archived=1' : ''}`) })
export const useCategories = () => useQuery({ queryKey: ['categories'], queryFn: () => cachedGet<Category[]>('/api/categories') })
export const useBudgets = () => useQuery({ queryKey: ['budgets'], queryFn: () => cachedGet<Budget[]>('/api/budgets') })
export const useDashboard = (month: string) =>
  useQuery({ queryKey: ['dashboard', month], queryFn: () => cachedGet<Dashboard>(`/api/dashboard?month=${month}`) })
export const useSummary = (month: string) =>
  useQuery({ queryKey: ['summary', month], queryFn: () => cachedGet<Summary>(`/api/budgets/summary?month=${month}`) })

export interface TxQuery {
  month?: string
  category_id?: string
  account_id?: string
  status?: string
  q?: string
  limit?: number
  offset?: number
}

export function txUrl(f: TxQuery) {
  const p = new URLSearchParams()
  Object.entries(f).forEach(([k, v]) => {
    if (v !== undefined && v !== '') p.set(k, String(v))
  })
  return `/api/transactions?${p}`
}

/** El servidor pagina de 200 en 200. Mostrar más conserva un único resultado y suma, sin pedir límites inválidos. */
export async function fetchTransactions(f: TxQuery, signal?: AbortSignal): Promise<TxPage> {
  const requested = f.limit ?? 50
  if (requested <= 200) return cachedGet<TxPage>(txUrl(f), signal)
  // Misma caché cifrada y estado sin conexión que cachedGet; solo cambia cómo se lee del servidor.
  return cachedGet<TxPage>(txUrl(f), signal, async () => {
    const page = await get<TxPage>(txUrl({ ...f, limit: 200 }), signal)
    const items = [...page.items]
    while (items.length < requested && items.length + (f.offset ?? 0) < page.total) {
      const next = await get<TxPage>(txUrl({ ...f, offset: (f.offset ?? 0) + items.length, limit: Math.min(200, requested - items.length) }), signal)
      if (!next.items.length) break
      items.push(...next.items)
    }
    return { ...page, items }
  })
}

/** Movimientos del servidor con los cambios hechos sin conexión encima, marcados como pendientes. */
export const useBreakdown = (month: string) =>
  useQuery({ queryKey: ['breakdown', month], queryFn: () => cachedGet<BudgetBreakdown>(`/api/budgets/breakdown?month=${month}`) })

export function useTransactions(f: TxQuery) {
  const { data: me } = useMe()
  const tz = me?.timezone ?? 'America/Guayaquil'
  const ops = usePendingOps()
  const q = useQuery({ queryKey: ['transactions', f], queryFn: ({ signal }) => fetchTransactions(f, signal),
    placeholderData: (previous, query) => {
      const prior = query?.queryKey[1] as TxQuery | undefined
      return prior && txUrl({ ...prior, limit: 0 }) === txUrl({ ...f, limit: 0 }) ? previous : undefined
    },
  })
  const data = useMemo(() => (q.data ? overlayTransactions(q.data, ops, f, tz) : q.data), [q.data, ops, f.month, f.category_id, f.account_id, f.q, f.status, tz]) // eslint-disable-line react-hooks/exhaustive-deps
  return { ...q, data }
}

export function useRecurring() {
  const ops = usePendingOps()
  const q = useQuery({ queryKey: ['recurring'], queryFn: () => cachedGet<Recurring[]>('/api/recurring') })
  const data = useMemo(() => (q.data ? overlayRecurring(q.data, ops) : q.data), [q.data, ops])
  return { ...q, data }
}

/** Lo que cambia al guardar un gasto. */
export const financeKeys = [['transactions'], ['dashboard'], ['summary'], ['breakdown'], ['reports'], ['projection'], ['reconciliation']]
