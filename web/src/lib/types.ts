export type AuthState = 'none' | 'enroll' | 'mfa' | 'full'

export interface Me {
  id: string
  email: string
  role: 'admin' | 'user'
  timezone: string
  theme: Theme
  created_at: string
  mfa: { totp: boolean; passkeys: number; recovery_remaining: number }
  step_up_valid: boolean
}

export type Theme = 'system' | 'light' | 'dark'

export type AccountType = 'bank' | 'credit_card' | 'debit_card' | 'cash' | 'wallet'

export interface Account {
  id: string
  name: string
  type: AccountType
  institution: string
  card_suffix: string
  currency: string
  color: string
  icon: string
  archived: boolean
}

export interface Category {
  id: string
  parent_id: string
  name: string
  color: string
  icon: string
  is_system: boolean
  archived: boolean
}

export interface Transaction {
  id: string
  account_id: string
  category_id: string
  amount_cents: number
  currency: string
  occurred_at: string
  kind: 'purchase' | 'refund'
  merchant: string
  description: string
  notes: string
  source: 'manual' | 'gmail' | 'email'
  status: 'confirmed' | 'pending_review'
}

export interface TxPage {
  items: Transaction[]
  total: number
  sum_cents: number
  limit: number
  offset: number
}

export interface Budget {
  id: string
  category_id: string
  amount_cents: number
  thresholds: string
  active: boolean
}

export interface Progress {
  budget_cents: number
  spent_cents: number
  remaining_cents: number
  percent: number
  projected_cents: number
  projected_over_cents: number
  days_elapsed: number
  days_in_month: number
  status: 'ok' | 'warning' | 'over'
}

export interface BudgetLine extends Progress {
  budget_id: string
  category_id: string
  name: string
  color: string
  icon: string
  thresholds: string
}

export interface Summary {
  month: string
  lines: BudgetLine[]
  totals: Progress
  unbudgeted_cents: number
  spent_cents: number
}

export interface Alert {
  budget_id: string
  category_id: string
  month: string
  threshold: number
  spent_cents: number
  created_at: string
}

export interface Dashboard {
  month: string
  spent_cents: number
  count: number
  daily_cents: number[]
  by_category: { category_id: string; name: string; color: string; icon: string; spent_cents: number }[]
  budget: Progress
  has_budgets: boolean
  recent: Transaction[]
  alerts: Alert[]
  pending_review: number
}

export interface SessionInfo {
  id: string
  ip: string
  user_agent: string
  remember: boolean
  created_at: string
  last_seen_at: string
  current: boolean
}

export interface PasskeyInfo {
  id: string
  name: string
  created_at: string
  last_used_at: string | null
}

export interface AuditEntry {
  id: number
  event: string
  ip: string
  user_agent: string
  meta: Record<string, string>
  created_at: string
}

export interface GmailStatus {
  available: boolean
  gemini_available: boolean
  realtime: boolean
  poll_minutes: number
  senders: string[]
  connected: boolean
  email?: string
  status?: 'active' | 'broken'
  last_error?: string
  last_sync_at?: string
  last_ok_at?: string
  next_sync_at?: string
  watch_expires_at?: string
  shadow: boolean
  gemini: boolean
  imported: number
}

export interface GmailServerConfig {
  source: '' | 'env' | 'ajustes'
  client_id: string
  client_secret_set: boolean
  pubsub_topic: string
  pubsub_sa: string
  gemini_key_set: boolean
  redirect_uri: string
  webhook_url: string
  audience: string
}

export interface GmailSyncResult {
  found: number
  created: number
  confirmed: number
  duplicate: number
  ignored: number
  unrecognized: number
  gemini: number
}
