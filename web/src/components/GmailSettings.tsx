import { useConfirm } from './Confirm'
import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api, del, errorMessage, get, patch, post } from '../lib/api'
import { dateTimeLabel } from '../lib/dates'
import { financeKeys } from '../lib/queries'
import type { GmailStatus, GmailSyncResult } from '../lib/types'
import { useToast } from './Toast'

const errorText: Record<string, string> = {
  revocado: 'Google retiró el acceso (lo quitaste desde tu cuenta de Google o cambiaste la contraseña).',
  sin_autorizacion: 'Google ya no acepta el acceso de Pulse.',
  limite_de_google: 'Google pidió esperar un momento.',
  google_no_disponible: 'Gmail no respondió.',
  red: 'No se pudo contactar con Google.',
  tiempo_agotado: 'Google tardó demasiado en responder.',
}

export function syncSummary(r: GmailSyncResult): string {
  if (r.created === 0) return r.found === 0 ? 'No hay correos nuevos del banco.' : 'No hay gastos nuevos.'
  const confirmed = r.confirmed ? `, ${r.confirmed} confirmado${r.confirmed > 1 ? 's' : ''} solo${r.confirmed > 1 ? 's' : ''}` : ''
  return `${r.created} gasto${r.created > 1 ? 's' : ''} nuevo${r.created > 1 ? 's' : ''}${confirmed}. Revísalos en Movimientos.`
}

export function GmailSettings({ tz }: { tz: string }) {
  const ask = useConfirm()
  const qc = useQueryClient()
  const toast = useToast()
  const status = useQuery({ queryKey: ['gmail'], queryFn: () => get<GmailStatus>('/api/gmail') })
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const s = status.data

  async function run(fn: () => Promise<void>) {
    setError('')
    setBusy(true)
    try {
      await fn()
    } catch (e) {
      setError(errorMessage(e))
    } finally {
      setBusy(false)
      qc.invalidateQueries({ queryKey: ['gmail'] })
    }
  }
  const connect = () =>
    run(async () => {
      // Pide confirmar identidad (step-up) y luego lleva a Google; vuelve a /ajustes/gmail.
      const { url } = await post<{ url: string }>('/api/gmail/connect')
      window.location.assign(url)
    })
  const sync = () =>
    run(async () => {
      const r = await post<GmailSyncResult>('/api/gmail/sync')
      financeKeys.forEach((k) => qc.invalidateQueries({ queryKey: k }))
      qc.invalidateQueries({ queryKey: ['accounts'] })
      toast(syncSummary(r))
    })
  const disconnect = async () => {
    if (!(await ask({ title: '¿Desconectar Gmail?', body: 'Pulse dejará de leer tus correos y Google revocará el acceso. Los gastos ya importados se conservan.', confirm: 'Desconectar Gmail', danger: true }))) return
    run(async () => {
      await del('/api/gmail')
      toast('Gmail desconectado')
    })
  }
  const setShadow = (shadow: boolean) =>
    run(async () => {
      await patch('/api/gmail', { shadow })
    })
  const setGemini = async (enabled: boolean) => {
    if (enabled && !(await ask({ title: '¿Activar Gemini?', body: 'Los correos del banco que Pulse no sepa leer se enviarán a Gemini (Google) sin tu nombre, emails ni números largos.', confirm: 'Activar Gemini' }))) return
    run(async () => {
      await api('PUT', '/api/gmail/gemini', { enabled })
      toast(enabled ? 'Gemini activado' : 'Gemini desactivado')
    })
  }

  return (
    <section className="section" aria-labelledby="h-gmail">
      <div className="section-head"><h2 id="h-gmail">Correo del banco (Gmail)</h2></div>
      {(error || status.error) && <p className="error" role="alert" style={{ marginBottom: 10 }}>{error || errorMessage(status.error)}</p>}
      {s && !s.available && (
        <p className="hint">La conexión con Gmail todavía no está configurada en el servidor. Mientras tanto puedes importar correos .eml desde Movimientos.</p>
      )}
      {s?.available && !s.connected && (
        <div className="list">
          <div className="settings-row">
            <span className="grow">
              Registrar gastos automáticamente
              <br />
              <span className="small faint">
                Pulse lee solo las notificaciones de tus bancos y nunca guarda tus correos.{' '}
                <a href="/privacidad#gmail">Qué datos usa</a>
              </span>
            </span>
            <button className="btn btn-sm btn-primary" disabled={busy} onClick={connect}>Conectar Gmail</button>
          </div>
        </div>
      )}
      {s?.connected && (
        <div className="list">
          <div className="settings-row">
            <span className="grow">
              {s.email} {s.status === 'active' ? <span className="badge">Conectado</span> : <span className="badge warn">Sin acceso</span>}
              <br />
              <span className="small faint" data-testid="gmail-sync-info">
                {s.status === 'broken'
                  ? (errorText[s.last_error ?? ''] ?? 'La conexión se perdió.') + ' Vuelve a conectar.'
                  : s.last_ok_at
                    ? `Última lectura ${dateTimeLabel(s.last_ok_at, tz)}${s.last_error ? ` · ${errorText[s.last_error] ?? 'reintentando'}` : ''}`
                    : 'Leyendo tus correos por primera vez…'}
                {s.status === 'active' && (s.realtime ? ' · al instante' : ` · cada ${s.poll_minutes} min`)}
                {s.imported > 0 && ` · ${s.imported} importado${s.imported > 1 ? 's' : ''}`}
              </span>
            </span>
            <span className="row-actions">
              {s.status === 'active' ? (
                <button className="btn btn-sm" disabled={busy} onClick={sync}>Sincronizar ahora</button>
              ) : (
                <button className="btn btn-sm btn-primary" disabled={busy} onClick={connect}>Reconectar</button>
              )}
              <button className="btn btn-sm btn-danger" disabled={busy} onClick={disconnect}>Desconectar</button>
            </span>
          </div>
          <label className="settings-row">
            <span className="grow">
              Modo sombra
              <br />
              <span className="small faint">Todo lo que llega por correo queda “Por revisar”. Desactívalo para confirmar solo los gastos de comercios con regla.</span>
            </span>
            <input type="checkbox" checked={s.shadow} disabled={busy} onChange={(e) => setShadow(e.target.checked)} />
          </label>
          {s.gemini_available && (
            <label className="settings-row">
              <span className="grow">
                Usar Gemini con correos no reconocidos
                <br />
                <span className="small faint">Solo para remitentes bancarios con un formato nuevo. Se envía sin tu nombre, emails ni números largos.</span>
              </span>
              <input type="checkbox" checked={s.gemini} disabled={busy} onChange={(e) => setGemini(e.target.checked)} />
            </label>
          )}
        </div>
      )}
      {s?.available && (
        <p className="hint" style={{ marginTop: 8 }}>
          Remitentes que lee Pulse: {s.senders.join(', ')}.
        </p>
      )}
    </section>
  )
}
