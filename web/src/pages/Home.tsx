import { useState, type CSSProperties, type PointerEvent } from 'react'
import { Link } from 'react-router-dom'
import { MonthSwitch, useTxSheet } from '../components/AppShell'
import { IconArrow, IconList, IconPlan, IconPlus, IconTag, IconWallet } from '../components/icons'
import { TxGroups } from '../components/TxList'
import { Donut, PulseArea, Ring } from '../components/Charts'
import { monthLabel, monthOf, shiftMonth } from '../lib/dates'
import { money } from '../lib/money'
import { QueryFeedback } from '../components/QueryFeedback'
import { CountUp } from '../components/CountUp'
import { useAccounts, useCategories, useDashboard, useMe, useRecurring } from '../lib/queries'
import type { Alert, Dashboard } from '../lib/types'

function topAlerts(alerts: Alert[]) {
  const best = new Map<string, Alert>()
  for (const a of alerts) if ((best.get(a.budget_id)?.threshold ?? 0) < a.threshold) best.set(a.budget_id, a)
  return [...best.values()].sort((a, b) => b.threshold - a.threshold).slice(0, 2)
}

/** Suma acumulada día a día; `upto` corta en el día de hoy para no dibujar el futuro. */
function cumulative(daily: number[], upto = daily.length) {
  const out: number[] = []
  daily.slice(0, upto).forEach((v, i) => out.push((out[i - 1] ?? 0) + v))
  return out
}

/** El brillo de las tarjetas sigue al puntero (solo decorativo). */
function followPointer(e: PointerEvent<HTMLElement>) {
  if (e.pointerType !== 'mouse') return
  const tile = (e.target as HTMLElement).closest<HTMLElement>('.tile')
  if (!tile) return
  const r = tile.getBoundingClientRect()
  tile.style.setProperty('--mx', `${e.clientX - r.left}px`)
  tile.style.setProperty('--my', `${e.clientY - r.top}px`)
}

const statusText = { ok: 'Vas bien', warning: 'Cerca del tope', over: 'Te pasaste' } as const
const tile = (i: number) => ({ '--i': i }) as CSSProperties

export function Home() {
  const { data: me } = useMe()
  const tz = me?.timezone ?? 'America/Guayaquil'
  const current = monthOf(new Date(), tz)
  const [month, setMonth] = useState(current)
  const { data: d, isLoading, isFetching, error, refetch } = useDashboard(month)
  const { data: prev } = useDashboard(shiftMonth(month, -1))
  const { data: categories = [] } = useCategories()
  const { data: accounts = [] } = useAccounts(true)
  const { data: recurring = [] } = useRecurring()
  const openTx = useTxSheet()
  const isCurrent = month === current
  const today = isCurrent ? Number(new Intl.DateTimeFormat('en-CA', { timeZone: tz, day: 'numeric' }).format(new Date())) : null
  const label = monthLabel(month)
  const days = d?.daily_cents.length ?? 1
  const elapsed = today ?? days
  const average = d ? Math.round(d.spent_cents / Math.max(elapsed, 1)) : 0
  const upcoming = [...recurring].sort((a, b) => a.next_date.localeCompare(b.next_date)).slice(0, 4)
  const cats = new Map(categories.map((c) => [c.id, c]))
  const spentCats = d?.by_category.filter((c) => c.spent_cents > 0) ?? []

  return (
    <>
      <div className="page-head home-heading">
        <div><p className="page-kicker">Centro de mando</p><h1>Inicio</h1><p className="page-description">Tu mes de un vistazo: cuánto llevas, a qué ritmo y qué viene.</p></div>
        <MonthSwitch month={month} onChange={setMonth} label={label} />
      </div>
      {isLoading && <div className="bento bento-skeleton" role="status" aria-label="Cargando resumen"><div className="tile tile-hero" /><div className="tile tile-budget" /><div className="tile tile-third" /><div className="tile tile-third" /><div className="tile tile-third" /></div>}
      <QueryFeedback error={error} retry={refetch} updating={isFetching && !isLoading} />
      {d && (
        <div className="dashboard bento" key={month} onPointerMove={followPointer}>
          <Hero d={d} prev={prev} label={label} today={today} days={days} />
          <BudgetTile d={d} today={today} days={days} cats={cats} />

          <section className="tile tile-third" style={tile(2)} aria-labelledby="h-cat">
            <div className="tile-head"><h2 id="h-cat">En qué se fue</h2><span className="tile-icon"><IconTag /></span></div>
            {spentCats.length > 0 ? (
              <div className="donut-wrap">
                <Donut parts={spentCats.map((c) => ({ key: c.category_id, value: c.spent_cents, color: c.color }))} size={116}>
                  <strong className="num">{spentCats.length}</strong><span>{spentCats.length === 1 ? 'categoría' : 'categorías'}</span>
                </Donut>
                <ul className="legend">{spentCats.slice(0, 4).map((c) => {
                  const pct = d.spent_cents > 0 ? Math.round((c.spent_cents / d.spent_cents) * 100) : 0
                  return <li key={c.category_id}><i style={{ background: c.color }} aria-hidden="true" /><span>{c.name}</span><strong className="num">{pct}%</strong></li>
                })}</ul>
              </div>
            ) : <p className="tile-empty">Cuando registres gastos verás aquí cómo se reparten.</p>}
            <Link className="tile-link" to="/reportes">Ver reportes <IconArrow /></Link>
          </section>

          <section className="tile tile-third" style={tile(3)} aria-labelledby="h-close">
            <div className="tile-head"><h2 id="h-close">{isCurrent && d.has_budgets ? 'Cierre estimado' : 'Promedio diario'}</h2></div>
            {isCurrent && d.has_budgets ? (
              <>
                <p className={`tile-figure num ${d.budget.projected_cents > d.budget.budget_cents ? 'st-over' : ''}`}><CountUp value={d.budget.projected_cents} format={(v) => money(Math.round(v))} /></p>
                <p className="tile-note">{d.budget.projected_cents > d.budget.budget_cents ? `${money(d.budget.projected_cents - d.budget.budget_cents)} por encima` : `${money(d.budget.budget_cents - d.budget.projected_cents)} por debajo`} del presupuesto si sigues a este ritmo.</p>
              </>
            ) : (
              <>
                <p className="tile-figure num"><CountUp value={average} format={(v) => money(Math.round(v))} /></p>
                <p className="tile-note">{isCurrent ? 'Define un presupuesto para estimar el cierre del mes.' : `Por día durante ${label.toLowerCase()}.`}</p>
              </>
            )}
            <dl className="tile-stats"><div><dt>Promedio diario</dt><dd className="num">{money(average)}</dd></div><div><dt>Movimientos</dt><dd className="num">{d.count}</dd></div></dl>
          </section>

          <section className={`tile tile-third tile-review ${d.pending_review > 0 ? 'has-pending' : ''}`} style={tile(4)} aria-labelledby="h-review">
            <div className="tile-head"><h2 id="h-review">Por revisar</h2>{d.pending_review > 0 && <span className="live-dot is-warn" aria-hidden="true" />}</div>
            <p className="tile-figure num">{d.pending_review}</p>
            <p className="tile-note">{d.pending_review > 0 ? 'Gastos leídos de tu correo que esperan tu confirmación.' : 'Todo confirmado. Los gastos que lleguen del correo aparecerán aquí.'}</p>
            {d.pending_review > 0 ? <Link className="btn btn-primary btn-sm" to="/movimientos">Revisar ahora</Link> : <button className="btn btn-sm" onClick={() => openTx()}><IconPlus /> Registrar gasto</button>}
          </section>

          <section className="tile tile-wide recent-card" style={tile(5)} aria-labelledby="h-recent">
            <div className="tile-head"><h2 id="h-recent">Actividad reciente</h2><Link className="text-link" to={`/movimientos?month=${month}`}>Ver todos <IconArrow /></Link></div>
            {d.recent.length === 0 ? (
              <div className="activity-empty">
                <div className="empty-symbol"><IconList /><span /></div>
                <h3>Tu primer gasto marca el comienzo</h3>
                <p>Regístralo y empieza a entender tu mes.</p>
                <button className="btn btn-primary" onClick={() => openTx()}><IconPlus /> Registrar gasto</button>
                <div className="setup-links">
                  <Link to="/ajustes/cuentas"><IconWallet /> Configura tus cuentas</Link>
                  <Link to="/ajustes/categorias"><IconTag /> Personaliza categorías</Link>
                  <Link to="/plan"><IconPlan /> Define tu plan</Link>
                </div>
              </div>
            ) : <TxGroups items={d.recent} categories={categories} accounts={accounts} tz={tz} />}
          </section>

          <section className="tile tile-side" style={tile(6)} aria-labelledby="h-fixed">
            <div className="tile-head"><h2 id="h-fixed">Próximos fijos</h2></div>
            {upcoming.length > 0 ? (
              <ul className="upcoming">{upcoming.map((r) => {
                const c = cats.get(r.category_id)
                const [y, m, day] = r.next_date.split('-')
                return <li key={r.id}><span className="upcoming-date"><strong className="num">{Number(day)}</strong><small>{monthLabel(`${y}-${m}`).slice(0, 3)}</small></span><span className="upcoming-name">{r.merchant || c?.name || 'Gasto fijo'}<small>{c?.name}</small></span><strong className="num">{money(r.amount_cents)}</strong></li>
              })}</ul>
            ) : <p className="tile-empty">Sin gastos fijos. Al registrar un gasto marca «Repetir cada mes» y lo verás aquí antes de que llegue.</p>}
            <Link className="tile-link" to="/plan">Ir al plan <IconArrow /></Link>
          </section>
        </div>
      )}
    </>
  )
}

function Hero({ d, prev, label, today, days }: { d: Dashboard; prev?: Dashboard; label: string; today: number | null; days: number }) {
  const upto = today ?? days
  const now = cumulative(d.daily_cents, upto)
  const before = prev && prev.spent_cents > 0 ? cumulative(prev.daily_cents, Math.min(prev.daily_cents.length, days)) : undefined
  const beforeSameDay = before ? before[Math.min(upto, before.length) - 1] ?? 0 : 0
  const delta = beforeSameDay > 0 ? Math.round(((d.spent_cents - beforeSameDay) / beforeSameDay) * 100) : null
  const prevName = monthLabel(shiftMonth(d.month, -1)).split(' ')[0]
  return (
    <section className="tile tile-hero dashboard-lead spending-card" style={tile(0)} aria-labelledby="h-spent">
      <div className="tile-head"><h2 id="h-spent">Gastado en {label.toLowerCase()}</h2>
        {delta !== null && <span className={`delta ${delta > 0 ? 'is-up' : 'is-down'}`}><span aria-hidden="true">{delta > 0 ? '▲' : '▼'}</span> {Math.abs(delta)}% <span className="delta-sub">vs. {prevName.toLowerCase()}{today ? ' a esta fecha' : ''}</span></span>}
      </div>
      <p className="hero-amount num" data-testid="month-total"><CountUp value={d.spent_cents} format={(v) => money(Math.round(v))} /></p>
      <p className="hero-sub">{d.count === 0 ? `Sin gastos en ${label.toLowerCase()}.` : `${d.count} ${d.count === 1 ? 'movimiento' : 'movimientos'}${d.has_budgets ? ` · presupuesto ${money(d.budget.budget_cents)}` : ''}`}</p>
      <PulseArea current={now} previous={before} previousName={prevName} days={days} budget={d.has_budgets ? d.budget.budget_cents : undefined}
        label={`Gasto acumulado de ${label.toLowerCase()}: ${money(d.spent_cents)}${before ? `; ${prevName.toLowerCase()} a la misma fecha, ${money(beforeSameDay)}` : ''}.`} />
    </section>
  )
}

function BudgetTile({ d, today, days, cats }: { d: Dashboard; today: number | null; days: number; cats: Map<string, { icon: string; name: string }> }) {
  const b = d.budget
  const used = Math.min(100, Math.max(0, b.percent))
  const pace = today ? Math.round((today / days) * 100) : null
  if (!d.has_budgets) {
    return (
      <section className="tile tile-budget" style={tile(1)} aria-labelledby="h-budget">
        <div className="tile-head"><h2 id="h-budget">Presupuesto</h2></div>
        <div className="budget-ring"><Ring percent={0}><strong>—</strong><span>sin tope</span></Ring></div>
        <p className="tile-note">Define un tope por categoría y verás aquí cuánto te queda y a qué ritmo vas.</p>
        <Link className="btn btn-sm" to="/plan"><IconPlan /> Crear presupuesto</Link>
      </section>
    )
  }
  return (
    <section className={`tile tile-budget budget-${b.status}`} style={tile(1)} aria-labelledby="h-budget">
      <div className="tile-head"><h2 id="h-budget">Presupuesto</h2><span className={`status-chip st-${b.status}`}><span className={`dot bg-${b.status}`} aria-hidden="true" />{statusText[b.status]}</span></div>
      <div className="budget-ring" role="progressbar" aria-label="Porcentaje del presupuesto utilizado" aria-valuenow={used} aria-valuemin={0} aria-valuemax={100} aria-valuetext={`${b.percent}% del presupuesto usado`}>
        <Ring percent={used} tone={b.status}><strong className="num">{b.percent}%</strong><span>usado</span></Ring>
        <div className="budget-figures">
          {b.remaining_cents >= 0
            ? <p><span>Te quedan</span><strong className="num st-ok">{money(b.remaining_cents)}</strong></p>
            : <p><span>Te pasaste por</span><strong className="num st-over">{money(-b.remaining_cents)}</strong></p>}
          <p><span>Presupuesto</span><strong className="num">{money(b.budget_cents)}</strong></p>
        </div>
      </div>
      {pace !== null && (
        <div className="pace" aria-hidden="true">
          <div><span>Gastado</span><div className="pace-track"><i className={`bg-${b.status}`} style={{ width: `${used}%` }} /></div><strong className="num">{b.percent}%</strong></div>
          <div><span>Mes</span><div className="pace-track"><i className="pace-month" style={{ width: `${pace}%` }} /></div><strong className="num">{pace}%</strong></div>
        </div>
      )}
      {pace !== null && <p className="tile-note">{used <= pace ? 'Vas por debajo del ritmo del mes.' : 'Vas por encima del ritmo del mes.'}</p>}
      {d.alerts.length > 0 && <ul className="tile-alerts" aria-label="Avisos del plan">{topAlerts(d.alerts).map((a) => {
        const c = cats.get(a.category_id)
        return <li key={`${a.budget_id}-${a.threshold}`} className={a.threshold >= 100 ? 'over' : ''}><span aria-hidden="true">{c?.icon}</span><span>{a.threshold >= 100 ? 'Llegaste al tope de ' : `Llevas el ${a.threshold}% de `}<strong>{c?.name}</strong>.</span></li>
      })}</ul>}
      <Link className="tile-link" to="/plan">Ver plan <IconArrow /></Link>
    </section>
  )
}
