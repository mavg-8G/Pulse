import { beforeEach, describe, expect, it } from 'vitest'
import { apiQuery, change, insights, readState, saveState, stateParams, stateRange } from './reports'
import type { ReportSummary } from './types'

describe('estado de Reportes', () => {
  beforeEach(() => sessionStorage.clear())

  it('lee la URL, valida los meses y genera los mismos parámetros para la API y la exportación', () => {
    const s = readState(new URLSearchParams('periodo=custom&desde=2026-02&hasta=2027-01&comparar=anio&categoria=c1&cuenta=a1'), '2026-10')
    expect(s).toEqual({ preset: 'custom', from: '2026-02', to: '2026-10', compare: 'year', category: 'c1', account: 'a1' }) // "hasta" no pasa de hoy
    expect(stateRange(s, '2026-10')).toEqual({ from: '2026-02', to: '2026-10' })
    expect(apiQuery(s, '2026-10')).toBe('from=2026-02&to=2026-10&compare=year&category=c1&account=a1')
    expect(apiQuery(s, '2026-10', false)).toBe('category=c1&account=a1')
    expect(readState(new URLSearchParams('periodo=3'), '2026-01')).toMatchObject({ preset: '3', compare: 'previous' })
    expect(stateRange(readState(new URLSearchParams('periodo=3'), '2026-01'), '2026-01')).toEqual({ from: '2025-11', to: '2026-01' })
    // Rango invertido o de más de tres años: se corrige.
    expect(readState(new URLSearchParams('periodo=custom&desde=2026-09&hasta=2026-03'), '2026-10')).toMatchObject({ from: '2026-01', to: '2026-03' })
    expect(readState(new URLSearchParams('periodo=custom&desde=2020-01&hasta=2026-10'), '2026-10').from).toBe('2023-11')
  })

  it('recuerda los filtros al volver a la página sin parámetros', () => {
    const s = readState(new URLSearchParams('periodo=12&cuenta=a1'), '2026-10')
    saveState(s)
    expect(readState(new URLSearchParams(), '2026-10')).toEqual(s)
    expect(stateParams(s).toString()).toBe('periodo=12&cuenta=a1')
  })
})

const base: ReportSummary = {
  from: '2026-10', to: '2026-10', total_cents: 17000, purchase_cents: 19000, refund_cents: 2000, purchase_count: 4, elapsed_days: 14,
  months: [], by_category: [{ id: 'comida', spent_cents: 16000 }, { id: 'ocio', spent_cents: 1000 }], by_account: [],
  forecast: { ready: false, history_days: 0, history_from: '', history_to: '', week_from: '', week_to: '', month_from: '', month_to: '', month_days: 30, week_cents: 0, month_cents: 0, by_category: [], week_model: null, month_model: null },
  account_categories: [], account_purchases: {}, weekdays: [], days: [],
  period: { from: '2026-10-01', to: '2026-10-14', days: 14, compare: 'previous', granularity: 'day' },
  previous: { from: '2026-09-01', to: '2026-09-14', days: 14, coverage: 'completo', total_cents: 14000, purchase_cents: 14000, refund_cents: 0, purchase_count: 2, by_category: [], by_account: [] },
  category_changes: [
    { id: 'comida', current_cents: 16000, previous_cents: 10000, diff_cents: 6000, pct: 60 },
    { id: 'ocio', current_cents: 1000, previous_cents: 4000, diff_cents: -3000, pct: -75 },
  ],
  merchants: [{ name: 'Supermaxi', spent_cents: 16000, purchase_count: 2, previous_cents: 10000 }],
}
const name = (id: string) => ({ comida: 'Comida', ocio: 'Ocio' })[id] ?? id
const money = (s: string) => s.replace(/ /g, ' ')

describe('conclusiones', () => {
  it('solo usa cifras del resumen y porcentajes cuando el periodo anterior es comparable', () => {
    const t = insights(base, name).map((x) => money(x.text))
    expect(t[0]).toBe('Gastaste $30,00 más que en el periodo anterior (+21,4 %).')
    expect(t).toContain('Comida subió $60,00 (+60 %).')
    expect(t).toContain('Ocio bajó $30,00 (-75 %).')
    expect(t).toContain('El comercio con más gasto fue Supermaxi: $160,00 en 2 compras (84 % de tus compras).')
    expect(t).toContain('Comida concentra el 94 % de tu gasto neto.')
    expect(t).toContain('Tu promedio diario pasó de $10,00 a $12,14 (+21,4 %).')
  })

  it('sin periodo anterior no compara, y con datos parciales no da porcentajes', () => {
    const none = insights({ ...base, previous: { ...base.previous!, coverage: 'sin_datos' } }, name).map((x) => x.text)
    expect(none.some((t) => /subió|bajó|Gastaste|promedio diario/.test(t))).toBe(false)
    const partial = insights({ ...base, previous: { ...base.previous!, coverage: 'parcial' }, category_changes: base.category_changes!.map((c) => ({ ...c, pct: null })) }, name).map((x) => money(x.text))
    expect(partial[0]).toBe('Gastaste $30,00 más que en el periodo anterior. Ese periodo tiene datos incompletos.')
    expect(partial.join(' ')).not.toMatch(/%\)/)
    expect(insights({ ...base, total_cents: 0, purchase_cents: 0, by_category: [], category_changes: [], merchants: [], previous: { ...base.previous!, total_cents: 0 } }, name)).toEqual([])
  })

  it('cambio porcentual sin base positiva no existe', () => {
    expect(change(100, 0, true)).toBeNull()
    expect(change(100, -50, true)).toBeNull()
    expect(change(150, 100, false)).toBeNull()
    expect(change(150, 100, true)).toBe(50)
  })
})
