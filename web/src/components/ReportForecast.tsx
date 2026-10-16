import { useState } from 'react'
import { monthLabel } from '../lib/dates'
import { money } from '../lib/money'
import type { Category, Confidence, HorizonModel, Projection, ReportSummary } from '../lib/types'
import { ReportLineChart, type ReportSeries } from './ReportLineChart'

// Proyecciones: cierre del mes en curso, próxima semana y próximo mes, con el método elegido explicado en simple.

const names: Record<string, string> = {
  promedio_reciente: 'Promedio diario reciente',
  regresion: 'Regresión lineal',
  robusto: 'Promedio robusto',
  media_movil: 'Media móvil ponderada',
  dia_semana: 'Por día de la semana',
  perfil: 'Estacionalidad del mes',
  holt: 'Tendencia amortiguada',
  holt_winters: 'Tendencia y ajuste semanal',
  combinado: 'Combinación de métodos',
}
const hints: Record<string, string> = {
  promedio_reciente: 'suma lo ya gastado, los gastos fijos programados y tu promedio diario de los últimos 90 días para los días que faltan',
  regresion: 'ajusta una recta al gasto acumulado del mes',
  robusto: 'promedia tus semanas recientes quitando peso a las que tuvieron gastos excepcionales',
  media_movil: 'da más peso a los días recientes',
  dia_semana: 'usa cuánto sueles gastar cada día de la semana',
  perfil: 'usa qué parte del mes sueles llevar gastada a esta altura',
  holt: 'sigue tu nivel de gasto semanal y su tendencia, que se va apagando para no exagerarla',
  holt_winters: 'sigue tu nivel de gasto, su tendencia y la diferencia entre días de la semana',
  combinado: 'promedia varios métodos para compensar sus errores',
}
export const methodLabel = (id: string, month: boolean) => (id === 'ritmo' ? (month ? 'Ritmo del mes' : 'Promedio diario') : names[id] ?? id)
const methodHint = (id: string, month: boolean) =>
  id === 'ritmo' ? (month ? 'divide lo gastado este mes entre los días transcurridos y lo multiplica por los días del mes' : 'promedia lo que gastaste por día en los últimos 90 días completos') : hints[id] ?? ''

const warningText: Record<string, string> = {
  sin_datos: 'Aún no hay movimientos para proyectar.',
  sin_historial: 'Hacen falta al menos 7 días completos de historial.',
  pocos_dias: 'Con menos de 5 días del mes, el cierre puede cambiar bastante.',
  historial_corto: 'Hay menos de 4 semanas de historial: la estimación puede variar mucho al registrar más gastos.',
  sin_validacion: 'Todavía no hay suficientes periodos pasados para comparar métodos: se usa el promedio diario, sin tendencias.',
  sin_meses_cerrados: 'Hacen falta 2 meses cerrados para comparar métodos: se usa una estimación prudente (lo gastado, los fijos y tu promedio diario).',
  pocos_meses_perfil: 'La estacionalidad del mes necesita 3 meses cerrados.',
  sin_rango: 'Sin historial suficiente para calcular un rango.',
}
const confidenceText: Record<Confidence, string> = { alta: 'Confianza alta', media: 'Confianza media', baja: 'Confianza baja' }

const shortDate = (date: string) => new Intl.DateTimeFormat('es-EC', { day: 'numeric', month: 'short', timeZone: 'UTC' }).format(new Date(`${date}T12:00:00Z`)).replace('.', '')
const longDate = (date: string) => new Intl.DateTimeFormat('es-EC', { weekday: 'short', day: 'numeric', month: 'short', timeZone: 'UTC' }).format(new Date(`${date}T12:00:00Z`))
const addDays = (date: string, n: number) => {
  const d = new Date(`${date}T12:00:00Z`)
  d.setUTCDate(d.getUTCDate() + n)
  return d.toISOString().slice(0, 10)
}
const pct = (v: number) => `${new Intl.NumberFormat('es-EC', { maximumFractionDigits: 1 }).format(v)} %`

interface Row { id: string; spent?: number; upcoming: number; cents: number }
interface View {
  title: string
  subtitle: string
  total: number
  low: number | null
  high: number | null
  upcoming: number
  method: string
  month: boolean
  backtests: number
  typical: number | null
  improvement: number | null
  coverage: number | null
  confidence: Confidence
  warnings: string[]
  rows: Row[]
  methods: { id: string; available: boolean; projected_cents: number; mae_cents: number | null; candidate?: boolean }[]
  chart: { labels: string[]; details: string[]; series: ReportSeries[]; initial?: number }
  note: string
}

function towards(from: number, to: number, start: number, end: number, i: number) {
  return end === start ? to : Math.round(from + ((to - from) * (i - start)) / (end - start))
}

function closeView(p: Projection): View {
  const D = p.days_in_month
  const labels = Array.from({ length: D }, (_, i) => String(i + 1))
  const details = labels.map((d) => `Día ${d} de ${monthLabel(p.month).toLowerCase()}`)
  const today = p.day - 1
  const spentNow = p.cumulative.at(-1) ?? 0
  const real: (number | null)[] = labels.map((_, i) => (i <= today ? p.cumulative[i] : null))
  const proj: (number | null)[] = labels.map((_, i) => (i >= today ? towards(spentNow, p.projected_cents, today, D - 1, i) : null))
  const band = p.low_cents !== null && p.high_cents !== null
    ? { name: 'rango 95 %', low: labels.map((_, i) => (i >= today ? towards(spentNow, p.low_cents!, today, D - 1, i) : null)), high: labels.map((_, i) => (i >= today ? towards(spentNow, p.high_cents!, today, D - 1, i) : null)) }
    : undefined
  const prev = labels.map((_, i) => p.previous_cumulative[Math.min(i, p.previous_cumulative.length - 1)] ?? null)
  return {
    title: `Cierre de ${monthLabel(p.month).toLowerCase()}`,
    subtitle: `Día ${p.day} de ${D} · gastado ${money(p.spent_cents)}${p.upcoming_cents > 0 ? ` · fijos por cobrar ${money(p.upcoming_cents)}` : ''}`,
    total: p.projected_cents,
    low: p.low_cents,
    high: p.high_cents,
    upcoming: p.upcoming_cents,
    method: p.method,
    month: true,
    backtests: p.backtests ?? p.accuracy.length,
    typical: p.typical_error_pct ?? null,
    improvement: p.improvement_pct ?? null,
    coverage: p.coverage ?? null,
    confidence: p.confidence ?? (p.accuracy.length >= 2 ? 'media' : 'baja'),
    warnings: p.warnings,
    rows: p.by_category.map((c) => ({ id: c.id, spent: c.spent_cents, upcoming: c.upcoming_cents, cents: c.projected_cents })),
    methods: p.methods,
    chart: { labels, details, initial: today, series: [{ name: 'Gastado', values: real }, { name: 'Proyección', values: proj, dashed: true, band }, { name: monthLabel(p.previous_month), values: prev, secondary: true }] },
    note: 'Cada método se prueba en los meses cerrados como si fuera hoy, con lo que se sabía ese día. Las compras atípicas y los gastos fijos no se extrapolan: los fijos programados se suman en su fecha.',
  }
}

function horizonView(m: HorizonModel, week: boolean): View {
  const labels = ['Hoy', ...Array.from({ length: m.days }, (_, i) => shortDate(addDays(m.from, i)))]
  const details = ['Antes del periodo', ...Array.from({ length: m.days }, (_, i) => longDate(addDays(m.from, i)))]
  const ranged = m.low_cents !== null && m.cumulative_low.length === m.days
  const curve = m.cumulative.length === m.days ? [0, ...m.cumulative] : labels.map((_, i) => Math.round((m.projected_cents * i) / m.days))
  return {
    title: week ? 'Próxima semana' : `Próximo mes · ${monthLabel(m.from.slice(0, 7)).toLowerCase()}`,
    subtitle: `${shortDate(m.from)} – ${shortDate(m.to)} · ${m.days} días`,
    total: m.projected_cents,
    low: m.low_cents,
    high: m.high_cents,
    upcoming: m.upcoming_cents,
    method: m.method,
    month: false,
    backtests: m.backtests,
    typical: m.typical_error_pct ?? null,
    improvement: m.improvement_pct ?? null,
    coverage: m.coverage,
    confidence: m.confidence ?? (m.backtests >= 3 ? 'media' : 'baja'),
    warnings: m.warnings ?? [],
    rows: m.by_category.filter((c) => c.projected_cents !== 0 || c.upcoming_cents !== 0).map((c) => ({ id: c.id, upcoming: c.upcoming_cents, cents: c.projected_cents })),
    methods: m.methods,
    chart: { labels, details, series: [{ name: 'Gasto estimado', values: curve, dashed: true, band: ranged ? { name: 'rango 95 %', low: [0, ...m.cumulative_low], high: [0, ...m.cumulative_high] } : undefined }] },
    note: `Base: ${m.history_days} días completos de historial, incluidos los días sin gasto. El rango sale de simular el periodo miles de veces con tus días reales${m.outliers ? `; ${m.outliers === 1 ? 'una compra atípica reciente no se repite' : `${m.outliers} compras atípicas recientes no se repiten`}` : ''}.`,
  }
}

type Tab = 'cierre' | 'semana' | 'mes'

export function ForecastSection({ projection, projectionError, projectionLoading, forecast, categories, scope }: { projection?: Projection; projectionError?: string; projectionLoading: boolean; forecast: ReportSummary['forecast']; categories: Category[]; scope: string }) {
  const [tab, setTab] = useState<Tab>('cierre')
  const name = (id: string) => categories.find((c) => c.id === id)?.name ?? (id ? 'Categoría' : 'Sin categoría')
  const model = tab === 'semana' ? forecast.week_model : tab === 'mes' ? forecast.month_model : null
  const view = tab === 'cierre' ? (projection ? closeView(projection) : null) : model && model.history_days >= 7 ? horizonView(model, tab === 'semana') : null
  return (
    <section className="panel report-card report-forecast" aria-labelledby="h-forecast" id="proyecciones">
      <div className="section-head">
        <div><span className="report-eyebrow">Mira hacia adelante{scope ? ` · ${scope}` : ''}</span><h2 id="h-forecast">Proyecciones</h2><p className="small muted">Estimaciones con tu historial, no resultados garantizados.</p></div>
        <div className="seg report-forecast-tabs" role="group" aria-label="Horizonte de proyección">
          {(['cierre', 'semana', 'mes'] as Tab[]).map((t) => <button key={t} aria-pressed={tab === t} onClick={() => setTab(t)}>{t === 'cierre' ? 'Cierre del mes' : t === 'semana' ? 'Próxima semana' : 'Próximo mes'}</button>)}
        </div>
      </div>
      {tab === 'cierre' && projectionLoading && <p className="loading" role="status">Calculando proyección…</p>}
      {tab === 'cierre' && projectionError && <p className="error" role="alert">No se pudo calcular el cierre del mes: {projectionError}</p>}
      {tab !== 'cierre' && !view && (
        <div className="report-forecast-empty"><h3>Aún falta historial para proyectar</h3><p className="muted">Hacen falta al menos 7 días completos con movimientos en los últimos 90 días. Por ahora hay {forecast.history_days} {forecast.history_days === 1 ? 'día' : 'días'}.</p></div>
      )}
      {view && <ForecastView key={tab} v={view} name={name} />}
    </section>
  )
}

function ForecastView({ v, name }: { v: View; name: (id: string) => string }) {
  const ranged = v.low !== null && v.high !== null
  const evaluated = v.backtests >= (v.month ? 2 : 3) && !v.warnings.includes('sin_validacion') && !v.warnings.includes('sin_meses_cerrados')
  const covered = v.coverage !== null ? Math.round(v.coverage * v.backtests) : null
  const top = [...v.rows].sort((a, b) => b.cents - a.cents)[0]
  return (
    <>
      <h3 className="report-forecast-title">{v.title} <span className="small muted">{v.subtitle}</span></h3>
      <dl className="report-forecast-metrics">
        <div className="report-metric is-primary"><dt>Total estimado</dt><dd className="num" data-testid="forecast-total">{money(v.total)}</dd>
          <dd className="report-metric-detail">{ranged ? <>Rango probable (95 %): <span className="num">{money(v.low!)} – {money(v.high!)}</span></> : 'Sin rango: falta historial'}</dd></div>
        <div className="report-metric"><dt>Precisión</dt><dd>{v.typical !== null && evaluated ? <span className="num">± {pct(v.typical)}</span> : 'Sin medir'}</dd>
          <dd className="report-metric-detail">{v.typical !== null && evaluated ? `Error típico en ${v.backtests} ${v.month ? 'meses' : 'periodos'} pasados${covered !== null ? ` · el gasto real cayó en el rango ${covered} de ${v.backtests} veces` : ''}` : 'Aparecerá cuando haya periodos pasados para comprobarla'}</dd></div>
        <div className="report-metric"><dt>Mayor gasto previsto</dt><dd>{top && top.cents > 0 ? name(top.id) : 'Sin gasto previsto'}</dd>
          <dd className="report-metric-detail">{top && top.cents > 0 ? `${money(top.cents)}${top.upcoming > 0 ? `, de los que ${money(top.upcoming)} son gastos fijos programados` : ''}` : '—'}</dd></div>
      </dl>
      <p className="report-model small" data-testid="forecast-model">
        <span className={`report-confidence is-${v.confidence}`}>{confidenceText[v.confidence]}</span>{' '}
        Método: <strong>{methodLabel(v.method, v.month)}</strong>, que {methodHint(v.method, v.month)}.{' '}
        {evaluated
          ? <>Se eligió porque, al predecir {v.backtests} {v.month ? 'meses' : 'periodos'} pasados usando solo lo que se sabía antes de cada uno, fue el que menos se equivocó{v.improvement !== null && v.improvement > 0 ? ` (un ${pct(v.improvement)} menos que el ${v.month ? 'ritmo del mes' : 'promedio diario'})` : v.method === 'ritmo' ? '; ningún método más complejo lo superó con claridad' : ''}.</>
          : <>Con poco historial no se comparan métodos: es una estimación prudente y puede cambiar bastante.</>}
      </p>
      {v.warnings.filter((w) => warningText[w]).length > 0 && <ul className="report-warnings small">{v.warnings.filter((w) => warningText[w]).map((w) => <li key={w}>{warningText[w]}</li>)}</ul>}
      <ReportLineChart label="Detalle del día" labels={v.chart.labels} details={v.chart.details} series={v.chart.series} initial={v.chart.initial} />
      <details className="more" open={false}><summary>Por categoría ({v.rows.length})</summary>
        <div className="report-table-scroll" tabIndex={0}><table className="report-table report-forecast-table"><caption className="sr-only">Proyección por categoría</caption>
          <thead><tr><th scope="col">Categoría</th>{v.month && <th scope="col">Gastado</th>}<th scope="col">Fijos</th><th scope="col">Estimado</th><th scope="col">Del total</th></tr></thead>
          <tbody>{v.rows.map((c) => <tr key={c.id || 'none'}><th scope="row">{name(c.id)}</th>{v.month && <td className="num">{money(c.spent ?? 0)}</td>}<td className="num muted">{c.upcoming ? money(c.upcoming) : '—'}</td><td className="num">{money(c.cents)}</td><td className="num muted">{v.total > 0 ? pct((c.cents / v.total) * 100) : '—'}</td></tr>)}</tbody>
          <tfoot><tr><th scope="row">Total</th>{v.month && <td className="num">{money(v.rows.reduce((t, c) => t + (c.spent ?? 0), 0))}</td>}<td className="num">{v.upcoming ? money(v.upcoming) : '—'}</td><td className="num">{money(v.total)}</td><td className="num">{v.total > 0 ? '100 %' : '—'}</td></tr></tfoot>
        </table></div>
      </details>
      <details className="more"><summary>Comparar métodos</summary>
        <div className="report-table-scroll" tabIndex={0}><table className="report-table report-forecast-table"><caption className="sr-only">Estimación de cada método y su error medio al predecir periodos pasados</caption>
          <thead><tr><th scope="col">Método</th><th scope="col">Estimado</th><th scope="col">Error medio</th></tr></thead>
          <tbody>{v.methods.map((m) => <tr key={m.id} className={m.id === v.method ? 'report-chosen' : ''}><th scope="row">{methodLabel(m.id, v.month)}{m.id === v.method ? ' (elegido)' : m.candidate === false && m.available ? ' (sin historial para compararlo)' : ''}</th><td className="num">{m.available ? money(m.projected_cents) : 'No disponible'}</td><td className="num">{m.mae_cents === null ? '—' : money(m.mae_cents)}</td></tr>)}</tbody>
        </table></div>
        <p className="small muted">Ningún método se elige por ser más complejo: uno más elaborado solo reemplaza al más simple si se equivoca al menos un 10 % menos y acierta mejor en la mayoría de los periodos.</p>
      </details>
      <p className="report-note small muted">{v.note}</p>
    </>
  )
}
