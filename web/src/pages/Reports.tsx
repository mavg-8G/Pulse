import { useEffect, useMemo, useState } from 'react'
import { keepPreviousData, useQuery, useQueryClient } from '@tanstack/react-query'
import { useSearchParams } from 'react-router-dom'
import { MonthSwitch } from '../components/AppShell'
import { useToast } from '../components/Toast'
import { CategoriesSection, Delta, EvolutionSection, fullDate, MerchantsCard, SummarySection } from '../components/ReportAnalytics'
import { ForecastSection } from '../components/ReportForecast'
import { AccountEvolution, SpendingHeatmap, WeekdayChart } from '../components/ReportCharts'
import { api, errorMessage, get } from '../lib/api'
import { monthLabel, monthOf, shiftMonth } from '../lib/dates'
import { centsToInput, money, moneyShort, parseAmount } from '../lib/money'
import { useAccounts, useCategories, useMe } from '../lib/queries'
import { apiQuery, defaultState, readState, saveState, stateParams, stateRange, type Preset, type ReportState } from '../lib/reports'
import type { Account, Category, Projection, Reconciliation, ReportSummary } from '../lib/types'

// short: lo que se ve en pantallas angostas; el nombre accesible siempre es el completo.
const presets: { id: Preset; label: string; short: string }[] = [
  { id: '1', label: 'Este mes', short: 'Mes' },
  { id: '3', label: '3 meses', short: '3 m' },
  { id: '6', label: '6 meses', short: '6 m' },
  { id: '12', label: '12 meses', short: '12 m' },
  { id: 'custom', label: 'Elegir', short: 'Elegir' },
]

export function Reports() {
  const { data: me } = useMe()
  const tz = me?.timezone ?? 'America/Guayaquil'
  const current = monthOf(new Date(), tz)
  const [search, setSearch] = useSearchParams()
  const state = useMemo(() => readState(search, current), [search, current])
  // Los filtros viven en la URL (se pueden compartir y sobreviven al recargar) y en la sesión (vuelven al regresar).
  useEffect(() => {
    saveState(state)
    const want = stateParams(state).toString()
    if (search.toString() !== want) setSearch(stateParams(state), { replace: true })
  }, [state, search, setSearch])
  const update = (patch: Partial<ReportState>) => setSearch(stateParams({ ...state, ...patch }))
  const range = stateRange(state, current)
  const query = apiQuery(state, current)
  const filterQuery = apiQuery(state, current, false)
  const { data: r, isLoading, isFetching, error, refetch } = useQuery({ queryKey: ['reports', query], queryFn: () => get<ReportSummary>(`/api/reports/summary?${query}`), placeholderData: keepPreviousData })
  const projection = useQuery({ queryKey: ['projection', filterQuery], queryFn: () => get<Projection>(`/api/reports/projection${filterQuery ? `?${filterQuery}` : ''}`) })
  const { data: categories = [] } = useCategories()
  const { data: accounts = [] } = useAccounts(true)
  const toast = useToast()
  const [busy, setBusy] = useState(false)
  const [filtersOpen, setFiltersOpen] = useState(false)
  const cats = new Map(categories.map((c) => [c.id, c]))
  const accs = new Map(accounts.map((a) => [a.id, a]))
  const roots = categories.filter((c) => !c.parent_id)
  const mains = accounts.filter((a) => !a.merged_into)
  const active = (state.category ? 1 : 0) + (state.account ? 1 : 0)
  const compareName = state.compare === 'year' ? 'el mismo periodo del año pasado' : 'el periodo anterior'
  const scope = [state.category && cats.get(state.category)?.name, state.account && accs.get(state.account)?.name].filter(Boolean).join(' · ')

  async function exportCsv() {
    setBusy(true)
    try {
      const csv = await api<string>('GET', `/api/reports/export.csv?${apiQuery({ ...state, compare: 'previous' }, current)}`) // pide step-up si hace falta
      const url = URL.createObjectURL(new Blob(['\ufeff' + csv], { type: 'text/csv;charset=utf-8' })) // BOM: Excel lee bien las tildes
      const a = Object.assign(document.createElement('a'), { href: url, download: `pulse-${range.from}-a-${range.to}${active ? '-filtrado' : ''}.csv` })
      a.click()
      URL.revokeObjectURL(url)
      toast(active ? 'CSV descargado con los filtros aplicados' : 'CSV descargado')
    } catch (e) {
      toast(errorMessage(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <div className="page-head">
        <div><p className="page-kicker">Una mirada más amplia</p><h1>Reportes</h1><p className="page-description">En qué gastas, cómo cambia tu consumo y cuánto podrías gastar después.</p></div>
      </div>
      <section className="panel report-toolbar" aria-label="Periodo y filtros">
        <div className="report-toolbar-main">
          <div className="seg report-presets" role="group" aria-label="Periodo">
            {presets.map((p) => <button key={p.id} aria-pressed={state.preset === p.id} aria-label={p.label} onClick={() => update(p.id === 'custom' ? { preset: 'custom', from: range.from, to: range.to } : { preset: p.id })}><span className="preset-long" aria-hidden="true">{p.label}</span><span className="preset-short" aria-hidden="true">{p.short}</span></button>)}
          </div>
          <div className="report-toolbar-actions">
            <button type="button" className="btn btn-sm report-filters-toggle" aria-expanded={filtersOpen} aria-controls="report-filters" onClick={() => setFiltersOpen((v) => !v)}>Filtros{active ? ` (${active})` : ''}</button>
            <button type="button" className="btn btn-sm" onClick={exportCsv} disabled={busy}>{busy ? 'Exportando…' : 'Exportar CSV'}</button>
          </div>
        </div>
        <div id="report-filters" className={`report-filter-row ${filtersOpen ? 'is-open' : ''}`}>
          {state.preset === 'custom' && (
            <>
              <label className="field-inline">Desde<input className="input" type="month" value={range.from} min={shiftMonth(range.to, -35)} max={range.to} onChange={(e) => e.target.value && update({ from: e.target.value })} /></label>
              <label className="field-inline">Hasta<input className="input" type="month" value={range.to} min={range.from} max={current} onChange={(e) => e.target.value && update({ to: e.target.value })} /></label>
            </>
          )}
          <label className="field-inline">Comparar con<select className="input" value={state.compare} onChange={(e) => update({ compare: e.target.value as ReportState['compare'] })}><option value="previous">Periodo anterior</option><option value="year">Mismo periodo del año pasado</option></select></label>
          <label className="field-inline">Categoría<select className="input" value={state.category} onChange={(e) => update({ category: e.target.value })}><option value="">Todas</option>{roots.map((c) => <option key={c.id} value={c.id}>{c.name}{c.archived ? ' (archivada)' : ''}</option>)}</select></label>
          <label className="field-inline">Cuenta<select className="input" value={state.account} onChange={(e) => update({ account: e.target.value })}><option value="">Todas</option>{mains.map((a) => <option key={a.id} value={a.id}>{a.name}{a.archived ? ' (archivada)' : ''}</option>)}</select></label>
          {(active > 0 || state.preset !== defaultState.preset || state.compare !== 'previous') && <button type="button" className="btn btn-sm btn-ghost" onClick={() => setSearch(stateParams(defaultState))}>Restablecer</button>}
        </div>
        <p className="small muted report-period-line" aria-live="polite">
          {r?.period ? <>{fullDate(r.period.from)} – {fullDate(r.period.to)} · {r.period.days} {r.period.days === 1 ? 'día' : 'días'}</> : <>{monthLabel(range.from)} – {monthLabel(range.to)}</>}
          {scope && <> · solo <strong>{scope}</strong></>}
          {isFetching && !isLoading && <span role="status"> · Actualizando…</span>}
        </p>
      </section>
      <nav className="report-jump" aria-label="Secciones de Reportes">
        {[['resumen', 'Resumen'], ['evolucion', 'Evolución'], ['categorias', 'Categorías'], ['cuentas', 'Cuentas'], ['proyecciones', 'Proyecciones'], ['habitos', 'Hábitos']].map(([id, label]) => <a key={id} href={`#${id}`} onClick={(e) => { e.preventDefault(); document.getElementById(id)?.scrollIntoView({ behavior: 'smooth', block: 'start' }) }}>{label}</a>)}
      </nav>
      {isLoading && <ReportSkeleton />}
      {error && !r && (
        <div className="panel report-card report-error" role="alert"><h2>No se pudo cargar el reporte</h2><p className="muted">{errorMessage(error)}</p><button className="btn btn-sm" onClick={() => refetch()}>Reintentar</button></div>
      )}
      {error && r && <p className="error small" role="alert">No se pudo actualizar: {errorMessage(error)}. Se muestra el último periodo cargado.</p>}
      {r && (
        <div className={`report-body ${isFetching ? 'is-refreshing' : ''}`}>
          {r.purchase_count === 0 && r.refund_cents === 0 && (
            <div className="panel report-card report-empty"><h2>Sin movimientos en este periodo</h2><p className="muted">{active ? 'Prueba con otros filtros o un periodo más largo.' : 'Registra gastos o elige un periodo más largo para ver tus tendencias.'}</p></div>
          )}
          <SummarySection report={r} categories={categories} compareName={compareName} />
          <EvolutionSection report={r} projection={projection.data} current={current} compareName={compareName} />
          <div className="report-grid">
            <CategoriesSection report={r} categories={categories} filtered={!!state.category} onFilter={(id) => update({ category: id })} />
            <MerchantsCard report={r} />
          </div>
          <div className="report-grid" id="cuentas">
            <AccountSpending report={r} accountName={(id) => accs.get(id)?.name ?? 'Cuenta'} account={(id) => accs.get(id)} category={(id) => cats.get(id)} />
            {r.months.length > 1 && <AccountEvolution report={r} accounts={accounts} />}
          </div>
          <ForecastSection projection={projection.data} projectionError={projection.error ? errorMessage(projection.error) : undefined} projectionLoading={projection.isLoading} forecast={r.forecast} categories={categories} scope={scope} />
          <div className="report-grid" id="habitos">
            <WeekdayChart report={r} />
            <SpendingHeatmap report={r} />
          </div>
        </div>
      )}
      <ReconciliationCard current={current} accountName={(id) => accs.get(id)?.name ?? 'Cuenta'} />
    </>
  )
}

function ReportSkeleton() {
  return (
    <div className="report-skeleton" role="status" aria-label="Cargando reporte">
      <div className="report-metrics">{[0, 1, 2, 3].map((i) => <div key={i} className="report-metric skeleton-block" />)}</div>
      <div className="panel skeleton-block report-skeleton-chart" />
    </div>
  )
}

/** Gasto del periodo por cuenta o tarjeta, con su reparto por categoría. */
function AccountSpending({ report: r, accountName, account, category }: { report: ReportSummary; accountName: (id: string) => string; account: (id: string) => Account | undefined; category: (id: string) => Category | undefined }) {
  const positive = r.by_account.reduce((t, x) => t + Math.max(0, x.spent_cents), 0)
  return (
    <section className="panel report-card" aria-labelledby="h-racc">
      <div className="section-head"><div><h2 id="h-racc">Por cuenta</h2><p className="small muted">Toca una cuenta para ver en qué categorías se gastó.</p></div></div>
      {r.by_account.length === 0 ? <p className="muted small">Sin gastos en este periodo.</p> : (
        <div className="account-spend-list">
          {r.by_account.map((x) => {
            const acc = account(x.id)
            const rows = r.account_categories.filter((c) => c.account_id === x.id)
            const plus = rows.reduce((t, c) => t + Math.max(0, c.spent_cents), 0)
            const pct = positive > 0 ? Math.round(Math.max(0, x.spent_cents) / positive * 100) : 0
            const count = r.account_purchases?.[x.id] ?? 0
            return (
              <details key={x.id} className="breakdown-item account-spend">
                <summary>
                  <span className="category-symbol" style={{ background: `color-mix(in srgb, ${acc?.color ?? '#888'} 14%, transparent)` }} aria-hidden="true">{acc?.icon || '•'}</span>
                  <span className="grow"><strong>{accountName(x.id)}</strong><span className="small muted"> · {count} {count === 1 ? 'compra' : 'compras'}</span>
                    <span className="account-spend-split" aria-hidden="true">{rows.filter((c) => c.spent_cents > 0).map((c) => <span key={c.category_id} style={{ width: `${plus > 0 ? c.spent_cents / plus * 100 : 0}%`, background: category(c.category_id)?.color ?? 'var(--accent)' }} />)}</span></span>
                  <span className="num breakdown-amount"><strong>{money(x.spent_cents)}</strong><span className="small muted">{pct}% del periodo</span><Delta cur={x.spent_cents} prev={r.previous?.by_account.find((p) => p.id === x.id)?.spent_cents ?? 0} coverage={r.previous?.coverage} label="" /></span>
                </summary>
                <div className="breakdown-body">
                  <div className="report-table-scroll" tabIndex={0}><table className="report-table report-forecast-table"><caption className="sr-only">Gasto de {accountName(x.id)} por categoría en el periodo</caption>
                    <thead><tr><th scope="col">Categoría</th><th scope="col">Compras</th><th scope="col">Gasto</th><th scope="col">De la cuenta</th></tr></thead>
                    <tbody>{rows.map((c) => <tr key={c.category_id}><th scope="row">{category(c.category_id)?.name ?? 'Categoría'}</th><td className="num">{c.purchase_count}</td><td className="num">{money(c.spent_cents)}</td><td className="num muted">{x.spent_cents > 0 ? Math.round(c.spent_cents / x.spent_cents * 100) : 0}%</td></tr>)}</tbody>
                  </table></div>
                </div>
              </details>
            )
          })}
        </div>
      )}
    </section>
  )
}

/** Conciliación: lo registrado en Pulse por cuenta contra el total del estado de cuenta. */
function ReconciliationCard({ current, accountName }: { current: string; accountName: (id: string) => string }) {
  const [month, setMonth] = useState(shiftMonth(current, -1))
  const qc = useQueryClient()
  const { data } = useQuery({ queryKey: ['reconciliation', month], queryFn: () => get<Reconciliation>(`/api/reports/reconciliation?month=${month}`) })
  return (
    <section className="panel report-card section" aria-labelledby="h-recon">
      <div className="section-head">
        <div><h2 id="h-recon">Conciliación</h2><p className="small muted">Escribe el total de tu estado de cuenta: la diferencia delata correos que no llegaron.</p></div>
        <MonthSwitch month={month} onChange={setMonth} label={monthLabel(month)} />
      </div>
      <ul className="list">
        {data?.accounts.map((a) => <ReconRow key={`${month}-${a.account_id}`} month={month} name={accountName(a.account_id)} row={a} onSaved={() => qc.invalidateQueries({ queryKey: ['reconciliation', month] })} />)}
      </ul>
    </section>
  )
}

function ReconRow({ month, name, row, onSaved }: { month: string; name: string; row: Reconciliation['accounts'][number]; onSaved: () => void }) {
  const [value, setValue] = useState(row.statement_cents === null ? '' : centsToInput(row.statement_cents))
  const [error, setError] = useState('')
  async function save() {
    const cents = value.trim() === '' ? null : parseAmount(value)
    if (value.trim() !== '' && cents === null) return setError('Monto inválido')
    if (cents === row.statement_cents) return
    setError('')
    try {
      await api('PUT', '/api/statements', { account_id: row.account_id, month, total_cents: cents })
      onSaved()
    } catch (e) {
      setError(errorMessage(e))
    }
  }
  const d = row.difference_cents
  const verdict = d === null ? null : d === 0 ? { cls: 'st-ok', text: 'Cuadra' } : d > 0 ? { cls: 'st-warning', text: `Faltan ${money(d)} en Pulse` } : { cls: 'st-over', text: `Pulse tiene ${money(-d)} de más` }
  const inputId = `stmt-${row.account_id}`
  return (
    <li className="settings-row recon-row">
      <span className="grow"><strong>{name}</strong><br /><span className="small faint">Registrado {money(row.recorded_cents)}{verdict && <> · <span className={verdict.cls}>{verdict.text}</span></>}</span></span>
      <span className="recon-input">
        <label htmlFor={inputId} className="small muted">Estado de cuenta</label>
        <input id={inputId} className="input" inputMode="decimal" placeholder={moneyShort(row.recorded_cents)} value={value} onChange={(e) => setValue(e.target.value)} onBlur={save} onKeyDown={(e) => e.key === 'Enter' && save()} aria-invalid={!!error} />
        {error && <span className="error small" role="alert">{error}</span>}
      </span>
    </li>
  )
}
