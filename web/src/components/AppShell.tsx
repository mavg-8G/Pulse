import { createContext, useContext, useEffect, useRef, useState, type ReactNode } from 'react'
import { Link, NavLink, useBlocker, useLocation, useSearchParams } from 'react-router-dom'
import { useConfirm } from './Confirm'
import { monthLabel, monthOf, shiftMonth } from '../lib/dates'
import { money } from '../lib/money'
import { useAccounts, useCategories, useDashboard, useMe } from '../lib/queries'
import type { Transaction } from '../lib/types'
import { IconArrow, IconHome, IconList, IconPlan, IconPlus, IconPulse, IconReport, IconSettings, IconTag, IconWallet, PulseMark } from './icons'
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
  { to: '/ajustes', label: 'Ajustes', icon: <IconSettings />, end: true },
]

export function AppShell({ children }: { children: ReactNode }) {
  const ask = useConfirm()
  const { data: me } = useMe()
  const tz = me?.timezone ?? 'America/Guayaquil'
  const current = monthOf(new Date(), tz)
  const { data: dashboard } = useDashboard(current)
  const { data: accounts = [] } = useAccounts(true)
  const { data: categories = [] } = useCategories()
  const [sheet, setSheet] = useState<{ open: boolean; tx?: Transaction }>({ open: false })
  const [formState, setFormState] = useState({ dirty: false, busy: false })
  const confirming = useRef(false)
  const finish = () => { setFormState({ dirty: false, busy: false }); setSheet({ open: false }) }
  const open = (tx?: Transaction) => { setFormState({ dirty: false, busy: false }); setSheet({ open: true, tx }) }
  const mayDiscard = async () => {
    if (formState.busy || confirming.current) return false
    if (!formState.dirty) return true
    confirming.current = true
    try { return await ask({ title: '¿Descartar los cambios?', body: 'Los cambios de este formulario todavía no se han guardado.', cancel: 'Seguir editando', confirm: 'Descartar cambios', danger: true }) }
    finally { confirming.current = false }
  }
  const close = async () => { if (await mayDiscard()) finish() }
  const blocker = useBlocker(sheet.open && (formState.dirty || formState.busy))
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
  useEffect(() => {
    if (!sheet.open || (!formState.dirty && !formState.busy)) return
    const beforeUnload = (e: BeforeUnloadEvent) => { e.preventDefault(); e.returnValue = '' }
    window.addEventListener('beforeunload', beforeUnload)
    return () => window.removeEventListener('beforeunload', beforeUnload)
  }, [sheet.open, formState.dirty, formState.busy])
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
  const manage = [
    { to: '/ajustes/cuentas', label: 'Cuentas y tarjetas', icon: <IconWallet />, count: accounts.filter((a) => !a.archived && !a.merged_into).length },
    { to: '/ajustes/categorias', label: 'Categorías', icon: <IconTag />, count: categories.filter((c) => !c.archived && !c.is_system).length },
  ]
  const pageName = location.pathname === '/ajustes/cuenta' ? 'Mi cuenta' : location.pathname === '/reportes' ? 'Reportes' : manage.find((n) => n.to === location.pathname)?.label ?? nav.find((n) => n.to === location.pathname)?.label ?? 'Mi espacio'
  const date = new Intl.DateTimeFormat('es-EC', { weekday: 'long', day: 'numeric', month: 'long', timeZone: tz }).format(new Date())
  const initial = me?.email.slice(0, 1).toUpperCase() ?? 'P'

  return (
    <MotionCtx.Provider value={{ motion, toggleMotion: () => setMotion((v) => !v) }}>
    <TxSheetCtx.Provider value={open}>
      <a href="#contenido" className="skip-link">Ir al contenido</a>
      <div className="shell">
        <nav className="sidebar" aria-label="Principal">
          <NavLink to="/" className="brand"><PulseMark /><span><span translate="no">Pulse</span><small>Encuentra tu equilibrio</small></span></NavLink>
          <button className="btn btn-primary side-add" onClick={() => open()}><IconPlus /> Nuevo gasto<span className="side-add-dot" aria-hidden="true" /></button>
          <p className="nav-section-label">Mi día a día</p>
          <div className="side-nav-group">{nav.slice(0, 4).map((n) => <NavLink key={n.to} to={n.to} end={n.end} className="side-link">{n.icon}<span>{n.label}</span>{n.to === '/movimientos' && !!dashboard?.pending_review && <span className="nav-count">{dashboard.pending_review}</span>}</NavLink>)}</div>
          <p className="nav-section-label">Organización</p>
          <div className="side-nav-group">{manage.map((n) => <NavLink key={n.to} to={n.to} className="side-link">{n.icon}<span>{n.label}</span><span className="nav-count" aria-hidden="true">{n.count}</span></NavLink>)}<NavLink to="/ajustes" end className="side-link"><IconSettings /><span>Ajustes</span></NavLink></div>
          <Link to="/plan" className="sidebar-insight"><div><span className="eyebrow">Este mes</span><span className="live-dot" /></div><strong className="num">{dashboard ? money(dashboard.spent_cents) : '—'}</strong><p>{dashboard?.has_budgets ? `Presupuesto: ${money(dashboard.budget.budget_cents)}` : 'Cada gasto cuenta. Tú marcas el ritmo.'}</p><svg className="sidebar-wave" viewBox="0 0 180 38" aria-hidden="true"><path className="wave-base" d="M0 23h180" /><path className="wave-line" pathLength="100" d="M0 23h34l6-6 5 6h13l8-17 10 30 9-21 7 8h22l5-9 7 9h54" /></svg><span className="sidebar-insight-link">{monthLabel(current)} <IconArrow /></span></Link>
          <Link className="side-profile" to="/ajustes/cuenta"><span className="avatar">{initial}</span><span><strong>Mi cuenta</strong><small>{me?.email}</small></span><IconSettings /></Link>
        </nav>
        <main className="main" id="contenido" tabIndex={-1}>
          <header className="workspace-topbar">
            <Link className="mobile-brand" to="/" aria-label="Pulse, inicio"><PulseMark /><strong>Pulse</strong></Link>
            <div className="workspace-label"><span>Espacio personal</span><span className="breadcrumb-divider">/</span><strong>{pageName}</strong></div>
            <div className="workspace-tools"><SyncStatus tz={tz} /><span className="workspace-date">{date}</span><button className="btn btn-primary mobile-add" onClick={() => open()} aria-label="Nuevo gasto"><IconPlus /><span>Nuevo</span></button><Link className="topbar-profile avatar" to="/ajustes/cuenta" aria-label="Mi cuenta">{initial}</Link></div>
          </header>
          <div className={`page-content ${['/ajustes', '/ajustes/cuenta'].includes(location.pathname) ? 'settings-layout' : ''}`} data-view={location.pathname.split('/').filter(Boolean).at(-1) ?? 'inicio'} key={location.pathname}>{children}</div>
          <footer className="workspace-footer"><span><IconPulse /> Pulse</span><span className="workspace-signoff">Un gasto a la vez. <BuildVersion tz={tz} /></span></footer>
        </main>
        <nav className="tabbar" aria-label="Principal">{nav.map((n) => <NavLink key={n.to} to={n.to} end={n.end} className="tab">{n.icon}{n.label}</NavLink>)}</nav>
      </div>
      <Sheet open={sheet.open} title={sheet.tx ? 'Editar gasto' : 'Nuevo gasto'} onClose={close}><TxForm key={sheet.tx ? `${sheet.tx.id}:${sheet.tx.version}` : 'new'} tx={sheet.tx} defaultAccountId={location.pathname === '/movimientos' ? params.get('account') ?? undefined : undefined} onStateChange={setFormState} onDone={finish} onReplace={(tx) => { setFormState({ dirty: false, busy: false }); setSheet({ open: true, tx }) }} /></Sheet>
    </TxSheetCtx.Provider>
    </MotionCtx.Provider>
  )
}

export function MonthSwitch({ month, onChange, label }: { month: string; onChange: (m: string) => void; label: string }) {
  return <div className="month-switch" role="group" aria-label="Mes"><button className="icon-btn" aria-label="Mes anterior" onClick={() => onChange(shiftMonth(month, -1))}><svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" aria-hidden="true"><path d="m15 6-6 6 6 6" /></svg></button><strong aria-live="polite">{label}</strong><button className="icon-btn" aria-label="Mes siguiente" onClick={() => onChange(shiftMonth(month, 1))}><svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" aria-hidden="true"><path d="m9 6 6 6-6 6" /></svg></button></div>
}
