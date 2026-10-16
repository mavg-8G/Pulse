// Fechas siempre mostradas en la zona horaria del usuario (no la del navegador).

export function monthOf(d: Date, tz: string): string {
  const p = new Intl.DateTimeFormat('en-CA', { timeZone: tz, year: 'numeric', month: '2-digit' }).formatToParts(d)
  return `${p.find((x) => x.type === 'year')!.value}-${p.find((x) => x.type === 'month')!.value}`
}

export function shiftMonth(month: string, delta: number): string {
  const [y, m] = month.split('-').map(Number)
  const d = new Date(Date.UTC(y, m - 1 + delta, 1))
  return `${d.getUTCFullYear()}-${String(d.getUTCMonth() + 1).padStart(2, '0')}`
}

export function monthLabel(month: string): string {
  const [y, m] = month.split('-').map(Number)
  const s = new Intl.DateTimeFormat('es-EC', { month: 'long', year: 'numeric', timeZone: 'UTC' }).format(new Date(Date.UTC(y, m - 1, 15)))
  return s.charAt(0).toUpperCase() + s.slice(1)
}

export function dayKey(iso: string, tz: string): string {
  return new Intl.DateTimeFormat('en-CA', { timeZone: tz, year: 'numeric', month: '2-digit', day: '2-digit' }).format(new Date(iso))
}

export function dayLabel(key: string, todayKey: string, yesterdayKey: string): string {
  if (key === todayKey) return 'Hoy'
  if (key === yesterdayKey) return 'Ayer'
  const [y, m, d] = key.split('-').map(Number)
  const s = new Intl.DateTimeFormat('es-EC', { weekday: 'long', day: 'numeric', month: 'long', timeZone: 'UTC' }).format(new Date(Date.UTC(y, m - 1, d, 12)))
  return s.charAt(0).toUpperCase() + s.slice(1)
}

export function timeLabel(iso: string, tz: string): string {
  return new Intl.DateTimeFormat('es-EC', { timeZone: tz, hour: '2-digit', minute: '2-digit', hour12: false }).format(new Date(iso))
}

export function dateTimeLabel(iso: string, tz: string): string {
  return new Intl.DateTimeFormat('es-EC', { timeZone: tz, day: 'numeric', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit', hour12: false }).format(new Date(iso))
}

/** ISO UTC → "AAAA-MM-DDTHH:MM" en la zona del usuario (para <input type="datetime-local">). */
export function toLocalInput(iso: string, tz: string): string {
  const p = Object.fromEntries(
    new Intl.DateTimeFormat('en-CA', { timeZone: tz, year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hourCycle: 'h23' })
      .formatToParts(new Date(iso))
      .map((x) => [x.type, x.value]),
  )
  return `${p.year}-${p.month}-${p.day}T${p.hour}:${p.minute}`
}

export function nowLocalInput(tz: string): string {
  return toLocalInput(new Date().toISOString(), tz)
}

/** Hora de una compilación en la zona del usuario: "V2026-10-16 01:18:00". */
export function versionLabel(iso: string | undefined, tz: string): string {
  if (!iso || Number.isNaN(Date.parse(iso))) return ''
  const p = Object.fromEntries(
    new Intl.DateTimeFormat('en-CA', { timeZone: tz, year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', hourCycle: 'h23' })
      .formatToParts(new Date(iso))
      .map((x) => [x.type, x.value]),
  )
  return `V${p.year}-${p.month}-${p.day} ${p.hour}:${p.minute}:${p.second}`
}
