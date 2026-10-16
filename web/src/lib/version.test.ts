import { describe, expect, it } from 'vitest'
import { versionLabel } from './version'

describe('versión del servidor en la zona del usuario', () => {
  it('convierte UTC a Guayaquil incluyendo los segundos', () => {
    expect(versionLabel('2026-10-16T06:18:00Z', 'America/Guayaquil')).toBe('V2026-10-16 01:18:00')
    expect(versionLabel('2026-10-16T00:00:01Z', 'America/Guayaquil')).toBe('V2026-10-15 19:00:01')
  })
  it('respeta el horario de verano y el cambio de año', () => {
    expect(versionLabel('2026-07-01T06:18:00Z', 'America/New_York')).toBe('V2026-07-01 02:18:00')
    expect(versionLabel('2027-01-01T00:00:00Z', 'America/Guayaquil')).toBe('V2026-12-31 19:00:00')
  })
  it('no inventa una fecha para compilaciones sin marca o datos inválidos', () => {
    expect(versionLabel('', 'America/Guayaquil')).toBeNull()
    expect(versionLabel('invalid', 'America/Guayaquil')).toBeNull()
  })
})
