const fmt = new Intl.NumberFormat('es-EC', { style: 'currency', currency: 'USD', minimumFractionDigits: 2 })
const fmtShort = new Intl.NumberFormat('es-EC', { style: 'currency', currency: 'USD', maximumFractionDigits: 0 })

/** 1768 → "$17,68" (formato ecuatoriano). */
export function money(cents: number): string {
  return fmt.format(cents / 100)
}

export function moneyShort(cents: number): string {
  return fmtShort.format(Math.round(cents / 100))
}

export const MAX_CENTS = 100_000_000

/**
 * Convierte lo que escribe la persona a centavos enteros, sin pasar por flotantes.
 * Acepta "17,68", "17.68", "$ 17,68", "1.234,56", "1,234.56", "1234", "0,5".
 * Si hay un solo separador seguido de 3 dígitos ("1.234") se interpreta como miles.
 * Devuelve null si no es un monto válido.
 */
export function parseAmount(input: string): number | null {
  let s = input.replace(/\s| /g, '').replace(/^(USD|US\$|\$)/i, '')
  if (!/^[0-9.,]+$/.test(s) || !/[0-9]/.test(s)) return null
  const lastDot = s.lastIndexOf('.')
  const lastComma = s.lastIndexOf(',')
  const decPos = Math.max(lastDot, lastComma)
  let intPart = s
  let frac = ''
  if (decPos >= 0) {
    const sep = s[decPos]
    const after = s.slice(decPos + 1)
    const count = s.split(sep).length - 1
    const otherSep = sep === '.' ? ',' : '.'
    const isThousands = after.length === 3 && count === 1 && !s.includes(otherSep)
    const multipleSame = count > 1 && !s.includes(otherSep)
    if (isThousands || multipleSame) {
      // solo separadores de miles
      if (multipleSame && !s.split(sep).slice(1).every((g) => g.length === 3)) return null
      intPart = s.split(sep).join('')
    } else {
      if (after.length > 2) return null
      intPart = s.slice(0, decPos)
      frac = after
      // en la parte entera solo puede quedar el otro separador, como miles en grupos de 3
      const groups = intPart.split(otherSep)
      if (intPart.includes(sep)) return null
      if (groups.length > 1 && !groups.slice(1).every((g) => g.length === 3)) return null
      intPart = groups.join('')
    }
  }
  if (intPart === '') intPart = '0'
  if (!/^\d+$/.test(intPart) || intPart.length > 9) return null
  const cents = Number(intPart) * 100 + Number((frac + '00').slice(0, 2))
  if (!Number.isSafeInteger(cents) || cents <= 0 || cents > MAX_CENTS) return null
  return cents
}

/** Centavos → texto editable "17,68". */
export function centsToInput(cents: number): string {
  return (cents / 100).toFixed(2).replace('.', ',')
}
