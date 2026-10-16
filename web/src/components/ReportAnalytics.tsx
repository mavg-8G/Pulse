import { useState } from 'react'
import { monthLabel } from '../lib/dates'
import { money } from '../lib/money'
import type { Category, ReportSummary } from '../lib/types'
import { ReportLineChart } from './ReportLineChart'

const shortMonth = (m: string) => new Intl.DateTimeFormat('es-EC', { month: 'short', year: '2-digit', timeZone: 'UTC' }).format(new Date(`${m}-15T12:00:00Z`)).replace('.', '')
const shortDate = (date: string) => new Intl.DateTimeFormat('es-EC', { day: 'numeric', month: 'short', timeZone: 'UTC' }).format(new Date(`${date}T12:00:00Z`))
const percent = (value: number) => new Intl.NumberFormat('es-EC', { maximumFractionDigits: 1 }).format(value)

function Metric({ label, value, detail }: { label: string; value: string; detail: string }) {
  return <div className="report-metric"><dt>{label}</dt><dd className="num">{value}</dd><dd className="report-metric-detail">{detail}</dd></div>
}

function CategorySelect({ value, onChange, categories, id }: { value: string; onChange: (value: string) => void; categories: Category[]; id: string }) {
  return <label className="report-category-select small muted" htmlFor={id}>Comparar categoría<select id={id} className="input" value={value} onChange={(e) => onChange(e.target.value)}><option value="">Solo total</option>{categories.filter((c) => !c.parent_id).map((c) => <option key={c.id} value={c.id}>{c.name}{c.archived ? ' (archivada)' : ''}</option>)}</select></label>
}

export function ReportOverview({ report: r, categories, current }: { report: ReportSummary; categories: Category[]; current: string }) {
  const [category, setCategory] = useState('')
  const name = categories.find((c) => c.id === category)?.name ?? 'Categoría'
  const closed = r.months.filter((m) => m.month < current)
  const last = closed.at(-1), previous = closed.at(-2)
  const change = last && previous && previous.spent_cents > 0 ? (last.spent_cents - previous.spent_cents) / previous.spent_cents * 100 : null
  const categoryValues = r.months.map((m) => m.by_category.find((c) => c.id === category)?.spent_cents ?? 0)
  return (
    <>
      <dl className="report-metrics" aria-label="Estadísticas del periodo">
        <Metric label="Gasto neto" value={money(r.total_cents)} detail="Compras menos devoluciones" />
        <Metric label="Promedio diario" value={money(r.elapsed_days > 0 ? Math.round(r.total_cents / r.elapsed_days) : 0)} detail={`${r.elapsed_days} días calendario del periodo`} />
        <Metric label="Compra promedio" value={money(r.purchase_count > 0 ? Math.round(r.purchase_cents / r.purchase_count) : 0)} detail={`${r.purchase_count} ${r.purchase_count === 1 ? 'compra registrada' : 'compras registradas'}`} />
        <Metric label="Cambio mensual" value={change === null ? 'Sin base' : `${change > 0 ? '+' : ''}${percent(change)}%`} detail={last && previous ? `${shortMonth(last.month)} vs. ${shortMonth(previous.month)} · meses cerrados` : 'Necesitas dos meses cerrados'} />
      </dl>
      <section className="panel report-card" aria-labelledby="h-months">
        <div className="section-head"><div><h2 id="h-months">Evolución del gasto</h2><p className="small muted">{monthLabel(r.from)} – {monthLabel(r.to)} · mes actual en curso</p></div><span className="num muted">Total {money(r.total_cents)}</span></div>
        <div className="report-chart-toolbar"><p className="small muted">Compras <strong className="num">{money(r.purchase_cents)}</strong> · Devoluciones <strong className="num">{money(r.refund_cents)}</strong></p><CategorySelect id="history-category" value={category} onChange={setCategory} categories={categories} /></div>
        <ReportLineChart label="Detalle del mes" labels={r.months.map((m) => shortMonth(m.month))} series={[{ name: 'Gasto del periodo', values: r.months.map((m) => m.spent_cents) }, ...(category ? [{ name, values: categoryValues, secondary: true }] : [])]} />
        <details className="more"><summary>Ver evolución como tabla</summary><div className="report-table-scroll"><table className="report-table"><caption className="sr-only">Gasto neto mensual del periodo seleccionado</caption><thead><tr><th scope="col">Mes</th><th scope="col">Total</th>{category && <th scope="col">{name}</th>}</tr></thead><tbody>{r.months.map((m, i) => <tr key={m.month}><th scope="row">{monthLabel(m.month)}{m.month === current && <span className="muted"> (en curso)</span>}</th><td className="num">{money(m.spent_cents)}</td>{category && <td className="num">{money(categoryValues[i])}</td>}</tr>)}</tbody></table></div></details>
        {r.purchase_count === 0 && <p className="report-note small muted">No hay compras registradas en este periodo.{r.refund_cents > 0 ? ' El saldo refleja las devoluciones.' : ' Agrega movimientos para empezar a ver tus tendencias.'}</p>}
      </section>
    </>
  )
}

const horizonMethod: Record<string, string> = {
  ritmo: 'Promedio diario',
  media_movil: 'Media móvil ponderada',
  dia_semana: 'Por día de la semana',
}
const horizonHint: Record<string, string> = {
  ritmo: 'promedio de compras menos devoluciones de los últimos días completos',
  media_movil: 'promedio con más peso a los días recientes (la mitad del peso en las últimas 3 semanas)',
  dia_semana: 'cuánto sueles gastar cada día de la semana, combinado con tu promedio general',
}

export function ForecastCard({ forecast: f, categories }: { forecast: ReportSummary['forecast']; categories: Category[] }) {
  const [horizon, setHorizon] = useState<'week' | 'month'>('week')
  const [category, setCategory] = useState('')
  const isWeek = horizon === 'week'
  const model = isWeek ? f.week_model : f.month_model
  const days = isWeek ? 7 : f.month_days
  const from = isWeek ? f.week_from : f.month_from, to = isWeek ? f.week_to : f.month_to
  const categoryName = (id: string) => categories.find((c) => c.id === id)?.name ?? 'Categoría'
  // Sin modelo (versión anterior del servidor) se usa el promedio diario.
  const rows = model ? model.by_category.map((c) => ({ id: c.id, cents: c.projected_cents, upcoming: c.upcoming_cents })) : f.by_category.map((c) => ({ id: c.id, cents: isWeek ? c.week_cents : c.month_cents, upcoming: 0 }))
  const total = model ? model.projected_cents : isWeek ? f.week_cents : f.month_cents
  const ranged = model && model.low_cents !== null && model.high_cents !== null
  const selected = rows.find((c) => c.id === category)
  const labels = Array.from({ length: days + 1 }, (_, i) => {
    if (i === 0) return 'Inicio'
    const date = new Date(`${from}T12:00:00Z`)
    date.setUTCDate(date.getUTCDate() + i - 1)
    return shortDate(date.toISOString().slice(0, 10))
  })
  const curve = model && model.cumulative.length === days ? [0, ...model.cumulative] : labels.map((_, i) => Math.round(total * i / days))
  const share = (cents: number) => curve.map((v) => (total > 0 ? Math.round(v * cents / total) : 0))
  const band = ranged && model.cumulative_low.length === days ? { name: 'rango del 95 %', low: [0, ...model.cumulative_low], high: [0, ...model.cumulative_high] } : undefined
  const chosen = model?.methods.find((m) => m.id === model.method)
  const covered = model?.coverage != null ? Math.round(model.coverage * model.backtests) : null
  return (
    <section className="panel report-card report-forecast" aria-labelledby="h-forecast">
      <div className="section-head"><div><span className="report-eyebrow">Mira hacia adelante</span><h2 id="h-forecast">Proyección de gastos</h2><p className="small muted">{shortDate(from)} – {shortDate(to)} · {isWeek ? 'próxima semana, de lunes a domingo' : monthLabel(from.slice(0, 7))}</p></div><div className="seg" role="group" aria-label="Horizonte de proyección"><button aria-pressed={isWeek} onClick={() => setHorizon('week')}>Próxima semana</button><button aria-pressed={!isWeek} onClick={() => setHorizon('month')}>Próximo mes</button></div></div>
      {!f.ready ? (
        <div className="report-forecast-empty"><h3>Aún falta historial para proyectar</h3><p className="muted">Necesitamos al menos 7 días completos desde tu primer movimiento dentro de los últimos 90 días. Por ahora hay {f.history_days} {f.history_days === 1 ? 'día disponible' : 'días disponibles'}.</p><p className="small muted">Las proyecciones aparecerán aquí para el total y cada categoría cuando haya suficiente historial.</p></div>
      ) : (
        <>
          <dl className="report-forecast-metrics">
            <Metric label={isWeek ? 'Total estimado · 7 días' : `Total estimado · ${days} días`} value={money(total)} detail={ranged ? `Rango del 95 %: ${money(model.low_cents!)} – ${money(model.high_cents!)}` : 'Aún sin rango: falta historial'} />
            <Metric label="Promedio diario estimado" value={money(Math.round(total / days))} detail={model && model.upcoming_cents > 0 ? `Incluye ${money(model.upcoming_cents)} de gastos fijos` : `Basado en ${model?.history_days ?? f.history_days} días completos`} />
            <Metric label="Mayor gasto previsto" value={rows[0] && rows[0].cents > 0 ? categoryName(rows[0].id) : 'Sin gasto neto'} detail={rows[0] ? money(rows[0].cents) : money(0)} />
          </dl>
          {model && (
            <p className="report-model small" data-testid="forecast-model">
              Método: <strong>{horizonMethod[model.method]}</strong> ({horizonHint[model.method]}).{' '}
              {model.backtests >= 3 && chosen?.mae_cents != null
                ? <>Se eligió porque fue el que menos se equivocó al proyectar {model.backtests} periodos pasados iguales (error medio {money(chosen.mae_cents)}).</>
                : <>Con poco historial se usa el promedio diario; cuando haya más periodos pasados se comparará con otros métodos.</>}
              {covered !== null && <> El gasto real cayó dentro del rango {covered} de {model.backtests} veces.</>}
            </p>
          )}
          <div className="report-chart-toolbar"><div><h3>Gasto acumulado estimado</h3><p className="small muted">La línea punteada es la estimación{band ? '; la banda, el rango del 95 %' : ''}.</p></div><CategorySelect id="forecast-category" value={category} onChange={setCategory} categories={categories} /></div>
          <ReportLineChart key={horizon} label="Detalle de la proyección" projected labels={labels} series={[{ name: 'Total estimado', values: curve, dashed: !!band, band }, ...(selected ? [{ name: categoryName(category), values: share(selected.cents), secondary: true }] : [])]} />
          <div className="report-table-scroll" tabIndex={0}><table className="report-table report-forecast-table"><caption>Proyección por categoría · {isWeek ? 'próxima semana' : 'próximo mes'}</caption><thead><tr><th scope="col">Categoría</th><th scope="col">Estimado</th><th scope="col">Gastos fijos</th><th scope="col">Del total</th></tr></thead><tbody>{rows.filter((c) => c.cents !== 0 || c.upcoming !== 0).map((c) => <tr key={c.id}><th scope="row">{categoryName(c.id)}</th><td className="num">{money(c.cents)}</td><td className="num muted">{c.upcoming ? money(c.upcoming) : '—'}</td><td className="num muted">{total > 0 ? percent(c.cents / total * 100) : '0'}%</td></tr>)}</tbody><tfoot><tr><th scope="row">Total estimado</th><td className="num">{money(total)}</td><td className="num">{model?.upcoming_cents ? money(model.upcoming_cents) : '—'}</td><td className="num">{total > 0 ? '100' : '0'}%</td></tr></tfoot></table></div>
          {model && model.methods.length > 1 && (
            <details className="more"><summary>Comparar métodos</summary><div className="report-table-scroll" tabIndex={0}><table className="report-table report-forecast-table"><caption className="sr-only">Estimación de cada método y su error medio en periodos pasados</caption><thead><tr><th scope="col">Método</th><th scope="col">Estimado</th><th scope="col">Error medio</th></tr></thead><tbody>{model.methods.map((m) => <tr key={m.id} className={m.id === model.method ? 'report-chosen' : ''}><th scope="row">{horizonMethod[m.id]}{m.id === model.method ? ' (elegido)' : ''}</th><td className="num">{m.available ? money(m.projected_cents) : 'No disponible'}</td><td className="num">{m.mae_cents === null ? '—' : money(m.mae_cents)}</td></tr>)}</tbody></table></div></details>
          )}
          <p className="report-note small muted">Base: hasta {model?.history_days ?? f.history_days} días completos ({shortDate(f.history_from)} – {shortDate(f.history_to)}), incluyendo días sin gasto. Los gastos fijos se suman en su fecha y no se reparten por día{model?.outliers ? `; ${model.outliers === 1 ? 'una compra atípica reciente no se repite' : `${model.outliers} compras atípicas recientes no se repiten`} en la estimación` : ''}. El rango sale de simular el periodo miles de veces con tus días reales: es una probabilidad, no una garantía.</p>
          {f.history_days < 28 && <p className="small st-warning">Historial limitado: la estimación puede variar bastante al registrar más movimientos.</p>}
        </>
      )}
    </section>
  )
}
