// Estado de Reportes (periodo, comparación y filtros) y conclusiones calculadas solo con los datos del servidor.
import { money } from './money'
import { shiftMonth } from './dates'
import type { ReportCompare, ReportSummary } from './types'

export type Preset = '1' | '3' | '6' | '12' | 'custom'

export interface ReportState {
  preset: Preset
  from: string // AAAA-MM (solo con preset custom)
  to: string
  compare: ReportCompare
  category: string
  account: string
}

export const defaultState: ReportState = { preset: '6', from: '', to: '', compare: 'previous', category: '', account: '' }
const STORAGE_KEY = 'pulse.reportes'
const monthRe = /^\d{4}-(0[1-9]|1[0-2])$/

/** Lee el estado de la URL; si no trae nada, el último usado en esta sesión del navegador. */
export function readState(search: URLSearchParams, current: string): ReportState {
  let src = search
  if (![...search.keys()].some((k) => ['periodo', 'desde', 'hasta', 'comparar', 'categoria', 'cuenta'].includes(k))) {
    try {
      src = new URLSearchParams(sessionStorage.getItem(STORAGE_KEY) ?? '')
    } catch {
      src = new URLSearchParams()
    }
  }
  const p = src.get('periodo')
  const preset: Preset = p === '1' || p === '3' || p === '6' || p === '12' || p === 'custom' ? p : defaultState.preset
  let from = src.get('desde') ?? ''
  let to = src.get('hasta') ?? ''
  if (preset === 'custom') {
    if (!monthRe.test(to) || to > current) to = current
    if (!monthRe.test(from) || from > to) from = shiftMonth(to, -2)
    if (from < shiftMonth(to, -35)) from = shiftMonth(to, -35) // el servidor acepta hasta tres años
  }
  return {
    preset,
    from: preset === 'custom' ? from : '',
    to: preset === 'custom' ? to : '',
    compare: src.get('comparar') === 'anio' ? 'year' : 'previous',
    category: src.get('categoria') ?? '',
    account: src.get('cuenta') ?? '',
  }
}

export function stateParams(s: ReportState): URLSearchParams {
  const p = new URLSearchParams()
  p.set('periodo', s.preset)
  if (s.preset === 'custom') {
    p.set('desde', s.from)
    p.set('hasta', s.to)
  }
  if (s.compare === 'year') p.set('comparar', 'anio')
  if (s.category) p.set('categoria', s.category)
  if (s.account) p.set('cuenta', s.account)
  return p
}

export function saveState(s: ReportState) {
  try {
    sessionStorage.setItem(STORAGE_KEY, stateParams(s).toString())
  } catch {
    // sin almacenamiento: los filtros siguen en la URL
  }
}

/** Meses del periodo elegido (AAAA-MM). */
export function stateRange(s: ReportState, current: string): { from: string; to: string } {
  if (s.preset === 'custom') return { from: s.from, to: s.to }
  return { from: shiftMonth(current, -(Number(s.preset) - 1)), to: current }
}

/** Parámetros de la API: los mismos para el resumen, la proyección y la exportación. */
export function apiQuery(s: ReportState, current: string, withRange = true): string {
  const p = new URLSearchParams()
  if (withRange) {
    const r = stateRange(s, current)
    p.set('from', r.from)
    p.set('to', r.to)
    if (s.compare === 'year') p.set('compare', 'year')
  }
  if (s.category) p.set('category', s.category)
  if (s.account) p.set('account', s.account)
  return p.toString()
}

const pctFmt = new Intl.NumberFormat('es-EC', { maximumFractionDigits: 1, signDisplay: 'exceptZero' })
export const signedPct = (v: number) => `${pctFmt.format(v)} %`
export const signedMoney = (cents: number) => (cents > 0 ? `+${money(cents)}` : cents < 0 ? `−${money(-cents)}` : money(0))

/** Variación porcentual solo cuando la base es comparable (periodo anterior completo y gasto positivo). */
export function change(cur: number, prev: number, comparable: boolean): number | null {
  if (!comparable || prev <= 0) return null
  return Math.round(((cur - prev) / prev) * 1000) / 10
}

export interface Insight {
  tone: 'up' | 'down' | 'neutral'
  text: string
}

/**
 * Conclusiones a partir del resumen: cambio total, categorías que más subieron o bajaron, comercios y promedio
 * diario. Cada frase usa solo cifras del resumen; si una comparación no es posible, no se dice nada de ella.
 */
export function insights(r: ReportSummary, categoryName: (id: string) => string): Insight[] {
  const out: Insight[] = []
  const prev = r.previous
  const comparable = prev?.coverage === 'completo'
  const periodName = r.period?.compare === 'year' ? 'el mismo periodo del año pasado' : 'el periodo anterior'
  if (prev && prev.coverage !== 'sin_datos') {
    const diff = r.total_cents - prev.total_cents
    const pct = change(r.total_cents, prev.total_cents, comparable)
    if (diff !== 0) {
      out.push({
        tone: diff > 0 ? 'up' : 'down',
        text: `Gastaste ${money(Math.abs(diff))} ${diff > 0 ? 'más' : 'menos'} que en ${periodName}${pct !== null ? ` (${signedPct(pct)})` : ''}.${comparable ? '' : ' Ese periodo tiene datos incompletos.'}`,
      })
    }
    const ups = (r.category_changes ?? []).filter((c) => c.diff_cents > 0).slice(0, 2)
    for (const c of ups) {
      out.push({ tone: 'up', text: `${categoryName(c.id)} subió ${money(c.diff_cents)}${c.pct !== null ? ` (${signedPct(c.pct)})` : c.previous_cents <= 0 ? ' (sin gasto antes)' : ''}.` })
    }
    const down = [...(r.category_changes ?? [])].filter((c) => c.diff_cents < 0).sort((a, b) => a.diff_cents - b.diff_cents)[0]
    if (down) out.push({ tone: 'down', text: `${categoryName(down.id)} bajó ${money(-down.diff_cents)}${down.pct !== null ? ` (${signedPct(down.pct)})` : ''}.` })
  }
  const top = r.merchants?.[0]
  if (top && r.purchase_cents > 0) {
    const share = Math.round((top.spent_cents / r.purchase_cents) * 100)
    out.push({ tone: 'neutral', text: `El comercio con más gasto fue ${top.name}: ${money(top.spent_cents)} en ${top.purchase_count} ${top.purchase_count === 1 ? 'compra' : 'compras'} (${share} % de tus compras).` })
  }
  const riser = (r.merchants ?? []).filter((m) => m.previous_cents > 0 && m.spent_cents - m.previous_cents > 0).sort((a, b) => b.spent_cents - b.previous_cents - (a.spent_cents - a.previous_cents))[0]
  if (riser && riser !== top && comparable) {
    out.push({ tone: 'up', text: `En ${riser.name} gastaste ${money(riser.spent_cents - riser.previous_cents)} más que en ${periodName}.` })
  }
  const big = r.by_category[0]
  if (big && r.total_cents > 0 && big.spent_cents / r.total_cents >= 0.4) {
    out.push({ tone: 'neutral', text: `${categoryName(big.id)} concentra el ${Math.round((big.spent_cents / r.total_cents) * 100)} % de tu gasto neto.` })
  }
  if (prev && comparable && r.period && r.period.days > 0 && prev.days > 0) {
    const now = Math.round(r.total_cents / r.period.days)
    const before = Math.round(prev.total_cents / prev.days)
    const pct = change(now, before, true)
    if (pct !== null && Math.abs(pct) >= 10) out.push({ tone: pct > 0 ? 'up' : 'down', text: `Tu promedio diario pasó de ${money(before)} a ${money(now)} (${signedPct(pct)}).` })
  }
  return out
}
