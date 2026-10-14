import { useEffect, useRef, useState, type FormEvent } from 'react'
import { errorMessage, onStepUpRequired, post } from '../lib/api'
import { useMe } from '../lib/queries'
import { passkeyAssertion, passkeyError, passkeysSupported } from '../lib/webauthn'
import { Sheet } from './Sheet'

/**
 * Re-autenticación para acciones sensibles (cambiar contraseña, factores, etc.).
 * Cuando la API responde step_up_required se abre este diálogo y, si la persona
 * confirma, la petición original se reintenta sola.
 */
export function StepUpHost() {
  const [open, setOpen] = useState(false)
  const resolver = useRef<((ok: boolean) => void) | null>(null)
  const { data: me } = useMe()
  const [code, setCode] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    onStepUpRequired(
      () =>
        new Promise<boolean>((resolve) => {
          resolver.current = resolve
          setCode('')
          setError('')
          setOpen(true)
        }),
    )
    return () => onStepUpRequired(null)
  }, [])

  const finish = (ok: boolean) => {
    setOpen(false)
    resolver.current?.(ok)
    resolver.current = null
  }
  async function withPasskey() {
    setBusy(true)
    setError('')
    try {
      const credential = await passkeyAssertion()
      await post('/api/auth/step-up/passkey', { credential })
      finish(true)
    } catch (e) {
      setError(passkeyError(e) ?? errorMessage(e))
    } finally {
      setBusy(false)
    }
  }
  async function withCode(e: FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      await post('/api/auth/step-up/totp', { code })
      finish(true)
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setBusy(false)
    }
  }
  const hasPasskey = (me?.mfa.passkeys ?? 0) > 0 && passkeysSupported()
  return (
    <Sheet open={open} title="Confirma que eres tú" onClose={() => finish(false)}>
      <div className="stack">
        <p className="muted">Esta acción cambia la seguridad de tu cuenta. Verifica tu identidad para continuar.</p>
        {hasPasskey && (
          <button className="btn btn-primary btn-block" onClick={withPasskey} disabled={busy}>Usar mi passkey</button>
        )}
        {me?.mfa.totp && (
          <form onSubmit={withCode} className="stack">
            <label className="field">
              <span>Código de tu app de autenticación</span>
              <input className="input num" inputMode="numeric" autoComplete="one-time-code" required maxLength={7} value={code} onChange={(e) => setCode(e.target.value)} />
            </label>
            <button className={`btn btn-block ${hasPasskey ? '' : 'btn-primary'}`} disabled={busy}>Verificar código</button>
          </form>
        )}
        {error && <p className="error" role="alert">{error}</p>}
      </div>
    </Sheet>
  )
}
