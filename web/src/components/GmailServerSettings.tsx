import { useEffect, useState, type FormEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api, del, errorMessage, get } from '../lib/api'
import type { GmailServerConfig } from '../lib/types'
import { useConfirm } from './Confirm'
import { useToast } from './Toast'
import { QueryFeedback } from './QueryFeedback'

const empty = { client_id: '', client_secret: '', pubsub_topic: '', pubsub_sa: '', gemini_key: '' }

/** Credenciales de Google del servidor (solo admin). Se aplican sin reiniciar Pulse. */
export function GmailServerSettings() {
  const ask = useConfirm()
  const qc = useQueryClient()
  const toast = useToast()
  const cfg = useQuery({ queryKey: ['admin-gmail'], queryFn: () => get<GmailServerConfig>('/api/admin/gmail') })
  const [f, setF] = useState(empty)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const c = cfg.data

  useEffect(() => {
    if (c) setF({ ...empty, client_id: c.client_id, pubsub_topic: c.pubsub_topic, pubsub_sa: c.pubsub_sa })
  }, [c])

  const set = (k: keyof typeof empty) => (e: { target: { value: string } }) => setF((p) => ({ ...p, [k]: e.target.value }))

  async function run(fn: () => Promise<unknown>, done: string) {
    setError('')
    setBusy(true)
    try {
      await fn()
      toast(done)
      qc.invalidateQueries({ queryKey: ['admin-gmail'] })
      qc.invalidateQueries({ queryKey: ['gmail'] })
    } catch (e) {
      setError(errorMessage(e))
    } finally {
      setBusy(false)
    }
  }

  const save = (e: FormEvent) => {
    e.preventDefault()
    run(() => api('PUT', '/api/admin/gmail', f), 'Credenciales de Google guardadas')
  }
  const removeGemini = () => run(() => api('PUT', '/api/admin/gmail', { ...f, gemini_key: '-' }), 'Clave de Gemini quitada')
  const clear = async () => {
    if (!(await ask({ title: '¿Borrar las credenciales de Google?', body: 'Pulse dejará de leer Gmail para todos hasta que vuelvas a configurarlas (salvo que el servidor tenga variables de entorno).', confirm: 'Borrar credenciales', danger: true }))) return
    run(() => del('/api/admin/gmail'), 'Credenciales borradas')
  }

  if (!c) return cfg.error ? <QueryFeedback error={cfg.error} retry={cfg.refetch} /> : null
  const changedClient = c.client_secret_set && f.client_id.trim() !== c.client_id

  return (
    <details className="more" style={{ marginTop: 12 }} open={!c.source}>
      <summary>Configuración del servidor (admin)</summary>
      <form className="stack" onSubmit={save} style={{ marginTop: 10 }} aria-label="Credenciales de Google">
        <p className="hint">
          {c.source === 'env'
            ? 'Ahora se usan las variables de entorno del servidor. Lo que guardes aquí tiene prioridad.'
            : c.source === 'ajustes'
              ? 'Pulse usa las credenciales guardadas aquí, cifradas con la clave maestra.'
              : 'Crea en Google Cloud un ID de cliente de OAuth (Aplicación web) con la Gmail API habilitada y pega aquí sus datos.'}
        </p>
        <p className="small">
          URI de redireccionamiento autorizado: <code data-testid="gmail-redirect-uri">{c.redirect_uri}</code>
        </p>
        <label className="field">
          <span>ID de cliente</span>
          <input className="input" required autoComplete="off" spellCheck={false} autoCapitalize="none" placeholder="1234-abc.apps.googleusercontent.com…" value={f.client_id} onChange={set('client_id')} />
        </label>
        <label className="field">
          <span>Secreto del cliente</span>
          <input className="input" type="password" autoComplete="off" spellCheck={false} required={!c.client_secret_set || changedClient}
            placeholder={c.client_secret_set && !changedClient ? 'Guardado; déjalo vacío para conservarlo' : 'GOCSPX-…'} value={f.client_secret} onChange={set('client_secret')} />
        </label>
        <details className="more">
          <summary>Tiempo real con Pub/Sub (opcional)</summary>
          <p className="small faint">Suscripción push al endpoint <code>{c.webhook_url}</code> con autenticación y audiencia <code>{c.audience}</code>.</p>
          <label className="field">
            <span>Tema</span>
            <input className="input" autoComplete="off" spellCheck={false} autoCapitalize="none" placeholder="projects/mi-proyecto/topics/gmail-pulse…" value={f.pubsub_topic} onChange={set('pubsub_topic')} />
          </label>
          <label className="field">
            <span>Cuenta de servicio que firma los avisos</span>
            <input className="input" autoComplete="off" spellCheck={false} autoCapitalize="none" placeholder="pubsub-push@mi-proyecto.iam.gserviceaccount.com…" value={f.pubsub_sa} onChange={set('pubsub_sa')} />
          </label>
        </details>
        <details className="more">
          <summary>Gemini para plantillas nuevas (opcional)</summary>
          <label className="field">
            <span>Clave de API de Gemini</span>
            <input className="input" type="password" autoComplete="off" spellCheck={false}
              placeholder={c.gemini_key_set ? 'Guardada; déjala vacía para conservarla' : 'AIza…'} value={f.gemini_key} onChange={set('gemini_key')} />
          </label>
          {c.gemini_key_set && <button type="button" className="btn btn-sm" disabled={busy} onClick={removeGemini}>Quitar clave de Gemini</button>}
        </details>
        {changedClient && <p className="hint">Con otro ID de cliente, cada persona tendrá que volver a conectar su Gmail.</p>}
        {error && <p className="error" role="alert">{error}</p>}
        <div className="row-actions">
          <button className="btn btn-primary" disabled={busy}>Guardar credenciales</button>
          {c.source === 'ajustes' && <button type="button" className="btn btn-danger" disabled={busy} onClick={clear}>Borrar</button>}
        </div>
      </form>
    </details>
  )
}
