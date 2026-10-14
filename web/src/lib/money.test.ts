import { describe, expect, it } from 'vitest'
import { centsToInput, money, parseAmount } from './money'

describe('parseAmount', () => {
  const ok: [string, number][] = [
    ['17,68', 1768],
    ['17.68', 1768],
    ['$ 17,68', 1768],
    ['USD 4.34', 434],
    ['8,12', 812],
    ['1234', 123400],
    ['0,5', 50],
    ['0,99', 99],
    [',5', 50],
    ['1.234,56', 123456],
    ['1,234.56', 123456],
    ['USD 1,234.56', 123456],
    ['1.234', 123400],
    ['1,234', 123400],
    ['17,689', 1768900], // un separador + 3 dígitos = miles (igual que "1.234")
    ['123.456', 12345600],
    ['999.999,99', 99999999],
    ['12 345,00', 1234500],
    ['1000000', 100000000],
  ]
  it.each(ok)('%s → %i', (input, want) => expect(parseAmount(input)).toBe(want))

  const bad = ['', 'abc', '0', '0,00', '-5', '1.234.567', '1.234.567,89', '1.2.3', '1,23.4.5', '12.34,5.6', '1000000,01', '9999999999', '1e5', '1.23,45', '$']
  it.each(bad)('rechaza %s', (input) => expect(parseAmount(input)).toBeNull())

  it('nunca usa coma flotante (0,1 + 0,2)', () => {
    expect(parseAmount('0,30')).toBe(30)
    expect(parseAmount('1,10')).toBe(110)
  })
})

describe('formato', () => {
  it('muestra en formato de Ecuador', () => {
    expect(money(1768).replace(/\s/g, '')).toMatch(/\$17,68|US\$17,68/)
    expect(centsToInput(123456)).toBe('1234,56')
  })
})
