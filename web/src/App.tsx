import { lazy, Suspense, useEffect } from 'react'
import { Navigate, Route, Routes } from 'react-router-dom'
import { AppShell } from './components/AppShell'
import { StepUpHost } from './components/StepUp'
import { useAuth } from './lib/auth'
import { useMe } from './lib/queries'
import { applyTheme } from './lib/theme'
import { EnrollPage, LoginPage, RegisterPage, VerifyPage } from './pages/AuthPages'
import { GmailCallback } from './pages/GmailCallback'
import { Home } from './pages/Home'
import { Landing } from './pages/Landing'
import { Movements } from './pages/Movements'
import { Plan } from './pages/Plan'

const Reports = lazy(() => import('./pages/Reports').then((m) => ({ default: m.Reports })))
const Accounts = lazy(() => import('./pages/Accounts').then((m) => ({ default: m.Accounts })))
const Categories = lazy(() => import('./pages/Categories').then((m) => ({ default: m.Categories })))
const Settings = lazy(() => import('./pages/Settings').then((m) => ({ default: m.Settings })))

function Authenticated() {
  const { data: me } = useMe()
  useEffect(() => {
    if (me) applyTheme(me.theme)
  }, [me?.theme])
  return (
    <AppShell>
      <StepUpHost />
      <Suspense fallback={<div className="content-loading" role="status">Cargando vista…</div>}><Routes>
        <Route path="/" element={<Home />} />
        <Route path="/movimientos" element={<Movements />} />
        <Route path="/plan" element={<Plan />} />
        <Route path="/reportes" element={<Reports />} />
        <Route path="/ajustes" element={<Settings />} />
        <Route path="/ajustes/cuenta" element={<Settings view="account" />} />
        <Route path="/ajustes/cuentas" element={<Accounts />} />
        <Route path="/ajustes/categorias" element={<Categories />} />
        <Route path="/ajustes/gmail" element={<GmailCallback />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes></Suspense>
    </AppShell>
  )
}

export function App() {
  const { state, userId } = useAuth()
  if (state === 'loading') return <p className="loading">Cargando…</p>
  if (state === 'mfa') return <VerifyPage />
  if (state === 'enroll') return <EnrollPage />
  if (state === 'none')
    return (
      <Routes>
        <Route path="/" element={<Landing />} />
        <Route path="/registro" element={<RegisterPage />} />
        {/* /entrar y cualquier ruta privada sin sesión: al entrar se abre esa misma ruta */}
        <Route path="*" element={<LoginPage />} />
      </Routes>
    )
  return <Authenticated key={userId ?? 'session'} />
}
