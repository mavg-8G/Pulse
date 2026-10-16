import 'fake-indexeddb/auto'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cachedGet, localInputToISO, overlayRecurring, overlayTransactions } from './offline'
import { __reset, enqueue, flush, loadOutbox, resetOutbox, resolve, sendOrQueue, snapshot, withDay } from './outbox'
import type { Transaction, TxPage } from './types'
import { OFFLINE_DAYS, allRecords, closeVault, getCache, lastUser, openVault, putCache, rememberUser, wipeVaults } from './vault'

type Call = { method: string; url: string; headers: Record<string, string>; body: any }
let calls: Call[] = []
let respond: (c: Call) => Response | Promise<Response> = () => json(200, {})
const json = (status: number, body: unknown) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
const offline = () => Promise.reject(new TypeError('Failed to fetch'))

beforeEach(async () => {
  calls = []
  respond = () => json(200, {})
  vi.stubGlobal('fetch', async (url: string, init: RequestInit = {}) => {
    const c: Call = { method: init.method ?? 'GET', url, headers: (init.headers ?? {}) as Record<string, string>, body: init.body ? JSON.parse(init.body as string) : undefined }
    calls.push(c)
    return respond(c)
  })
  __reset()
  await wipeVaults()
  await openVault('ana')
})

afterEach(() => {
  closeVault()
  vi.unstubAllGlobals()
})

const tx = (over: Partial<Transaction> = {}): Transaction => ({
  id: 't1', account_id: 'a1', category_id: 'c1', amount_cents: 1000, currency: 'USD', occurred_at: '2026-10-10T17:00:00Z', kind: 'purchase',
  merchant: 'Supermaxi', description: '', notes: '', source: 'manual', status: 'confirmed', version: 3, ...over,
})

describe('almacén cifrado por persona', () => {
  it('cada persona solo lee lo suyo y una entrada copiada a otra base no se descifra', async () => {
    await putCache('/api/me', { email: 'ana@example.com' })
    expect((await getCache<{ email: string }>('/api/me'))?.value.email).toBe('ana@example.com')

    // Lo guardado no está en claro en IndexedDB.
    const raw = await new Promise<any>((ok) => {
      const r = indexedDB.open('pulse-u-ana')
      r.onsuccess = () => {
        const g = r.result.transaction('cache').objectStore('cache').get('/api/me')
        g.onsuccess = () => {
          ok(g.result)
          r.result.close()
        }
      }
    })
    expect(new TextDecoder().decode(raw.data)).not.toContain('ana@example.com')

    await openVault('beto')
    expect(await getCache('/api/me')).toBeNull()
    // Copiar la entrada cifrada de Ana a la base de Beto no sirve: otra clave y otros datos asociados.
    await new Promise<void>((ok) => {
      const r = indexedDB.open('pulse-u-beto')
      r.onsuccess = () => {
        const t = r.result.transaction('cache', 'readwrite')
        t.objectStore('cache').put(raw)
        t.oncomplete = () => {
          r.result.close()
          ok()
        }
      }
    })
    expect(await getCache('/api/me')).toBeNull()
  })

  it('recuerda a la última persona solo por un tiempo y lo olvida al cerrar sesión', async () => {
    rememberUser('ana')
    expect(lastUser()).toBe('ana')
    expect(lastUser(Date.now() + OFFLINE_DAYS * 864e5 + 1000)).toBeNull()
    rememberUser('../../otro') // ids raros no se guardan
    expect(lastUser()).toBe('ana')
    await wipeVaults()
    expect(lastUser()).toBeNull()
    const names = (await indexedDB.databases()).map((d) => d.name)
    expect(names).not.toContain('pulse-u-ana')
  })
})

describe('cola de cambios sin conexión', () => {
  it('una edición hecha sin conexión llega una sola vez al reconectar, con versión e id de cambio', async () => {
    respond = offline
    expect(await sendOrQueue({ kind: 'tx.update', entity: 't1', body: { amount_cents: 2000 }, version: 3, base: { ...tx() }, label: 'Supermaxi $20,00' })).toBe('queued')
    await flush()
    expect(snapshot()).toHaveLength(1)

    respond = (c) => json(200, { ...tx(), amount_cents: c.body.amount_cents, version: 4 })
    calls = [] // los intentos sin red no llegaron al servidor
    const r1 = await flush()
    const r2 = await flush()
    expect(r1.sent).toBe(1)
    expect(r2.sent).toBe(0)
    const patches = calls.filter((c) => c.method === 'PATCH')
    expect(patches).toHaveLength(1)
    expect(patches[0].url).toBe('/api/transactions/t1')
    expect(patches[0].headers['If-Match']).toBe('3')
    expect(patches[0].headers['Idempotency-Key']).toMatch(/^[0-9a-f-]{36}$/)
    expect(snapshot()).toHaveLength(0)
    expect(await allRecords()).toHaveLength(0)
  })

  it('recargar la cola mientras se envía (p. ej. al volver a entrar) no reenvía lo que ya llegó', async () => {
    respond = offline
    await sendOrQueue({ kind: 'tx.create', entity: 'n1', body: { account_id: 'a1', category_id: 'c1', amount_cents: 350, occurred_at: '2026-10-10T12:00', merchant: 'Café' }, label: 'Café' })
    let release!: () => void
    const gate = new Promise<void>((ok) => (release = ok))
    respond = async () => {
      await gate
      return json(201, { ...tx({ id: 'n1' }), version: 1 })
    }
    calls = []
    const sending = flush()
    const reloading = loadOutbox() // lee el almacén mientras el alta está en vuelo
    release()
    await Promise.all([sending, reloading])
    await flush()
    expect(calls.filter((c) => c.method === 'POST')).toHaveLength(1)
    expect(snapshot()).toHaveLength(0)
  })

  it('combina los cambios del mismo registro', async () => {
    respond = offline
    await enqueue({ kind: 'tx.create', entity: 'n1', body: { amount_cents: 500, category_id: 'c1', account_id: 'a1', occurred_at: '2026-10-11T09:00' }, label: 'Nuevo' })
    await enqueue({ kind: 'tx.update', entity: 'n1', body: { amount_cents: 700, learn_rule: true, status: 'confirmed' }, label: 'Nuevo' })
    await enqueue({ kind: 'tx.create', entity: 'n2', body: { amount_cents: 100 }, label: 'Otro' })
    await enqueue({ kind: 'tx.delete', entity: 'n2', label: 'Otro' })
    await enqueue({ kind: 'tx.update', entity: 't1', body: { merchant: 'A' }, version: 3, label: 'A' })
    await enqueue({ kind: 'tx.update', entity: 't1', body: { notes: 'B' }, version: 9, label: 'A' })
    expect(snapshot()).toHaveLength(2)

    respond = (c) => json(c.method === 'POST' ? 201 : 200, { version: 4 })
    await flush()
    const posts = calls.filter((c) => c.method === 'POST')
    expect(posts).toHaveLength(1)
    expect(posts[0].body).toEqual({ amount_cents: 700, category_id: 'c1', account_id: 'a1', occurred_at: '2026-10-11T09:00', id: 'n1' }) // sin learn_rule ni status
    const patch = calls.find((c) => c.method === 'PATCH')!
    expect(patch.body).toEqual({ merchant: 'A', notes: 'B' })
    expect(patch.headers['If-Match']).toBe('3') // la versión que se vio primero
    expect(calls.some((c) => c.url.includes('n2'))).toBe(false)
  })

  it('si lo cambiaron en otro dispositivo, pide elegir y "usar la mía" reintenta sobre la versión actual', async () => {
    respond = offline
    await enqueue({ kind: 'tx.update', entity: 't1', body: { amount_cents: 2000 }, version: 3, base: { ...tx() }, label: 'Supermaxi' })
    respond = (c) =>
      c.headers['If-Match'] === '3'
        ? json(409, { error: { code: 'version_conflict', message: 'Este registro cambió en otro dispositivo.' }, current: { ...tx(), amount_cents: 1500, version: 5 } })
        : json(200, { version: 6 })
    const r = await flush()
    expect(r.conflicts).toBe(1)
    const [op] = snapshot()
    expect(op.state).toBe('conflict')
    expect(op.current?.amount_cents).toBe(1500)
    // Mientras no se resuelva, no se reintenta.
    expect((await flush()).sent).toBe(0)
    await resolve(op.id, 'mine')
    expect((await flush()).sent).toBe(1)
    expect(calls.at(-1)?.headers['If-Match']).toBe('5')
    expect(snapshot()).toHaveLength(0)
  })

  it('"conservar la del servidor" descarta el cambio local', async () => {
    respond = () => json(409, { error: { code: 'version_conflict', message: 'x' }, current: { ...tx(), version: 5 } })
    await enqueue({ kind: 'tx.delete', entity: 't1', version: 3, label: 'Supermaxi' })
    await flush()
    await resolve(snapshot()[0].id, 'theirs')
    expect(snapshot()).toHaveLength(0)
    expect(await allRecords()).toHaveLength(0)
  })

  it('borrado en otro dispositivo: se puede volver a crear con lo editado', async () => {
    respond = (c) => (c.method === 'PATCH' ? json(404, { error: { code: 'not_found', message: 'No existe.' } }) : json(201, { version: 1 }))
    await enqueue({ kind: 'tx.update', entity: 't1', body: { amount_cents: 2500 }, version: 3, base: { ...tx() }, label: 'Supermaxi' })
    const r = await flush()
    expect(r.conflicts).toBe(1)
    expect(snapshot()[0].reason).toBe('deleted')
    await resolve(snapshot()[0].id, 'recreate')
    await flush()
    const post = calls.find((c) => c.method === 'POST')!
    expect(post.body.amount_cents).toBe(2500)
    expect(post.body.merchant).toBe('Supermaxi')
    expect(post.body.id).not.toBe('t1')
    expect(Object.keys(post.body).sort()).toEqual(['account_id', 'amount_cents', 'category_id', 'description', 'id', 'kind', 'merchant', 'notes', 'occurred_at'])
  })

  it('borrar algo que ya no existe cuenta como hecho; un dato inválido se descarta y se avisa', async () => {
    respond = (c) => (c.method === 'DELETE' ? json(404, { error: { code: 'not_found', message: 'No existe.' } }) : json(400, { error: { code: 'invalid', message: 'Monto inválido.' } }))
    await enqueue({ kind: 'tx.delete', entity: 't1', version: 3, label: 'Uno' })
    await enqueue({ kind: 'tx.create', entity: 'n9', body: { amount_cents: -1 }, label: 'Dos' })
    const r = await flush()
    expect(r.sent).toBe(1)
    expect(r.rejected).toEqual(['Dos: Monto inválido.'])
    expect(snapshot()).toHaveLength(0)
  })

  it('sin red o con el servidor caído se detiene y conserva el orden', async () => {
    respond = () => json(503, { error: { code: 'unavailable', message: 'x' } })
    await enqueue({ kind: 'tx.create', entity: 'a', body: { amount_cents: 1 }, label: 'a' })
    await enqueue({ kind: 'tx.create', entity: 'b', body: { amount_cents: 2 }, label: 'b' })
    await flush()
    expect(calls).toHaveLength(1)
    expect(snapshot().map((o) => o.entity)).toEqual(['a', 'b'])
  })

  it('gastos fijos: crear y luego cambiar el día sin conexión envía un solo alta con la fecha ajustada', async () => {
    respond = offline
    await sendOrQueue({ kind: 'rec.create', entity: 'r1', body: { account_id: 'a1', category_id: 'c1', amount_cents: 5000, merchant: 'Arriendo', start_date: '2026-11-05' }, label: 'Arriendo' })
    await sendOrQueue({ kind: 'rec.update', entity: 'r1', body: { day: 31, amount_cents: 5200 }, label: 'Arriendo' })
    respond = () => json(201, { version: 1 })
    calls = []
    await flush()
    const posts = calls.filter((c) => c.method === 'POST')
    expect(posts).toHaveLength(1)
    expect(posts[0].url).toBe('/api/recurring')
    expect(posts[0].body).toEqual({ account_id: 'a1', category_id: 'c1', amount_cents: 5200, merchant: 'Arriendo', start_date: '2026-11-30', id: 'r1' })
    expect(withDay('2028-02-10', 31)).toBe('2028-02-29')
  })

  it('la cola queda vacía al cerrar sesión y no se mezcla entre personas del mismo navegador', async () => {
    respond = offline
    await enqueue({ kind: 'tx.create', entity: 'x', body: { amount_cents: 1 }, label: 'x' })
    await openVault('beto')
    await loadOutbox()
    expect(snapshot()).toHaveLength(0) // Beto no ve ni envía lo de Ana
    await openVault('ana')
    await loadOutbox()
    expect(snapshot()).toHaveLength(1)
    resetOutbox()
    await wipeVaults()
    await openVault('ana')
    await loadOutbox()
    expect(snapshot()).toHaveLength(0)
    expect(await allRecords()).toHaveLength(0)
  })
})

describe('lectura sin conexión', () => {
  it('guarda Inicio, Movimientos recientes y Plan, y los devuelve sin red', async () => {
    respond = () => json(200, { month: '2026-10', spent_cents: 123 })
    await cachedGet('/api/dashboard?month=2026-10')
    await cachedGet('/api/transactions?month=2026-10&q=uber') // búsquedas no se guardan
    await new Promise((r) => setTimeout(r, 10))
    respond = offline
    expect(await cachedGet<{ spent_cents: number }>('/api/dashboard?month=2026-10')).toEqual({ month: '2026-10', spent_cents: 123 })
    await expect(cachedGet('/api/transactions?month=2026-10&q=uber')).rejects.toMatchObject({ status: 0 })
    await expect(cachedGet('/api/reports/summary')).rejects.toMatchObject({ status: 0 })
  })

  it('muestra en la lista lo pendiente: nuevos, cambios y borrados', () => {
    const page: TxPage = { items: [tx(), tx({ id: 't2', amount_cents: 300, occurred_at: '2026-10-09T15:00:00Z' })], total: 2, sum_cents: 1300, limit: 50, offset: 0 }
    const base = { seq: 1, at: Date.parse('2026-10-12T10:00:00Z'), state: 'pending' as const, label: '' }
    const ops = [
      { ...base, id: 'o1', kind: 'tx.create' as const, entity: 'n1', body: { amount_cents: 450, kind: 'purchase', category_id: 'c1', account_id: 'a1', occurred_at: '2026-10-12T08:30' } },
      { ...base, id: 'o2', kind: 'tx.update' as const, entity: 't1', body: { amount_cents: 2000 } },
      { ...base, id: 'o3', kind: 'tx.delete' as const, entity: 't2' },
      { ...base, id: 'o4', kind: 'tx.create' as const, entity: 'n2', body: { amount_cents: 1, occurred_at: '2026-09-30T23:00' } }, // septiembre
    ]
    const out = overlayTransactions(page, ops, { month: '2026-10' }, 'America/Guayaquil')
    expect(out.items.map((t) => [t.id, t.amount_cents, !!t.pending])).toEqual([
      ['n1', 450, true],
      ['t1', 2000, true],
    ])
    expect(out.items[0].occurred_at).toBe('2026-10-12T13:30:00.000Z')
    expect(out.total).toBe(2)
    expect(out.sum_cents).toBe(2450)
    // Con búsqueda no se inventan resultados.
    expect(overlayTransactions(page, ops, { month: '2026-10', q: 'x' }, 'America/Guayaquil').items.map((t) => t.id)).toEqual(['t1'])
    const rec = overlayRecurring([{ id: 'r1', account_id: 'a', category_id: 'c', amount_cents: 1, merchant: 'Luz', day: 5, next_date: '2026-11-05', version: 2 }],
      [{ ...base, id: 'o5', kind: 'rec.update', entity: 'r1', body: { amount_cents: 9 } }])
    expect(rec[0]).toMatchObject({ amount_cents: 9, pending: true })
  })

  it('convierte la hora local a UTC también con cambio de horario', () => {
    expect(localInputToISO('2026-10-12T08:30', 'America/Guayaquil')).toBe('2026-10-12T13:30:00.000Z')
    expect(localInputToISO('2026-07-01T12:00', 'America/New_York')).toBe('2026-07-01T16:00:00.000Z')
    expect(localInputToISO('2026-11-02T12:00', 'America/New_York')).toBe('2026-11-02T17:00:00.000Z')
  })
})
