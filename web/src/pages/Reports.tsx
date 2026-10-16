import { useState } from 'react'
import { keepPreviousData, useQuery, useQueryClient } from '@tanstack/react-query'
import { MonthSwitch } from '../components/AppShell'
import { useToast } from '../components/Toast'
import { ReportOverview, ForecastCard } from '../components/ReportAnalytics'
import { AccountEvolution, CategoryCompare, ProjectionCard, SpendingHeatmap, WeekdayChart } from '../components/ReportCharts'
import { api, errorMessage, get } from '../lib/api'
import { monthLabel, monthOf, shiftMonth } from '../lib/dates'
import { centsToInput, money, moneyShort, parseAmount } from '../lib/money'
import { useAccounts, useCategories, useMe } from '../lib/queries'
import type { Account, Category, Reconciliation, ReportSummary } from '../lib/types'

const ranges = [3, 6, 12] as const

export function Reports() {
  const { data: me } = useMe()
  const tz = me?.timezone ?? 'America/Guayaquil'
  const current = monthOf(new Date(), tz)
  const [span, setSpan] = useState<(typeof ranges)[number]>(6)
  const from = shiftMonth(current, -(span - 1))
  const { data: r, isLoading, isPlaceholderData, error } = useQuery({ queryKey: ['reports', from, current], queryFn: () => get<ReportSummary>(`/api/reports/summary?from=${from}&to=${current}`), placeholderData: keepPreviousData })
  const { data: categories = [] } = useCategories()
  const { data: accounts = [] } = useAccounts(true)
  const toast = useToast()
  const [busy, setBusy] = useState(false)
  const cats = new Map(categories.map((c) => [c.id, c]))
  const accs = new Map(accounts.map((a) => [a.id, a]))

  async function exportCsv() {
    setBusy(true)
    try {
      const csv = await api<string>('GET', `/api/reports/export.csv?from=${from}&to=${current}`) // pide step-up si hace falta
      const url = URL.createObjectURL(new Blob(['﻿' + csv], { type: 'text/csv;charset=utf-8' })) // BOM: Excel lee bien las tildes
      const a = Object.assign(document.createElement('a'), { href: url, download: `pulse-${from}-a-${current}.csv` })
      a.click()
      URL.revokeObjectURL(url)
      toast('CSV descargado')
    } catch (e) {
      toast(errorMessage(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <div className="page-head">
        <div><p className="page-kicker">Una mirada más amplia</p><h1>Reportes</h1><p className="page-description">Entiende tus gastos y anticipa lo que viene.</p></div>
        <div className="report-tools">
          <div className="seg" role="group" aria-label="Periodo">
            {ranges.map((n) => <button key={n} aria-pressed={span === n} onClick={() => setSpan(n)}>{n} meses</button>)}
          </div>
          <button className="btn btn-sm" onClick={exportCsv} disabled={busy}>Exportar CSV</button>
        </div>
      </div>
      {isLoading && <p className="loading">Cargando…</p>}
      {isPlaceholderData && <p className="small muted" role="status">Actualizando periodo…</p>}
      {error && <p className="error" role="alert">{errorMessage(error)}</p>}
      {r && (
        <>
          <ReportOverview report={r} categories={categories} current={current} />
          <ProjectionCard categories={categories} />
          <CategoryCompare report={r} categories={categories} current={current} />
          <ForecastCard forecast={r.forecast} categories={categories} />
          <div className="report-grid">
            <Breakdown title="Por categoría" id="h-rcat" total={r.total_cents} rows={r.by_category.map((x) => ({ key: x.id, cents: x.spent_cents, name: cats.get(x.id)?.name ?? 'Categoría', icon: cats.get(x.id)?.icon, color: cats.get(x.id)?.color }))} />
            <AccountSpending report={r} accountName={(id) => accs.get(id)?.name ?? 'Cuenta'} account={(id) => accs.get(id)} category={(id) => cats.get(id)} />
          </div>
          <AccountEvolution report={r} accounts={accounts} />
          <div className="report-grid">
            <WeekdayChart report={r} />
            <SpendingHeatmap report={r} />
          </div>
        </>
      )}
      <ReconciliationCard current={current} accountName={(id) => accs.get(id)?.name ?? 'Cuenta'} />
    </>
  )
}

function Breakdown({ title, id, total, rows }: { title: string; id: string; total: number; rows: { key: string; cents: number; name: string; icon?: string; color?: string }[] }) {
  return (
    <section className="panel report-card" aria-labelledby={id}>
      <div className="section-head"><h2 id={id}>{title}</h2></div>
      {rows.length === 0 ? <p className="muted small">Sin gastos en este periodo.</p> : (
        <div className="category-breakdown">
          {rows.map((x) => {
            const pct = total > 0 ? Math.min(100, Math.max(0, Math.round((x.cents / total) * 100))) : 0
            return (
              <div className="category-summary" key={x.key}>
                <span className="category-symbol" style={{ background: `color-mix(in srgb, ${x.color ?? '#888'} 14%, transparent)` }} aria-hidden="true">{x.icon || '•'}</span>
                <div><div className="category-summary-top"><strong>{x.name}</strong><span className="num">{money(x.cents)}</span></div><div className="track" aria-hidden="true"><div className="fill" style={{ width: `${pct}%`, background: x.color ?? 'var(--accent)' }} /></div></div>
                <span className="category-percent num">{pct}%</span>
              </div>
            )
          })}
        </div>
      )}
    </section>
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
                  <span className="num breakdown-amount"><strong>{money(x.spent_cents)}</strong><span className="small muted">{pct}% del periodo</span></span>
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
