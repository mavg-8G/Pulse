// Gastos creados sin conexión: se guardan en IndexedDB solo hasta enviarlos. Al cerrar sesión,
// wipeLocalData() borra todas las bases de IndexedDB, incluida esta.
import { ApiError, post } from './api'

const DB = 'pulse-outbox'
const STORE = 'tx'
type Pending = { id: string } & Record<string, unknown>

function open(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open(DB, 1)
    req.onupgradeneeded = () => req.result.createObjectStore(STORE, { keyPath: 'id' })
    req.onsuccess = () => resolve(req.result)
    req.onerror = () => reject(req.error)
  })
}

async function run<T>(mode: IDBTransactionMode, fn: (s: IDBObjectStore) => IDBRequest<T>): Promise<T> {
  const db = await open()
  try {
    return await new Promise<T>((resolve, reject) => {
      const req = fn(db.transaction(STORE, mode).objectStore(STORE))
      req.onsuccess = () => resolve(req.result)
      req.onerror = () => reject(req.error)
    })
  } finally {
    db.close()
  }
}

export const enqueue = (tx: Pending) => run('readwrite', (s) => s.put(tx))

/** Envía lo pendiente. El id lo generó el cliente: reenviar un gasto que sí llegó no lo duplica. */
export async function flush(): Promise<{ sent: number; rejected: number }> {
  const out = { sent: 0, rejected: 0 }
  if (!('indexedDB' in window)) return out
  const pending = await run<Pending[]>('readonly', (s) => s.getAll() as IDBRequest<Pending[]>)
  for (const tx of pending) {
    try {
      await post('/api/transactions', tx)
      out.sent++
    } catch (e) {
      // Sin red, sesión caída o error del servidor: se reintenta luego. Un 4xx no se arreglará reintentando.
      if (!(e instanceof ApiError) || e.status === 0 || e.status === 401 || e.status >= 500) break
      out.rejected++
    }
    await run('readwrite', (s) => s.delete(tx.id))
  }
  return out
}
