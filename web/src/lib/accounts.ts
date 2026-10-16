import type { Account } from './types'

// Cuentas fusionadas: una vinculada (merged_into) se muestra y se elige como su cuenta principal.

/** Cuentas que se pueden elegir: las principales, más `keep` si es una vinculada (p. ej. la del gasto que se edita). */
export function pickable(accounts: Account[], keep?: string): Account[] {
  return accounts.filter((a) => !a.merged_into || a.id === keep)
}

/** Devuelve, para cualquier id, la cuenta principal de su grupo (o la misma si no está fusionada). */
export function accountResolver(accounts: Account[]): (id: string) => Account | undefined {
  const byId = new Map(accounts.map((a) => [a.id, a]))
  return (id) => {
    const a = byId.get(id)
    return (a?.merged_into && byId.get(a.merged_into)) || a
  }
}

/** Cuentas vinculadas a la principal `id`. */
export function linkedTo(accounts: Account[], id: string): Account[] {
  return accounts.filter((a) => a.merged_into === id)
}

/** Ordena para mostrar cada principal seguida de sus vinculadas, conservando el orden recibido. */
export function groupedOrder(accounts: Account[]): Account[] {
  const known = new Set(accounts.map((a) => a.id))
  const out: Account[] = []
  for (const a of accounts) {
    if (a.merged_into && known.has(a.merged_into)) continue
    out.push(a, ...linkedTo(accounts, a.id))
  }
  return out
}
