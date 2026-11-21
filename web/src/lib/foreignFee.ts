import type { Transaction } from './types'

export function foreignFeeCents(tx: Transaction): number {
  if (tx.foreign_fee_cents) return tx.foreign_fee_cents
  if (tx.source === 'manual') return 0
  // Las importaciones anteriores a este campo guardaban el cargo en las notas.
  const fee = /El correo informa un cargo adicional de USD (\d{1,7})\.(\d{2}), no incluido en este monto\./.exec(tx.notes)
  return fee ? Number(fee[1]) * 100 + Number(fee[2]) : 0
}
