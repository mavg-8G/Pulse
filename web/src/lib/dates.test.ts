import { describe, expect, it } from 'vitest'
import { dayKey, monthLabel, monthOf, shiftMonth, toLocalInput, versionLabel } from './dates'

const gye = 'America/Guayaquil'

describe('fechas en la zona del usuario', () => {
  it('31 oct 23:30 en Guayaquil sigue siendo octubre aunque en UTC sea noviembre', () => {
    const d = new Date('2026-11-01T04:30:00Z')
    expect(monthOf(d, gye)).toBe('2026-10')
    expect(dayKey(d.toISOString(), gye)).toBe('2026-10-31')
    expect(toLocalInput(d.toISOString(), gye)).toBe('2026-10-31T23:30')
  })
  it('cambia de mes y año', () => {
    expect(shiftMonth('2026-12', 1)).toBe('2027-01')
    expect(shiftMonth('2026-01', -1)).toBe('2025-12')
  })
  it('nombre del mes en español', () => {
    expect(monthLabel('2026-10').toLowerCase()).toContain('octubre')
  })
})

describe('versionLabel', () => {
  it('convierte la hora UTC de la compilación a la zona del usuario', () => {
    expect(versionLabel('2026-10-16T06:18:00Z', 'America/Guayaquil')).toBe('V2026-10-16 01:18:00')
    expect(versionLabel('2026-10-16T06:18:00Z', 'UTC')).toBe('V2026-10-16 06:18:00')
  })
  it('sin fecha válida no muestra nada', () => {
    expect(versionLabel(undefined, 'UTC')).toBe('')
    expect(versionLabel('x', 'UTC')).toBe('')
  })
})
