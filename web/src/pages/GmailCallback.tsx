import { useEffect, useRef, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { useQueryClient } from '@tanstack/react-query'
import { useToast } from '../components/Toast'
import { errorMessage, post } from '../lib/api'

/**
 * Google vuelve aquí con ?code=…&state=… tras aceptar el permiso. El canje se hace con
 * fetch desde el propio origen (la cookie de sesión es SameSite=Strict) y el código se
 * borra de la barra de direcciones de inmediato.
 */
export function GmailCallback() {
  const navigate = useNavigate()
  const toast = useToast()
  const qc = useQueryClient()
  const [error, setError] = useState('')
  const started = useRef(false)

  useEffect(() => {
    if (started.current) return // StrictMode monta dos veces: el código solo vale una
    started.current = true
    const q = new URLSearchParams(window.location.search)
    const code = q.get('code') ?? ''
    const state = q.get('state') ?? ''
    const denied = q.get('error')
    window.history.replaceState(null, '', '/ajustes/gmail')
    if (denied) {
      setError(denied === 'access_denied' ? 'Cancelaste el permiso en Google. Gmail no se conectó.' : 'Google no completó la conexión.')
      return
    }
    if (!code || !state) {
      navigate('/ajustes', { replace: true })
      return
    }
    post<{ email: string }>('/api/gmail/callback', { code, state })
      .then((r) => {
        qc.invalidateQueries({ queryKey: ['gmail'] })
        qc.invalidateQueries({ queryKey: ['activity'] })
        toast(`Gmail conectado: ${r.email}. Buscando gastos de los últimos 7 días…`)
        navigate('/ajustes', { replace: true })
      })
      .catch((e) => setError(errorMessage(e)))
  }, [])

  return (
    <>
      <div className="page-head">
        <h1>Conectar Gmail</h1>
      </div>
      {error ? (
        <div className="stack">
          <p className="error" role="alert">{error}</p>
          <Link className="btn" to="/ajustes">Volver a Ajustes</Link>
        </div>
      ) : (
        <p className="muted">Terminando la conexión con Google…</p>
      )}
    </>
  )
}
