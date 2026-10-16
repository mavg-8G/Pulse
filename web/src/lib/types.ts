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
  /** Cuenta principal con la que se fusionó; vacío si no está fusionada. */
  merged_into?: string
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
  version: number
  /** Solo en la app: el cambio está guardado en el dispositivo y falta enviarlo. */
  pending?: boolean
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
  account_id: string
  amount_cents: number
  thresholds: string
  active: boolean
  rollover: boolean
  rollover_from: string
  months: Record<string, number> | null
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
  kind: 'category' | 'account'
  category_id: string
  account_id: string
  name: string
  color: string
  icon: string
  thresholds: string
  base_cents: number
  cap_cents: number
  month_cap: boolean
  carry_cents: number
  rollover: boolean
  rollover_from: string
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
  accuracy: { parser: string; bank: string; reviewed: number; correct: number; trusted: boolean }[]
  template_alerts: { parser: string; bank: string; subject: string; created_at: string }[]
  template_alerts_total: number
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

export interface ReportSummary {
  from: string
  to: string
  total_cents: number
  purchase_cents: number
  refund_cents: number
  purchase_count: number
  elapsed_days: number
  months: { month: string; spent_cents: number; by_category: { id: string; spent_cents: number }[]; by_account: { id: string; spent_cents: number }[] }[]
  by_category: { id: string; spent_cents: number }[]
  by_account: { id: string; spent_cents: number }[]
  forecast: {
    ready: boolean
    history_days: number
    history_from: string
    history_to: string
    week_from: string
    week_to: string
    month_from: string
    month_to: string
    month_days: number
    week_cents: number
    month_cents: number
    by_category: { id: string; week_cents: number; month_cents: number }[]
    week_model: HorizonModel | null
    month_model: HorizonModel | null
  }
  account_categories: { account_id: string; category_id: string; spent_cents: number; purchase_count: number }[]
  account_purchases: Record<string, number>
  weekdays: { weekday: number; spent_cents: number; days: number; avg_cents: number }[] // 0 = lunes
  days: { date: string; spent_cents: number }[] // últimos 91 días
  // Filtros aplicados, periodo transcurrido y periodo anterior equivalente (fechas locales, ambas incluidas).
  filters?: { category: string; account: string }
  period?: { from: string; to: string; days: number; compare: ReportCompare; granularity: 'day' | 'week' | 'month' }
  previous?: {
    from: string
    to: string
    days: number
    coverage: 'completo' | 'parcial' | 'sin_datos'
    total_cents: number
    purchase_cents: number
    refund_cents: number
    purchase_count: number
    by_category: { id: string; spent_cents: number }[]
    by_account: { id: string; spent_cents: number }[]
  }
  series?: { start: string; end: string; net_cents: number; purchase_cents: number; refund_cents: number; previous_cents: number }[]
  category_changes?: { id: string; current_cents: number; previous_cents: number; diff_cents: number; pct: number | null }[]
  merchants?: { name: string; spent_cents: number; purchase_count: number; previous_cents: number }[]
}

export type ReportCompare = 'previous' | 'year'
export type Confidence = 'alta' | 'media' | 'baja'
export type HorizonMethodId = 'ritmo' | 'robusto' | 'media_movil' | 'dia_semana' | 'holt' | 'holt_winters' | 'combinado'


/** Proyección de la próxima semana o el próximo mes con el método que menos se equivocó y su rango del 95 %. */
export interface HorizonModel {
  from: string
  to: string
  days: number
  history_days: number
  method: HorizonMethodId
  projected_cents: number
  low_cents: number | null
  high_cents: number | null
  upcoming_cents: number
  baseline_cents: number
  methods: { id: HorizonMethodId; available: boolean; projected_cents: number; mae_cents: number | null; evaluated: number; candidate?: boolean }[]
  by_category: { id: string; projected_cents: number; upcoming_cents: number }[]
  backtests: number
  coverage: number | null
  outliers: number
  typical_error_pct?: number | null
  improvement_pct?: number | null
  confidence?: Confidence
  warnings?: string[]
  cumulative: number[]
  cumulative_low: number[]
  cumulative_high: number[]
}

export interface BudgetBreakdown {
  month: string
  categories: {
    id: string
    spent_cents: number
    purchase_count: number
    refund_cents: number
    pending_review: number
    subcategories: { id: string; spent_cents: number }[]
    merchants: { name: string; count: number; spent_cents: number }[]
    other_cents: number
    other_count: number
  }[]
}

export type ProjectionMethodId = 'ritmo' | 'promedio_reciente' | 'regresion' | 'robusto' | 'media_movil' | 'dia_semana' | 'perfil' | 'holt' | 'holt_winters' | 'combinado'

export interface Projection {
  month: string
  day: number
  days_in_month: number
  spent_cents: number
  upcoming_cents: number
  method: ProjectionMethodId
  projected_cents: number
  low_cents: number | null
  high_cents: number | null
  baseline_cents: number
  methods: { id: ProjectionMethodId; available: boolean; projected_cents: number; low_cents: number | null; high_cents: number | null; mae_cents: number | null; mape: number | null; evaluated: number; candidate: boolean }[]
  by_category: { id: string; spent_cents: number; upcoming_cents: number; projected_cents: number }[]
  accuracy: { month: string; day: number; actual_cents: number; predicted_cents: number; baseline_cents: number; error_cents: number; error_pct: number | null; baseline_error_pct: number | null }[]
  outliers: { date: string; category_id: string; amount_cents: number }[]
  outlier_threshold_cents: number | null
  warnings: string[]
  backtests?: number
  typical_error_pct?: number | null
  improvement_pct?: number | null
  coverage?: number | null
  confidence?: Confidence
  cumulative: number[]
  previous_cumulative: number[]
  previous_month: string
}

export interface Reconciliation {
  month: string
  accounts: { account_id: string; recorded_cents: number; statement_cents: number | null; difference_cents: number | null }[]
}

export interface Recurring {
  id: string
  account_id: string
  category_id: string
  amount_cents: number
  merchant: string
  day: number
  next_date: string
  version: number
  pending?: boolean
}

export interface BackupRun {
  id: string
  trigger: 'manual' | 'scheduled'
  started_at: string
  finished_at: string
  status: 'ok' | 'failed'
  name: string
  size_bytes: number
  sha256: string
  attempts: number
  error: string
}

export interface BackupStatus {
  configured: boolean
  target: string
  interval: string
  state: 'off' | 'pending' | 'ok' | 'stale' | 'failing'
  last_ok: BackupRun | null
  runs: BackupRun[]
  running: boolean
  download: boolean
}
