import { useEffect, useState, type ComponentType } from 'react'
import { IconBell, IconChevronRight, IconExchange, IconKey, IconMail, IconReport, IconSettings, IconWallet } from './icons'

type Section = { id: string; label: string; icon: ComponentType }
const groups: { label: string; sections: Section[] }[] = [
  { label: 'Personal', sections: [
    { id: 'h-look', label: 'Apariencia', icon: IconSettings },
    { id: 'h-notifications', label: 'Notificaciones', icon: IconBell },
  ] },
  { label: 'Finanzas', sections: [
    { id: 'h-data', label: 'Tus datos', icon: IconWallet },
    { id: 'h-gmail', label: 'Correo del banco', icon: IconMail },
    { id: 'h-exchange', label: 'Cambio de moneda', icon: IconExchange },
  ] },
  { label: 'Privacidad y respaldo', sections: [
    { id: 'h-legal', label: 'Privacidad', icon: IconKey },
    { id: 'h-backup', label: 'Respaldo', icon: IconReport },
  ] },
]

export function SettingsNavigation({ admin }: { admin: boolean }) {
  const [hash, setHash] = useState(() => window.location.hash)
  useEffect(() => {
    const update = () => setHash(window.location.hash)
    window.addEventListener('hashchange', update)
    return () => window.removeEventListener('hashchange', update)
  }, [])
  useEffect(() => {
    // A direct URL can finish loading after the browser's first fragment lookup.
    if (!window.location.hash) return
    const frame = requestAnimationFrame(() => document.getElementById(window.location.hash.slice(1))?.scrollIntoView())
    return () => cancelAnimationFrame(frame)
  }, [admin])

  return (
    <nav className="settings-index" aria-label="Secciones de ajustes">
      {groups.map((group) => (
        <div className="settings-nav-group" key={group.label}>
          <p>{group.label}</p>
          <ul>
            {group.sections.filter(({ id }) => id !== 'h-backup' || admin).map(({ id, label, icon: Icon }) => (
              <li key={id}>
                <a href={`#${id}`} aria-current={hash === `#${id}` ? 'location' : undefined}>
                  <span className="settings-icon"><Icon /></span>
                  <span>{label}</span>
                  <span className="settings-nav-chevron"><IconChevronRight /></span>
                </a>
              </li>
            ))}
          </ul>
        </div>
      ))}
    </nav>
  )
}

export function SettingsHeading({ id, title, description }: { id: string; title: string; description?: string }) {
  const Icon = groups.flatMap(({ sections }) => sections).find((section) => section.id === id)?.icon ?? IconSettings
  return (
    <div className="section-head settings-heading">
      <span className="settings-icon"><Icon /></span>
      <div>
        <h2 id={id} tabIndex={-1}>{title}</h2>
        {description && <p className="small muted">{description}</p>}
      </div>
    </div>
  )
}
