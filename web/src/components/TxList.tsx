import { dayKey, dayLabel, timeLabel } from '../lib/dates'
import { money } from '../lib/money'
import type { Account, Category, Transaction } from '../lib/types'
import { useTxSheet } from './AppShell'

export function TxRow({ tx, cat, acc, tz }: { tx: Transaction; cat?: Category; acc?: Account; tz: string }) {
  const open = useTxSheet()
  const title = tx.merchant || cat?.name || 'Gasto'
  return (
    <button className="tx" onClick={() => open(tx)}>
      <span className="tx-icon" style={{ background: `color-mix(in srgb, ${cat?.color ?? '#888'} 18%, transparent)` }} aria-hidden="true">
        {cat?.icon || '•'}
      </span>
      <span className="tx-main">
        <span className="tx-title">
          {title} {tx.status === 'pending_review' && <span className="badge warn">Por revisar</span>}
          {tx.pending && <span className="badge pending">Pendiente de enviar</span>}
        </span>
        <span className="tx-meta">
          {cat?.name}
          {acc ? ` en ${acc.name}` : ''} a las {timeLabel(tx.occurred_at, tz)}
        </span>
      </span>
      <span className={`tx-amount ${tx.kind === 'refund' ? 'refund' : ''}`}>
        {tx.kind === 'refund' ? '+' : ''}
        {money(tx.amount_cents)}
      </span>
    </button>
  )
}

export function TxGroups({ items, categories, accounts, tz }: { items: Transaction[]; categories: Category[]; accounts: Account[]; tz: string }) {
  const cats = new Map(categories.map((c) => [c.id, c]))
  const accs = new Map(accounts.map((a) => [a.id, a]))
  const today = dayKey(new Date().toISOString(), tz)
  const yesterday = dayKey(new Date(Date.now() - 864e5).toISOString(), tz)
  const groups: { key: string; items: Transaction[] }[] = []
  for (const t of items) {
    const k = dayKey(t.occurred_at, tz)
    if (groups.at(-1)?.key !== k) groups.push({ key: k, items: [] })
    groups.at(-1)!.items.push(t)
  }
  return (
    <>
      {groups.map((g) => {
        const sum = g.items.reduce((a, t) => a + (t.kind === 'refund' ? -t.amount_cents : t.amount_cents), 0)
        return (
          <section key={g.key} aria-label={dayLabel(g.key, today, yesterday)}>
            <div className="day-head">
              <span>{dayLabel(g.key, today, yesterday)}</span>
              <span className="num">{money(sum)}</span>
            </div>
            <ul className="list">
              {g.items.map((t) => (
                <li key={t.id}>
                  <TxRow tx={t} cat={cats.get(t.category_id)} acc={accs.get(t.account_id)} tz={tz} />
                </li>
              ))}
            </ul>
          </section>
        )
      })}
    </>
  )
}
