import { useEffect, useState, type FormEvent } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import QRCode from 'qrcode'
import { errorMessage, post } from '../lib/api'
import { useAuth } from '../lib/auth'
import { deviceName, passkeyAssertion, passkeyError, passkeysSupported, registerPasskey } from '../lib/webauthn'
import { PulseMark } from '../components/icons'
import { finePointer } from '../lib/device'

function AuthLayout({ title, lead, children }: { title: string; lead?: string; children: React.ReactNode }) {
  return (
    <main className="auth">
      <div className="auth-card">
        <Link to="/" className="brand" aria-label="Pulse, volver al inicio">
          <PulseMark />
          <span translate="no">Pulse</span>
        </Link>
        <h1>{title}</h1>
        {lead && <p className="muted">{lead}</p>}
        {children}
        <p className="auth-legal small muted">
          <Link to="/">Qué es Pulse</Link>
          <a href="/privacidad">Privacidad</a>
          <a href="/terminos">Términos</a>
        </p>
      </div>
    </main>
  )
}

export function LoginPage() {
  const { refresh } = useAuth()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [remember, setRemember] = useState(true)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  async function submit(e: FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      await post('/api/auth/login', { email, password, remember })
      await refresh()
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setBusy(false)
    }
  }
  return (
    <AuthLayout title="Entrar" lead="Tus gastos, tu plan del mes.">
      <form onSubmit={submit} className="stack">
        <label className="field">
          <span>Email</span>
          <input className="input" type="email" name="email" autoComplete="username" autoCapitalize="none" spellCheck={false} required value={email} onChange={(e) => setEmail(e.target.value)} />
        </label>
        <label className="field">
          <span>Contraseña</span>
          <input className="input" type="password" name="password" autoComplete="current-password" required value={password} onChange={(e) => setPassword(e.target.value)} />
        </label>
        <label className="check">
          <input type="checkbox" checked={remember} onChange={(e) => setRemember(e.target.checked)} />
          <span>Recordar este dispositivo durante 7 días</span>
        </label>
        {error && <p className="error" role="alert">{error}</p>}
        <button className="btn btn-primary btn-block" disabled={busy}>
          {busy ? 'Entrando…' : 'Continuar'}
        </button>
        <p className="muted small">
          ¿Tienes una invitación? <Link to="/registro">Crea tu cuenta</Link>
        </p>
      </form>
    </AuthLayout>
  )
}

export function RegisterPage() {
  const { refresh } = useAuth()
  const navigate = useNavigate()
  const [form, setForm] = useState({ invite_code: '', email: '', password: '', confirm: '' })
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const set = (k: keyof typeof form) => (e: React.ChangeEvent<HTMLInputElement>) => setForm({ ...form, [k]: e.target.value })
  async function submit(e: FormEvent) {
    e.preventDefault()
    setError('')
    if (form.password !== form.confirm) return setError('Las contraseñas no coinciden.')
    if ([...form.password].length < 12) return setError('La contraseña debe tener al menos 12 caracteres.')
    setBusy(true)
    try {
      await post('/api/auth/register', {
        invite_code: form.invite_code,
        email: form.email,
        password: form.password,
        timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
      })
      await refresh()
      navigate('/')
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setBusy(false)
    }
  }
  return (
    <AuthLayout title="Crear cuenta" lead="Pulse es solo por invitación. Pide tu código a quien administra el servidor.">
      <form onSubmit={submit} className="stack">
        <label className="field">
          <span>Código de invitación</span>
          <input className="input num" name="invite_code" required autoComplete="off" autoCapitalize="characters" spellCheck={false} placeholder="PLS-XXXX-XXXX-XXXX-XXXX-XXXX" value={form.invite_code} onChange={set('invite_code')} />
        </label>
        <label className="field">
          <span>Email</span>
          <input className="input" type="email" name="email" autoComplete="username" autoCapitalize="none" spellCheck={false} required value={form.email} onChange={set('email')} />
        </label>
        <div className="field">
          <label htmlFor="reg-pw">Contraseña</label>
          <input id="reg-pw" className="input" type="password" name="new-password" autoComplete="new-password" required minLength={12} value={form.password} onChange={set('password')} aria-describedby="pw-hint" />
          <small id="pw-hint" className="hint">Mínimo 12 caracteres. Una frase de varias palabras es fácil de recordar y difícil de adivinar.</small>
        </div>
        <label className="field">
          <span>Repite la contraseña</span>
          <input className="input" type="password" autoComplete="new-password" required value={form.confirm} onChange={set('confirm')} />
        </label>
        {error && <p className="error" role="alert">{error}</p>}
        <p className="small muted">
          Al crear tu cuenta aceptas los <a href="/terminos">Términos y condiciones</a> y confirmas que leíste la{' '}
          <a href="/privacidad">Política de privacidad</a>.
        </p>
        <button className="btn btn-primary btn-block" disabled={busy}>
          {busy ? 'Creando…' : 'Crear cuenta'}
        </button>
        <p className="muted small">
          ¿Ya tienes cuenta? <Link to="/entrar">Entrar</Link>
        </p>
      </form>
    </AuthLayout>
  )
}

export function VerifyPage() {
  const { methods = [], email, refresh } = useAuth()
  const [mode, setMode] = useState<'passkey' | 'totp' | 'recovery'>(methods.includes('passkey') && passkeysSupported() ? 'passkey' : 'totp')
  const [code, setCode] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  async function withPasskey() {
    setBusy(true)
    setError('')
    try {
      const credential = await passkeyAssertion()
      await post('/api/auth/mfa/passkey/finish', { credential })
      await refresh()
    } catch (err) {
      setError(passkeyError(err) ?? errorMessage(err))
    } finally {
      setBusy(false)
    }
  }
  async function withCode(e: FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      await post(mode === 'totp' ? '/api/auth/mfa/totp' : '/api/auth/mfa/recovery', { code })
      await refresh()
    } catch (err) {
      setError(errorMessage(err))
      setCode('')
    } finally {
      setBusy(false)
    }
  }
  async function cancel() {
    await post('/api/auth/logout').catch(() => {})
    await refresh()
  }

  return (
    <AuthLayout title="Confirma que eres tú" lead={email ? `Segundo paso para ${email}.` : undefined}>
      <div className="stack" style={{ marginTop: 24 }}>
        {mode === 'passkey' ? (
          <button className="btn btn-primary btn-block" onClick={withPasskey} disabled={busy}>
            Usar mi passkey
          </button>
        ) : (
          <form onSubmit={withCode} className="stack" style={{ marginTop: 0 }}>
            <label className="field">
              <span>{mode === 'totp' ? 'Código de tu app de autenticación' : 'Código de recuperación'}</span>
              <input
                className="input num"
                autoFocus={finePointer()}
                required
                autoComplete="one-time-code"
                inputMode={mode === 'totp' ? 'numeric' : 'text'}
                pattern={mode === 'totp' ? '[0-9 ]{6,7}' : undefined}
                maxLength={mode === 'totp' ? 7 : 14}
                value={code}
                onChange={(e) => setCode(e.target.value)}
              />
            </label>
            <button className="btn btn-primary btn-block" disabled={busy}>
              Verificar
            </button>
          </form>
        )}
        {error && <p className="error" role="alert">{error}</p>}
        <div className="row-actions">
          {mode !== 'passkey' && methods.includes('passkey') && passkeysSupported() && (
            <button className="btn btn-ghost btn-sm" onClick={() => setMode('passkey')}>Usar passkey</button>
          )}
          {mode !== 'totp' && methods.includes('totp') && (
            <button className="btn btn-ghost btn-sm" onClick={() => setMode('totp')}>Usar código de la app</button>
          )}
          {mode !== 'recovery' && methods.includes('recovery') && (
            <button className="btn btn-ghost btn-sm" onClick={() => setMode('recovery')}>Usar código de recuperación</button>
          )}
          <button className="btn btn-ghost btn-sm" onClick={cancel}>Cancelar</button>
        </div>
      </div>
    </AuthLayout>
  )
}

/** Muestra los códigos de recuperación una sola vez. */
export function RecoveryCodes({ codes, onDone }: { codes: string[]; onDone: () => void }) {
  const [saved, setSaved] = useState(false)
  const text = `Pulse — códigos de recuperación\nCada código sirve una sola vez.\n\n${codes.join('\n')}\n`
  function download() {
    const url = URL.createObjectURL(new Blob([text], { type: 'text/plain' }))
    const a = document.createElement('a')
    a.href = url
    a.download = 'pulse-codigos-de-recuperacion.txt'
    a.click()
    URL.revokeObjectURL(url)
  }
  return (
    <div className="stack">
      <p className="muted">
        Si pierdes tu teléfono o tu passkey, estos códigos te dejan entrar. Cada uno sirve una vez. Guárdalos en tu gestor de contraseñas: no se volverán a mostrar.
      </p>
      <div className="codes" data-testid="recovery-codes">
        {codes.map((c) => (
          <span key={c}>{c}</span>
        ))}
      </div>
      <div className="row-actions">
        <button className="btn btn-sm" onClick={() => navigator.clipboard?.writeText(text)}>Copiar</button>
        <button className="btn btn-sm" onClick={download}>Descargar .txt</button>
      </div>
      <label className="check">
        <input type="checkbox" checked={saved} onChange={(e) => setSaved(e.target.checked)} />
        <span>Ya los guardé en un lugar seguro</span>
      </label>
      <button className="btn btn-primary btn-block" disabled={!saved} onClick={onDone}>
        Continuar
      </button>
    </div>
  )
}

export function TotpSetup({ onDone }: { onDone: (codes?: string[]) => void }) {
  const [data, setData] = useState<{ secret: string; uri: string } | null>(null)
  const [qr, setQr] = useState('')
  const [code, setCode] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    post<{ secret: string; uri: string }>('/api/auth/enroll/totp/begin')
      .then(async (d) => {
        setData(d)
        setQr(await QRCode.toDataURL(d.uri, { margin: 0, width: 344, errorCorrectionLevel: 'M' }))
      })
      .catch((e) => setError(errorMessage(e)))
  }, [])
  async function submit(e: FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      const r = await post<{ recovery_codes?: string[] }>('/api/auth/enroll/totp/finish', { code })
      onDone(r.recovery_codes)
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setBusy(false)
    }
  }
  return (
    <form onSubmit={submit} className="stack">
      <p className="muted">Escanea el código con Google Authenticator, 1Password, Aegis u otra app compatible.</p>
      {qr ? <img className="qr" src={qr} width={196} height={196} alt="Código QR para la app de autenticación" /> : <div className="qr" aria-hidden="true" />}
      {data && (
        <details>
          <summary className="small muted">¿No puedes escanear? Escribe la clave</summary>
          <p className="secret" data-testid="totp-secret">{data.secret}</p>
        </details>
      )}
      <label className="field">
        <span>Código de 6 dígitos que muestra la app</span>
        <input className="input num" name="totp" required inputMode="numeric" autoComplete="one-time-code" maxLength={7} value={code} onChange={(e) => setCode(e.target.value)} />
      </label>
      {error && <p className="error" role="alert">{error}</p>}
      <button className="btn btn-primary btn-block" disabled={busy || !data}>Activar</button>
    </form>
  )
}

export function EnrollPage() {
  const { refresh } = useAuth()
  const [step, setStep] = useState<'choose' | 'totp' | 'codes'>('choose')
  const [codes, setCodes] = useState<string[]>([])
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const done = (c?: string[]) => {
    if (c?.length) {
      setCodes(c)
      setStep('codes')
    } else refresh()
  }
  async function passkey() {
    setBusy(true)
    setError('')
    try {
      const r = await registerPasskey(deviceName())
      done(r.recovery_codes)
    } catch (err) {
      setError(passkeyError(err) ?? errorMessage(err))
    } finally {
      setBusy(false)
    }
  }
  if (step === 'codes')
    return (
      <AuthLayout title="Guarda tus códigos de recuperación">
        <div style={{ marginTop: 20 }}>
          <RecoveryCodes codes={codes} onDone={refresh} />
        </div>
      </AuthLayout>
    )
  if (step === 'totp')
    return (
      <AuthLayout title="App de autenticación">
        <div style={{ marginTop: 20 }}>
          <TotpSetup onDone={done} />
          <button className="btn btn-ghost btn-sm" style={{ marginTop: 12 }} onClick={() => setStep('choose')}>Volver</button>
        </div>
      </AuthLayout>
    )
  return (
    <AuthLayout title="Protege tu cuenta" lead="Pulse guarda datos de tus tarjetas. Además de la contraseña, cada inicio de sesión pedirá un segundo factor.">
      <div className="stack" style={{ marginTop: 24 }}>
        {passkeysSupported() && (
          <div className="panel stack">
            <h3>Passkey (recomendada)</h3>
            <p className="muted small">Usa la huella, la cara o el PIN de este dispositivo. No se puede robar con un correo falso.</p>
            <button className="btn btn-primary" onClick={passkey} disabled={busy}>Crear passkey</button>
          </div>
        )}
        <div className="panel stack">
          <h3>App de autenticación</h3>
          <p className="muted small">Un código de 6 dígitos que cambia cada 30 segundos.</p>
          <button className="btn" onClick={() => setStep('totp')}>Configurar app</button>
        </div>
        {error && <p className="error" role="alert">{error}</p>}
      </div>
    </AuthLayout>
  )
}
