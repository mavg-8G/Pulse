import { useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useToast } from '../components/Toast'
import { ApiError } from '../lib/api'
import { financeKeys } from '../lib/queries'
import { MonthSwitch, useTxSheet } from '../components/AppShell'
import { TxGroups } from '../components/TxList'
import { monthLabel, monthOf } from '../lib/dates'
import { money } from '../lib/money'
import { useAccounts, useCategories, useMe, useTransactions } from '../lib/queries'

const PAGE = 50

export function Movements() {
  const { data: me } = useMe()
  const tz = me?.timezone ?? 'America/Guayaquil'
  const [month, setMonth] = useState(() => {
    const requested = new URLSearchParams(window.location.search).get('month') ?? ''
    return /^\d{4}-(0[1-9]|1[0-2])$/.test(requested) ? requested : monthOf(new Date(), tz)
  })
  const [q, setQ] = useState('')
  const [category, setCategory] = useState('')
  const [account, setAccount] = useState('')
  const [limit, setLimit] = useState(PAGE)
  const { data: categories = [] } = useCategories()
  const { data: accounts = [] } = useAccounts(true)
  const { data, isLoading, error } = useTransactions({ month, q: q.trim(), category_id: category, account_id: account, limit })
  const openTx = useTxSheet()
  const fileRef = useRef<HTMLInputElement>(null)
  const qc = useQueryClient()
  const toast = useToast()
  async function importFiles(files: FileList | null) {
    if (!files?.length) return
    const results: string[] = []
    for (const f of Array.from(files)) {
      try {
        const res = await fetch('/api/import/eml', { method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'message/rfc822' }, body: f })
        const data = await res.json().catch(() => ({}))
        if (!res.ok) throw new ApiError(res.status, data?.error?.code ?? 'error', data?.error?.message ?? 'No se pudo importar.')
        results.push(importMessage(data))
      } catch (e) {
        results.push(`${f.name}: ${e instanceof Error ? e.message : 'error'}`)
      }
    }
    financeKeys.forEach((k) => qc.invalidateQueries({ queryKey: k }))
    qc.invalidateQueries({ queryKey: ['accounts'] })
    toast(results.join('. '))
    if (fileRef.current) fileRef.current.value = ''
  }
  const tops = categories.filter((c) => !c.parent_id)
  const filtered = q.trim() !== '' || category !== '' || account !== ''

  return (
    <>
      <div className="page-head">
        <div><p className="page-kicker">Tu actividad</p><h1>Movimientos</h1><p className="page-description">Cada compra, devolución y detalle, en un solo lugar.</p></div>
        <div className="row-actions" style={{ alignItems: 'center' }}>
          <MonthSwitch month={month} onChange={(m) => { setMonth(m); setLimit(PAGE) }} label={monthLabel(month)} />
          <button className="btn btn-sm" onClick={() => fileRef.current?.click()}>Importar correo</button>
          <input ref={fileRef} type="file" accept=".eml,message/rfc822" multiple hidden aria-label="Archivo .eml del banco" onChange={(e) => importFiles(e.target.files)} />
        </div>
      </div>
      <div className="filters panel" role="search" aria-label="Filtrar movimientos">
        <input className="input search" type="search" name="q" autoComplete="off" placeholder="Buscar comercio o nota…" aria-label="Buscar" value={q} onChange={(e) => setQ(e.target.value)} />
        <select className="select" aria-label="Categoría" value={category} onChange={(e) => setCategory(e.target.value)}>
          <option value="">Todas las categorías</option>
          {tops.map((c) => (
            <option key={c.id} value={c.id}>{c.icon} {c.name}</option>
          ))}
        </select>
        <select className="select" aria-label="Cuenta" value={account} onChange={(e) => setAccount(e.target.value)}>
          <option value="">Todas las cuentas</option>
          {accounts.map((a) => (
            <option key={a.id} value={a.id}>{a.name}</option>
          ))}
        </select>
      </div>
      {data && data.total > 0 && (
        <p className="totals-line">
          <span>{data.total} {data.total === 1 ? 'movimiento' : 'movimientos'}</span>
          <strong className="num" data-testid="list-sum">{money(data.sum_cents)}</strong>
        </p>
      )}
      {isLoading && <p className="loading">Cargando…</p>}
      {error && <p className="error" role="alert">No se pudieron cargar los movimientos.</p>}
      {data && data.total === 0 && (
        <div className="panel empty" style={{ marginTop: 16 }}>
          {filtered ? (
            <>
              <h3>Nada coincide con esos filtros</h3>
              <p>Prueba con otra palabra u otro mes.</p>
            </>
          ) : (
            <>
              <h3>Sin movimientos en {monthLabel(month).toLowerCase()}</h3>
              <button className="btn btn-primary" onClick={() => openTx()}>Registrar gasto</button>
            </>
          )}
        </div>
      )}
      {data && <TxGroups items={data.items} categories={categories} accounts={accounts} tz={tz} />}
      {data && data.items.length < data.total && (
        <button className="btn btn-block" style={{ marginTop: 16 }} onClick={() => setLimit(limit + PAGE)}>
          Mostrar más
        </button>
      )}
    </>
  )
}

function importMessage(d: any): string {
  switch (d.status) {
    case 'created':
      return `Agregado para revisar: ${d.transaction.merchant} en ${d.category}${d.new_account ? ` (cuenta nueva: ${d.account})` : ''}`
    case 'duplicate':
      return 'Ese correo ya estaba importado'
    case 'ignored':
      return `No es un gasto: ${d.reason}`
    case 'unrecognized':
      return `No se pudo leer: ${d.reason}`
    default:
      return 'Ese remitente no es un banco soportado'
  }
}
