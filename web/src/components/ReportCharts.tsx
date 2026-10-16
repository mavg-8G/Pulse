import { useState } from 'react'
import { monthLabel } from '../lib/dates'
import { money } from '../lib/money'
import type { Account, ReportSummary } from '../lib/types'
import { ReportLineChart, type ReportSeries } from './ReportLineChart'

const weekdays = ['Lunes', 'Martes', 'Miércoles', 'Jueves', 'Viernes', 'Sábado', 'Domingo']
const dayDate = (d: string) => new Intl.DateTimeFormat('es-EC', { weekday: 'short', day: 'numeric', month: 'short', timeZone: 'UTC' }).format(new Date(`${d}T12:00:00Z`))

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
