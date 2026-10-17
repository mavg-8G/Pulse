import { errorMessage } from '../lib/api'

export function QueryFeedback({ error, retry, updating = false }: { error?: unknown; retry: () => unknown; updating?: boolean }) {
  if (error) return <div className="query-feedback error" role="alert"><p>{errorMessage(error)}</p><button className="btn btn-sm" type="button" onClick={() => retry()}>Reintentar</button></div>
  return <div className="query-feedback" role="status">{updating ? 'Actualizando…' : ''}</div>
}
