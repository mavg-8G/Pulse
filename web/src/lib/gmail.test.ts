import { describe, expect, it } from 'vitest'
import { syncSummary } from '../components/GmailSettings'

const r = (x: Partial<Parameters<typeof syncSummary>[0]>) => ({ found: 0, created: 0, confirmed: 0, duplicate: 0, ignored: 0, unrecognized: 0, gemini: 0, ...x })

describe('resumen de sincronización de Gmail', () => {
  it('explica cuando no hay nada', () => {
    expect(syncSummary(r({}))).toBe('No hay correos nuevos del banco.')
    expect(syncSummary(r({ found: 3, duplicate: 3 }))).toBe('No hay gastos nuevos.')
  })
  it('cuenta en singular y plural', () => {
    expect(syncSummary(r({ found: 1, created: 1 }))).toBe('1 gasto nuevo. Revísalos en Movimientos.')
    expect(syncSummary(r({ found: 4, created: 3, confirmed: 2 }))).toBe('3 gastos nuevos, 2 confirmados solos. Revísalos en Movimientos.')
    expect(syncSummary(r({ found: 2, created: 2, confirmed: 1 }))).toBe('2 gastos nuevos, 1 confirmado solo. Revísalos en Movimientos.')
  })
})
