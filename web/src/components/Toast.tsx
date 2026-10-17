import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react'

const Ctx = createContext<(msg: string) => void>(() => {})

export function ToastProvider({ children }: { children: ReactNode }) {
  const [msg, setMsg] = useState<string | null>(null)
  const show = useCallback((m: string) => setMsg(m), [])
  useEffect(() => {
    if (!msg) return
    const t = setTimeout(() => setMsg(null), Math.min(8000, Math.max(4500, msg.length * 55)))
    return () => clearTimeout(t)
  }, [msg])
  return (
    <Ctx.Provider value={show}>
      {children}
      <div role="status" aria-live="polite">
        {msg && <div className="toast">{msg}</div>}
      </div>
    </Ctx.Provider>
  )
}

export const useToast = () => useContext(Ctx)
