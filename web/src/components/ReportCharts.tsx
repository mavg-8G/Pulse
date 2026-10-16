import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { errorMessage, get } from '../lib/api'
import { monthLabel } from '../lib/dates'
import { money } from '../lib/money'
import type { Account, Category, Projection, ProjectionMethodId, ReportSummary } from '../lib/types'
import { ReportLineChart, type ReportSeries } from './ReportLineChart'

export const methodName: Record<ProjectionMethodId, string> = {
  ritmo: 'Ritmo del mes (método actual)',
  regresion: 'Regresión lineal',
  media_movil: 'Media móvil ponderada',
  dia_semana: 'Ajuste por día de la semana',
  perfil: 'Estacionalidad mensual',
}

const warningText: Record<string, string> = {
  sin_datos: 'Aún no hay movimientos para proyectar.',
  pocos_dias: 'Con menos de 5 días del mes la regresión no es confiable todavía.',
  historial_corto: 'La media móvil y el ajuste por día de la semana necesitan al menos 4 semanas de historial.',
  sin_meses_cerrados: 'Hacen falta 2 meses cerrados para comparar métodos; mientras tanto se usa el ritmo del mes.',
  pocos_meses_perfil: 'La estacionalidad mensual necesita 3 meses cerrados.',
}

const weekdays = ['Lunes', 'Martes', 'Miércoles', 'Jueves', 'Viernes', 'Sábado', 'Domingo']
const pct = (v: number | null) => (v === null ? '—' : `${new Intl.NumberFormat('es-EC', { maximumFractionDigits: 1 }).format(v)} %`)
const dayDate = (d: string) => new Intl.DateTimeFormat('es-EC', { weekday: 'short', day: 'numeric', month: 'short', timeZone: 'UTC' }).format(new Date(`${d}T12:00:00Z`))

/** Interpola linealmente desde el valor de hoy hasta el valor al cierre. */
function towards(from: number, to: number, start: number, end: number, i: number) {
  return end === start ? to : Math.round(from + (to - from) * (i - start) / (end - start))
}

export function ProjectionCard({ categories }: { categories: Category[] }) {
  const { data: p, error, isLoading } = useQuery({ queryKey: ['projection'], queryFn: () => get<Projection>('/api/reports/projection') })
  const cat = (id: string) => categories.find((c) => c.id === id)?.name ?? (id ? 'Categoría' : 'Sin categoría')
  if (isLoading) return <section className="panel report-card"><p className="loading">Calculando proyección…</p></section>
  if (error || !p) return <section className="panel report-card"><p className="error" role="alert">{errorMessage(error)}</p></section>
  const D = p.days_in_month
  const labels = Array.from({ length: D }, (_, i) => `Día ${i + 1}`)
  const today = p.day - 1
  const spentNow = p.cumulative.at(-1) ?? 0
  const actual: (number | null)[] = labels.map((_, i) => (i <= today ? p.cumulative[i] : null))
  const projected: (number | null)[] = labels.map((_, i) => (i >= today ? towards(spentNow, p.projected_cents, today, D - 1, i) : null))
  const band = p.low_cents !== null && p.high_cents !== null
    ? { name: 'rango', low: labels.map((_, i) => (i >= today ? towards(spentNow, p.low_cents!, today, D - 1, i) : null)), high: labels.map((_, i) => (i >= today ? towards(spentNow, p.high_cents!, today, D - 1, i) : null)) }
    : undefined
  const previous: (number | null)[] = labels.map((_, i) => p.previous_cumulative[Math.min(i, p.previous_cumulative.length - 1)] ?? null)
  const series: ReportSeries[] = [
    { name: 'Este mes', values: actual },
    { name: 'Proyección', values: projected, dashed: true, band },
    { name: monthLabel(p.previous_month), values: previous, tone: 4 },
  ]
  const chosen = p.methods.find((m) => m.id === p.method)
  const baseline = p.methods.find((m) => m.id === 'ritmo')
  const evaluated = p.methods.some((m) => m.evaluated > 0)
  return (
    <section className="panel report-card report-forecast" aria-labelledby="h-projection">
      <div className="section-head">
        <div>
          <span className="report-eyebrow">Cierre de {monthLabel(p.month).toLowerCase()}</span>
          <h2 id="h-projection">Proyección de fin de mes</h2>
          <p className="small muted">Día {p.day} de {D} · gastado {money(p.spent_cents)}{p.upcoming_cents > 0 ? ` · gastos fijos por cobrar ${money(p.upcoming_cents)}` : ''}</p>
        </div>
      </div>
      <dl className="report-forecast-metrics">
        <div className="report-metric"><dt>Cierre estimado</dt><dd className="num" data-testid="projection-value">{money(p.projected_cents)}</dd>
          <dd className="report-metric-detail">{p.low_cents !== null && p.high_cents !== null ? <>Rango probable (95 %): <span className="num">{money(p.low_cents)} – {money(p.high_cents)}</span></> : 'Sin historial suficiente para un rango'}</dd></div>
        <div className="report-metric"><dt>Método</dt><dd>{methodName[p.method]}</dd>
          <dd className="report-metric-detail">{evaluated && chosen?.mae_cents !== null && chosen?.mae_cents !== undefined ? `Error medio ${money(chosen.mae_cents)} en ${chosen.evaluated} ${chosen.evaluated === 1 ? 'mes cerrado' : 'meses cerrados'}` : 'Se elige solo cuando haya meses cerrados para comparar'}</dd></div>
        <div className="report-metric"><dt>Con el método actual</dt><dd className="num">{money(p.baseline_cents)}</dd>
          <dd className="report-metric-detail">{baseline?.mae_cents !== null && baseline?.mae_cents !== undefined ? `Error medio ${money(baseline.mae_cents)}` : 'Gastado ÷ días transcurridos × días del mes'}</dd></div>
      </dl>
      {p.warnings.length > 0 && <ul className="report-warnings small muted">{p.warnings.map((w) => <li key={w}>{warningText[w] ?? w}</li>)}</ul>}
      <ReportLineChart label="Detalle del día" labels={labels} series={series} initial={today} />
      <details className="more"><summary>Ver acumulado y proyección como tabla</summary>
        <div className="report-table-scroll" tabIndex={0}><table className="report-table"><caption className="sr-only">Acumulado diario del mes, proyección y mes anterior</caption>
          <thead><tr><th scope="col">Día</th><th scope="col">Este mes</th><th scope="col">Proyección</th><th scope="col">{monthLabel(p.previous_month)}</th></tr></thead>
          <tbody>{labels.map((l, i) => <tr key={l}><th scope="row">{i + 1}</th><td className="num">{actual[i] === null ? '—' : money(actual[i]!)}</td><td className="num">{projected[i] === null ? '—' : money(projected[i]!)}</td><td className="num">{previous[i] === null ? '—' : money(previous[i]!)}</td></tr>)}</tbody>
        </table></div>
      </details>
      {p.by_category.length > 0 && (
        <details className="more"><summary>Proyección por categoría</summary>
          <div className="report-table-scroll" tabIndex={0}><table className="report-table report-forecast-table"><caption className="sr-only">Gastado, gastos fijos por cobrar y cierre estimado por categoría</caption>
            <thead><tr><th scope="col">Categoría</th><th scope="col">Gastado</th><th scope="col">Fijos por cobrar</th><th scope="col">Cierre estimado</th></tr></thead>
            <tbody>{p.by_category.map((c) => <tr key={c.id || 'none'}><th scope="row">{cat(c.id)}</th><td className="num">{money(c.spent_cents)}</td><td className="num">{c.upcoming_cents ? money(c.upcoming_cents) : '—'}</td><td className="num">{money(c.projected_cents)}</td></tr>)}</tbody>
            <tfoot><tr><th scope="row">Total</th><td className="num">{money(p.spent_cents)}</td><td className="num">{money(p.upcoming_cents)}</td><td className="num">{money(p.projected_cents)}</td></tr></tfoot>
          </table></div>
        </details>
      )}
      <details className="more"><summary>Precisión: proyección contra el cierre real</summary>
        {p.accuracy.length === 0 ? <p className="small muted">Aparecerá cuando tengas un mes cerrado con movimientos desde su inicio.</p> : (
          <div className="report-table-scroll" tabIndex={0}><table className="report-table report-forecast-table"><caption className="sr-only">Para cada mes cerrado, lo que se habría proyectado el mismo día del mes y lo que realmente se gastó</caption>
            <thead><tr><th scope="col">Mes</th><th scope="col">Cierre real</th><th scope="col">Proyectado al día {p.day}</th><th scope="col">Error</th><th scope="col">Error del método actual</th></tr></thead>
            <tbody>{p.accuracy.map((a) => <tr key={a.month}><th scope="row">{monthLabel(a.month)}</th><td className="num">{money(a.actual_cents)}</td><td className="num">{money(a.predicted_cents)}</td><td className="num">{pct(a.error_pct)}</td><td className="num">{pct(a.baseline_error_pct)}</td></tr>)}</tbody>
          </table></div>
        )}
        <div className="report-table-scroll" tabIndex={0}><table className="report-table report-forecast-table"><caption>Métodos comparados</caption>
          <thead><tr><th scope="col">Método</th><th scope="col">Cierre estimado</th><th scope="col">Error medio</th><th scope="col">Error %</th></tr></thead>
          <tbody>{p.methods.map((m) => <tr key={m.id} className={m.id === p.method ? 'report-chosen' : ''}><th scope="row">{methodName[m.id]}{m.id === p.method ? ' (elegido)' : ''}</th><td className="num">{m.available ? money(m.projected_cents) : 'No disponible'}</td><td className="num">{m.mae_cents === null ? '—' : money(m.mae_cents)}</td><td className="num">{pct(m.mape)}</td></tr>)}</tbody>
        </table></div>
        <p className="small muted">Cada método se prueba en los meses cerrados como si fuera el día {p.day}, con lo que se sabía ese día, y se elige el de menor error medio. El método actual siempre participa, así que el elegido nunca se equivoca más que él.</p>
      </details>
      {p.outliers.length > 0 && (
        <details className="more"><summary>Gastos atípicos que no se extrapolan ({p.outliers.length})</summary>
          <p className="small muted">Compras por encima de {money(p.outlier_threshold_cents ?? 0)}: cuentan en lo gastado, pero no se usan para estimar el ritmo.</p>
          <ul className="list">{p.outliers.map((o, i) => <li key={i} className="settings-row"><span className="grow">{dayDate(o.date)} · {cat(o.category_id)}</span><span className="num">{money(o.amount_cents)}</span></li>)}</ul>
        </details>
      )}
    </section>
  )
}

/** Barras horizontales: este mes contra el anterior, por categoría. */
export function CategoryCompare({ report, categories, current }: { report: ReportSummary; categories: Category[]; current: string }) {
  const months = report.months
  const cur = months.find((m) => m.month === current) ?? months.at(-1)
  const idx = cur ? months.indexOf(cur) : -1
  const prev = idx > 0 ? months[idx - 1] : undefined
  if (!cur) return null
  const rows = categories.filter((c) => !c.parent_id).map((c) => ({
    id: c.id, name: c.name, now: cur.by_category.find((x) => x.id === c.id)?.spent_cents ?? 0, before: prev?.by_category.find((x) => x.id === c.id)?.spent_cents ?? 0,
  })).filter((r) => r.now !== 0 || r.before !== 0).sort((a, b) => b.now - a.now)
  const max = Math.max(1, ...rows.flatMap((r) => [Math.abs(r.now), Math.abs(r.before)]))
  return (
    <section className="panel report-card" aria-labelledby="h-compare">
      <div className="section-head"><div><h2 id="h-compare">Categorías: {monthLabel(cur.month).toLowerCase()} contra {prev ? monthLabel(prev.month).toLowerCase() : 'el mes anterior'}</h2><p className="small muted">Barra superior: este mes · inferior: mes anterior{cur.month === current ? ' (este mes va en curso)' : ''}</p></div></div>
      {rows.length === 0 ? <p className="small muted">Sin gastos en estos meses.</p> : (
        <>
          <div className="report-bars" aria-hidden="true">
            {rows.map((r) => (
              <div className="report-bar-row" key={r.id}>
                <span>{r.name}</span>
                <div className="report-bar-pair">
                  <div className={`report-bar ${r.now < 0 ? 'is-negative' : ''}`} style={{ width: `${Math.abs(r.now) / max * 100}%` }} />
                  <div className="report-bar is-previous" style={{ width: `${Math.abs(r.before) / max * 100}%` }} />
                </div>
              </div>
            ))}
          </div>
          <details className="more"><summary>Ver como tabla</summary>
            <div className="report-table-scroll" tabIndex={0}><table className="report-table report-forecast-table"><caption className="sr-only">Gasto por categoría este mes y el anterior</caption>
              <thead><tr><th scope="col">Categoría</th><th scope="col">{monthLabel(cur.month)}</th><th scope="col">{prev ? monthLabel(prev.month) : 'Mes anterior'}</th><th scope="col">Diferencia</th></tr></thead>
              <tbody>{rows.map((r) => <tr key={r.id}><th scope="row">{r.name}</th><td className="num">{money(r.now)}</td><td className="num">{money(r.before)}</td><td className="num">{r.now - r.before > 0 ? '+' : ''}{money(r.now - r.before)}</td></tr>)}</tbody>
            </table></div>
          </details>
        </>
      )}
    </section>
  )
}

/** Promedio por día de la semana del periodo, sin dividir entre cero. */
export function WeekdayChart({ report }: { report: ReportSummary }) {
  const rows = report.weekdays ?? []
  const max = Math.max(1, ...rows.map((w) => Math.abs(w.avg_cents)))
  const any = rows.some((w) => w.days > 0)
  return (
    <section className="panel report-card" aria-labelledby="h-weekday">
      <div className="section-head"><div><h2 id="h-weekday">Por día de la semana</h2><p className="small muted">Gasto promedio de cada día en el periodo</p></div></div>
      {!any ? <p className="small muted">El periodo todavía no tiene días transcurridos.</p> : (
        <>
          <div className="report-bars" aria-hidden="true">
            {rows.map((w) => (
              <div className="report-bar-row" key={w.weekday}><span>{weekdays[w.weekday]}</span><div className="report-bar-pair"><div className={`report-bar ${w.avg_cents < 0 ? 'is-negative' : ''}`} style={{ width: `${Math.abs(w.avg_cents) / max * 100}%` }} /></div></div>
            ))}
          </div>
          <details className="more"><summary>Ver como tabla</summary>
            <div className="report-table-scroll" tabIndex={0}><table className="report-table report-forecast-table"><caption className="sr-only">Gasto por día de la semana</caption>
              <thead><tr><th scope="col">Día</th><th scope="col">Total</th><th scope="col">Días</th><th scope="col">Promedio</th></tr></thead>
              <tbody>{rows.map((w) => <tr key={w.weekday}><th scope="row">{weekdays[w.weekday]}</th><td className="num">{money(w.spent_cents)}</td><td className="num">{w.days}</td><td className="num">{money(w.avg_cents)}</td></tr>)}</tbody>
            </table></div>
          </details>
        </>
      )}
    </section>
  )
}

/** Mapa de calor de las últimas 13 semanas. Con el teclado: flechas para moverse entre días. */
export function SpendingHeatmap({ report }: { report: ReportSummary }) {
  const days = report.days ?? []
  const [active, setActive] = useState(days.length - 1)
  if (days.length === 0) return null
  // Columnas por semana empezando en lunes: se rellenan los días previos al primero.
  const first = new Date(`${days[0].date}T12:00:00Z`).getUTCDay()
  const lead = (first + 6) % 7
  const max = Math.max(1, ...days.map((d) => d.spent_cents))
  const level = (v: number) => (v <= 0 ? 0 : Math.min(4, Math.ceil(v / max * 4)))
  const a = days[Math.max(0, Math.min(active, days.length - 1))]
  const move = (delta: number) => setActive((i) => Math.max(0, Math.min(days.length - 1, i + delta)))
  return (
    <section className="panel report-card" aria-labelledby="h-heatmap">
      <div className="section-head"><div><h2 id="h-heatmap">Gasto por día</h2><p className="small muted">Últimas 13 semanas · más oscuro, más gasto</p></div></div>
      <div className="report-heatmap-wrap" tabIndex={0} role="group" aria-label="Mapa de calor del gasto diario. Usa las flechas para recorrer los días."
        onKeyDown={(e) => {
          const step: Record<string, number> = { ArrowUp: -1, ArrowDown: 1, ArrowLeft: -7, ArrowRight: 7, Home: -days.length, End: days.length }
          if (e.key in step) {
            e.preventDefault()
            move(step[e.key])
          }
        }}>
        <div className="report-heatmap" aria-hidden="true">
          {Array.from({ length: lead }, (_, i) => <span key={`pad-${i}`} />)}
          {days.map((d, i) => <span key={d.date} className={`report-heatmap-cell ${i === active ? 'is-active' : ''}`} data-level={level(d.spent_cents)} onPointerEnter={() => setActive(i)} />)}
        </div>
      </div>
      <p className="small" aria-live="polite" style={{ marginTop: 10 }}><strong>{dayDate(a.date)}</strong>: <span className="num">{money(a.spent_cents)}</span></p>
      <details className="more"><summary>Ver como tabla</summary>
        <div className="report-table-scroll" tabIndex={0}><table className="report-table"><caption className="sr-only">Gasto de cada uno de los últimos 91 días</caption>
          <thead><tr><th scope="col">Fecha</th><th scope="col">Gasto</th></tr></thead>
          <tbody>{[...days].reverse().map((d) => <tr key={d.date}><th scope="row">{dayDate(d.date)}</th><td className="num">{money(d.spent_cents)}</td></tr>)}</tbody>
        </table></div>
      </details>
    </section>
  )
}

/** Evolución mensual por cuenta (las 5 con más gasto). */
export function AccountEvolution({ report, accounts }: { report: ReportSummary; accounts: Account[] }) {
  const totals = new Map<string, number>()
  report.months.forEach((m) => (m.by_account ?? []).forEach((a) => totals.set(a.id, (totals.get(a.id) ?? 0) + a.spent_cents)))
  const top = [...totals.entries()].sort((a, b) => b[1] - a[1]).slice(0, 5).map(([id]) => id)
  if (top.length === 0) return null
  const name = (id: string) => accounts.find((a) => a.id === id)?.name ?? 'Cuenta'
  const short = (m: string) => new Intl.DateTimeFormat('es-EC', { month: 'short', year: '2-digit', timeZone: 'UTC' }).format(new Date(`${m}-15T12:00:00Z`)).replace('.', '')
  const series: ReportSeries[] = top.map((id, i) => ({ name: name(id), tone: i, values: report.months.map((m) => m.by_account?.find((a) => a.id === id)?.spent_cents ?? 0) }))
  return (
    <section className="panel report-card" aria-labelledby="h-account-evo">
      <div className="section-head"><div><h2 id="h-account-evo">Evolución por cuenta</h2><p className="small muted">Gasto neto mensual de cada cuenta</p></div></div>
      <ReportLineChart label="Detalle del mes por cuenta" labels={report.months.map((m) => short(m.month))} series={series} />
      <details className="more"><summary>Ver como tabla</summary>
        <div className="report-table-scroll" tabIndex={0}><table className="report-table report-forecast-table"><caption className="sr-only">Gasto mensual por cuenta</caption>
          <thead><tr><th scope="col">Mes</th>{top.map((id) => <th key={id} scope="col">{name(id)}</th>)}</tr></thead>
          <tbody>{report.months.map((m, i) => <tr key={m.month}><th scope="row">{monthLabel(m.month)}</th>{series.map((s) => <td key={s.name} className="num">{money(s.values[i] ?? 0)}</td>)}</tr>)}</tbody>
        </table></div>
      </details>
    </section>
  )
}
