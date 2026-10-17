import { useEffect, useRef, type ReactNode } from 'react'
import { IconClose } from './icons'

// Hoja modal con <dialog> nativo: atrapa el foco, Esc cierra y el resto queda inerte.
export function Sheet({ open, title, onClose, children }: { open: boolean; title: string; onClose: () => void; children: ReactNode }) {
  const ref = useRef<HTMLDialogElement>(null)
  const opener = useRef<HTMLElement | null>(null)
  useEffect(() => {
    const d = ref.current
    if (!d) return
    if (open && !d.open) {
      opener.current = document.activeElement instanceof HTMLElement ? document.activeElement : null
      d.showModal()
    }
    if (!open && d.open) {
      d.close()
      const target = opener.current?.isConnected ? opener.current : document.querySelector<HTMLElement>('h1')
      if (target) { if (!target.hasAttribute('tabindex') && target.tagName === 'H1') target.tabIndex = -1; target.focus({ preventScroll: true }) }
    }
  }, [open])
  return (
    <dialog
      ref={ref}
      className="sheet"
      aria-label={title}
      onCancel={(e) => {
        e.preventDefault()
        onClose()
      }}
      onClick={(e) => {
        if (e.target === ref.current) onClose()
      }}
    >
      {open && (
        <>
          <div className="sheet-head">
            <h2>{title}</h2>
            <button type="button" className="icon-btn" onClick={onClose} aria-label="Cerrar">
              <IconClose />
            </button>
          </div>
          <div className="sheet-body">{children}</div>
        </>
      )}
    </dialog>
  )
}
