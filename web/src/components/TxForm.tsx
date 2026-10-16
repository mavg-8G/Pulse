import { useConfirm } from './Confirm'
import { useMemo, useRef, useState, type FormEvent } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { errorMessage } from '../lib/api'
import { sendOrQueue } from '../lib/outbox'
import { nowLocalInput, toLocalInput } from '../lib/dates'
import { centsToInput, money, parseAmount } from '../lib/money'
import { financeKeys, useAccounts, useCategories, useMe } from '../lib/queries'
import type { Transaction } from '../lib/types'
import { useToast } from './Toast'
import { finePointer } from '../lib/device'

export function TxForm({ tx, onDone }: { tx?: Transaction; onDone: () => void }) {
  const ask = useConfirm()
  const { data: me } = useMe()
  const tz = me?.timezone ?? 'America/Guayaquil'
  const { data: accounts = [] } = useAccounts()
  const { data: categories = [] } = useCategories()
  const qc = useQueryClient()
  const toast = useToast()

  const [amount, setAmount] = useState(tx ? centsToInput(tx.amount_cents) : '')
  const [kind, setKind] = useState<'purchase' | 'refund'>(tx?.kind ?? 'purchase')
  const [categoryId, setCategoryId] = useState(tx?.category_id ?? '')
  const [accountId, setAccountId] = useState(tx?.account_id ?? '')
  const [when, setWhen] = useState(tx ? toLocalInput(tx.occurred_at, tz) : nowLocalInput(tz))
  const [merchant, setMerchant] = useState(tx?.merchant ?? '')
  const [notes, setNotes] = useState(tx?.notes ?? '')
  const [learn, setLearn] = useState(false)
  const [repeat, setRepeat] = useState(false)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const effectiveAccount = accountId || accounts[0]?.id || ''
  const tops = useMemo(() => categories.filter((c) => !c.parent_id && !c.archived), [categories])
  const children = (id: string) => categories.filter((c) => c.parent_id === id && !c.archived)
  const selectedParent = categories.find((c) => c.id === categoryId)?.parent_id || categoryId
  const cents = parseAmount(amount)

  const categoryChanged = !!tx && categoryId !== tx.category_id && merchant.trim() !== ''
  const categoryName = categories.find((c) => c.id === categoryId)?.name ?? ''
  const pending = tx?.status === 'pending_review'
  const confirmRef = useRef(false)

  async function submit(e: FormEvent) {
    e.preventDefault()
    setError('')
    if (cents === null) return setError('Escribe un monto válido, por ejemplo 17,68.')
    if (!categoryId) return setError('Elige una categoría.')
    if (!effectiveAccount) return setError('Primero crea una cuenta en Ajustes → Cuentas.')
    setBusy(true)
    const body = { amount_cents: cents, kind, category_id: categoryId, account_id: effectiveAccount, occurred_at: when, merchant, notes }
    const label = `${merchant.trim() || categoryName || 'Gasto'} ${money(cents)}`
    try {
      let result: 'sent' | 'queued'
      if (tx) {
        const changes = { ...body, ...(learn && categoryChanged ? { learn_rule: true } : {}), ...(confirmRef.current ? { status: 'confirmed' } : {}) }
        result = await sendOrQueue({ kind: 'tx.update', entity: tx.id, body: changes, version: tx.version || undefined, base: { ...tx }, label })
      } else if (repeat) {
        const id = crypto.randomUUID() // id del cliente: reenviarlo desde la cola no lo duplica
        result = await sendOrQueue({ kind: 'rec.create', entity: id, body: { account_id: effectiveAccount, category_id: categoryId, amount_cents: cents, merchant, start_date: when.slice(0, 10) }, label })
      } else {
        result = await sendOrQueue({ kind: 'tx.create', entity: crypto.randomUUID(), body, label })
      }
      financeKeys.forEach((k) => qc.invalidateQueries({ queryKey: k }))
      if (repeat) qc.invalidateQueries({ queryKey: ['recurring'] })
      const what = confirmRef.current ? 'Gasto confirmado' : tx ? 'Gasto actualizado' : repeat ? 'Gasto fijo creado: se registrará cada mes' : 'Gasto guardado'
      toast(result === 'queued' ? `${what} en este dispositivo: se enviará al volver la conexión` : what)
      onDone()
    } catch (err) {
      setError(errorMessage(err))
      if (tx) financeKeys.forEach((k) => qc.invalidateQueries({ queryKey: k })) // p. ej. cambió en otro dispositivo: se recarga
    } finally {
      setBusy(false)
      confirmRef.current = false
    }
  }
  async function remove() {
    if (!tx || !(await ask({ title: '¿Borrar este gasto?', body: 'No se puede deshacer.', confirm: 'Borrar gasto', danger: true }))) return
    try {
      const label = `${tx.merchant || categoryName || 'Gasto'} ${money(tx.amount_cents)}`
      const result = await sendOrQueue({ kind: 'tx.delete', entity: tx.id, version: tx.version || undefined, base: { ...tx }, label })
      financeKeys.forEach((k) => qc.invalidateQueries({ queryKey: k }))
      toast(result === 'queued' ? 'Gasto borrado en este dispositivo: se enviará al volver la conexión' : 'Gasto borrado')
      onDone()
    } catch (err) {
      setError(errorMessage(err))
      financeKeys.forEach((k) => qc.invalidateQueries({ queryKey: k }))
    }
  }

  return (
    <form onSubmit={submit} className="stack" noValidate>
      <label className="amount-input">
        <span aria-hidden="true">$</span>
        <input
          aria-label="Monto"
          inputMode="decimal"
          autoComplete="off"
          placeholder="0,00"
          autoFocus={!tx && finePointer()}
          value={amount}
          onChange={(e) => setAmount(e.target.value)}
          aria-invalid={amount !== '' && cents === null}
        />
      </label>
      <div className="seg" role="group" aria-label="Tipo">
        <button type="button" aria-pressed={kind === 'purchase'} onClick={() => setKind('purchase')}>Gasto</button>
        <button type="button" aria-pressed={kind === 'refund'} onClick={() => setKind('refund')}>Reverso</button>
      </div>
      <fieldset style={{ border: 0, padding: 0, margin: '14px 0 0' }}>
        <legend className="small muted" style={{ fontWeight: 600, marginBottom: 8 }}>Categoría</legend>
        <div className="chips">
          {tops.map((c) => (
            <button type="button" key={c.id} className="chip" aria-pressed={selectedParent === c.id && categoryId === c.id} onClick={() => setCategoryId(c.id)}>
              <span aria-hidden="true">{c.icon}</span>
              {c.name}
            </button>
          ))}
        </div>
        {selectedParent && children(selectedParent).length > 0 && (
          <div className="chips" style={{ marginTop: 8 }}>
            {children(selectedParent).map((c) => (
              <button type="button" key={c.id} className="chip" aria-pressed={categoryId === c.id} onClick={() => setCategoryId(c.id)}>
                {c.name}
              </button>
            ))}
          </div>
        )}
      </fieldset>
      <div className="grid-2">
        <label className="field">
          <span>Cuenta</span>
          <select className="select" value={effectiveAccount} onChange={(e) => setAccountId(e.target.value)}>
            {accounts.map((a) => (
              <option key={a.id} value={a.id}>
                {a.name}
                {a.card_suffix ? ` (…${a.card_suffix})` : ''}
              </option>
            ))}
          </select>
        </label>
        <label className="field">
          <span>Fecha y hora</span>
          <input className="input" type="datetime-local" required value={when} onChange={(e) => setWhen(e.target.value)} />
        </label>
      </div>
      <label className="field">
        <span>Comercio</span>
        <input className="input" name="merchant" autoComplete="off" maxLength={120} placeholder="Ej. Supermaxi…" value={merchant} onChange={(e) => setMerchant(e.target.value)} />
      </label>
      <details className="more" open={!!tx?.notes}>
        <summary>Notas</summary>
        <textarea className="textarea" maxLength={2000} value={notes} onChange={(e) => setNotes(e.target.value)} aria-label="Notas" />
      </details>
      {!tx && kind === 'purchase' && (
        <label className="check">
          <input type="checkbox" checked={repeat} onChange={(e) => setRepeat(e.target.checked)} />
          <span>Repetir cada mes este día (gasto fijo)</span>
        </label>
      )}
      {categoryChanged && (
        <label className="check">
          <input type="checkbox" checked={learn} onChange={(e) => setLearn(e.target.checked)} />
          <span>Usar siempre {categoryName} para “{merchant.trim()}”</span>
        </label>
      )}
      {pending && <p className="small muted">Este gasto llegó por correo. Revísalo y confírmalo.</p>}
      {error && <p className="error" role="alert">{error}</p>}
      {pending ? (
        <button className="btn btn-primary btn-block" disabled={busy} onClick={() => (confirmRef.current = true)}>Confirmar gasto</button>
      ) : (
        <button className="btn btn-primary btn-block" disabled={busy}>{tx ? 'Guardar cambios' : 'Guardar gasto'}</button>
      )}
      {tx && (
        <button type="button" className="btn btn-danger btn-block" onClick={remove}>Borrar gasto</button>
      )}
    </form>
  )
}
