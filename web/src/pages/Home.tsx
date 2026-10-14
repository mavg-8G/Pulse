import { useState } from 'react'
import { Link } from 'react-router-dom'
import { MonthSwitch, useTxSheet } from '../components/AppShell'
import { IconArrow, IconList, IconPlan, IconPlus, IconPulse, IconTag, IconWallet } from '../components/icons'
import { PulseTrace } from '../components/PulseTrace'
import { TxGroups } from '../components/TxList'
import { monthLabel, monthOf } from '../lib/dates'
import { money } from '../lib/money'
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
  const { data: d, isLoading, error } = useDashboard(month)
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
  const activeAccounts = accounts.filter((a) => !a.archived)

  return (
    <>
      <div className="page-head home-heading">
        <div><p className="eyebrow">TU RESUMEN MENSUAL</p><h1>Inicio</h1><p className="page-description">Tu dinero, a tu ritmo. Todo el mes en un vistazo.</p></div>
        <MonthSwitch month={month} onChange={setMonth} label={label} />
      </div>
      {isLoading && <div className="dashboard-skeleton" role="status" aria-label="Cargando resumen"><div /><div /><div /></div>}
      {error && <p className="error" role="alert">No se pudo cargar el resumen. Revisa tu conexión.</p>}
      {d && (
        <div className="dashboard" key={month}>
          <div className="dashboard-lead">
            <section className="panel spending-card" aria-label="Gastado en el mes">
              <div className="card-heading"><span className="eyebrow">GASTADO ESTE MES</span><span className="activity-badge"><span className="live-dot" />{d.count ? 'Tu pulso del mes' : 'Listo para empezar'}</span></div>
              <p className="hero-amount" data-testid="month-total">{money(d.spent_cents)}</p>
              <p className="hero-sub">{d.count === 0 ? `Sin gastos en ${label.toLowerCase()}.` : `${d.count} ${d.count === 1 ? 'movimiento' : 'movimientos'} en ${label.toLowerCase()}`}</p>
              <PulseTrace daily={d.daily_cents} today={today} />
              <div className="chart-foot"><span><IconPulse />{d.count ? 'Cada pico representa un día de gastos' : 'Cada gasto dará forma a tu pulso'}</span><span>{today ? `Día ${today} de ${days}` : `${days} días`}</span></div>
            </section>
            <BudgetOverview dashboard={d} current={isCurrent} />
          </div>

          <div className="metric-grid" aria-label="Indicadores del mes">
            <div className="metric-card"><span className="metric-icon"><IconPulse /></span><div><span className="metric-label">Promedio diario</span><strong className="num">{money(average)}</strong><span className="metric-note">{isCurrent ? 'En los días transcurridos' : 'En el mes seleccionado'}</span></div></div>
            <div className="metric-card"><span className="metric-icon"><IconList /></span><div><span className="metric-label">Movimientos</span><strong className="num">{d.count}<span className="metric-unit"> este mes</span></strong><span className="metric-note">{d.pending_review ? `${d.pending_review} pendientes de revisión` : 'Tu registro de gastos'}</span></div></div>
            <Link className="metric-card metric-link" to="/ajustes/cuentas"><span className="metric-icon"><IconWallet /></span><div><span className="metric-label">Cuentas y tarjetas</span><strong className="num">{activeAccounts.length}<span className="metric-unit"> {activeAccounts.length === 1 ? 'activa' : 'activas'}</span></strong><span className="metric-note">Organiza de dónde sale tu dinero</span></div><IconArrow /></Link>
          </div>

          {d.alerts.length > 0 && <div className="dashboard-alerts" aria-label="Avisos del plan">{topAlerts(d.alerts).map((a) => {
            const c = cats.get(a.category_id)
            return <div key={`${a.budget_id}-${a.threshold}`} className={`notice ${a.threshold >= 100 ? 'over' : ''}`}><span aria-hidden="true">{c?.icon}</span><span>{a.threshold >= 100 ? 'Llegaste al tope de ' : `Llevas el ${a.threshold}% de `}<strong>{c?.name}</strong>.</span><Link to="/plan">Ver plan <IconArrow /></Link></div>
          })}</div>}

          <div className="dashboard-lower">
            <section className="panel recent-card" aria-labelledby="h-recent">
              <div className="section-head"><div><p className="eyebrow">TU ACTIVIDAD</p><h2 id="h-recent">Últimos movimientos</h2></div><Link className="text-link" to={`/movimientos?month=${month}`}>Ver todos <IconArrow /></Link></div>
              {d.recent.length === 0 ? (
                <div className="activity-empty"><div className="empty-symbol"><IconList /><span /></div><h3>Tu primer gasto marca el comienzo</h3><p>Un café, una compra, un viaje.<br />Regístralo y empieza a entender tu mes.</p><button className="btn btn-primary" onClick={() => openTx()}><IconPlus /> Registrar gasto</button><span className="empty-caption">Solo necesitas el monto y la categoría.</span></div>
              ) : <TxGroups items={d.recent} categories={categories} accounts={accounts} tz={tz} />}
            </section>

            <section className="panel categories-card" aria-labelledby="h-cat">
              <div className="section-head"><div><p className="eyebrow">DISTRIBUCIÓN</p><h2 id="h-cat">En qué se fue</h2></div><span className="section-icon"><IconTag /></span></div>
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

function BudgetOverview({ dashboard: d, current }: { dashboard: Dashboard; current: boolean }) {
  const percent = Math.max(0, d.budget.percent)
  const used = Math.min(100, percent)
  return (
    <section className={`panel budget-overview ${d.has_budgets ? `budget-${d.budget.status}` : 'budget-unset'}`} aria-labelledby="h-budget-overview">
      <div className="card-heading"><h2 id="h-budget-overview">Tu plan del mes</h2><span className="section-icon"><IconPlan /></span></div>
      <div className="budget-ring" role={d.has_budgets ? 'progressbar' : undefined} aria-label={d.has_budgets ? 'Porcentaje del presupuesto utilizado' : undefined} aria-valuenow={d.has_budgets ? used : undefined} aria-valuetext={d.has_budgets ? `${percent}% del plan utilizado` : undefined} aria-valuemin={d.has_budgets ? 0 : undefined} aria-valuemax={d.has_budgets ? 100 : undefined}>
        <svg viewBox="0 0 160 160" aria-hidden="true"><circle className="ring-base" cx="80" cy="80" r="67" /><circle className="ring-value" cx="80" cy="80" r="67" pathLength="100" strokeDasharray={d.has_budgets ? `${used} 100` : '3 5'} /></svg>
        <div>{d.has_budgets ? <><strong className="num">{percent}<span>%</span></strong><span>del plan utilizado</span></> : <><IconPlan /><strong>Sin tope aún</strong><span>Tú marcas el ritmo</span></>}</div>
      </div>
      {d.has_budgets ? <><div className="budget-overview-amounts"><div><span>Disponible</span><strong className={`num ${d.budget.remaining_cents < 0 ? 'st-over' : ''}`}>{money(d.budget.remaining_cents)}</strong></div><div><span>Presupuesto</span><strong className="num">{money(d.budget.budget_cents)}</strong></div></div>{current && <p className="budget-projection">Cierre estimado <strong className="num">{money(d.budget.projected_cents)}</strong></p>}<Link className="card-bottom-link" to="/plan">Revisar mi plan <IconArrow /></Link></> : <><p className="budget-intro">Decide cuánto quieres gastar.<br />Pulse te avisa cuando te acerques al límite.</p><Link className="btn btn-soft btn-block" to="/plan">Crear presupuesto <IconArrow /></Link></>}
    </section>
  )
}
