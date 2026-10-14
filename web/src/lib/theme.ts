import type { Theme } from './types'

export function applyTheme(t: Theme) {
  const root = document.documentElement
  if (t === 'system') delete root.dataset.theme
  else root.dataset.theme = t
  try {
    if (t === 'system') localStorage.removeItem('pulse-theme')
    else localStorage.setItem('pulse-theme', t)
  } catch {
    /* almacenamiento bloqueado: el tema igual se aplica en esta visita */
  }
}
