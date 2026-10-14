import { useQuery } from '@tanstack/react-query'
import { get } from './api'
import type { Account, Budget, Category, Dashboard, Me, Summary, TxPage } from './types'

export const useMe = () => useQuery({ queryKey: ['me'], queryFn: () => get<Me>('/api/me') })
export const useAccounts = (archived = false) =>
  useQuery({ queryKey: ['accounts', archived], queryFn: () => get<Account[]>(`/api/accounts${archived ? '?archived=1' : ''}`) })
export const useCategories = () => useQuery({ queryKey: ['categories'], queryFn: () => get<Category[]>('/api/categories') })
export const useBudgets = () => useQuery({ queryKey: ['budgets'], queryFn: () => get<Budget[]>('/api/budgets') })
export const useDashboard = (month: string) =>
  useQuery({ queryKey: ['dashboard', month], queryFn: () => get<Dashboard>(`/api/dashboard?month=${month}`) })
export const useSummary = (month: string) =>
  useQuery({ queryKey: ['summary', month], queryFn: () => get<Summary>(`/api/budgets/summary?month=${month}`) })

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

export const useTransactions = (f: TxQuery) => useQuery({ queryKey: ['transactions', f], queryFn: () => get<TxPage>(txUrl(f)) })

/** Lo que cambia al guardar un gasto. */
export const financeKeys = [['transactions'], ['dashboard'], ['summary']]
