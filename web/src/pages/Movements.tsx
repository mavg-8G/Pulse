import { useEffect, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { QueryFeedback } from '../components/QueryFeedback'
import { ApiError, errorMessage } from '../lib/api'
import { useDebounced, useMovementView, useViewScroll } from '../lib/viewState'
import { financeKeys } from '../lib/queries'
import { MonthSwitch, useTxSheet } from '../components/AppShell'
import { TxGroups } from '../components/TxList'
import { monthLabel, monthOf } from '../lib/dates'
import { money } from '../lib/money'
import { pickable } from '../lib/accounts'
import { useAccounts, useCategories, useMe, useTransactions } from '../lib/queries'

const PAGE = 50

export function Movements() {
  const { data: me } = useMe()
  const tz = me?.timezone ?? 'America/Guayaquil'
  const current = monthOf(new Date(), tz)
  const { month, category, account, limit, q, setQ, update } = useMovementView(current)
  const search = useDebounced(q.trim())
  const cats = useCategories()
  const accs = useAccounts(true)
  const categories = cats.data ?? []
  const accounts = accs.data ?? []
  const { data, isLoading, isFetching, error, refetch } = useTransactions({ month, q: search, category_id: category, account_id: account, limit })
  useViewScroll('movements', `${month}|${category}|${account}|${search}`, !!data)
  useEffect(() => {
    if (category && cats.data && !cats.data.some((c) => c.id === category && !c.parent_id)) update({ category: '' })
    else if (account && accs.data && !pickable(accs.data).some((a) => a.id === account)) {
      const main = accs.data.find((a) => a.id === account)?.merged_into
      update({ account: main ?? '' })
    }
  }, [category, account, cats.data, accs.data])
  const openTx = useTxSheet()
  const fileRef = useRef<HTMLInputElement>(null)
  const qc = useQueryClient()
  const [importResults, setImportResults] = useState<string[]>([])
  const [importing, setImporting] = useState(false)
  useEffect(() => { if (importResults.length) document.getElementById('import-results')?.focus() }, [importResults])
  const importLock = useRef(false)
  async function importFiles(files: FileList | null) {
    if (!files?.length || importLock.current) return
    importLock.current = true
    setImporting(true)
    setImportResults([])
    const results: string[] = []
    for (const f of Array.from(files)) {
      try {
        const res = await fetch('/api/import/eml', { method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'message/rfc822' }, body: f })
        const data = await res.json().catch(() => ({}))
        if (!res.ok) throw new ApiError(res.status, data?.error?.code ?? 'error', data?.error?.message ?? 'No se pudo importar.')
        results.push(`${f.name}: ${importMessage(data)}`)
      } catch (e) {
        results.push(`${f.name}: ${errorMessage(e)}`)
      }
    }
    financeKeys.forEach((k) => qc.invalidateQueries({ queryKey: k }))
    qc.invalidateQueries({ queryKey: ['accounts'] })
    setImportResults(results)
    setImporting(false)
    importLock.current = false
    if (fileRef.current) fileRef.current.value = ''
  }
  const tops = categories.filter((c) => !c.parent_id)
  const filtered = q.trim() !== '' || category !== '' || account !== ''

  return (
    <>
      <div className="page-head">
        <div><p className="page-kicker">Tu actividad</p><h1>Movimientos</h1><p className="page-description">Cada compra, devolución y detalle, en un solo lugar.</p></div>
        <div className="row-actions" style={{ alignItems: 'center' }}>
          <MonthSwitch month={month} onChange={(m) => update({ month: m, limit: PAGE }, true)} label={monthLabel(month)} />
          {month !== current && <button className="btn btn-sm" onClick={() => update({ month: current, limit: PAGE }, true)}>Volver al mes actual</button>}
          <button className="btn btn-sm" disabled={importing} onClick={() => fileRef.current?.click()}>{importing ? 'Importando…' : 'Importar correo'}</button>
          <input ref={fileRef} type="file" accept=".eml,message/rfc822" multiple hidden aria-label="Archivo .eml del banco" onChange={(e) => importFiles(e.target.files)} />
        </div>
      </div>
      <div className="filters panel" role="search" aria-label="Filtrar movimientos">
        <input className="input search" type="search" name="q" autoComplete="off" placeholder="Buscar comercio o nota…" aria-label="Buscar" value={q} onChange={(e) => setQ(e.target.value)} />
        <select className="select" aria-label="Categoría" value={category} onChange={(e) => update({ category: e.target.value, limit: PAGE })}>
          <option value="">Todas las categorías</option>
          {tops.map((c) => (
            <option key={c.id} value={c.id}>{c.icon} {c.name}</option>
          ))}
        </select>
        <select className="select" aria-label="Cuenta" value={account} onChange={(e) => update({ account: e.target.value, limit: PAGE })}>
          <option value="">Todas las cuentas</option>
          {pickable(accounts).map((a) => (
            <option key={a.id} value={a.id}>{a.name}</option>
          ))}
        </select>
      </div>
      {filtered && <button className="btn btn-sm" onClick={() => { setQ(''); update({ category: '', account: '', limit: PAGE }) }}>Limpiar filtros</button>}
      {importResults.length > 0 && <section className="panel import-results" aria-labelledby="import-results"><h2 id="import-results" tabIndex={-1}>Resultado de la importación</h2><p className="small">{importResults.length} {importResults.length === 1 ? 'archivo procesado' : 'archivos procesados'}. Revisa el resultado de cada uno.</p><ul>{importResults.map((r, i) => <li key={i}>{r}</li>)}</ul><button className="btn btn-sm" onClick={() => setImportResults([])}>Cerrar resultados</button></section>}
      {data && data.total > 0 && (
        <p className="totals-line">
          <span>{data.total} {data.total === 1 ? 'movimiento' : 'movimientos'}</span>
          <strong className="num" data-testid="list-sum">{money(data.sum_cents)}</strong>
        </p>
      )}
      {isLoading && <div className="list-skeleton" role="status" aria-label="Cargando movimientos"><div /><div /><div /></div>}
      <QueryFeedback error={error} retry={refetch} updating={isFetching || search !== q.trim()} />
      {data && data.total === 0 && search === q.trim() && !isFetching && (
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
      {data && data.items.length < data.total && limit < 5000 && (
        <button className="btn btn-block" style={{ marginTop: 16 }} disabled={isFetching || search !== q.trim()} onClick={() => update({ limit: Math.min(5000, limit + PAGE) })}>
          Mostrar más
        </button>
      )}
      {data && limit >= 5000 && data.items.length < data.total && <p className="hint">Hay más movimientos. Acota los filtros para encontrarlos.</p>}
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
