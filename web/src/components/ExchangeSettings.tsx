import { useState, type FormEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api, del, errorMessage, get } from '../lib/api'
import { QueryFeedback } from './QueryFeedback'
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
  return <section className="section" aria-labelledby="h-exchange">
    <div className="section-head"><h2 id="h-exchange">Cambio de moneda</h2></div>
    <QueryFeedback error={status.error} retry={status.refetch} />
    <form className="stack" onSubmit={save}>
      <p className="small muted">Usa la tasa más reciente de Exchange Rates API para convertir entre EUR y USD. La clave se guarda cifrada para tu usuario. Sin clave se usa la tasa histórica de referencia del BCE.</p>
      <label className="field"><span>Clave de Exchange Rates API</span>
        <input className="input" type="password" autoComplete="off" spellCheck={false} value={key} onChange={(e) => setKey(e.target.value)} placeholder={status.data?.configured ? 'Clave guardada; escribe otra para cambiarla' : 'Pega tu clave de API'} required />
      </label>
      {error && <p className="error" role="alert">{error}</p>}
      <div className="row-actions"><button className="btn btn-primary btn-sm" disabled={busy}>Guardar clave</button>
        {status.data?.configured && <button type="button" className="btn btn-sm" disabled={busy} onClick={() => void run(() => del('/api/exchange-settings'), 'Clave eliminada')}>Quitar clave</button>}</div>
      <a href="https://manage.exchangeratesapi.io/dashboard" target="_blank" rel="noreferrer">Abrir Exchange Rates API</a>
    </form>
  </section>
}
