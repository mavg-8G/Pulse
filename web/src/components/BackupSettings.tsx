import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { errorMessage, get, getBlob, post } from '../lib/api'
import { dateTimeLabel } from '../lib/dates'
import type { BackupRun, BackupStatus } from '../lib/types'
import { useToast } from './Toast'

const stateText: Record<BackupStatus['state'], string> = {
  off: 'Apagado',
  pending: 'Aún no hay respaldos',
  ok: 'Al día',
  stale: 'Desactualizado',
  failing: 'El último intento falló',
}

export function sizeLabel(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1).replace('.', ',')} KB`
  return `${(bytes / 1024 / 1024).toFixed(1).replace('.', ',')} MB`
}

/** Respaldo externo cifrado (solo admin). Respaldar y descargar piden confirmar identidad. */
export function BackupSettings({ tz }: { tz: string }) {
  const qc = useQueryClient()
  const toast = useToast()
  const st = useQuery({ queryKey: ['admin-backups'], queryFn: () => get<BackupStatus>('/api/admin/backups') })
  const [busy, setBusy] = useState<'' | 'run' | 'download'>('')
  const [error, setError] = useState('')
  const s = st.data

  async function runNow() {
    setError('')
    setBusy('run')
    try {
      const run = await post<BackupRun>('/api/admin/backups')
      if (run.status === 'ok') toast(`Respaldo cifrado enviado (${sizeLabel(run.size_bytes)})`)
      else setError(`El respaldo falló: ${run.error}`)
    } catch (e) {
      setError(errorMessage(e))
    } finally {
      setBusy('')
      qc.invalidateQueries({ queryKey: ['admin-backups'] })
    }
  }

  async function download() {
    setError('')
    setBusy('download')
    try {
      const blob = await getBlob('/api/admin/backups/latest')
      const name = s?.last_ok?.name || 'pulse-respaldo.db.age'
      const url = URL.createObjectURL(blob)
      const a = Object.assign(document.createElement('a'), { href: url, download: name })
      a.click()
      setTimeout(() => URL.revokeObjectURL(url), 1000)
    } catch (e) {
      setError(errorMessage(e))
    } finally {
      setBusy('')
    }
  }

  return (
    <section className="section" aria-labelledby="h-backup">
      <div className="section-head"><h2 id="h-backup">Respaldo (admin)</h2></div>
      {!s ? (
        st.error ? <p className="error" role="alert">{errorMessage(st.error)}</p> : <p className="hint">Cargando…</p>
      ) : !s.configured ? (
        <p className="hint">
          El respaldo externo está apagado. Se activa en el servidor con <code>PULSE_BACKUP_TARGET</code> y tu clave pública age
          (ver la guía de despliegue).
        </p>
      ) : (
        <div className="stack">
          <div className="list">
            <div className="settings-row">
              <span className="grow">
                Estado<br />
                <span className="small faint">Cifrado con age y enviado a {s.target} · {s.interval === 'manual' ? 'solo a mano' : `cada ${s.interval}`}</span>
              </span>
              <span className={s.state === 'ok' ? 'muted' : 'error'} data-testid="backup-state">{stateText[s.state]}</span>
            </div>
            {s.last_ok && (
              <div className="settings-row">
                <span className="grow">
                  Último respaldo correcto<br />
                  <span className="small faint">{dateTimeLabel(s.last_ok.finished_at, tz)} · {sizeLabel(s.last_ok.size_bytes)}</span>
                </span>
              </div>
            )}
          </div>
          {error && <p className="error" role="alert">{error}</p>}
          <div className="row-actions">
            <button className="btn btn-primary" disabled={busy !== '' || s.running} onClick={runNow}>
              {busy === 'run' || s.running ? 'Respaldando…' : 'Respaldar ahora'}
            </button>
            {s.download && (
              <button className="btn" disabled={busy !== ''} onClick={download}>
                {busy === 'download' ? 'Descargando…' : 'Descargar copia cifrada'}
              </button>
            )}
          </div>
          <p className="small faint">
            Pulse solo tiene la clave pública: para leer un respaldo hacen falta tu identidad age y la clave maestra, que guardas fuera del servidor.
          </p>
          {s.runs.length > 0 && (
            <div className="report-table-scroll" tabIndex={0}>
              <table className="report-table">
                <caption className="sr-only">Últimos intentos de respaldo</caption>
                <thead>
                  <tr><th scope="col">Fecha</th><th scope="col">Origen</th><th scope="col">Tamaño</th><th scope="col">Resultado</th></tr>
                </thead>
                <tbody>
                  {s.runs.map((r) => (
                    <tr key={r.id}>
                      <th scope="row">{dateTimeLabel(r.finished_at, tz)}</th>
                      <td>{r.trigger === 'manual' ? 'A mano' : 'Programado'}</td>
                      <td className="num">{r.status === 'ok' ? sizeLabel(r.size_bytes) : '—'}</td>
                      <td>
                        {r.status === 'ok' ? (
                          <span title={`SHA-256 ${r.sha256}`}>Correcto</span>
                        ) : (
                          <span className="error">Falló tras {r.attempts} {r.attempts === 1 ? 'intento' : 'intentos'}: {r.error}</span>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          {s.last_ok && (
            <p className="small faint">Huella del último respaldo correcto (para <code>pulse restore --sha256</code>): <code style={{ wordBreak: 'break-all' }}>{s.last_ok.sha256}</code></p>
          )}
        </div>
      )}
    </section>
  )
}
