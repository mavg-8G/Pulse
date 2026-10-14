import { StrictMode, useEffect, useState } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { registerSW } from 'virtual:pwa-register'
import '@fontsource-variable/bricolage-grotesque'
import '@fontsource-variable/public-sans'
import './styles.css'
import { App } from './App'
import { ToastProvider } from './components/Toast'
import { ApiError } from './lib/api'
import { AuthProvider } from './lib/auth'

const qc = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      retry: (n, e) => !(e instanceof ApiError && e.status >= 400 && e.status < 500) && n < 2,
      refetchOnWindowFocus: true,
    },
  },
})

let updateSW: ((reload?: boolean) => Promise<void>) | undefined
let notifyUpdate: (() => void) | undefined
if ('serviceWorker' in navigator && import.meta.env.PROD) {
  updateSW = registerSW({ onNeedRefresh: () => notifyUpdate?.() })
}

function UpdateBar() {
  const [show, setShow] = useState(false)
  useEffect(() => {
    notifyUpdate = () => setShow(true)
  }, [])
  if (!show) return null
  return (
    <div className="update-bar" role="status">
      Hay una versión nueva de Pulse.
      <button className="btn btn-primary btn-sm" onClick={() => updateSW?.(true)}>Actualizar</button>
    </div>
  )
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={qc}>
      <BrowserRouter future={{ v7_startTransition: true, v7_relativeSplatPath: true }}>
        <AuthProvider>
          <ToastProvider>
            <UpdateBar />
            <App />
          </ToastProvider>
        </AuthProvider>
      </BrowserRouter>
    </QueryClientProvider>
  </StrictMode>,
)
