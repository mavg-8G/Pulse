import { useConfirm } from '../components/Confirm'
import { useState, type FormEvent } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Sheet } from '../components/Sheet'
import { useMotion } from '../components/AppShell'
import { IconPause, IconPulse } from '../components/icons'
import { BackupSettings } from '../components/BackupSettings'
import { GmailSettings } from '../components/GmailSettings'
import { NotificationSettings } from '../components/NotificationSettings'
import { useToast } from '../components/Toast'
import { ApiError, del, errorMessage, get, patch, post } from '../lib/api'
import { useAuth, wipeLocalData } from '../lib/auth'
import { snapshot } from '../lib/outbox'
import { dateTimeLabel } from '../lib/dates'
import { useMe } from '../lib/queries'
import { applyTheme } from '../lib/theme'
import type { AuditEntry, PasskeyInfo, SessionInfo, Theme } from '../lib/types'
import { deviceName, passkeyError, passkeysSupported, registerPasskey } from '../lib/webauthn'
import { RecoveryCodes, TotpSetup } from './AuthPages'

const eventText: Record<string, string> = {
  register: 'Cuenta creada',
  login_password_ok: 'Contraseña correcta',
  login_ok: 'Inicio de sesión',
  login_failed: 'Intento de inicio fallido',
  login_locked: 'Intento con la cuenta bloqueada',
  mfa_failed: 'Segundo factor incorrecto',
  mfa_enrolled: 'Segundo factor configurado',
  totp_enabled: 'App de autenticación activada',
  totp_disabled: 'App de autenticación quitada',
  passkey_added: 'Passkey agregada',
  passkey_removed: 'Passkey quitada',
  passkey_register_failed: 'Passkey rechazada',
  recovery_regenerated: 'Códigos de recuperación nuevos',
  password_changed: 'Contraseña cambiada',
  password_change_failed: 'Cambio de contraseña fallido',
  session_revoked: 'Sesión cerrada a distancia',
  logout: 'Cierre de sesión',
  step_up: 'Identidad confirmada',
  gmail_connected: 'Gmail conectado',
  gmail_disconnected: 'Gmail desconectado',
  gmail_broken: 'Pulse perdió el acceso a Gmail',
  gmail_connect_failed: 'Conexión con Gmail fallida',
  gmail_shadow_changed: 'Modo sombra cambiado',
  gmail_server_configured: 'Credenciales de Google del servidor cambiadas',
  gmail_server_cleared: 'Credenciales de Google del servidor borradas',
  gemini_enabled: 'Gemini activado',
  gemini_disabled: 'Gemini desactivado',
  email_imported: 'Correo importado',
  csv_exported: 'Movimientos exportados a CSV',
  template_unknown: 'Correo del banco que Pulse no supo leer',
  backup_manual: 'Respaldo cifrado hecho a mano',
  backup_downloaded: 'Copia cifrada del respaldo descargada',
}

function browserName(ua: string) {
  const b = /Edg\//.test(ua) ? 'Edge' : /Firefox\//.test(ua) ? 'Firefox' : /Chrome\//.test(ua) ? 'Chrome' : /Safari\//.test(ua) ? 'Safari' : 'Navegador'
  const os = /iPhone|iPad/.test(ua) ? 'iOS' : /Android/.test(ua) ? 'Android' : /Windows/.test(ua) ? 'Windows' : /Mac OS/.test(ua) ? 'macOS' : /Linux/.test(ua) ? 'Linux' : ''
  return os ? `${b} en ${os}` : b
}

/** Ajustes (preferencias) y Mi cuenta (sesiones y seguridad) comparten estado; `view` elige qué se muestra. */
export function Settings({ view = 'prefs' }: { view?: 'prefs' | 'account' }) {
  const ask = useConfirm()
  const { motion, toggleMotion } = useMotion()
  const { data: me } = useMe()
  const { refresh } = useAuth()
  const qc = useQueryClient()
  const toast = useToast()
  const navigate = useNavigate()
  const tz = me?.timezone ?? 'America/Guayaquil'

  const sessions = useQuery({ queryKey: ['sessions'], queryFn: () => get<SessionInfo[]>('/api/security/sessions') })
  const passkeys = useQuery({ queryKey: ['passkeys'], queryFn: () => get<PasskeyInfo[]>('/api/security/passkeys') })
  const activity = useQuery({ queryKey: ['activity'], queryFn: () => get<AuditEntry[]>('/api/security/activity') })
  const [sheet, setSheet] = useState<null | 'totp' | 'codes' | 'password'>(null)
  const [codes, setCodes] = useState<string[]>([])
  const [error, setError] = useState('')

  const reloadSecurity = () => ['me', 'passkeys', 'sessions', 'activity'].forEach((k) => qc.invalidateQueries({ queryKey: [k] }))

  async function setTheme(t: Theme) {
    applyTheme(t)
    await patch('/api/me', { theme: t })
    qc.invalidateQueries({ queryKey: ['me'] })
  }
  async function addPasskey() {
    setError('')
    try {
      // Pide step-up primero (el reintento automático no sirve aquí: la ceremonia WebAuthn necesita un desafío nuevo).
      const ready = await post('/api/auth/enroll/passkey/begin').then(() => true, () => false)
      if (!ready) return
      await registerPasskey(deviceName())
      toast('Passkey agregada')
      reloadSecurity()
    } catch (e) {
      setError(passkeyError(e) ?? errorMessage(e))
    }
  }
  async function removePasskey(p: PasskeyInfo) {
    if (!(await ask({ title: `¿Quitar la passkey “${p.name}”?`, body: 'Ya no podrás entrar con ella.', confirm: 'Quitar passkey', danger: true }))) return
    try {
      await del(`/api/security/passkeys/${p.id}`)
      toast('Passkey quitada')
      reloadSecurity()
    } catch (e) {
      setError(errorMessage(e))
    }
  }
  async function removeTotp() {
    if (!(await ask({ title: '¿Quitar la app de autenticación?', body: 'Sus códigos dejarán de servir para entrar.', confirm: 'Quitar app', danger: true }))) return
    try {
      await del('/api/security/totp')
      toast('App de autenticación quitada')
      reloadSecurity()
    } catch (e) {
      setError(errorMessage(e))
    }
  }
  async function newCodes() {
    if (!(await ask({ title: '¿Generar códigos nuevos?', body: 'Los códigos de recuperación anteriores dejarán de funcionar.', confirm: 'Generar códigos' }))) return
    try {
      const r = await post<{ recovery_codes: string[] }>('/api/security/recovery-codes')
      setCodes(r.recovery_codes)
      setSheet('codes')
      reloadSecurity()
    } catch (e) {
      setError(errorMessage(e))
    }
  }
  async function revoke(s: SessionInfo) {
    await del(`/api/security/sessions/${s.id}`)
    toast('Sesión cerrada')
    reloadSecurity()
  }
  async function logout() {
    const waiting = snapshot().length
    if (waiting > 0 && !(await ask({ title: `¿Cerrar sesión con ${waiting === 1 ? 'un cambio' : `${waiting} cambios`} sin enviar?`,
      body: 'Están guardados solo en este dispositivo. Al cerrar sesión se borran y no llegarán a tu cuenta. Conéctate y espera a que se envíen si quieres conservarlos.',
      confirm: 'Cerrar sesión y borrarlos', danger: true }))) return
    try {
      await post('/api/auth/logout')
    } catch (e) {
      // Sin red la sesión seguiría abierta en el servidor: no se finge un cierre que no ocurrió.
      if (e instanceof ApiError && e.status === 0) return toast('Sin conexión: conéctate para cerrar la sesión.')
    }
    qc.clear()
    await wipeLocalData()
    await refresh()
    navigate('/entrar')
  }

  return (
    <>
      {view === 'account' ? <>
      <div className="page-head">
        <div><p className="small"><Link to="/ajustes">Ajustes</Link></p><h1>Mi cuenta</h1><p className="page-description">Tu perfil, tus dispositivos y la seguridad de tu espacio.</p></div>
      </div>

      <section className="account-card" aria-label="Tu cuenta">
        <span className="avatar avatar-lg" aria-hidden="true">{me?.email.slice(0, 1).toUpperCase()}</span>
        <span className="grow"><strong>{me?.email}</strong><span className="small faint">Zona horaria {tz}</span></span>
        <span className="row-actions">
          <button className="btn btn-sm btn-danger" onClick={logout}>Cerrar sesión</button>
        </span>
      </section>

      <section className="section" aria-labelledby="h-sessions">
        <div className="section-head"><h2 id="h-sessions">Dispositivos con sesión abierta</h2></div>
        <ul className="list">
          {(sessions.data ?? []).map((s) => (
            <li key={s.id} className="settings-row">
              <span className="grow">
                {browserName(s.user_agent)} {s.current && <span className="badge">Este dispositivo</span>}
                <br />
                <span className="small faint">IP {s.ip}, activo {dateTimeLabel(s.last_seen_at, tz)}</span>
              </span>
              {!s.current && <button className="btn btn-sm btn-danger" onClick={() => revoke(s)} aria-label={`Cerrar la sesión de ${browserName(s.user_agent)}`}>Cerrar</button>}
            </li>
          ))}
        </ul>
      </section>

      <section className="section" aria-labelledby="h-sec">
        <div className="section-head"><h2 id="h-sec">Seguridad</h2></div>
        {error && <p className="error" role="alert" style={{ marginBottom: 10 }}>{error}</p>}
        <ul className="list">
          {(passkeys.data ?? []).map((p) => (
            <li key={p.id} className="settings-row">
              <span className="grow">
                Passkey: {p.name}
                <br />
                <span className="small faint">
                  {p.last_used_at ? `Último uso ${dateTimeLabel(p.last_used_at, tz)}` : `Agregada ${dateTimeLabel(p.created_at, tz)}`}
                </span>
              </span>
              <button className="btn btn-sm btn-danger" onClick={() => removePasskey(p)}>Quitar</button>
            </li>
          ))}
          {passkeysSupported() && (
            <li className="settings-row">
              <span className="grow">Agregar una passkey de este dispositivo</span>
              <button className="btn btn-sm" onClick={addPasskey}>Agregar passkey</button>
            </li>
          )}
          <li className="settings-row">
            <span className="grow">
              App de autenticación
              <br />
              <span className="small faint">{me?.mfa.totp ? 'Activada' : 'Sin configurar'}</span>
            </span>
            <span className="row-actions">
              <button className="btn btn-sm" onClick={() => setSheet('totp')}>{me?.mfa.totp ? 'Cambiar' : 'Configurar'}</button>
              {me?.mfa.totp && <button className="btn btn-sm btn-danger" onClick={removeTotp}>Quitar</button>}
            </span>
          </li>
          <li className="settings-row">
            <span className="grow">
              Códigos de recuperación
              <br />
              <span className="small faint">Te quedan {me?.mfa.recovery_remaining ?? 0} sin usar</span>
            </span>
            <button className="btn btn-sm" onClick={newCodes}>Generar nuevos</button>
          </li>
          <li className="settings-row">
            <span className="grow">Contraseña</span>
            <button className="btn btn-sm" onClick={() => setSheet('password')}>Cambiar</button>
          </li>
        </ul>
      </section>

      <section className="section" aria-labelledby="h-activity">
        <div className="section-head"><h2 id="h-activity">Actividad de seguridad</h2></div>
        <ul className="list">
          {(activity.data ?? []).slice(0, 12).map((a) => (
            <li key={a.id} className="settings-row">
              <span className="grow">
                {eventText[a.event] ?? a.event}
                {a.meta?.method && <span className="faint"> con {a.meta.method === 'totp' ? 'código' : a.meta.method === 'passkey' ? 'passkey' : 'código de recuperación'}</span>}
                <br />
                <span className="small faint">{dateTimeLabel(a.created_at, tz)}, IP {a.ip}</span>
              </span>
            </li>
          ))}
        </ul>
      </section>

      </> : <>
      <div className="page-head">
        <div><p className="page-kicker">Hecho a tu medida</p><h1>Ajustes</h1><p className="page-description">Personaliza tu espacio y conecta lo que necesitas.</p></div>
      </div>

      <section aria-labelledby="h-look">
        <div className="section-head"><h2 id="h-look">Apariencia</h2></div>
        <div className="list">
          <div className="settings-row">
            <span className="grow">Tema</span>
            <div className="seg" role="group" aria-label="Tema">
              {(['system', 'light', 'dark'] as Theme[]).map((t) => (
                <button key={t} aria-pressed={me?.theme === t} onClick={() => setTheme(t)}>
                  {t === 'system' ? 'Sistema' : t === 'light' ? 'Claro' : 'Oscuro'}
                </button>
              ))}
            </div>
          </div>
          <div className="settings-row">
            <span className="grow">Animaciones<br /><span className="small faint">El latido de Pulse y las transiciones. Tu preferencia se guarda en este dispositivo.</span></span>
            <button className="btn btn-sm" onClick={toggleMotion} aria-pressed={motion} aria-label={motion ? 'Pausar animaciones' : 'Activar animaciones'}>
              {motion ? <IconPause /> : <IconPulse />}{motion ? 'Pausar' : 'Activar'}
            </button>
          </div>
          <div className="settings-row">
            <span className="grow">Zona horaria<br /><span className="small faint">Define a qué día y mes pertenece cada gasto.</span></span>
            <span className="muted">{tz}</span>
          </div>
        </div>
      </section>

      <section className="section" aria-labelledby="h-data">
        <div className="section-head"><h2 id="h-data">Tus datos</h2></div>
        <div className="list">
          <Link className="settings-row settings-link" to="/ajustes/cuenta">
            <span className="grow">Mi cuenta<br /><span className="small faint">Sesiones abiertas, contraseña y seguridad</span></span><span className="faint">›</span>
          </Link>
          <Link className="settings-row settings-link" to="/reportes">
            <span className="grow">Reportes y exportar<br /><span className="small faint">Gasto por mes, categoría y cuenta; conciliación; CSV</span></span><span className="faint">›</span>
          </Link>
          <Link className="settings-row settings-link" to="/ajustes/cuentas">
            <span className="grow">Cuentas y tarjetas</span><span className="faint">›</span>
          </Link>
          <Link className="settings-row settings-link" to="/ajustes/categorias">
            <span className="grow">Categorías</span><span className="faint">›</span>
          </Link>
        </div>
      </section>

      <GmailSettings tz={tz} />

      <NotificationSettings />

      {me?.role === 'admin' && <BackupSettings tz={tz} />}

      <section className="section" aria-labelledby="h-legal">
        <div className="section-head"><h2 id="h-legal">Privacidad</h2></div>
        <div className="list">
          <a className="settings-row" href="/privacidad">
            <span className="grow">Política de privacidad<br /><span className="small muted">Qué datos usa Pulse y cómo descargar o eliminar los tuyos</span></span>
          </a>
          <a className="settings-row" href="/terminos">
            <span className="grow">Términos y condiciones</span>
          </a>
        </div>
      </section>


      </>}

      <Sheet open={sheet === 'totp'} title="App de autenticación" onClose={() => setSheet(null)}>
        <TotpSetup onDone={() => { setSheet(null); toast('App de autenticación activada'); reloadSecurity() }} />
      </Sheet>
      <Sheet open={sheet === 'codes'} title="Códigos de recuperación nuevos" onClose={() => setSheet(null)}>
        <RecoveryCodes codes={codes} onDone={() => setSheet(null)} />
      </Sheet>
      <Sheet open={sheet === 'password'} title="Cambiar contraseña" onClose={() => setSheet(null)}>
        <PasswordForm onDone={() => { setSheet(null); toast('Contraseña cambiada. Cerramos tus otras sesiones.'); reloadSecurity() }} />
      </Sheet>
    </>
  )
}

function PasswordForm({ onDone }: { onDone: () => void }) {
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [error, setError] = useState('')
  async function submit(e: FormEvent) {
    e.preventDefault()
    setError('')
    try {
      await post('/api/me/password', { current_password: current, new_password: next })
      onDone()
    } catch (err) {
      setError(errorMessage(err))
    }
  }
  return (
    <form onSubmit={submit} className="stack">
      <label className="field">
        <span>Contraseña actual</span>
        <input className="input" type="password" autoComplete="current-password" required value={current} onChange={(e) => setCurrent(e.target.value)} />
      </label>
      <div className="field">
        <label htmlFor="new-pw">Contraseña nueva</label>
        <input id="new-pw" className="input" type="password" autoComplete="new-password" required minLength={12} value={next} onChange={(e) => setNext(e.target.value)} aria-describedby="new-pw-hint" />
        <small id="new-pw-hint" className="hint">Mínimo 12 caracteres. Al cambiarla se cierran tus otras sesiones.</small>
      </div>
      {error && <p className="error" role="alert">{error}</p>}
      <button className="btn btn-primary btn-block">Cambiar contraseña</button>
    </form>
  )
}
