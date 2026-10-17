import { useState } from 'react'
import { Link } from 'react-router-dom'
import { MonthSwitch, useTxSheet } from '../components/AppShell'
import { IconArrow, IconList, IconPlan, IconPlus, IconTag, IconWallet } from '../components/icons'
import { TxGroups } from '../components/TxList'
import { monthLabel, monthOf } from '../lib/dates'
import { money } from '../lib/money'
import { QueryFeedback } from '../components/QueryFeedback'
import { useAccounts, useCategories, useDashboard, useMe } from '../lib/queries'
import type { Alert, Dashboard } from '../lib/types'

function topAlerts(alerts: Alert[]) {
  const best = new Map<string, Alert>()
  for (const a of alerts) if ((best.get(a.budget_id)?.threshold ?? 0) < a.threshold) best.set(a.budget_id, a)
  return [...best.values()].sort((a, b) => b.threshold - a.threshold).slice(0, 3)
}

export function Home() {
  const { data: me } = useMe()
  const tz = me?.timezone ?? 'America/Guayaquil'
  const current = monthOf(new Date(), tz)
  const [month, setMonth] = useState(current)
  const { data: d, isLoading, isFetching, error, refetch } = useDashboard(month)
  const { data: categories = [] } = useCategories()
  const { data: accounts = [] } = useAccounts(true)
  const openTx = useTxSheet()
  const isCurrent = month === current
  const today = isCurrent ? Number(new Intl.DateTimeFormat('en-CA', { timeZone: tz, day: 'numeric' }).format(new Date())) : null
  const cats = new Map(categories.map((c) => [c.id, c]))
  const label = monthLabel(month)
  const days = d?.daily_cents.length ?? 1
  const elapsed = today ?? days
  const average = d ? Math.round(d.spent_cents / Math.max(elapsed, 1)) : 0

  return (
    <>
      <div className="page-head home-heading">
        <div><p className="page-kicker">Tu espacio personal</p><h1>Inicio</h1><p className="page-description">Tu dinero, a tu ritmo. Todo el mes en un vistazo.</p></div>
        <MonthSwitch month={month} onChange={setMonth} label={label} />
      </div>
      {isLoading && <div className="dashboard-skeleton" role="status" aria-label="Cargando resumen"><div /><div /><div /></div>}
      <QueryFeedback error={error} retry={refetch} updating={isFetching && !isLoading} />
      {d && (
        <div className="dashboard" key={month}>
          <div className="dashboard-lead">
            <SpendingCard dashboard={d} label={label} average={average} today={today} days={days} current={isCurrent} />
          </div>

          {d.alerts.length > 0 && <div className="dashboard-alerts" aria-label="Avisos del plan">{topAlerts(d.alerts).map((a) => {
            const c = cats.get(a.category_id)
            return <div key={`${a.budget_id}-${a.threshold}`} className={`notice ${a.threshold >= 100 ? 'over' : ''}`}><span aria-hidden="true">{c?.icon}</span><span>{a.threshold >= 100 ? 'Llegaste al tope de ' : `Llevas el ${a.threshold}% de `}<strong>{c?.name}</strong>.</span><Link to="/plan">Ver plan <IconArrow /></Link></div>
          })}</div>}

          <div className="dashboard-lower">
            <section className="panel recent-card" aria-labelledby="h-recent">
              <div className="section-head"><div><h2 id="h-recent">Últimos movimientos</h2></div><Link className="text-link" to={`/movimientos?month=${month}`}>Ver todos <IconArrow /></Link></div>
              {d.recent.length === 0 ? (
                <div className="activity-empty"><div className="empty-symbol"><IconList /><span /></div><h3>Tu primer gasto marca el comienzo</h3><p>Un café, una compra, un viaje.<br />Regístralo y empieza a entender tu mes.</p><button className="btn btn-primary" onClick={() => openTx()}><IconPlus /> Registrar gasto</button><span className="empty-caption">Solo necesitas el monto y la categoría.</span></div>
              ) : <TxGroups items={d.recent} categories={categories} accounts={accounts} tz={tz} />}
            </section>

            <section className="panel categories-card" aria-labelledby="h-cat">
              <div className="section-head"><div><h2 id="h-cat">En qué se fue</h2></div><span className="section-icon"><IconTag /></span></div>
              {d.by_category.some((c) => c.spent_cents > 0) ? (
                <div className="category-breakdown">{d.by_category.filter((c) => c.spent_cents > 0).slice(0, 5).map((c) => {
                  const pct = d.spent_cents > 0 ? Math.min(100, Math.max(0, Math.round(c.spent_cents / d.spent_cents * 100))) : 0
                  return <div className="category-summary" key={c.category_id}><span className="category-symbol" style={{ background: `color-mix(in srgb, ${c.color} 14%, transparent)` }} aria-hidden="true">{c.icon}</span><div><div className="category-summary-top"><strong>{c.name}</strong><span className="num">{money(c.spent_cents)}</span></div><div className="track" aria-hidden="true"><div className="fill" style={{ width: `${pct}%`, background: c.color }} /></div></div><span className="category-percent num">{pct}%</span></div>
                })}</div>
              ) : <div className="category-empty"><p>Una categoría para cada parte de tu vida.</p><div className="category-preview">{categories.filter((c) => !c.parent_id && !c.is_system && !c.archived).slice(0, 4).map((c) => <Link to="/ajustes/categorias" key={c.id}><span style={{ background: `color-mix(in srgb, ${c.color} 14%, transparent)` }} aria-hidden="true">{c.icon}</span><strong>{c.name}</strong><span className="category-dash">—</span></Link>)}</div><p className="hint">Aquí verás cómo se distribuyen tus gastos cuando registres el primero.</p></div>}
              <Link className="card-bottom-link" to="/ajustes/categorias">Organizar categorías <IconArrow /></Link>
            </section>
          </div>

          <div className="quick-actions" aria-label="Accesos rápidos">
            <span className="quick-caption">A tu manera</span>
            <Link to="/ajustes/cuentas"><IconWallet /><span>Configura tus cuentas<small>Tarjetas, bancos y efectivo</small></span><IconArrow /></Link>
            <Link to="/ajustes/categorias"><IconTag /><span>Personaliza categorías<small>Organiza lo que importa para ti</small></span><IconArrow /></Link>
            <Link to="/plan"><IconPlan /><span>Define tu plan<small>Un tope para cada categoría</small></span><IconArrow /></Link>
          </div>
        </div>
      )}
    </>
  )
}

const statusText = { ok: 'Vas bien', warning: 'Cerca del tope', over: 'Te pasaste' } as const

/** Gastado vs. presupuesto: el monto, una barra y lo que queda; se lee sin explicación. */
function SpendingCard({ dashboard: d, label, average, today, days, current }: { dashboard: Dashboard; label: string; average: number; today: number | null; days: number; current: boolean }) {
  const b = d.budget
  const used = Math.min(100, Math.max(0, b.percent))
  const pace = today ? Math.round((today / days) * 100) : null
  return (
    <section className={`panel spending-card ${d.has_budgets ? `budget-${b.status}` : 'budget-unset'}`} aria-labelledby="h-spent">
      <div className="card-heading">
        <h2 id="h-spent" className="eyebrow">Gastado en {label.toLowerCase()}</h2>
        {d.has_budgets && <span className={`status-chip st-${b.status}`}><span className={`dot bg-${b.status}`} aria-hidden="true" />{statusText[b.status]}</span>}
      </div>
      <p className="hero-amount num" data-testid="month-total">{money(d.spent_cents)}</p>
      {d.has_budgets ? (
        <>
          <p className="hero-sub">de <strong className="num">{money(b.budget_cents)}</strong> de presupuesto</p>
          <div className="meter" role="progressbar" aria-label="Porcentaje del presupuesto utilizado" aria-valuenow={used} aria-valuemin={0} aria-valuemax={100} aria-valuetext={`${b.percent}% del presupuesto usado`}>
            <div className={`meter-fill bg-${b.status}`} style={{ width: `${used}%` }} />
            {current && pace !== null && <span className="meter-pace" style={{ left: `${pace}%` }} title="Lo que llevas del mes" />}
          </div>
          <div className="meter-legend">
            <span className="num">{b.percent}% usado</span>
            {b.remaining_cents >= 0
              ? <span>Te quedan <strong className="num st-ok">{money(b.remaining_cents)}</strong></span>
              : <span>Te pasaste por <strong className="num st-over">{money(-b.remaining_cents)}</strong></span>}
          </div>
          {current && pace !== null && <p className="meter-hint"><span className="meter-pace-key" aria-hidden="true" />La marca es el día {today} de {days}: si la barra no la pasa, vas a buen ritmo.</p>}
        </>
      ) : (
        <>
          <p className="hero-sub">{d.count === 0 ? `Sin gastos en ${label.toLowerCase()}.` : `${d.count} ${d.count === 1 ? 'movimiento' : 'movimientos'} en ${label.toLowerCase()}`}</p>
          <Link className="budget-cta" to="/plan"><IconPlan /><span><strong>Define un presupuesto</strong><small>y verás aquí cuánto te queda cada mes</small></span><IconArrow /></Link>
        </>
      )}
      <dl className="spend-stats">
        <div><dt>Promedio diario</dt><dd className="num">{money(average)}</dd></div>
        {d.has_budgets && current && <div><dt>Cierre estimado</dt><dd className={`num ${b.projected_cents > b.budget_cents ? 'st-over' : ''}`}>{money(b.projected_cents)}</dd></div>}
        <div><dt>Movimientos</dt><dd className="num">{d.count}{d.pending_review > 0 && <Link to="/movimientos" className="stat-badge">{d.pending_review} por revisar</Link>}</dd></div>
      </dl>
    </section>
  )
}
