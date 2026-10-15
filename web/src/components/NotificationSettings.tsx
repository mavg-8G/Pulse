import { useState, type FormEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api, del, errorMessage, get, post } from '../lib/api'
import { disablePush, enablePush, pushSupported, type NotificationPreferences, type PushDevice } from '../lib/push'
import { useToast } from './Toast'

export function NotificationSettings() {
  const qc = useQueryClient()
  const toast = useToast()
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const config = useQuery({ queryKey: ['push-config'], queryFn: () => get<{ enabled: boolean }>('/api/push/vapid-key') })
  const devices = useQuery({ queryKey: ['push-devices'], queryFn: () => get<PushDevice[]>('/api/push/subscriptions') })
  const prefs = useQuery({ queryKey: ['push-prefs'], queryFn: () => get<NotificationPreferences>('/api/push/preferences') })
  const current = devices.data?.some((d) => d.current)
  const reload = () => qc.invalidateQueries({ queryKey: ['push-devices'] })
  async function action(fn: () => Promise<unknown>, message: string) {
    setError(''); setBusy(true)
    try { await fn(); await reload(); toast(message) } catch (e) { setError(errorMessage(e)) } finally { setBusy(false) }
  }
  return (
    <section className="section" aria-labelledby="h-notifications">
      <div className="section-head"><h2 id="h-notifications">Notificaciones</h2></div>
      {(error || config.error || devices.error || prefs.error) && <p className="error" role="alert">{error || errorMessage(config.error || devices.error || prefs.error)}</p>}
      {config.data && !config.data.enabled && <p className="hint">Las notificaciones todavía no están configuradas en el servidor.</p>}
      {!pushSupported() && <p className="hint">Este navegador no admite notificaciones. En iPhone, instala Pulse en la pantalla de inicio.</p>}
      <div className="list">
        <div className="settings-row">
          <span className="grow">Este dispositivo<br /><span className="small faint">{current ? 'Notificaciones activadas' : 'Notificaciones desactivadas'}</span></span>
          <button className="btn btn-sm" disabled={busy || !config.data?.enabled || !pushSupported()} onClick={() => action(current ? disablePush : enablePush, current ? 'Notificaciones desactivadas' : 'Notificaciones activadas')}>
            {current ? 'Desactivar notificaciones' : 'Activar notificaciones'}
          </button>
        </div>
        {(devices.data ?? []).filter((d) => !d.current).map((d) => (
          <div key={d.id} className="settings-row"><span className="grow">{d.device_label}</span><button className="btn btn-sm btn-danger" disabled={busy} onClick={() => action(() => del(`/api/push/subscriptions/${d.id}`), 'Dispositivo quitado')}>Quitar dispositivo</button></div>
        ))}
      </div>
      {prefs.data && <PreferenceForm key={JSON.stringify(prefs.data)} prefs={prefs.data} onSave={async (p) => {
        await api('PUT', '/api/push/preferences', p)
        await qc.invalidateQueries({ queryKey: ['push-prefs'] })
        toast('Preferencias guardadas')
      }} />}
      {current && <button className="btn btn-sm" disabled={busy} onClick={() => action(() => post('/api/push/test'), 'Aviso en cola. Se enviará al terminar el horario silencioso, si está activo.')}>Enviar notificación de prueba</button>}
      <p className="hint">Resúmenes: domingos y primer día del mes, desde las 09:00 en tu zona horaria. Al cerrar la sesión de un dispositivo, deja de recibir avisos.</p>
    </section>
  )
}

const timeText = (n: number) => `${String(Math.floor(n / 60)).padStart(2, '0')}:${String(n % 60).padStart(2, '0')}`
const minutes = (text: string) => { const [h, m] = text.split(':').map(Number); return h * 60 + m }

function PreferenceForm({ prefs, onSave }: { prefs: NotificationPreferences; onSave: (p: NotificationPreferences) => Promise<void> }) {
  const [form, setForm] = useState(prefs)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  async function save(e: FormEvent) {
    e.preventDefault(); setError(''); setBusy(true)
    try { await onSave(form) } catch (e) { setError(errorMessage(e)) } finally { setBusy(false) }
  }
  const toggles = [['budgets', 'Alertas de presupuesto'], ['security', 'Avisos de seguridad'], ['weekly', 'Resumen semanal'], ['monthly', 'Resumen mensual'], ['hide_amounts', 'Ocultar montos en las notificaciones'], ['quiet_enabled', 'Horario silencioso']] as const
  return (
    <form className="stack section" onSubmit={save}>
      <div className="list toggle-list">{toggles.map(([key, label]) => <label key={key} className="settings-row"><span className="grow">{label}</span><input type="checkbox" checked={form[key]} onChange={(e) => setForm({ ...form, [key]: e.target.checked })} /></label>)}</div>
      {form.quiet_enabled && <div className="row-actions">
        <label className="field"><span>Silencio desde</span><input className="input" type="time" required value={timeText(form.quiet_start)} onChange={(e) => setForm({ ...form, quiet_start: minutes(e.target.value) })} /></label>
        <label className="field"><span>Silencio hasta</span><input className="input" type="time" required value={timeText(form.quiet_end)} onChange={(e) => setForm({ ...form, quiet_end: minutes(e.target.value) })} /></label>
      </div>}
      {error && <p className="error" role="alert">{error}</p>}
      <button className="btn btn-sm" disabled={busy}>Guardar notificaciones</button>
    </form>
  )
}
