import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from 'react'
import { Link, NavLink, useBlocker, useLocation, useSearchParams } from 'react-router-dom'
import { useConfirm } from './Confirm'
import { monthOf, shiftMonth } from '../lib/dates'
import { useDashboard, useMe } from '../lib/queries'
import type { Transaction } from '../lib/types'
import { IconHome, IconList, IconPlan, IconPlus, IconPulse, IconReport, IconSettings, PulseMark } from './icons'
import { Sheet } from './Sheet'
import { SyncStatus } from './SyncStatus'
import { TxForm } from './TxForm'
import { BuildVersion } from './BuildVersion'

const TxSheetCtx = createContext<(tx?: Transaction) => void>(() => {})
export const useTxSheet = () => useContext(TxSheetCtx)
const MotionCtx = createContext({ motion: true, toggleMotion: () => {} })
export const useMotion = () => useContext(MotionCtx)
const nav = [
  { to: '/', label: 'Inicio', icon: <IconHome />, end: true },
  { to: '/movimientos', label: 'Movimientos', icon: <IconList /> },
  { to: '/plan', label: 'Plan', icon: <IconPlan /> },
  { to: '/reportes', label: 'Reportes', icon: <IconReport /> },
]
const manage = [
  { to: '/ajustes/cuentas', label: 'Cuentas' },
  { to: '/ajustes/categorias', label: 'Categorías' },
]

export function AppShell({ children }: { children: ReactNode }) {
  const ask = useConfirm()
  const { data: me } = useMe()
  const tz = me?.timezone ?? 'America/Guayaquil'
  const current = monthOf(new Date(), tz)
  const { data: dashboard } = useDashboard(current)
  const [sheet, setSheet] = useState<{ open: boolean; tx?: Transaction }>({ open: false })
  // El bloqueo consulta el último formulario ya renderizado, sin esperar otra
  // renderización del shell ni volver a registrar el blocker después de teclear.
  const formState = useRef({ open: false, dirty: false, busy: false })
  const setFormState = useCallback((state: { dirty: boolean; busy: boolean }) => { formState.current = { ...formState.current, ...state } }, [])
  const confirming = useRef(false)
  const finish = () => { formState.current = { open: false, dirty: false, busy: false }; setSheet({ open: false }) }
  const open = (tx?: Transaction) => { formState.current = { open: true, dirty: false, busy: false }; setSheet({ open: true, tx }) }
  const mayDiscard = async () => {
    if (formState.current.busy || confirming.current) return false
    if (!formState.current.dirty) return true
    confirming.current = true
    try { return await ask({ title: '¿Descartar los cambios?', body: 'Los cambios de este formulario todavía no se han guardado.', cancel: 'Seguir editando', confirm: 'Descartar cambios', danger: true }) }
    finally { confirming.current = false }
  }
  const close = async () => { if (await mayDiscard()) finish() }
  const blocker = useBlocker(useCallback(() => {
    const state = formState.current
    return state.open && (state.dirty || state.busy)
  }, []))
  const blockedNavigation = useRef(false)
  const latestBlocker = useRef(blocker)
  latestBlocker.current = blocker
  useEffect(() => {
    if (blocker.state !== 'blocked' || blockedNavigation.current) return
    blockedNavigation.current = true
    mayDiscard().then((yes) => {
      blockedNavigation.current = false
      const pending = latestBlocker.current
      if (pending.state !== 'blocked') return
      if (yes) { finish(); pending.proceed() } else pending.reset()
    })
  }, [blocker])
  const [params, setParams] = useSearchParams()
  const location = useLocation()
  const [motion, setMotion] = useState(() => {
    if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) return false
    try { return localStorage.getItem('pulse-motion') !== 'paused' } catch { return true }
  })
  useEffect(() => {
    document.documentElement.dataset.motion = motion ? 'on' : 'paused'
    try { localStorage.setItem('pulse-motion', motion ? 'on' : 'paused') } catch { /* preferencia opcional */ }
  }, [motion])
  useEffect(() => {
    if (params.get('nuevo') === '1') {
      open()
      const next = new URLSearchParams(params)
      next.delete('nuevo')
      setParams(next, { replace: true })
    }
  }, [params, setParams])
  const initial = me?.email.slice(0, 1).toUpperCase() ?? 'P'
  const pending = dashboard?.pending_review ?? 0
  const count = pending > 0 ? <span className="nav-count"><span aria-hidden="true">{pending}</span><span className="sr-only">, {pending} sin confirmar</span></span> : null

  return (
    <MotionCtx.Provider value={{ motion, toggleMotion: () => setMotion((v) => !v) }}>
    <TxSheetCtx.Provider value={open}>
      <a href="#contenido" className="skip-link">Ir al contenido</a>
      <div className="shell">
        <header className="topbar">
          <div className="topbar-inner">
            <Link to="/" className="brand" aria-label="Pulse, inicio"><PulseMark /><span translate="no">Pulse</span></Link>
            <nav className="topnav" aria-label="Principal">
              {nav.map((n) => <NavLink key={n.to} to={n.to} end={n.end} className="topnav-link" viewTransition>{n.label}{n.to === '/movimientos' && count}</NavLink>)}
              {manage.map((n) => <NavLink key={n.to} to={n.to} className="topnav-link" viewTransition>{n.label}</NavLink>)}
            </nav>
            <div className="topbar-tools">
              <SyncStatus tz={tz} />
              <button className="btn btn-primary topbar-add" onClick={() => open()}><IconPlus /><span>Nuevo gasto</span></button>
              <NavLink to="/ajustes" end className="icon-btn topbar-settings" aria-label="Ajustes"><IconSettings /></NavLink>
              <Link className="avatar" to="/ajustes/cuenta" aria-label="Mi cuenta">{initial}</Link>
            </div>
          </div>
          <svg className="topbar-trace" viewBox="0 0 1000 8" preserveAspectRatio="none" aria-hidden="true"><path pathLength="100" d="M0 6h420l10-5 12 7 9-4 6 2h543" /></svg>
        </header>
        <main className="main" id="contenido" tabIndex={-1}>
          <div className={`page-content ${['/ajustes', '/ajustes/cuenta'].includes(location.pathname) ? 'settings-layout' : ''}`} data-view={location.pathname.split('/').filter(Boolean).at(-1) ?? 'inicio'} key={location.pathname}>{children}</div>
          <footer className="workspace-footer"><span><IconPulse /> <span translate="no">Pulse</span></span><span className="workspace-signoff">Un gasto a la vez. <BuildVersion tz={tz} /></span></footer>
        </main>
        <nav className="tabbar" aria-label="Principal">
          {nav.slice(0, 2).map((n) => <NavLink key={n.to} to={n.to} end={n.end} className="tab" viewTransition>{n.icon}<span>{n.label}</span>{n.to === '/movimientos' && pending > 0 && <><span className="tab-dot" aria-hidden="true" /><span className="sr-only">, {pending} sin confirmar</span></>}</NavLink>)}
          <button className="tab-add" onClick={() => open()} aria-label="Nuevo gasto"><IconPlus /></button>
          {nav.slice(2).map((n) => <NavLink key={n.to} to={n.to} end={n.end} className="tab" viewTransition>{n.icon}<span>{n.label}</span></NavLink>)}
        </nav>
      </div>
      <Sheet open={sheet.open} title={sheet.tx ? 'Editar gasto' : 'Nuevo gasto'} onClose={close}><TxForm key={sheet.tx ? `${sheet.tx.id}:${sheet.tx.version}` : 'new'} tx={sheet.tx} defaultAccountId={location.pathname === '/movimientos' ? params.get('account') ?? undefined : undefined} onStateChange={setFormState} onDone={finish} onReplace={open} /></Sheet>
    </TxSheetCtx.Provider>
    </MotionCtx.Provider>
  )
}

export function MonthSwitch({ month, onChange, label }: { month: string; onChange: (m: string) => void; label: string }) {
  return <div className="month-switch" role="group" aria-label="Mes"><button className="icon-btn" aria-label="Mes anterior" onClick={() => onChange(shiftMonth(month, -1))}><svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" aria-hidden="true"><path d="m15 6-6 6 6 6" /></svg></button><strong aria-live="polite">{label}</strong><button className="icon-btn" aria-label="Mes siguiente" onClick={() => onChange(shiftMonth(month, 1))}><svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" aria-hidden="true"><path d="m9 6 6 6-6 6" /></svg></button></div>
}
