import { useState } from 'react'
import { monthLabel } from '../lib/dates'
import { money } from '../lib/money'
import { change, insights, signedMoney, signedPct } from '../lib/reports'
import type { Category, Projection, ReportSummary } from '../lib/types'
import { ReportLineChart, type ReportSeries } from './ReportLineChart'

const shortMonth = (m: string) => new Intl.DateTimeFormat('es-EC', { month: 'short', year: '2-digit', timeZone: 'UTC' }).format(new Date(`${m}-15T12:00:00Z`)).replace('.', '')
export const shortDate = (date: string) => new Intl.DateTimeFormat('es-EC', { day: 'numeric', month: 'short', timeZone: 'UTC' }).format(new Date(`${date}T12:00:00Z`)).replace('.', '')
export const fullDate = (date: string) => new Intl.DateTimeFormat('es-EC', { day: 'numeric', month: 'short', year: 'numeric', timeZone: 'UTC' }).format(new Date(`${date}T12:00:00Z`)).replace('.', '')
const longDate = (date: string) => new Intl.DateTimeFormat('es-EC', { weekday: 'long', day: 'numeric', month: 'long', timeZone: 'UTC' }).format(new Date(`${date}T12:00:00Z`))

/** Diferencia contra el periodo anterior: importe siempre que haya datos y porcentaje solo si es comparable. */
export function Delta({ cur, prev, coverage, label = 'vs. periodo anterior' }: { cur: number; prev: number; coverage?: string; label?: string }) {
  if (!coverage || coverage === 'sin_datos') return <span className="report-delta is-none">Sin datos para comparar</span>
  const diff = cur - prev
  const p = change(cur, prev, coverage === 'completo')
  const tone = diff > 0 ? 'is-up' : diff < 0 ? 'is-down' : 'is-flat'
  return (
    <span className={`report-delta ${tone}`}>
      <span aria-hidden="true">{diff > 0 ? '▲' : diff < 0 ? '▼' : '='}</span> <span className="num">{signedMoney(diff)}</span>{p !== null && <span className="num"> ({signedPct(p)})</span>} {label && <span className="muted">{label}</span>}
    </span>
  )
}

function Metric({ label, value, children }: { label: string; value: string; children?: React.ReactNode }) {
  return <div className="report-metric"><dt>{label}</dt><dd className="num">{value}</dd><dd className="report-metric-detail">{children}</dd></div>
}

export function SummarySection({ report: r, categories, compareName }: { report: ReportSummary; categories: Category[]; compareName: string }) {
  const prev = r.previous
  const days = r.period?.days ?? r.elapsed_days
  const daily = days > 0 ? Math.round(r.total_cents / days) : 0
  const prevDaily = prev && prev.days > 0 ? Math.round(prev.total_cents / prev.days) : 0
  const name = (id: string) => categories.find((c) => c.id === id)?.name ?? 'Categoría'
  const list = insights(r, name)
  return (
    <section aria-labelledby="h-summary" id="resumen" className="report-section">
      <h2 id="h-summary" className="report-section-title">Resumen del periodo</h2>
      <dl className="report-metrics">
        <Metric label="Gasto neto" value={money(r.total_cents)}><Delta cur={r.total_cents} prev={prev?.total_cents ?? 0} coverage={prev?.coverage} /></Metric>
        <Metric label="Compras" value={money(r.purchase_cents)}>
          {r.purchase_count} {r.purchase_count === 1 ? 'compra' : 'compras'}{r.purchase_count > 0 && ` · promedio ${money(Math.round(r.purchase_cents / r.purchase_count))}`}<br />
          <Delta cur={r.purchase_cents} prev={prev?.purchase_cents ?? 0} coverage={prev?.coverage} label="" />
        </Metric>
        <Metric label="Devoluciones" value={money(r.refund_cents)}>
          {r.purchase_cents > 0 ? `${Math.round((r.refund_cents / r.purchase_cents) * 100)} % de lo comprado` : 'Sin compras en el periodo'}<br />
          <Delta cur={r.refund_cents} prev={prev?.refund_cents ?? 0} coverage={prev?.coverage} label="" />
        </Metric>
        <Metric label="Promedio diario" value={money(daily)}>{days} {days === 1 ? 'día' : 'días'} · <Delta cur={daily} prev={prevDaily} coverage={prev?.coverage} label="" /></Metric>
      </dl>
      {prev && <p className="small muted report-compare-line">Comparado con {compareName}: {fullDate(prev.from)} – {fullDate(prev.to)} ({prev.days} días){prev.coverage === 'parcial' ? '. Empezaste a registrar a mitad de ese periodo: se muestran importes, no porcentajes.' : prev.coverage === 'sin_datos' ? '. No hay movimientos registrados en esas fechas.' : '.'}</p>}
      <section className="panel report-card report-insights" aria-labelledby="h-insights">
        <h3 id="h-insights">Conclusiones</h3>
        {list.length === 0 ? <p className="small muted">Todavía no hay suficientes movimientos para sacar conclusiones de este periodo.</p> : (
          <ul>{list.map((x, i) => <li key={i} className={`is-${x.tone}`}><span aria-hidden="true">{x.tone === 'up' ? '▲' : x.tone === 'down' ? '▼' : '•'}</span>{x.text}</li>)}</ul>
        )}
      </section>
    </section>
  )
}

/** Evolución del periodo contra el anterior, por tramo o acumulada; con el cierre estimado del mes en curso. */
export function EvolutionSection({ report: r, projection, current, compareName }: { report: ReportSummary; projection?: Projection; current: string; compareName: string }) {
  const series = r.series ?? []
  const g = r.period?.granularity ?? 'month'
  const [mode, setMode] = useState<'tramo' | 'acumulado'>(g === 'day' ? 'acumulado' : 'tramo')
  if (series.length === 0) return null
  const labels = series.map((p) => (g === 'month' ? shortMonth(p.start.slice(0, 7)) : shortDate(p.start)))
  const details = series.map((p) => (g === 'month' ? `${monthLabel(p.start.slice(0, 7))}${p.start.slice(0, 7) === current ? ' (en curso)' : ''}` : g === 'week' ? `Semana del ${shortDate(p.start)} al ${shortDate(p.end)}` : longDate(p.start)))
  const acc = (v: number[]) => v.reduce<number[]>((out, x) => [...out, (out.at(-1) ?? 0) + x], [])
  const net = series.map((p) => p.net_cents)
  const prevVals = series.map((p) => p.previous_cents)
  const cur = mode === 'acumulado' ? acc(net) : net
  const before = mode === 'acumulado' ? acc(prevVals) : prevVals
  const hasPrev = r.previous && r.previous.coverage !== 'sin_datos'
  const s: ReportSeries[] = [{ name: 'Periodo actual', values: cur }]
  if (hasPrev) s.push({ name: r.period?.compare === 'year' ? 'Año anterior' : 'Periodo anterior', values: before, secondary: true })
  // El mes en curso por meses: un tramo punteado hasta el cierre estimado, con su rango.
  const last = series.length - 1
  const projected = projection && g === 'month' && mode === 'tramo' && series[last].start.slice(0, 7) === current && projection.month === current && last > 0
  if (projected) {
    const values: (number | null)[] = series.map(() => null)
    values[last - 1] = cur[last - 1]
    values[last] = projection!.projected_cents
    const band = projection!.low_cents !== null && projection!.high_cents !== null
      ? { name: 'rango 95 %', low: values.map((v, i) => (i === last ? projection!.low_cents : v)), high: values.map((v, i) => (i === last ? projection!.high_cents : v)) }
      : undefined
    s.push({ name: 'Cierre estimado del mes', values, dashed: true, band })
  }
  const granularity = g === 'day' ? 'por día' : g === 'week' ? 'por semana' : 'por mes'
  return (
    <section className="panel report-card" aria-labelledby="h-evolution" id="evolucion">
      <div className="section-head">
        <div><h2 id="h-evolution">Evolución del gasto</h2><p className="small muted">Gasto neto {granularity}{mode === 'acumulado' ? ', acumulado' : ''}{hasPrev ? `, junto ${compareName.replace(/^el /, 'al ')}` : ''}.</p></div>
        <div className="seg" role="group" aria-label="Forma de ver la evolución">
          <button aria-pressed={mode === 'tramo'} onClick={() => setMode('tramo')}>{g === 'day' ? 'Por día' : g === 'week' ? 'Por semana' : 'Por mes'}</button>
          <button aria-pressed={mode === 'acumulado'} onClick={() => setMode('acumulado')}>Acumulado</button>
        </div>
      </div>
      <ReportLineChart key={mode} label="Detalle del tramo" labels={labels} details={details} series={s} />
      {projected && <p className="small muted">La línea punteada es una estimación del cierre de este mes, no un gasto real.</p>}
      <details className="more"><summary>Ver como tabla</summary>
        <div className="report-table-scroll" tabIndex={0}><table className="report-table report-forecast-table"><caption className="sr-only">Gasto neto del periodo {granularity}{hasPrev ? ` y ${compareName}` : ''}</caption>
          <thead><tr><th scope="col">Tramo</th><th scope="col">Compras</th><th scope="col">Devoluciones</th><th scope="col">Neto</th>{hasPrev && <th scope="col">Anterior</th>}</tr></thead>
          <tbody>{series.map((p, i) => <tr key={p.start}><th scope="row">{details[i]}</th><td className="num">{money(p.purchase_cents)}</td><td className="num">{money(p.refund_cents)}</td><td className="num">{money(p.net_cents)}</td>{hasPrev && <td className="num muted">{money(p.previous_cents)}</td>}</tr>)}</tbody>
          <tfoot><tr><th scope="row">Total</th><td className="num">{money(r.purchase_cents)}</td><td className="num">{money(r.refund_cents)}</td><td className="num">{money(r.total_cents)}</td>{hasPrev && <td className="num">{money(r.previous!.total_cents)}</td>}</tr></tfoot>
        </table></div>
      </details>
    </section>
  )
}

/** Categorías del periodo: peso, cambio contra el anterior y acceso directo a filtrar por una. */
export function CategoriesSection({ report: r, categories, onFilter, filtered }: { report: ReportSummary; categories: Category[]; onFilter: (id: string) => void; filtered: boolean }) {
  const [all, setAll] = useState(false)
  const cat = (id: string) => categories.find((c) => c.id === id)
  const changes = new Map((r.category_changes ?? []).map((c) => [c.id, c]))
  const positive = r.by_category.reduce((t, x) => t + Math.max(0, x.spent_cents), 0)
  const rows = r.by_category
  const shown = all ? rows : rows.slice(0, 6)
  const coverage = r.previous?.coverage
  return (
    <section className="panel report-card" aria-labelledby="h-rcat" id="categorias">
      <div className="section-head"><div><h2 id="h-rcat">Categorías</h2><p className="small muted">En qué se fue tu gasto y cómo cambió{filtered ? '' : '. Toca una para ver solo esa categoría'}.</p></div></div>
      {rows.length === 0 ? <p className="muted small">Sin gastos en este periodo.</p> : (
        <>
          <ul className="report-category-list">
            {shown.map((x) => {
              const c = cat(x.id)
              const ch = changes.get(x.id)
              const share = positive > 0 ? Math.max(0, Math.round((x.spent_cents / positive) * 100)) : 0
              return (
                <li key={x.id}>
                  <button type="button" className="report-category-row" onClick={() => onFilter(x.id)} disabled={filtered} aria-label={`${c?.name ?? 'Categoría'}: ${money(x.spent_cents)}, ${share} % del gasto${filtered ? '' : '. Filtrar por esta categoría'}`}>
                    <span className="category-symbol" style={{ background: `color-mix(in srgb, ${c?.color ?? '#888'} 16%, transparent)` }} aria-hidden="true">{c?.icon || '•'}</span>
                    <span className="grow">
                      <span className="report-category-top"><strong>{c?.name ?? 'Categoría'}</strong><span className="num">{money(x.spent_cents)}</span></span>
                      <span className="track" aria-hidden="true"><span className="fill" style={{ width: `${share}%`, background: c?.color ?? 'var(--accent)' }} /></span>
                      <span className="report-category-meta small"><span className="muted">{share} % del gasto</span>{ch && <Delta cur={ch.current_cents} prev={ch.previous_cents} coverage={coverage} label="" />}</span>
                    </span>
                  </button>
                </li>
              )
            })}
          </ul>
          {rows.length > 6 && <button type="button" className="btn btn-sm btn-ghost" onClick={() => setAll((v) => !v)} aria-expanded={all}>{all ? 'Ver menos' : `Ver todas (${rows.length})`}</button>}
          <details className="more"><summary>Ver como tabla</summary>
            <div className="report-table-scroll" tabIndex={0}><table className="report-table report-forecast-table"><caption className="sr-only">Gasto por categoría en el periodo y en el anterior</caption>
              <thead><tr><th scope="col">Categoría</th><th scope="col">Periodo</th><th scope="col">Anterior</th><th scope="col">Diferencia</th><th scope="col">Cambio</th></tr></thead>
              <tbody>{(r.category_changes ?? []).map((c) => <tr key={c.id}><th scope="row">{cat(c.id)?.name ?? 'Categoría'}</th><td className="num">{money(c.current_cents)}</td><td className="num">{money(c.previous_cents)}</td><td className="num">{signedMoney(c.diff_cents)}</td><td className="num">{c.pct === null ? '—' : signedPct(c.pct)}</td></tr>)}</tbody>
            </table></div>
          </details>
        </>
      )}
    </section>
  )
}

export function MerchantsCard({ report: r }: { report: ReportSummary }) {
  const list = r.merchants ?? []
  const coverage = r.previous?.coverage
  return (
    <section className="panel report-card" aria-labelledby="h-merchants">
      <div className="section-head"><div><h2 id="h-merchants">Comercios con más gasto</h2><p className="small muted">Gasto neto en cada comercio durante el periodo.</p></div></div>
      {list.length === 0 ? <p className="small muted">Sin compras con comercio registrado en este periodo.</p> : (
        <ol className="report-merchants">
          {list.map((m) => (
            <li key={m.name}>
              <span className="grow"><strong>{m.name}</strong><span className="small muted"> · {m.purchase_count} {m.purchase_count === 1 ? 'compra' : 'compras'}</span><br /><Delta cur={m.spent_cents} prev={m.previous_cents} coverage={coverage} label="" /></span>
              <span className="num">{money(m.spent_cents)}</span>
            </li>
          ))}
        </ol>
      )}
    </section>
  )
}
