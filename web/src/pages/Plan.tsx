import { useState, type FormEvent } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { MonthSwitch } from '../components/AppShell'
import { Sheet } from '../components/Sheet'
import { useToast } from '../components/Toast'
import { del, errorMessage, patch, post } from '../lib/api'
import { monthLabel, monthOf } from '../lib/dates'
import { centsToInput, money, parseAmount } from '../lib/money'
import { useCategories, useMe, useSummary } from '../lib/queries'
import type { BudgetLine } from '../lib/types'

const statusText = { ok: 'Vas bien', warning: 'Cuidado', over: 'Te pasaste' }

export function Plan() {
  const { data: me } = useMe()
  const tz = me?.timezone ?? 'America/Guayaquil'
  const current = monthOf(new Date(), tz)
  const [month, setMonth] = useState(current)
  const { data: s, isLoading } = useSummary(month)
  const [edit, setEdit] = useState<{ open: boolean; line?: BudgetLine }>({ open: false })
  const isCurrent = month === current

  return (
    <>
      <div className="page-head">
        <h1>Plan de gastos</h1>
        <MonthSwitch month={month} onChange={setMonth} label={monthLabel(month)} />
      </div>
      {isLoading && <p className="loading">Cargando…</p>}
      {s && s.lines.length === 0 && (
        <div className="panel empty">
          <h3>Define cuánto quieres gastar</h3>
          <p>Pon un tope mensual por categoría. Pulse te avisa al 50, 80 y 100 % y te dice cómo cerrarás el mes a tu ritmo actual.</p>
          <button className="btn btn-primary" onClick={() => setEdit({ open: true })}>Crear presupuesto</button>
        </div>
      )}
      {s && s.lines.length > 0 && (
        <>
          <div className="panel" aria-label="Total del plan">
            <div className="budget-top">
              <span className="muted">Total del plan</span>
              <span className="num">
                <strong>{money(s.totals.spent_cents)}</strong> de {money(s.totals.budget_cents)}
              </span>
            </div>
            <Bar p={s.totals} showProjection={isCurrent} />
            <div className="budget-foot">
              <span>{s.totals.remaining_cents >= 0 ? `Quedan ${money(s.totals.remaining_cents)}` : `Excedido por ${money(-s.totals.remaining_cents)}`}</span>
              {isCurrent && <span>Cierre estimado: {money(s.totals.projected_cents)}</span>}
            </div>
            {s.unbudgeted_cents > 0 && <p className="small faint" style={{ marginTop: 8 }}>Fuera del plan este mes: {money(s.unbudgeted_cents)}</p>}
          </div>
          <ul className="list section">
            {s.lines.map((l) => (
              <li key={l.budget_id}>
                <button className="tx budget" style={{ display: 'block' }} onClick={() => setEdit({ open: true, line: l })} aria-label={`${l.name}: ${l.percent}% usado. Editar presupuesto`}>
                  <div className="budget-top">
                    <span className="budget-name">
                      <span aria-hidden="true">{l.icon}</span>
                      {l.name}
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
                </button>
              </li>
            ))}
          </ul>
          <button className="btn" style={{ marginTop: 16 }} onClick={() => setEdit({ open: true })}>Agregar presupuesto</button>
          {isCurrent && <p className="small faint" style={{ marginTop: 12 }}>La marca vertical indica el cierre estimado si sigues gastando al mismo ritmo.</p>}
        </>
      )}
      <Sheet open={edit.open} title={edit.line ? `Presupuesto de ${edit.line.name}` : 'Nuevo presupuesto'} onClose={() => setEdit({ open: false })}>
        <BudgetForm key={edit.line?.budget_id ?? 'new'} line={edit.line} used={s?.lines.map((l) => l.category_id) ?? []} onDone={() => setEdit({ open: false })} />
      </Sheet>
    </>
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

function BudgetForm({ line, used, onDone }: { line?: BudgetLine; used: string[]; onDone: () => void }) {
  const { data: categories = [] } = useCategories()
  const qc = useQueryClient()
  const toast = useToast()
  const [category, setCategory] = useState(line?.category_id ?? '')
  const [amount, setAmount] = useState(line ? centsToInput(line.budget_cents) : '')
  const [thresholds, setThresholds] = useState(line?.thresholds ?? '50,80,100')
  const [error, setError] = useState('')
  const available = categories.filter((c) => !c.archived && (!used.includes(c.id) || c.id === line?.category_id))

  const refresh = () => ['summary', 'dashboard', 'budgets'].forEach((k) => qc.invalidateQueries({ queryKey: [k] }))
  async function submit(e: FormEvent) {
    e.preventDefault()
    setError('')
    const cents = parseAmount(amount)
    if (cents === null) return setError('Escribe un monto válido.')
    try {
      if (line) await patch(`/api/budgets/${line.budget_id}`, { amount_cents: cents, thresholds })
      else await post('/api/budgets', { category_id: category, amount_cents: cents, thresholds })
      refresh()
      toast('Presupuesto guardado')
      onDone()
    } catch (err) {
      setError(errorMessage(err))
    }
  }
  async function remove() {
    if (!line || !confirm(`¿Quitar el presupuesto de ${line.name}?`)) return
    await del(`/api/budgets/${line.budget_id}`)
    refresh()
    toast('Presupuesto quitado')
    onDone()
  }
  return (
    <form onSubmit={submit} className="stack">
      {!line && (
        <label className="field">
          <span>Categoría</span>
          <select className="select" required value={category} onChange={(e) => setCategory(e.target.value)}>
            <option value="" disabled>Elige una categoría</option>
            {available.map((c) => (
              <option key={c.id} value={c.id}>
                {c.parent_id ? '— ' : ''}
                {c.icon} {c.name}
              </option>
            ))}
          </select>
        </label>
      )}
      <label className="field">
        <span>Tope mensual</span>
        <input className="input num" inputMode="decimal" required placeholder="200,00" value={amount} onChange={(e) => setAmount(e.target.value)} />
      </label>
      <div className="field">
        <label htmlFor="th-input">Avisarme al llegar a (%)</label>
        <input id="th-input" className="input num" value={thresholds} onChange={(e) => setThresholds(e.target.value)} aria-describedby="th-hint" />
        <small id="th-hint" className="hint">Separados por comas. Cada aviso llega una sola vez por mes.</small>
      </div>
      {error && <p className="error" role="alert">{error}</p>}
      <button className="btn btn-primary btn-block">Guardar presupuesto</button>
      {line && <button type="button" className="btn btn-danger btn-block" onClick={remove}>Quitar presupuesto</button>}
    </form>
  )
}
