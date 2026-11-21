import { useState, type FormEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api, del, errorMessage, get } from '../lib/api'
import { QueryFeedback } from './QueryFeedback'
import { SettingsHeading } from './SettingsNavigation'
import { useToast } from './Toast'

export function ExchangeSettings() {
  const qc = useQueryClient()
  const toast = useToast()
  const status = useQuery({ queryKey: ['exchange-settings'], queryFn: () => get<{ configured: boolean }>('/api/exchange-settings') })
  const [key, setKey] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  async function run(fn: () => Promise<unknown>, message: string) {
    setBusy(true); setError('')
    try { await fn(); setKey(''); await qc.invalidateQueries({ queryKey: ['exchange-settings'] }); toast(message) }
    catch (e) { setError(errorMessage(e)) }
    finally { setBusy(false) }
  }
  function save(e: FormEvent) {
    e.preventDefault()
    void run(() => api('PUT', '/api/exchange-settings', { api_key: key }), 'Clave guardada')
  }
  const configured = status.data?.configured === true
  const keyStatus = status.isPending
    ? 'Consultando estado…'
    : configured
      ? 'Clave guardada'
      : 'Sin clave guardada'

  return <section className="section" aria-labelledby="h-exchange">
    <SettingsHeading id="h-exchange" title="Cambio de moneda" description="Conversión automática entre EUR y USD." />
    <div className="settings-exchange">
      <div className="settings-provider">
        <h3>Exchange Rates API</h3>
        {status.isError
          ? <QueryFeedback error={status.error} retry={status.refetch} />
          : <p role="status"><span className={configured ? 'badge' : 'settings-provider-status'}>{keyStatus}</span></p>}
        <p className="small muted">Pulse reutiliza la tasa durante 8 horas. Sin clave usa la tasa histórica de referencia del BCE.</p>
        <a className="small" href="https://manage.exchangeratesapi.io/dashboard" target="_blank" rel="noreferrer">Abrir Exchange Rates API ↗</a>
      </div>
      <form className="stack" onSubmit={save}>
      <label className="field"><span>Clave de Exchange Rates API</span>
        <input className="input" type="password" autoComplete="off" spellCheck={false} value={key} onChange={(e) => setKey(e.target.value)} placeholder={configured ? 'Escribe una clave nueva para cambiarla' : 'Pega tu clave de API'} required />
        <small className="hint">Se guarda cifrada en el servidor para tu usuario.</small>
      </label>
      {error && <p className="error" role="alert">{error}</p>}
      <div className="row-actions"><button className="btn btn-primary btn-sm" disabled={busy}>Guardar clave</button>
        {configured && <button type="button" className="btn btn-sm" disabled={busy} onClick={() => void run(() => del('/api/exchange-settings'), 'Clave eliminada')}>Quitar clave</button>}</div>
      </form>
    </div>
  </section>
}
