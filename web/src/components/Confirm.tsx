import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from 'react'

export type ConfirmOptions = {
  title: string
  body?: string
  /** Texto del botón que confirma: dice exactamente lo que pasa ("Borrar gasto"). */
  confirm: string
  cancel?: string
  danger?: boolean
}

type Ask = (o: ConfirmOptions) => Promise<boolean>
const Ctx = createContext<Ask>(async () => false)

/**
 * Confirmación con el estilo de Pulse (en lugar de window.confirm): <dialog> nativo, foco atrapado,
 * Esc o clic fuera cancelan y el foco arranca en "Cancelar" para que Enter nunca borre por accidente.
 */
export function ConfirmProvider({ children }: { children: ReactNode }) {
  const [opts, setOpts] = useState<ConfirmOptions | null>(null)
  const resolver = useRef<((v: boolean) => void) | null>(null)
  const ref = useRef<HTMLDialogElement>(null)
  const cancelRef = useRef<HTMLButtonElement>(null)

  const ask = useCallback<Ask>((o) => {
    resolver.current?.(false)
    setOpts(o)
    return new Promise<boolean>((resolve) => (resolver.current = resolve))
  }, [])

  const finish = (v: boolean) => {
    resolver.current?.(v)
    resolver.current = null
    ref.current?.close()
    setOpts(null)
  }

  useEffect(() => {
    if (opts && ref.current && !ref.current.open) {
      ref.current.showModal()
      cancelRef.current?.focus()
    }
  }, [opts])

  return (
    <Ctx.Provider value={ask}>
      {children}
      <dialog
        ref={ref}
        className="sheet confirm"
        role="alertdialog"
        aria-labelledby="confirm-title"
        aria-describedby={opts?.body ? 'confirm-body' : undefined}
        onCancel={(e) => {
          e.preventDefault()
          finish(false)
        }}
        onClick={(e) => {
          if (e.target === ref.current) finish(false)
        }}
      >
        {opts && (
          <div className="confirm-body">
            <h2 id="confirm-title">{opts.title}</h2>
            {opts.body && <p id="confirm-body" className="muted">{opts.body}</p>}
            <div className="confirm-actions">
              <button ref={cancelRef} type="button" className="btn" onClick={() => finish(false)}>
                {opts.cancel ?? 'Cancelar'}
              </button>
              <button type="button" className={`btn ${opts.danger ? 'btn-danger-solid' : 'btn-primary'}`} onClick={() => finish(true)}>
                {opts.confirm}
              </button>
            </div>
          </div>
        )}
      </dialog>
    </Ctx.Provider>
  )
}

export const useConfirm = () => useContext(Ctx)
