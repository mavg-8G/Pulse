import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
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

/** Movimientos del servidor con los cambios hechos sin conexión encima, marcados como pendientes. */
export const useBreakdown = (month: string) =>
  useQuery({ queryKey: ['breakdown', month], queryFn: () => cachedGet<BudgetBreakdown>(`/api/budgets/breakdown?month=${month}`) })

export function useTransactions(f: TxQuery) {
  const { data: me } = useMe()
  const tz = me?.timezone ?? 'America/Guayaquil'
  const ops = usePendingOps()
  const q = useQuery({ queryKey: ['transactions', f], queryFn: () => cachedGet<TxPage>(txUrl(f)) })
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
export const financeKeys = [['transactions'], ['dashboard'], ['summary'], ['breakdown']]
