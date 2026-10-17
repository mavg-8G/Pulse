import { useConfirm } from './Confirm'
import { useEffect, useMemo, useRef, useState, type FormEvent } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { ApiError, errorMessage } from '../lib/api'
import { getPreference, putPreference } from '../lib/vault'
import { sendOrQueue } from '../lib/outbox'
import { nowLocalInput, toLocalInput } from '../lib/dates'
import { centsToInput, money, parseAmount } from '../lib/money'
import { pickable } from '../lib/accounts'
import { financeKeys, useAccounts, useCategories, useMe } from '../lib/queries'
import type { Transaction } from '../lib/types'
import { useToast } from './Toast'
import { finePointer } from '../lib/device'

export function TxForm({ tx, onDone, defaultAccountId, onStateChange, onReplace }: { tx?: Transaction; onDone: () => void; defaultAccountId?: string; onStateChange?: (s: { dirty: boolean; busy: boolean }) => void; onReplace?: (tx: Transaction) => void }) {
  const ask = useConfirm()
  const { data: me } = useMe()
  const tz = me?.timezone ?? 'America/Guayaquil'
  const { data: accounts = [] } = useAccounts(!!tx)
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
  const lock = useRef(false)
  const form = useRef<HTMLFormElement>(null)
  const [fieldError, setFieldError] = useState('')
  const [remembered, setRemembered] = useState('')
  const [conflict, setConflict] = useState<Transaction | null>(null)
  // La misma identidad para un reintento del mismo contenido, incluso si no hay almacén disponible.
  const operation = useRef({ content: '', id: crypto.randomUUID(), entity: crypto.randomUUID() })
  const deletion = useRef(crypto.randomUUID())
  const initial = useRef(JSON.stringify({ amount, kind, categoryId, accountId, when, merchant, notes, learn, repeat }))
  const dirty = initial.current !== JSON.stringify({ amount, kind, categoryId, accountId, when, merchant, notes, learn, repeat })
  useEffect(() => { onStateChange?.({ dirty, busy }) }, [dirty, busy, onStateChange])
  useEffect(() => {
    let active = true
    getPreference<string>('last-account').then((id) => { if (active && id) setRemembered(id) }).catch(() => {})
    return () => { active = false }
  }, [])

  const choices = pickable(accounts, tx?.account_id).filter((a) => !a.archived || a.id === tx?.account_id)
  const eligible = (id?: string) => choices.some((a) => a.id === id) ? id! : ''
  const effectiveAccount = tx ? accountId : eligible(accountId) || eligible(defaultAccountId) || eligible(remembered) || choices[0]?.id || ''
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
    if (lock.current) return
    setError('')
    setFieldError('')
    const invalid = (field: string, message: string) => {
      setFieldError(field); setError(message); confirmRef.current = false
      form.current?.querySelector<HTMLElement>(`[data-field="${field}"]`)?.focus()
    }
    if (cents === null) return invalid('amount', 'Escribe un monto válido, por ejemplo 17,68.')
    if (!categoryId) return invalid('category', 'Elige una categoría.')
    if (!effectiveAccount) return invalid('account', 'Primero crea una cuenta en Ajustes → Cuentas.')
    if (!when || !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/.test(when)) return invalid('when', 'Elige una fecha y hora válidas.')
    lock.current = true
    setBusy(true)
    const body = { amount_cents: cents, kind, category_id: categoryId, account_id: effectiveAccount, occurred_at: when, merchant, notes }
    const label = `${merchant.trim() || categoryName || 'Gasto'} ${money(cents)}`
    const content = JSON.stringify({ body, repeat, learn, confirm: confirmRef.current })
    if (content !== operation.current.content) operation.current = { content, id: crypto.randomUUID(), entity: crypto.randomUUID() }
    try {
      let result: 'sent' | 'queued'
      if (tx) {
        const changes = { ...body, ...(learn && categoryChanged ? { learn_rule: true } : {}), ...(confirmRef.current ? { status: 'confirmed' } : {}) }
        result = await sendOrQueue({ kind: 'tx.update', entity: tx.id, body: changes, version: tx.version || undefined, base: { ...tx }, label }, operation.current.id)
      } else if (repeat) {
        result = await sendOrQueue({ kind: 'rec.create', entity: operation.current.entity, body: { account_id: effectiveAccount, category_id: categoryId, amount_cents: cents, merchant, start_date: when.slice(0, 10) }, label }, operation.current.id)
      } else {
        result = await sendOrQueue({ kind: 'tx.create', entity: operation.current.entity, body, label }, operation.current.id)
      }
      financeKeys.forEach((k) => qc.invalidateQueries({ queryKey: k }))
      if (repeat) qc.invalidateQueries({ queryKey: ['recurring'] })
      const what = confirmRef.current ? 'Gasto confirmado' : tx ? 'Gasto actualizado' : repeat ? 'Gasto fijo creado: se registrará cada mes' : 'Gasto guardado'
      toast(result === 'queued' ? `${what} en este dispositivo: se enviará al volver la conexión` : what)
      if (!tx) await putPreference('last-account', effectiveAccount).catch(() => {})
      onStateChange?.({ dirty: false, busy: false })
      onDone()
    } catch (err) {
      if (err instanceof ApiError && err.code === 'version_conflict' && err.current) setConflict(err.current as Transaction)
      setError(errorMessage(err))
      if (tx) financeKeys.forEach((k) => qc.invalidateQueries({ queryKey: k })) // p. ej. cambió en otro dispositivo: se recarga
    } finally {
      setBusy(false)
      lock.current = false
      confirmRef.current = false
    }
  }
  async function remove() {
    if (!tx || lock.current) return
    lock.current = true
    if (!(await ask({ title: '¿Borrar este gasto?', body: `Se borrará ${tx.merchant || 'este gasto'} por ${money(tx.amount_cents)}. No se puede deshacer.`, confirm: 'Borrar gasto', danger: true }))) { lock.current = false; return }
    setBusy(true)
    setError('')
    try {
      const label = `${tx.merchant || categoryName || 'Gasto'} ${money(tx.amount_cents)}`
      const result = await sendOrQueue({ kind: 'tx.delete', entity: tx.id, version: tx.version || undefined, base: { ...tx }, label }, deletion.current)
      financeKeys.forEach((k) => qc.invalidateQueries({ queryKey: k }))
      toast(result === 'queued' ? 'Gasto borrado en este dispositivo: se enviará al volver la conexión' : 'Gasto borrado')
      onStateChange?.({ dirty: false, busy: false }); onDone()
    } catch (err) {
      setError(errorMessage(err))
      financeKeys.forEach((k) => qc.invalidateQueries({ queryKey: k }))
    } finally { lock.current = false; setBusy(false) }
  }

  return (
    <form ref={form} onSubmit={submit} className="stack tx-form" noValidate aria-busy={busy}>
      <fieldset className="form-controls" disabled={busy}>
      <label className="amount-input">
        <span aria-hidden="true">$</span>
        <input
          aria-label="Monto"
          data-field="amount"
          aria-describedby={fieldError === 'amount' ? 'tx-amount-error' : undefined}
          inputMode="decimal"
          autoComplete="off"
          placeholder="0,00"
          autoFocus={!tx && finePointer()}
          value={amount}
          onChange={(e) => setAmount(e.target.value)}
          aria-invalid={fieldError === 'amount' || (amount !== '' && cents === null)}
        />
      </label>
      {fieldError === 'amount' && <p id="tx-amount-error" className="error" role="alert">{error}</p>}
      <div className="seg" role="group" aria-label="Tipo">
        <button type="button" aria-pressed={kind === 'purchase'} onClick={() => setKind('purchase')}>Gasto</button>
        <button type="button" aria-pressed={kind === 'refund'} onClick={() => { setKind('refund'); setRepeat(false) }}>Devolución</button>
      </div>
      <fieldset tabIndex={-1} data-field="category" aria-describedby={fieldError === 'category' ? 'tx-category-error' : undefined} aria-invalid={fieldError === 'category'} style={{ border: 0, padding: 0, margin: '14px 0 0' }}>
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
      {fieldError === 'category' && <p id="tx-category-error" className="error" role="alert">{error}</p>}
      <div className="grid-2">
        <label className="field">
          <span id="tx-account-label">Cuenta</span>
          <select className="select" data-field="account" aria-labelledby="tx-account-label" aria-invalid={fieldError === 'account'} aria-describedby={fieldError === 'account' ? 'tx-account-error' : undefined} value={effectiveAccount} onChange={(e) => setAccountId(e.target.value)}>
            {choices.map((a) => (
              <option key={a.id} value={a.id}>
                {a.name}
                {a.card_suffix ? ` (…${a.card_suffix})` : ''}
              </option>
            ))}
          </select>
          {fieldError === 'account' && <span id="tx-account-error" className="error" role="alert">{error}</span>}
        </label>
        <label className="field">
          <span id="tx-when-label">Fecha y hora</span>
          <input className="input" data-field="when" aria-labelledby="tx-when-label" aria-invalid={fieldError === 'when'} aria-describedby={fieldError === 'when' ? 'tx-when-error' : undefined} type="datetime-local" required value={when} onChange={(e) => setWhen(e.target.value)} />
          {fieldError === 'when' && <span id="tx-when-error" className="error" role="alert">{error}</span>}
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
      {error && !fieldError && <p className={error.startsWith('Confirmación cancelada') ? 'hint' : 'error'} role={error.startsWith('Confirmación cancelada') ? 'status' : 'alert'}>{error}</p>}
      {conflict && onReplace && <button type="button" className="btn" onClick={async () => {
        if (await ask({ title: '¿Cargar la versión actual?', body: 'Se descartarán los cambios de este formulario.', cancel: 'Seguir editando', confirm: 'Cargar versión actual' })) onReplace(conflict)
      }}>Cargar versión actual</button>}
      {pending ? (
        <button className="btn btn-primary btn-block" disabled={busy} onClick={() => (confirmRef.current = true)}>{busy ? 'Guardando…' : 'Confirmar gasto'}</button>
      ) : (
        <button className="btn btn-primary btn-block" disabled={busy}>{busy ? 'Guardando…' : tx ? 'Guardar cambios' : 'Guardar gasto'}</button>
      )}
      {tx && (
        <button type="button" disabled={busy} className="btn btn-danger btn-block" onClick={remove}>Borrar gasto</button>
      )}
      </fieldset>
    </form>
  )
}
