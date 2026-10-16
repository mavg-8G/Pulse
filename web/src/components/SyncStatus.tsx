import { useEffect, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { dateTimeLabel } from '../lib/dates'
import { money } from '../lib/money'
import { useNetwork, usePendingOps } from '../lib/offline'
import { conflicts, flush, pendingCount, resolve, type Op } from '../lib/outbox'
import { financeKeys } from '../lib/queries'
import { Sheet } from './Sheet'
import { useToast } from './Toast'

const RETRY_MS = 30_000

const verbText: Record<string, string> = {
  'tx.create': 'Gasto nuevo',
  'tx.update': 'Cambio en un gasto',
  'tx.delete': 'Gasto borrado',
  'rec.create': 'Gasto fijo nuevo',
  'rec.update': 'Cambio en un gasto fijo',
  'rec.delete': 'Gasto fijo quitado',
}

/** Envía la cola al volver la conexión (y cada 30 s mientras quede algo) y muestra qué falta enviar. */
export function SyncStatus({ tz }: { tz: string }) {
  const ops = usePendingOps()
  const net = useNetwork()
  const qc = useQueryClient()
  const toast = useToast()
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const waiting = pendingCount(ops)
  const conflicted = conflicts(ops)

  async function sync(manual = false) {
    setBusy(manual)
    try {
      const r = await flush()
      if (r.sent) {
        financeKeys.forEach((k) => qc.invalidateQueries({ queryKey: k }))
        qc.invalidateQueries({ queryKey: ['recurring'] })
        toast(r.sent === 1 ? 'Se envió un cambio hecho sin conexión' : `Se enviaron ${r.sent} cambios hechos sin conexión`)
      }
      if (r.conflicts) {
        toast(r.conflicts === 1 ? 'Un cambio choca con otro dispositivo: elige cuál conservar' : `${r.conflicts} cambios chocan con otro dispositivo: elige cuáles conservar`)
        setOpen(true)
      }
      r.rejected.forEach((m) => toast(`No se pudo guardar: ${m}`))
      if (manual && !r.sent && !r.conflicts && waiting) toast('Todavía sin conexión. Se reintentará solo.')
    } catch {
      /* sin almacén local */
    } finally {
      setBusy(false)
    }
  }

  useEffect(() => {
    const onOnline = () => sync()
    window.addEventListener('online', onOnline)
    const t = waiting > 0 ? window.setInterval(() => navigator.onLine !== false && sync(), RETRY_MS) : undefined
    return () => {
      window.removeEventListener('online', onOnline)
      if (t) window.clearInterval(t)
    }
  }, [waiting]) // eslint-disable-line react-hooks/exhaustive-deps

  if (!net.offline && ops.length === 0) return null
  const label = conflicted.length
    ? `${conflicted.length} por resolver`
    : waiting
      ? `${waiting} pendiente${waiting === 1 ? '' : 's'} de enviar`
      : 'Sin conexión'
  return (
    <>
      <button className={`sync-pill ${conflicted.length ? 'sync-conflict' : ''}`} onClick={() => setOpen(true)} aria-haspopup="dialog" data-testid="sync-status">
        <span className="sync-dot" aria-hidden="true" />
        {/* En móvil se ve solo el número (vía CSS, sin duplicar el texto accesible). */}
        <span className="sync-long" data-short={conflicted.length || waiting || 'Sin red'}>{label}</span>
      </button>
      <Sheet open={open} title="Cambios en este dispositivo" onClose={() => setOpen(false)}>
        <div className="stack">
          {net.offline && (
            <p className="hint" role="status">
              Sin conexión. {net.cachedAt ? `Ves lo guardado el ${dateTimeLabel(new Date(net.cachedAt).toISOString(), tz)}.` : ''} Lo que hagas se guarda cifrado en este dispositivo y se envía solo al volver la conexión.
            </p>
          )}
          {conflicted.map((op) => (
            <Conflict key={op.id} op={op} tz={tz} onDone={() => { sync(); financeKeys.forEach((k) => qc.invalidateQueries({ queryKey: k })) }} />
          ))}
          {waiting > 0 && (
            <>
              <h3 className="small">Pendientes de enviar</h3>
              <ul className="list">
                {ops.filter((o) => o.state === 'pending').map((o) => (
                  <li key={o.id} className="settings-row">
                    <span className="grow">{verbText[o.kind]}<br /><span className="small faint">{o.label} · {dateTimeLabel(new Date(o.at).toISOString(), tz)}</span></span>
                  </li>
                ))}
              </ul>
              <button className="btn btn-primary" disabled={busy} onClick={() => sync(true)}>{busy ? 'Enviando…' : 'Reintentar ahora'}</button>
            </>
          )}
          {ops.length === 0 && <p className="hint">No hay cambios pendientes.</p>}
        </div>
      </Sheet>
    </>
  )
}

function describe(v: Record<string, unknown> | null | undefined) {
  if (!v) return ''
  const parts = [v.merchant, typeof v.amount_cents === 'number' ? money(v.amount_cents) : '', v.kind === 'refund' ? 'reverso' : ''].filter(Boolean)
  return parts.join(' · ')
}

function Conflict({ op, tz, onDone }: { op: Op; tz: string; onDone: () => void }) {
  const choose = async (c: 'mine' | 'theirs' | 'recreate') => {
    await resolve(op.id, c)
    onDone()
  }
  const isDelete = op.kind.endsWith('.delete')
  return (
    <div className="panel conflict" role="group" aria-label={`Conflicto: ${op.label}`}>
      <p><strong>{op.label}</strong></p>
      {op.reason === 'deleted' ? (
        <>
          <p className="small">Lo borraron en otro dispositivo mientras lo editabas aquí ({dateTimeLabel(new Date(op.at).toISOString(), tz)}).</p>
          <div className="row-actions">
            <button className="btn btn-primary btn-sm" onClick={() => choose('recreate')}>Volver a crearlo con mis cambios</button>
            <button className="btn btn-sm" onClick={() => choose('theirs')}>Descartar mis cambios</button>
          </div>
        </>
      ) : (
        <>
          <p className="small">
            {isDelete ? 'Lo borraste aquí, pero cambió en otro dispositivo.' : 'Lo cambiaste aquí y también en otro dispositivo.'}
            <br />En el servidor ahora: {describe(op.current)}
            {!isDelete && <><br />Tu versión: {describe({ ...op.base, ...op.body })}</>}
          </p>
          <div className="row-actions">
            <button className="btn btn-primary btn-sm" onClick={() => choose('mine')}>{isDelete ? 'Borrarlo igual' : 'Usar la mía'}</button>
            <button className="btn btn-sm" onClick={() => choose('theirs')}>Conservar la del servidor</button>
          </div>
        </>
      )}
    </div>
  )
}
