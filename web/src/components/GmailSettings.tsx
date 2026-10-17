import { useConfirm } from './Confirm'
import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { QueryFeedback } from './QueryFeedback'
import { api, del, errorMessage, get, patch, post } from '../lib/api'
import { dateTimeLabel } from '../lib/dates'
import { financeKeys } from '../lib/queries'
import type { GmailStatus, GmailSyncResult } from '../lib/types'
import { useToast } from './Toast'
import { GmailServerSettings } from './GmailServerSettings'
import { useMe } from '../lib/queries'

const errorText: Record<string, string> = {
  revocado: 'Google retiró el acceso (lo quitaste desde tu cuenta de Google o cambiaste la contraseña).',
  sin_autorizacion: 'Google ya no acepta el acceso de Pulse.',
  limite_de_google: 'Google pidió esperar un momento.',
  google_no_disponible: 'Gmail no respondió.',
  red: 'No se pudo contactar con Google.',
  tiempo_agotado: 'Google tardó demasiado en responder.',
}

export function syncSummary(r: GmailSyncResult): string {
  const pending = r.unrecognized ? ` ${r.unrecognized} correo${r.unrecognized > 1 ? 's siguen' : ' sigue'} sin reconocer; consulta los pendientes abajo.` : ''
  if (r.created === 0) return (r.found === 0 ? 'No hay correos nuevos del banco.' : 'No hay gastos nuevos.') + pending
  const confirmed = r.confirmed ? `, ${r.confirmed} confirmado${r.confirmed > 1 ? 's' : ''} solo${r.confirmed > 1 ? 's' : ''}` : ''
  return `${r.created} gasto${r.created > 1 ? 's' : ''} nuevo${r.created > 1 ? 's' : ''}${confirmed}. Revísalos en Movimientos.` + pending
}

export function GmailSettings({ tz }: { tz: string }) {
  const ask = useConfirm()
  const qc = useQueryClient()
  const toast = useToast()
  const isAdmin = useMe().data?.role === 'admin'
  const status = useQuery({ queryKey: ['gmail'], queryFn: () => get<GmailStatus>('/api/gmail') })
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const s = status.data
  const pendingCount = s?.template_alerts_total ?? s?.template_alerts.length ?? 0

  async function run(fn: () => Promise<void | string>) {
    setError('')
    setBusy(true)
    let message: void | string = undefined
    try {
      message = await fn()
    } catch (e) {
      setError(errorMessage(e))
    } finally {
      await qc.invalidateQueries({ queryKey: ['gmail'] })
      setBusy(false)
    }
    if (message) toast(message)
  }
  const connect = () =>
    run(async () => {
      // Pide confirmar identidad (step-up) y luego lleva a Google; vuelve a /ajustes/gmail.
      const { url } = await post<{ url: string }>('/api/gmail/connect')
      window.location.assign(url)
    })
  const sync = (retry = false) =>
    run(async () => {
      const r = await post<GmailSyncResult>(`/api/gmail/sync${retry ? '?retry_unrecognized=true' : ''}`)
      financeKeys.forEach((k) => qc.invalidateQueries({ queryKey: k }))
      qc.invalidateQueries({ queryKey: ['accounts'] })
      return retry ? `Reintento terminado. ${syncSummary(r)}` : syncSummary(r)
    })
  const disconnect = async () => {
    if (!(await ask({ title: '¿Desconectar Gmail?', body: 'Pulse dejará de leer tus correos y Google revocará el acceso. Los gastos ya importados se conservan.', confirm: 'Desconectar Gmail', danger: true }))) return
    run(async () => {
      await del('/api/gmail')
      return 'Gmail desconectado'
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
      return enabled ? 'Gemini activado' : 'Gemini desactivado'
    })
  }

  return (
    <section className="section" aria-labelledby="h-gmail">
      <div className="section-head"><h2 id="h-gmail">Correo del banco (Gmail)</h2></div>
      {error && <p className="error" role="alert" style={{ marginBottom: 10 }}>{error}</p>}
      {status.error && <QueryFeedback error={status.error} retry={status.refetch} />}
      {s && !s.available && (
        <p className="hint">
          La conexión con Gmail todavía no está configurada{isAdmin ? '. Añade abajo las credenciales de Google.' : '; pide a quien administra Pulse que la active en Ajustes.'} Mientras tanto puedes importar correos .eml desde Movimientos.
        </p>
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
                <button className="btn btn-sm" disabled={busy} onClick={() => sync()}>Sincronizar ahora</button>
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
              <span className="small faint">Todo lo que llega por correo queda “Por revisar”. Desactívalo para que se confirmen solos los gastos de comercios con regla y, con la categoría clara, los de bancos con 98 % de aciertos en tus revisiones.</span>
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
      {s && s.accuracy.length > 0 && (
        <div className="section">
          <h3 className="small">Aciertos al leer tus correos</h3>
          <ul className="list" aria-label="Aciertos por banco">
            {s.accuracy.map((a) => (
              <li key={a.parser} className="settings-row">
                <span className="grow">{a.bank}<br /><span className="small faint">{a.correct} de {a.reviewed} revisados sin corregir{a.reviewed < 20 ? ` · faltan ${20 - a.reviewed} revisiones para evaluarlo` : ''}</span></span>
                <span className={`badge ${a.trusted ? '' : 'warn'}`}>{a.trusted ? 'Confiable' : `${Math.round((a.correct / a.reviewed) * 100)} %`}</span>
              </li>
            ))}
          </ul>
        </div>
      )}
      {s && s.template_alerts.length > 0 && (
        <div className="section">
          <p className="notice over" role="status">{pendingCount === 1 ? 'Hay un correo bancario pendiente de lectura' : `Hay ${pendingCount} correos bancarios pendientes de lectura`}, detectado{pendingCount === 1 ? '' : 's'} en los últimos 30 días. Reintenta su lectura con el formato actualizado. Las promociones y los pagos de tarjeta no se registran como gastos.</p>
          {s.connected && s.status === 'active' && (
            <p><button className="btn btn-sm" disabled={busy} onClick={() => sync(true)}>Reintentar correos pendientes</button></p>
          )}
          <p className="hint">El reintento busca correos de los últimos 90 días y conserva los gastos ya importados. Si alguno sigue sin reconocerse, puedes importar su archivo .eml desde Movimientos para comprobarlo o registrar el gasto a mano.</p>
          {pendingCount > s.template_alerts.length && <p className="small faint">Mostrando los {s.template_alerts.length} pendientes más recientes.</p>}
          <ul className="list" aria-label="Correos no reconocidos">
            {s.template_alerts.map((a, i) => (
              <li key={i} className="settings-row"><span className="grow">{a.subject || '(sin asunto)'}<br /><span className="small faint">{a.bank} · Detectado {dateTimeLabel(a.created_at, tz)}</span></span></li>
            ))}
          </ul>
        </div>
      )}
      {s?.available && (
        <p className="hint" style={{ marginTop: 8 }}>
          Remitentes que lee Pulse: {s.senders.join(', ')}.
        </p>
      )}
      {isAdmin && <GmailServerSettings />}
    </section>
  )
}
