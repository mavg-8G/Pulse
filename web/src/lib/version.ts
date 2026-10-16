export interface ServerVersion {
  built_at: string
  revision: string
}

/** Build time in the user's calendar, including seconds; never the current clock. */
export function versionLabel(builtAt: string, tz: string): string | null {
  const date = new Date(builtAt)
  if (!builtAt || Number.isNaN(date.getTime())) return null
  const parts = Object.fromEntries(new Intl.DateTimeFormat('en-CA', {
    timeZone: tz, year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', second: '2-digit', hourCycle: 'h23',
  }).formatToParts(date).map((p) => [p.type, p.value]))
  return `V${parts.year}-${parts.month}-${parts.day} ${parts.hour}:${parts.minute}:${parts.second}`
}
