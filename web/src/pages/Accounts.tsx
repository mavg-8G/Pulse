import { useConfirm } from '../components/Confirm'
import { useState, type CSSProperties, type FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { useQueryClient } from '@tanstack/react-query'
import { Sheet } from '../components/Sheet'
import { QueryFeedback } from '../components/QueryFeedback'
import { useToast } from '../components/Toast'
import { del, errorMessage, patch, post } from '../lib/api'
import { accountResolver, groupedOrder, linkedTo } from '../lib/accounts'
import { financeKeys, useAccounts } from '../lib/queries'
import type { Account, AccountType } from '../lib/types'

const typeText: Record<AccountType, string> = {
  credit_card: 'Tarjeta de crédito',
  debit_card: 'Tarjeta de débito',
  bank: 'Cuenta bancaria',
  cash: 'Efectivo',
  wallet: 'Billetera digital',
}

export function Accounts() {
  const { data: accounts = [], error, refetch, isFetching } = useAccounts(true)
  const [edit, setEdit] = useState<{ open: boolean; acc?: Account }>({ open: false })
  const main = accountResolver(accounts)
  return (
    <>
      <p className="small"><Link to="/ajustes">Ajustes</Link></p>
      <div className="page-head">
        <div><p className="page-kicker">Tu organización</p><h1>Cuentas y tarjetas</h1><p className="page-description">Un lugar para cada forma de pagar.</p></div>
        <button className="btn btn-primary" onClick={() => setEdit({ open: true })}>Agregar cuenta</button>
      </div>
      <p className="muted" style={{ marginBottom: 16 }}>
        Los últimos dígitos de cada tarjeta permitirán asignar automáticamente los gastos que lleguen por correo.
      </p>
      <QueryFeedback error={error} retry={refetch} updating={isFetching} />
      <ul className="list account-collection">
        {groupedOrder(accounts).map((a) => (
          <li key={a.id} className={a.archived ? 'is-archived' : ''} style={{ '--acc': a.color } as CSSProperties}>
            <button className="tx" onClick={() => setEdit({ open: true, acc: a })}>
              <span className="tx-icon" style={{ background: `color-mix(in srgb, ${a.color} 20%, transparent)` }} aria-hidden="true">{a.icon || '💳'}</span>
              <span className="tx-main">
                <span className="tx-title">
                  {a.name} {a.archived && <span className="badge warn">Archivada</span>}
                  {a.merged_into && <span className="badge">Fusionada</span>}
                </span>
                <span className="tx-meta">
                  {typeText[a.type]}
                  {a.institution ? ` de ${a.institution}` : ''}
                  {a.card_suffix ? `, termina en ${a.card_suffix}` : ''}
                  {a.merged_into ? `. Sus gastos se ven en ${main(a.id)?.name ?? 'otra cuenta'}` : ''}
                  {linkedTo(accounts, a.id).length > 0 ? `. Incluye ${linkedTo(accounts, a.id).map((x) => x.name).join(', ')}` : ''}
                </span>
              </span>
              <span className="faint">›</span>
              {a.card_suffix && <span className="acc-number num" aria-hidden="true">•••• {a.card_suffix}</span>}
            </button>
          </li>
        ))}
      </ul>
      <Sheet open={edit.open} title={edit.acc ? 'Editar cuenta' : 'Nueva cuenta'} onClose={() => setEdit({ open: false })}>
        <AccountForm key={edit.acc?.id ?? 'new'} acc={edit.acc} accounts={accounts} onDone={() => setEdit({ open: false })} />
      </Sheet>
    </>
  )
}

function AccountForm({ acc, accounts, onDone }: { acc?: Account; accounts: Account[]; onDone: () => void }) {
  const ask = useConfirm()
  const qc = useQueryClient()
  const toast = useToast()
  const [f, setF] = useState({
    name: acc?.name ?? '',
    type: acc?.type ?? ('credit_card' as AccountType),
    institution: acc?.institution ?? '',
    card_suffix: acc?.card_suffix ?? '',
    color: acc?.color ?? '#0b7a53',
    icon: acc?.icon ?? '💳',
  })
  const [error, setError] = useState('')
  const set = (k: keyof typeof f) => (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) => setF({ ...f, [k]: e.target.value })
  const done = (msg: string) => {
    qc.invalidateQueries({ queryKey: ['accounts'] })
    toast(msg)
    onDone()
  }
  // Fusión lógica: ninguna cuenta se borra; los correos siguen asignando gastos a cualquiera de las dos.
  const targets = acc ? accounts.filter((a) => a.id !== acc.id && !a.merged_into && !a.archived) : []
  const [into, setInto] = useState('')
  const mainName = accounts.find((a) => a.id === acc?.merged_into)?.name ?? 'otra cuenta'
  const refreshAll = (msg: string) => {
    financeKeys.forEach((k) => qc.invalidateQueries({ queryKey: k }))
    ;['reports', 'reconciliation', 'budgets'].forEach((k) => qc.invalidateQueries({ queryKey: [k] }))
    done(msg)
  }
  async function merge() {
    const target = targets.find((a) => a.id === into)
    if (!acc || !target) return setError('Elige la cuenta con la que se fusiona.')
    const ok = await ask({
      title: `¿Fusionar ${acc.name} con ${target.name}?`,
      body: `Sus gastos, reportes y presupuestos se verán como una sola cuenta: ${target.name}. Ninguna se borra, los correos siguen llegando a las dos y puedes separarlas cuando quieras.`,
      confirm: 'Fusionar',
    })
    if (!ok) return
    try {
      await post(`/api/accounts/${acc.id}/merge`, { into: target.id })
      refreshAll(`${acc.name} ahora se ve junto con ${target.name}`)
    } catch (err) {
      setError(errorMessage(err))
    }
  }
  async function unmerge() {
    if (!acc) return
    try {
      await del(`/api/accounts/${acc.id}/merge`)
      refreshAll('Cuentas separadas')
    } catch (err) {
      setError(errorMessage(err))
    }
  }
  async function submit(e: FormEvent) {
    e.preventDefault()
    setError('')
    try {
      if (acc) await patch(`/api/accounts/${acc.id}`, f)
      else await post('/api/accounts', f)
      done('Cuenta guardada')
    } catch (err) {
      setError(errorMessage(err))
    }
  }
  async function toggleArchive() {
    if (!acc) return
    await patch(`/api/accounts/${acc.id}`, { archived: !acc.archived })
    done(acc.archived ? 'Cuenta reactivada' : 'Cuenta archivada')
  }
  async function remove() {
    if (!acc || !(await ask({ title: `¿Borrar ${acc.name}?`, confirm: 'Borrar cuenta', danger: true }))) return
    try {
      await del(`/api/accounts/${acc.id}`)
      done('Cuenta borrada')
    } catch (err) {
      setError(errorMessage(err))
    }
  }
  const isCard = f.type === 'credit_card' || f.type === 'debit_card'
  return (
    <form onSubmit={submit} className="stack">
      <label className="field">
        <span>Nombre</span>
        <input className="input" required maxLength={60} autoComplete="off" placeholder="Ej. Visa Pichincha…" value={f.name} onChange={set('name')} />
      </label>
      <label className="field">
        <span>Tipo</span>
        <select className="select" value={f.type} onChange={set('type')}>
          {Object.entries(typeText).map(([k, v]) => (
            <option key={k} value={k}>{v}</option>
          ))}
        </select>
      </label>
      <div className="grid-2">
        <label className="field">
          <span>Banco o emisor</span>
          <input className="input" maxLength={60} autoComplete="off" placeholder="Ej. Produbanco…" value={f.institution} onChange={set('institution')} />
        </label>
        {isCard && (
          <label className="field">
            <span>Últimos dígitos</span>
            <input className="input num" inputMode="numeric" pattern="[0-9]{3,4}" maxLength={4} placeholder="7828" value={f.card_suffix} onChange={set('card_suffix')} />
          </label>
        )}
      </div>
      <div className="grid-2">
        <label className="field">
          <span>Color</span>
          <input className="input" type="color" value={f.color} onChange={set('color')} style={{ padding: 4 }} />
        </label>
        <label className="field">
          <span>Ícono</span>
          <input className="input" maxLength={4} value={f.icon} onChange={set('icon')} />
        </label>
      </div>
      {error && <p className="error" role="alert">{error}</p>}
      <button className="btn btn-primary btn-block">Guardar cuenta</button>
      {acc?.merged_into && (
        <div className="stack" role="group" aria-label="Cuenta fusionada">
          <p className="small muted">Fusionada con <strong>{mainName}</strong>: sus gastos se ven allí. Separarla no mueve ni borra ningún gasto.</p>
          <button type="button" className="btn btn-sm" onClick={unmerge}>Separar de {mainName}</button>
        </div>
      )}
      {acc && !acc.merged_into && targets.length > 0 && (
        <div className="stack" role="group" aria-label="Fusionar cuentas">
          <label className="field">
            <span>Fusionar con…</span>
            <select className="select" value={into} onChange={(e) => setInto(e.target.value)}>
              <option value="">Elige la cuenta principal</option>
              {targets.map((a) => <option key={a.id} value={a.id}>{a.name}</option>)}
            </select>
            <small className="hint">Úsalo cuando dos cuentas son la misma tarjeta. Ninguna se borra.</small>
          </label>
          <button type="button" className="btn btn-sm" disabled={!into} onClick={merge}>Fusionar</button>
        </div>
      )}
      {acc && (
        <div className="row-actions">
          <button type="button" className="btn btn-sm" onClick={toggleArchive}>{acc.archived ? 'Reactivar' : 'Archivar'}</button>
          <button type="button" className="btn btn-sm btn-danger" onClick={remove}>Borrar</button>
        </div>
      )}
    </form>
  )
}
