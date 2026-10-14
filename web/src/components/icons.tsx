// Íconos de trazo propio (sin librerías externas).
const base = { fill: 'none', stroke: 'currentColor', strokeWidth: 1.8, strokeLinecap: 'round' as const, strokeLinejoin: 'round' as const, 'aria-hidden': true }

export const IconHome = () => (
  <svg viewBox="0 0 24 24" {...base}><path d="M3 11.5 12 4l9 7.5M5.5 9.5V20h13V9.5" /></svg>
)
export const IconList = () => (
  <svg viewBox="0 0 24 24" {...base}><path d="M8 6h12M8 12h12M8 18h12M4 6h.01M4 12h.01M4 18h.01" /></svg>
)
export const IconPlan = () => (
  <svg viewBox="0 0 24 24" {...base}><path d="M4 19V5M4 19h16M8 15v-3M12 15V8M16 15v-5" /></svg>
)
export const IconSettings = () => (
  <svg viewBox="0 0 24 24" {...base}><circle cx="12" cy="12" r="3" /><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1Z" /></svg>
)
export const IconWallet = () => (
  <svg viewBox="0 0 24 24" {...base}><path d="M4 7.5A2.5 2.5 0 0 1 6.5 5H18v4M4 7.5V17a2 2 0 0 0 2 2h14V9H6.5A2.5 2.5 0 0 1 4 7.5Z" /><path d="M16 14h.01" /></svg>
)
export const IconTag = () => (
  <svg viewBox="0 0 24 24" {...base}><path d="M3 12V4h8l9 9-8 8-9-9Z" /><circle cx="7.5" cy="8.5" r="1" /></svg>
)
export const IconPlus = () => (
  <svg viewBox="0 0 24 24" {...base} strokeWidth={2.2}><path d="M12 5v14M5 12h14" /></svg>
)
export const IconChevronLeft = () => (
  <svg viewBox="0 0 24 24" {...base}><path d="m15 6-6 6 6 6" /></svg>
)
export const IconChevronRight = () => (
  <svg viewBox="0 0 24 24" {...base}><path d="m9 6 6 6-6 6" /></svg>
)
export const IconClose = () => (
  <svg viewBox="0 0 24 24" {...base}><path d="M6 6l12 12M18 6 6 18" /></svg>
)
export const IconKey = () => (
  <svg viewBox="0 0 24 24" {...base}><circle cx="8" cy="15" r="4" /><path d="m11 12 9-9M17 6l3 3M14 9l2 2" /></svg>
)
export const IconPulse = () => (
  <svg viewBox="0 0 24 24" {...base}><path d="M2 12h5l3-7 4 14 3-7h5" /></svg>
)
export const IconArrow = () => (
  <svg viewBox="0 0 24 24" {...base}><path d="M5 12h14m-5-5 5 5-5 5" /></svg>
)
export const IconPause = () => (
  <svg viewBox="0 0 24 24" {...base}><path d="M9 5v14M15 5v14" /></svg>
)
export const PulseMark = () => (
  <svg className="pulse-mark" viewBox="0 0 64 64" aria-hidden="true">
    <rect width="64" height="64" rx="16" fill="var(--accent)" />
    <path className="pulse-mark-line" pathLength="100" d="M8 36h12l5-14 8 26 6-18 4 6h13" fill="none" stroke="var(--accent-ink)" strokeWidth="4.5" strokeLinecap="round" strokeLinejoin="round" />
  </svg>
)
