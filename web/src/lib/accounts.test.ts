import { describe, expect, it } from 'vitest'
import { accountResolver, groupedOrder, linkedTo, pickable } from './accounts'
import type { Account } from './types'

const acc = (id: string, merged_into = ''): Account => ({
  id, name: id, type: 'credit_card', institution: '', card_suffix: '', currency: 'USD', color: '#000', icon: '', archived: false, merged_into,
})
const list = [acc('efectivo'), acc('titanium666', 'titanium'), acc('titanium'), acc('visa')]

describe('cuentas fusionadas', () => {
  it('resuelve una vinculada a su principal', () => {
    const main = accountResolver(list)
    expect(main('titanium666')?.id).toBe('titanium')
    expect(main('titanium')?.id).toBe('titanium')
    expect(main('nada')).toBeUndefined()
  })
  it('deja elegir solo principales, salvo la que ya tiene el gasto', () => {
    expect(pickable(list).map((a) => a.id)).toEqual(['efectivo', 'titanium', 'visa'])
    expect(pickable(list, 'titanium666').map((a) => a.id)).toContain('titanium666')
  })
  it('agrupa cada principal con sus vinculadas', () => {
    expect(linkedTo(list, 'titanium').map((a) => a.id)).toEqual(['titanium666'])
    expect(groupedOrder(list).map((a) => a.id)).toEqual(['efectivo', 'titanium', 'titanium666', 'visa'])
  })
  it('muestra una vinculada cuya principal no vino en la lista', () => {
    expect(groupedOrder([acc('sola', 'archivada')]).map((a) => a.id)).toEqual(['sola'])
  })
})
