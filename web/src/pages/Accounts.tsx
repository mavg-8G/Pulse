import { useState, type FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { useQueryClient } from '@tanstack/react-query'
import { Sheet } from '../components/Sheet'
import { useToast } from '../components/Toast'
import { del, errorMessage, patch, post } from '../lib/api'
import { useAccounts } from '../lib/queries'
import type { Account, AccountType } from '../lib/types'

const typeText: Record<AccountType, string> = {
  credit_card: 'Tarjeta de crédito',
  debit_card: 'Tarjeta de débito',
  bank: 'Cuenta bancaria',
  cash: 'Efectivo',
  wallet: 'Billetera digital',
}

export function Accounts() {
  const { data: accounts = [] } = useAccounts(true)
  const [edit, setEdit] = useState<{ open: boolean; acc?: Account }>({ open: false })
  return (
    <>
      <p className="small"><Link to="/ajustes">Ajustes</Link></p>
      <div className="page-head">
        <h1>Cuentas y tarjetas</h1>
        <button className="btn btn-primary" onClick={() => setEdit({ open: true })}>Agregar cuenta</button>
      </div>
      <p className="muted" style={{ marginBottom: 16 }}>
        Los últimos dígitos de cada tarjeta permitirán asignar automáticamente los gastos que lleguen por correo.
      </p>
      <ul className="list">
        {accounts.map((a) => (
          <li key={a.id}>
            <button className="tx" onClick={() => setEdit({ open: true, acc: a })}>
              <span className="tx-icon" style={{ background: `color-mix(in srgb, ${a.color} 20%, transparent)` }} aria-hidden="true">{a.icon || '💳'}</span>
              <span className="tx-main">
                <span className="tx-title">
                  {a.name} {a.archived && <span className="badge warn">Archivada</span>}
                </span>
                <span className="tx-meta">
                  {typeText[a.type]}
                  {a.institution ? ` de ${a.institution}` : ''}
                  {a.card_suffix ? `, termina en ${a.card_suffix}` : ''}
                </span>
              </span>
              <span className="faint">›</span>
            </button>
          </li>
        ))}
      </ul>
      <Sheet open={edit.open} title={edit.acc ? 'Editar cuenta' : 'Nueva cuenta'} onClose={() => setEdit({ open: false })}>
        <AccountForm key={edit.acc?.id ?? 'new'} acc={edit.acc} onDone={() => setEdit({ open: false })} />
      </Sheet>
    </>
  )
}

function AccountForm({ acc, onDone }: { acc?: Account; onDone: () => void }) {
  const qc = useQueryClient()
  const toast = useToast()
  const [f, setF] = useState({
    name: acc?.name ?? '',
    type: acc?.type ?? ('credit_card' as AccountType),
    institution: acc?.institution ?? '',
    card_suffix: acc?.card_suffix ?? '',
    color: acc?.color ?? '#a4452f',
    icon: acc?.icon ?? '💳',
  })
  const [error, setError] = useState('')
  const set = (k: keyof typeof f) => (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) => setF({ ...f, [k]: e.target.value })
  const done = (msg: string) => {
    qc.invalidateQueries({ queryKey: ['accounts'] })
    toast(msg)
    onDone()
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
    if (!acc || !confirm(`¿Borrar ${acc.name}?`)) return
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
        <input className="input" required maxLength={60} placeholder="Ej. Visa Pichincha" value={f.name} onChange={set('name')} />
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
          <input className="input" maxLength={60} placeholder="Ej. Produbanco" value={f.institution} onChange={set('institution')} />
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
      {acc && (
        <div className="row-actions">
          <button type="button" className="btn btn-sm" onClick={toggleArchive}>{acc.archived ? 'Reactivar' : 'Archivar'}</button>
          <button type="button" className="btn btn-sm btn-danger" onClick={remove}>Borrar</button>
        </div>
      )}
    </form>
  )
}
