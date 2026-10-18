import { useConfirm } from '../components/Confirm'
import { useState, type FormEvent } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { MonthSwitch } from '../components/AppShell'
import { Ring } from '../components/Charts'
import { CountUp } from '../components/CountUp'
import { Sheet } from '../components/Sheet'
import { QueryFeedback } from '../components/QueryFeedback'
import { useViewMonth, useViewScroll } from '../lib/viewState'
import { useToast } from '../components/Toast'
import { api, del, errorMessage, patch, post } from '../lib/api'
import { monthLabel, monthOf, shiftMonth } from '../lib/dates'
import { centsToInput, money, parseAmount } from '../lib/money'
import { sendOrQueue } from '../lib/outbox'
import { accountResolver, pickable } from '../lib/accounts'
import { financeKeys, useAccounts, useBreakdown, useBudgets, useCategories, useMe, useRecurring, useSummary } from '../lib/queries'
import type { Budget, BudgetLine, Recurring } from '../lib/types'

const statusText = { ok: 'Vas bien', warning: 'Cuidado', over: 'Te pasaste' }

export function Plan() {
  const ask = useConfirm()
  const toast = useToast()
  const qc = useQueryClient()
  const { data: me } = useMe()
  const tz = me?.timezone ?? 'America/Guayaquil'
  const current = monthOf(new Date(), tz)
  const [month, setMonth] = useViewMonth('plan', current)
  const { data: s, isLoading, isFetching, error, refetch } = useSummary(month)
  useViewScroll('plan', month, !!s)
  const [edit, setEdit] = useState<{ open: boolean; line?: BudgetLine }>({ open: false })
  const isCurrent = month === current
  const categoryLines = s?.lines.filter((l) => l.kind !== 'account') ?? []
  const accountLines = s?.lines.filter((l) => l.kind === 'account') ?? []
  const prev = shiftMonth(month, -1)

  async function copyPrevious() {
    if (!(await ask({ title: `¿Copiar el plan de ${monthLabel(prev).toLowerCase()} a ${monthLabel(month).toLowerCase()}?`,
      body: `Cada presupuesto tendrá en ${monthLabel(month).toLowerCase()} el mismo tope que tuvo en ${monthLabel(prev).toLowerCase()}. El tope general no cambia.`, confirm: 'Copiar plan' }))) return
    try {
      const r = await post<{ copied: number }>('/api/budgets/copy', { from: prev, to: month })
      ;['summary', 'dashboard', 'budgets'].forEach((k) => qc.invalidateQueries({ queryKey: [k] }))
      toast(r.copied ? `Plan copiado (${r.copied} ${r.copied === 1 ? 'presupuesto' : 'presupuestos'})` : 'No había presupuestos activos que copiar')
    } catch (e) {
      toast(errorMessage(e))
    }
  }

  return (
    <>
      <div className="page-head">
        <div><p className="page-kicker">A tu ritmo</p><h1>Plan de gastos</h1><p className="page-description">Haz espacio para lo que importa y sigue tu presupuesto.</p></div>
        <div className="row-actions" style={{ alignItems: 'center' }}>
          <MonthSwitch month={month} onChange={setMonth} label={monthLabel(month)} />
          {s && s.lines.length > 0 && month >= current && <button className="btn btn-sm" onClick={copyPrevious}>Copiar plan de {monthLabel(prev).toLowerCase()}</button>}
        </div>
      </div>
      {isLoading && <p className="loading">Cargando…</p>}
      <QueryFeedback error={error} retry={refetch} updating={isFetching && !isLoading} />
      {s && s.lines.length === 0 && (
        <div className="panel empty">
          <h3>Define cuánto quieres gastar</h3>
          <p>Pon un tope mensual por categoría o por cuenta. Pulse te avisa al llegar a los porcentajes que elijas y te dice cómo cerrarás el mes a tu ritmo actual.</p>
          <button className="btn btn-primary" onClick={() => setEdit({ open: true })}>Crear presupuesto</button>
        </div>
      )}
      {s && s.lines.length > 0 && (
        <>
          {categoryLines.length > 0 && (
            <div className="plan-overview">
              <div className="tile plan-total" aria-label="Total del plan">
                <Ring percent={Math.min(100, s.totals.percent)} tone={s.totals.status} size={150}><strong className="num">{s.totals.percent}%</strong><span>del plan</span></Ring>
                <div className="plan-total-figures">
                  <p className="plan-total-main"><span className="muted">Total del plan</span><span className="num"><strong><CountUp value={s.totals.spent_cents} format={(v) => money(Math.round(v))} /></strong> de {money(s.totals.budget_cents)}</span></p>
                  <Bar p={s.totals} showProjection={isCurrent} />
                  <dl className="plan-total-stats">
                    <div><dt>{s.totals.remaining_cents >= 0 ? 'Quedan' : 'Excedido por'}</dt><dd className={`num ${s.totals.remaining_cents >= 0 ? 'st-ok' : 'st-over'}`}>{money(Math.abs(s.totals.remaining_cents))}</dd></div>
                    {isCurrent && <div><dt>Cierre estimado</dt><dd className={`num ${s.totals.projected_cents > s.totals.budget_cents ? 'st-over' : ''}`}>{money(s.totals.projected_cents)}</dd></div>}
                    <div><dt>Fuera del plan</dt><dd className="num">{money(s.unbudgeted_cents)}</dd></div>
                  </dl>
                </div>
              </div>
            </div>
          )}
          <BudgetGrid lines={categoryLines} isCurrent={isCurrent} onEdit={(l) => setEdit({ open: true, line: l })} />
          {accountLines.length > 0 && (
            <section className="section" aria-labelledby="h-account-caps">
              <div className="section-head"><h2 id="h-account-caps">Topes por cuenta</h2></div>
              <BudgetGrid lines={accountLines} isCurrent={isCurrent} onEdit={(l) => setEdit({ open: true, line: l })} />
            </section>
          )}
          <button className="btn" style={{ marginTop: 16 }} onClick={() => setEdit({ open: true })}>Agregar presupuesto</button>
          {isCurrent && <p className="small faint" style={{ marginTop: 12 }}>La marca vertical indica el cierre estimado si sigues gastando al mismo ritmo.</p>}
        </>
      )}
      <SpendingBreakdown month={month} lines={categoryLines} isCurrent={isCurrent} />
      <RecurringList tz={tz} />
      <Sheet open={edit.open} title={edit.line ? `Presupuesto de ${edit.line.name}` : 'Nuevo presupuesto'} onClose={() => setEdit({ open: false })}>
        <BudgetForm key={edit.line?.budget_id ?? 'new'} line={edit.line} month={month} current={current} usedCategories={categoryLines.map((l) => l.category_id)}
          usedAccounts={accountLines.map((l) => l.account_id)} onDone={() => setEdit({ open: false })} />
      </Sheet>
    </>
  )
}

/** En qué se gastó cada categoría del mes: subcategorías, comercios que más suman, devoluciones y pendientes. */
function SpendingBreakdown({ month, lines, isCurrent }: { month: string; lines: BudgetLine[]; isCurrent: boolean }) {
  const { data, isLoading, error, refetch } = useBreakdown(month)
  const { data: categories = [] } = useCategories()
  const cats = new Map(categories.map((c) => [c.id, c]))
  const rows = data?.categories ?? []
  const total = rows.reduce((t, c) => t + Math.max(0, c.spent_cents), 0)
  const idle = categories.filter((c) => !c.parent_id && !c.archived && !rows.some((r) => r.id === c.id))
  return (
    <section className="section" aria-labelledby="h-breakdown">
      <div className="section-head"><div><h2 id="h-breakdown">En qué se gastó</h2><p className="small muted">{monthLabel(month)}{isCurrent ? ' hasta hoy' : ''} · toca una categoría para ver el detalle</p></div></div>
      {error && <QueryFeedback error={error} retry={refetch} />}
      {isLoading && <p className="loading">Cargando…</p>}
      {data && rows.length === 0 && <p className="hint">No hay movimientos en {monthLabel(month).toLowerCase()}.</p>}
      <div className="breakdown-list">
        {rows.map((c) => {
          const cat = cats.get(c.id)
          const line = lines.find((l) => l.category_id === c.id)
          const pct = total > 0 ? Math.round(Math.max(0, c.spent_cents) / total * 100) : 0
          return (
            <details key={c.id} className="panel breakdown-item">
              <summary>
                <span className="category-symbol" style={{ background: `color-mix(in srgb, ${cat?.color ?? '#888'} 14%, transparent)` }} aria-hidden="true">{cat?.icon || '•'}</span>
                <span className="grow"><strong>{cat?.name ?? 'Categoría'}</strong><span className="small muted"> · {c.purchase_count} {c.purchase_count === 1 ? 'compra' : 'compras'}{line ? ` · tope ${money(line.budget_cents)}` : ''}</span>
                  <span className="track" aria-hidden="true"><span className="fill" style={{ width: `${pct}%`, background: cat?.color ?? 'var(--accent)' }} /></span></span>
                <span className="num breakdown-amount"><strong>{money(c.spent_cents)}</strong><span className="small muted">{pct}% del mes</span></span>
              </summary>
              <div className="breakdown-body">
                {c.subcategories.length > 0 && (
                  <>
                    <h3 className="small">Subcategorías</h3>
                    <ul className="breakdown-rows">{c.subcategories.map((x) => <li key={x.id}><span>{cats.get(x.id)?.name ?? 'Subcategoría'}</span><span className="num">{money(x.spent_cents)}</span></li>)}
                      {c.spent_cents !== c.subcategories.reduce((t, x) => t + x.spent_cents, 0) && <li className="muted"><span>Directo en {cat?.name ?? 'la categoría'}</span><span className="num">{money(c.spent_cents - c.subcategories.reduce((t, x) => t + x.spent_cents, 0))}</span></li>}
                    </ul>
                  </>
                )}
                <h3 className="small">Dónde</h3>
                <ul className="breakdown-rows">
                  {c.merchants.map((m) => <li key={m.name || '-'}><span>{m.name || 'Sin comercio'} <span className="muted small">· {m.count} {m.count === 1 ? 'movimiento' : 'movimientos'}</span></span><span className="num">{money(m.spent_cents)}</span></li>)}
                  {c.other_count > 0 && <li className="muted"><span>Otros comercios · {c.other_count} {c.other_count === 1 ? 'movimiento' : 'movimientos'}</span><span className="num">{money(c.other_cents)}</span></li>}
                </ul>
                {(c.refund_cents > 0 || c.pending_review > 0) && <p className="small muted">{c.refund_cents > 0 ? `Incluye ${money(c.refund_cents)} en devoluciones. ` : ''}{c.pending_review > 0 ? `${c.pending_review} ${c.pending_review === 1 ? 'movimiento está' : 'movimientos están'} por revisar.` : ''}</p>}
              </div>
            </details>
          )
        })}
      </div>
      {data && rows.length > 0 && idle.length > 0 && <p className="small faint" style={{ marginTop: 10 }}>Sin gastos este mes: {idle.map((c) => c.name).join(', ')}.</p>}
    </section>
  )
}

function BudgetGrid({ lines, isCurrent, onEdit }: { lines: BudgetLine[]; isCurrent: boolean; onEdit: (l: BudgetLine) => void }) {
  if (lines.length === 0) return null
  return (
    <ul className="list section budget-grid">
      {lines.map((l) => (
        <li key={l.budget_id}>
          <button className="tx budget" style={{ display: 'block' }} onClick={() => onEdit(l)} aria-label={`${l.name}: ${l.percent}% usado. Editar presupuesto`}>
            <div className="budget-top">
              <span className="budget-name">
                <Ring percent={Math.min(100, l.percent)} tone={l.status} size={52}><span className="budget-ring-icon" aria-hidden="true">{l.icon}</span></Ring>
                {l.name}
                {l.month_cap && <span className="badge">Tope de este mes</span>}
              </span>
              <span className="num">
                <strong>{money(l.spent_cents)}</strong> <span className="muted">de {money(l.budget_cents)}</span>
              </span>
            </div>
            <Bar p={l} showProjection={isCurrent} />
            <div className="budget-foot">
              <span className={`st-${l.status}`}>
                {statusText[l.status]}, {l.percent}% usado
              </span>
              {isCurrent && l.projected_over_cents > 0 ? (
                <span className="st-warning">A este ritmo te pasarás {money(l.projected_over_cents)}</span>
              ) : (
                <span>{l.remaining_cents >= 0 ? `Quedan ${money(l.remaining_cents)}` : `Excedido ${money(-l.remaining_cents)}`}</span>
              )}
            </div>
            {l.rollover && l.carry_cents !== 0 && (
              <p className="small faint" style={{ marginTop: 6 }}>
                Tope {money(l.cap_cents)} {l.carry_cents > 0 ? `+ ${money(l.carry_cents)} que sobró` : `− ${money(-l.carry_cents)} de exceso`} de meses anteriores
              </p>
            )}
          </button>
        </li>
      ))}
    </ul>
  )
}

function Bar({ p, showProjection }: { p: { percent: number; projected_cents: number; budget_cents: number; status: string }; showProjection: boolean }) {
  const proj = p.budget_cents > 0 ? Math.min(100, (p.projected_cents / p.budget_cents) * 100) : 0
  return (
    <div className="track" role="progressbar" aria-valuenow={p.percent} aria-valuemin={0} aria-valuemax={100} aria-label="Porcentaje usado">
      <div className={`fill bg-${p.status}`} style={{ width: `${Math.min(100, p.percent)}%` }} />
      {showProjection && proj > Math.min(100, p.percent) && <div className="marker" style={{ left: `calc(${proj}% - 1px)` }} />}
    </div>
  )
}

function BudgetForm({ line, month, current, usedCategories, usedAccounts, onDone }: {
  line?: BudgetLine; month: string; current: string; usedCategories: string[]; usedAccounts: string[]; onDone: () => void
}) {
  const ask = useConfirm()
  const { data: categories = [] } = useCategories()
  const { data: accounts = [] } = useAccounts()
  const { data: budgets = [] } = useBudgets()
  const qc = useQueryClient()
  const toast = useToast()
  const saved: Budget | undefined = budgets.find((b) => b.id === line?.budget_id)
  const [kind, setKind] = useState<'category' | 'account'>(line?.kind ?? 'category')
  const [category, setCategory] = useState(line?.category_id ?? '')
  const [account, setAccount] = useState(line?.account_id ?? '')
  const [amount, setAmount] = useState(line ? centsToInput(line.base_cents || line.budget_cents) : '')
  const [thresholds, setThresholds] = useState(line?.thresholds ?? '50,80,100')
  const [rollover, setRollover] = useState(line?.rollover ?? false)
  const [rollFrom, setRollFrom] = useState(line?.rollover_from || current)
  const ownCap = saved?.months?.[month]
  const [monthCap, setMonthCap] = useState(ownCap ? centsToInput(ownCap) : '')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const availableCats = categories.filter((c) => !c.archived && (!usedCategories.includes(c.id) || c.id === line?.category_id))
  const availableAccs = pickable(accounts, line?.account_id).filter((a) => !a.archived && (!usedAccounts.includes(a.id) || a.id === line?.account_id))
  const fromOptions = Array.from({ length: 13 }, (_, i) => shiftMonth(current, -i))

  const refresh = () => ['summary', 'dashboard', 'budgets'].forEach((k) => qc.invalidateQueries({ queryKey: [k] }))
  async function submit(e: FormEvent) {
    e.preventDefault()
    setError('')
    const cents = parseAmount(amount)
    if (cents === null || cents <= 0) return setError('Escribe un tope válido.')
    const capCents = monthCap.trim() === '' ? null : parseAmount(monthCap)
    if (monthCap.trim() !== '' && (capCents === null || capCents <= 0)) return setError('El tope de este mes debe ser un monto mayor a cero, o déjalo vacío.')
    const body: Record<string, unknown> = { amount_cents: cents, thresholds, rollover }
    if (rollover && (!line?.rollover || rollFrom !== line.rollover_from)) body.rollover_from = rollFrom
    setBusy(true)
    try {
      let id = line?.budget_id
      if (line) await patch(`/api/budgets/${line.budget_id}`, body)
      else {
        if (kind === 'category' && !category) throw new Error('Elige una categoría.')
        if (kind === 'account' && !account) throw new Error('Elige una cuenta.')
        const created = await post<Budget>('/api/budgets', { ...body, ...(kind === 'category' ? { category_id: category } : { account_id: account }) })
        id = created.id
      }
      if (id && (capCents ?? 0) !== (ownCap ?? 0)) await api('PUT', `/api/budgets/${id}/months/${month}`, { amount_cents: capCents })
      refresh()
      toast('Presupuesto guardado')
      onDone()
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setBusy(false)
    }
  }
  async function remove() {
    if (!line || !(await ask({ title: `¿Quitar el presupuesto de ${line.name}?`, confirm: 'Quitar presupuesto', danger: true }))) return
    try {
      await del(`/api/budgets/${line.budget_id}`)
      refresh()
      toast('Presupuesto quitado')
      onDone()
    } catch (err) {
      setError(errorMessage(err))
    }
  }
  return (
    <form onSubmit={submit} className="stack">
      {!line && (
        <>
          <div className="seg" role="group" aria-label="Tipo de presupuesto">
            <button type="button" aria-pressed={kind === 'category'} onClick={() => setKind('category')}>Por categoría</button>
            <button type="button" aria-pressed={kind === 'account'} onClick={() => setKind('account')}>Por cuenta</button>
          </div>
          {kind === 'category' ? (
            <label className="field">
              <span>Categoría</span>
              <select className="select" required value={category} onChange={(e) => setCategory(e.target.value)}>
                <option value="" disabled>Elige una categoría</option>
                {availableCats.map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.parent_id ? '— ' : ''}
                    {c.icon} {c.name}
                  </option>
                ))}
              </select>
            </label>
          ) : (
            <label className="field">
              <span>Cuenta o tarjeta</span>
              <select className="select" required value={account} onChange={(e) => setAccount(e.target.value)}>
                <option value="" disabled>Elige una cuenta</option>
                {availableAccs.map((a) => (
                  <option key={a.id} value={a.id}>{a.name}{a.card_suffix ? ` (…${a.card_suffix})` : ''}</option>
                ))}
              </select>
            </label>
          )}
        </>
      )}
      <label className="field">
        <span>Tope mensual</span>
        <input className="input num" inputMode="decimal" required placeholder="200,00" value={amount} onChange={(e) => setAmount(e.target.value)} />
      </label>
      <label className="field">
        <span>Tope solo para {monthLabel(month).toLowerCase()} (opcional)</span>
        <input className="input num" inputMode="decimal" placeholder="Igual al mensual" value={monthCap} onChange={(e) => setMonthCap(e.target.value)} aria-describedby="cap-hint" />
        <small id="cap-hint" className="hint">Para un mes distinto (fiestas, viaje). Déjalo vacío para usar el tope mensual.</small>
      </label>
      <div className="field">
        <label htmlFor="th-input">Avisarme al llegar a (%)</label>
        <input id="th-input" className="input num" value={thresholds} onChange={(e) => setThresholds(e.target.value)} aria-describedby="th-hint" />
        <small id="th-hint" className="hint">Separados por comas, hasta 5. Cada aviso llega una sola vez por mes, aunque cambies el tope.</small>
      </div>
      <label className="check">
        <input type="checkbox" checked={rollover} onChange={(e) => setRollover(e.target.checked)} />
        <span>Pasar al mes siguiente lo que sobre (o lo que me pase)</span>
      </label>
      {rollover && (
        <label className="field">
          <span>Contar desde</span>
          <select className="select" value={rollFrom} onChange={(e) => setRollFrom(e.target.value)}>
            {fromOptions.map((m) => <option key={m} value={m}>{monthLabel(m)}</option>)}
          </select>
        </label>
      )}
      {error && <p className="error" role="alert">{error}</p>}
      <button className="btn btn-primary btn-block" disabled={busy}>Guardar presupuesto</button>
      {line && <button type="button" className="btn btn-danger btn-block" onClick={remove}>Quitar presupuesto</button>}
    </form>
  )
}

/** Gastos fijos: se registran solos cada mes. Se crean desde "Nuevo gasto" → "Repetir cada mes" y se editan aquí, también sin conexión. */
function RecurringList({ tz }: { tz: string }) {
  const ask = useConfirm()
  const qc = useQueryClient()
  const toast = useToast()
  const { data: list = [], error, refetch } = useRecurring()
  const { data: categories = [] } = useCategories()
  const { data: accounts = [] } = useAccounts(true)
  const [editing, setEditing] = useState<Recurring | null>(null)
  if (list.length === 0) return error ? <section className="section" aria-label="Gastos fijos"><h2>Gastos fijos</h2><QueryFeedback error={error} retry={refetch} /></section> : null
  const cat = (id: string) => categories.find((c) => c.id === id)
  const name = (r: Recurring) => r.merchant || cat(r.category_id)?.name || 'Gasto fijo'
  async function remove(r: Recurring) {
    if (!(await ask({ title: `¿Dejar de registrar “${name(r)}”?`, body: 'Los gastos ya registrados se conservan.', confirm: 'Quitar gasto fijo', danger: true }))) return
    try {
      const res = await sendOrQueue({ kind: 'rec.delete', entity: r.id, version: r.version || undefined, base: { ...r }, label: `${name(r)} ${money(r.amount_cents)}` })
      qc.invalidateQueries({ queryKey: ['recurring'] })
      financeKeys.forEach((k) => qc.invalidateQueries({ queryKey: k }))
      toast(res === 'queued' ? 'Gasto fijo quitado en este dispositivo: se enviará al volver la conexión' : 'Gasto fijo quitado')
    } catch (e) {
      toast(errorMessage(e))
      qc.invalidateQueries({ queryKey: ['recurring'] })
    }
  }
  const total = list.reduce((a, r) => a + r.amount_cents, 0)
  return (
    <section className="section" aria-labelledby="h-recurring">
      <div className="section-head"><h2 id="h-recurring">Gastos fijos</h2><span className="num muted">{money(total)} al mes</span></div>
      <ul className="list">
        {list.map((r) => (
          <li key={r.id} className="settings-row">
            <span className="grow">
              <span aria-hidden="true">{cat(r.category_id)?.icon} </span>{name(r)} · <strong className="num">{money(r.amount_cents)}</strong>
              {r.pending && <span className="badge pending">Pendiente de enviar</span>}
              <br />
              <span className="small faint">Cada día {r.day} en {accountResolver(accounts)(r.account_id)?.name ?? 'tu cuenta'}{r.next_date && ` · próximo ${new Intl.DateTimeFormat('es-EC', { day: 'numeric', month: 'long', timeZone: 'UTC' }).format(new Date(`${r.next_date}T12:00:00Z`))}`}</span>
            </span>
            <button className="btn btn-sm" onClick={() => setEditing(r)} aria-label={`Editar ${name(r)}`}>Editar</button>
            <button className="btn btn-sm btn-danger" onClick={() => remove(r)} aria-label={`Quitar ${name(r)}`}>Quitar</button>
          </li>
        ))}
      </ul>
      <Sheet open={!!editing} title="Editar gasto fijo" onClose={() => setEditing(null)}>
        {editing && <RecurringForm key={editing.id} r={editing} tz={tz} onDone={() => setEditing(null)} />}
      </Sheet>
    </section>
  )
}

function RecurringForm({ r, onDone }: { r: Recurring; tz: string; onDone: () => void }) {
  const qc = useQueryClient()
  const toast = useToast()
  const { data: categories = [] } = useCategories()
  const { data: accounts = [] } = useAccounts()
  const [amount, setAmount] = useState(centsToInput(r.amount_cents))
  const [merchant, setMerchant] = useState(r.merchant)
  const [day, setDay] = useState(String(r.day))
  const [category, setCategory] = useState(r.category_id)
  const [account, setAccount] = useState(r.account_id)
  const [error, setError] = useState('')
  async function submit(e: FormEvent) {
    e.preventDefault()
    setError('')
    const cents = parseAmount(amount)
    const d = Number(day)
    if (cents === null || cents <= 0) return setError('Escribe un monto válido.')
    if (!Number.isInteger(d) || d < 1 || d > 31) return setError('El día debe estar entre 1 y 31.')
    const body: Record<string, unknown> = {}
    if (cents !== r.amount_cents) body.amount_cents = cents
    if (merchant !== r.merchant) body.merchant = merchant
    if (d !== r.day) body.day = d
    if (category !== r.category_id) body.category_id = category
    if (account !== r.account_id) body.account_id = account
    if (Object.keys(body).length === 0) return onDone()
    try {
      const res = await sendOrQueue({ kind: 'rec.update', entity: r.id, body, version: r.version || undefined, base: { ...r }, label: `${merchant || 'Gasto fijo'} ${money(cents)}` })
      qc.invalidateQueries({ queryKey: ['recurring'] })
      financeKeys.forEach((k) => qc.invalidateQueries({ queryKey: k }))
      toast(res === 'queued' ? 'Cambio guardado en este dispositivo: se enviará al volver la conexión' : 'Gasto fijo actualizado')
      onDone()
    } catch (err) {
      setError(errorMessage(err))
      qc.invalidateQueries({ queryKey: ['recurring'] })
    }
  }
  return (
    <form className="stack" onSubmit={submit}>
      <label className="field"><span>Monto</span><input className="input num" inputMode="decimal" required value={amount} onChange={(e) => setAmount(e.target.value)} /></label>
      <label className="field"><span>Comercio</span><input className="input" maxLength={120} value={merchant} onChange={(e) => setMerchant(e.target.value)} /></label>
      <label className="field"><span>Día de cada mes</span><input className="input num" inputMode="numeric" required value={day} onChange={(e) => setDay(e.target.value)} aria-describedby="day-hint" /><small id="day-hint" className="hint">Si el mes es más corto, se cobra el último día.</small></label>
      <div className="grid-2">
        <label className="field"><span>Categoría</span>
          <select className="select" value={category} onChange={(e) => setCategory(e.target.value)}>
            {categories.filter((c) => !c.archived || c.id === r.category_id).map((c) => <option key={c.id} value={c.id}>{c.parent_id ? '— ' : ''}{c.icon} {c.name}</option>)}
          </select>
        </label>
        <label className="field"><span>Cuenta</span>
          <select className="select" value={account} onChange={(e) => setAccount(e.target.value)}>
            {pickable(accounts, r.account_id).map((a) => <option key={a.id} value={a.id}>{a.name}</option>)}
          </select>
        </label>
      </div>
      {error && <p className="error" role="alert">{error}</p>}
      <button className="btn btn-primary btn-block">Guardar cambios</button>
    </form>
  )
}
