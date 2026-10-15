import type { Theme } from './types'

export function applyTheme(t: Theme) {
  const root = document.documentElement
  if (t === 'system') delete root.dataset.theme
  else root.dataset.theme = t
  syncThemeColor()
  try {
    if (t === 'system') localStorage.removeItem('pulse-theme')
    else localStorage.setItem('pulse-theme', t)
  } catch {
    /* almacenamiento bloqueado: el tema igual se aplica en esta visita */
  }
}

/** La barra del navegador (theme-color) sigue al tema elegido en Pulse, no solo al del sistema. */
export function syncThemeColor() {
  const metas = document.querySelectorAll<HTMLMetaElement>('meta[name="theme-color"]')
  const forced = document.documentElement.dataset.theme
  metas.forEach((m) => {
    if (!m.dataset.original) m.dataset.original = m.content
    m.content = forced === 'dark' ? '#171411' : forced === 'light' ? '#f7f3ed' : m.dataset.original
  })
}
