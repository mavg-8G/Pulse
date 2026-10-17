// Almacén local cifrado para el uso sin conexión (fase 10).
//
// Cada persona tiene su propia base de IndexedDB ("pulse-u-<id>") con su propia clave AES-GCM de 256 bits,
// generada en el navegador como NO extraíble: el código de la página puede usarla pero no leerla, y no sale
// del dispositivo. Todo lo que se guarda (lo último que se vio de Inicio, Movimientos y Plan, y los cambios
// pendientes de enviar) va cifrado y autenticado con datos asociados que lo atan a esa persona y a esa entrada,
// así que una entrada copiada a otra base o a otra clave no se descifra. Al cerrar sesión se borra todo.
//
// Sin conexión, la app vuelve a abrir el almacén de la última persona que entró, solo durante OFFLINE_DAYS.

const LAST_USER = 'pulse-last-user'
const PREFIX = 'pulse-u-'
export const OFFLINE_DAYS = 7
const MAX_CACHE = 40
const ID_RE = /^[A-Za-z0-9_-]{1,64}$/

type Sealed = { k: string; iv: Uint8Array<ArrayBuffer>; data: ArrayBuffer; at: number }

interface Vault {
  uid: string
  db: IDBDatabase
  key: CryptoKey
}

let vault: Vault | null = null
let opening: Promise<Vault | null> | null = null

function req<T>(r: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    r.onsuccess = () => resolve(r.result)
    r.onerror = () => reject(r.error)
  })
}

function done(tx: IDBTransaction): Promise<void> {
  return new Promise((resolve, reject) => {
    tx.oncomplete = () => resolve()
    tx.onerror = () => reject(tx.error)
    tx.onabort = () => reject(tx.error)
  })
}

export function supported(): boolean {
  return typeof indexedDB !== 'undefined' && typeof crypto !== 'undefined' && !!crypto.subtle
}

/** Recuerda quién entró por última vez (solo su id opaco) para poder abrir su almacén sin conexión. */
export function rememberUser(uid: string) {
  if (!ID_RE.test(uid)) return
  try {
    localStorage.setItem(LAST_USER, JSON.stringify({ uid, at: Date.now() }))
  } catch {
    /* almacenamiento bloqueado: sin modo sin conexión */
  }
}

export function lastUser(now = Date.now()): string | null {
  try {
    const v = JSON.parse(localStorage.getItem(LAST_USER) ?? 'null')
    if (v && typeof v.uid === 'string' && ID_RE.test(v.uid) && typeof v.at === 'number' && now - v.at < OFFLINE_DAYS * 864e5 && v.at <= now + 60e3) return v.uid
  } catch {
    /* valor dañado */
  }
  return null
}

export function currentUser(): string | null {
  return vault?.uid ?? null
}

async function openDB(uid: string): Promise<IDBDatabase> {
  const r = indexedDB.open(PREFIX + uid, 1)
  r.onupgradeneeded = () => {
    const db = r.result
    db.createObjectStore('meta')
    db.createObjectStore('cache', { keyPath: 'k' })
    db.createObjectStore('ops', { keyPath: 'k' })
  }
  const db = await req(r)
  db.onversionchange = () => {
    db.close() // otra pestaña está borrando la base (cierre de sesión): se suelta
    if (vault?.db === db) vault = null
  }
  return db
}

/** Abre (o crea) el almacén de esta persona. Cierra el de cualquier otra. */
export async function openVault(uid: string): Promise<boolean> {
  if (!supported() || !ID_RE.test(uid)) return false
  if (vault?.uid === uid) return true
  closeVault()
  opening = (async () => {
    const db = await openDB(uid)
    const tx = db.transaction('meta', 'readwrite')
    let key = (await req(tx.objectStore('meta').get('key'))) as CryptoKey | undefined
    await done(tx).catch(() => {})
    if (!key) {
      key = await crypto.subtle.generateKey({ name: 'AES-GCM', length: 256 }, false, ['encrypt', 'decrypt'])
      const w = db.transaction('meta', 'readwrite')
      w.objectStore('meta').put(key, 'key')
      await done(w)
    }
    return { uid, db, key }
  })()
  try {
    vault = await opening
    return true
  } catch {
    vault = null
    return false
  } finally {
    opening = null
  }
}

export function closeVault() {
  vault?.db.close()
  vault = null
}

async function ready(): Promise<Vault | null> {
  if (opening) await opening.catch(() => null)
  return vault
}

const enc = new TextEncoder()
const dec = new TextDecoder()
const aad = (v: Vault, store: string, k: string) => enc.encode(`pulse|${v.uid}|${store}|${k}`)

async function seal(v: Vault, store: string, k: string, value: unknown): Promise<Sealed> {
  const iv = crypto.getRandomValues(new Uint8Array(12))
  const data = await crypto.subtle.encrypt({ name: 'AES-GCM', iv, additionalData: aad(v, store, k) }, v.key, enc.encode(JSON.stringify(value)))
  return { k, iv, data, at: Date.now() }
}

async function open<T>(v: Vault, store: string, s: Sealed): Promise<T | null> {
  try {
    const plain = await crypto.subtle.decrypt({ name: 'AES-GCM', iv: s.iv, additionalData: aad(v, store, s.k) }, v.key, s.data)
    return JSON.parse(dec.decode(plain)) as T
  } catch {
    return null // alterado o de otra persona: se ignora
  }
}

/** Guarda la última respuesta de una consulta para mostrarla sin conexión. */
export async function putCache(k: string, value: unknown): Promise<void> {
  const v = await ready()
  if (!v) return
  const s = await seal(v, 'cache', k, value)
  const tx = v.db.transaction('cache', 'readwrite')
  const store = tx.objectStore('cache')
  store.put(s)
  const all = (await req(store.getAll())) as Sealed[]
  if (all.length > MAX_CACHE) {
    all.sort((a, b) => a.at - b.at).slice(0, all.length - MAX_CACHE).forEach((old) => store.delete(old.k))
  }
  await done(tx)
}

export async function getCache<T>(k: string): Promise<{ value: T; at: number } | null> {
  const v = await ready()
  if (!v) return null
  const s = (await req(v.db.transaction('cache').objectStore('cache').get(k))) as Sealed | undefined
  if (!s) return null
  const value = await open<T>(v, 'cache', s)
  return value === null ? null : { value, at: s.at }
}

/** Preferencias cifradas separadas de la caché rotativa; no se confunden con operaciones pendientes. */
export async function putPreference(k: string, value: unknown): Promise<void> {
  const v = await ready()
  if (!v) return
  const key = `preference:${k}`
  const sealed = await seal(v, 'meta', key, value)
  const tx = v.db.transaction('meta', 'readwrite')
  tx.objectStore('meta').put(sealed, key)
  await done(tx)
}

export async function getPreference<T>(k: string): Promise<T | null> {
  const v = await ready()
  if (!v) return null
  const sealed = await req(v.db.transaction('meta').objectStore('meta').get(`preference:${k}`)) as Sealed | undefined
  return sealed ? open<T>(v, 'meta', sealed) : null
}

export async function putRecord(k: string, value: unknown): Promise<void> {
  const v = await ready()
  if (!v) throw new Error('No hay un almacén local abierto.')
  const s = await seal(v, 'ops', k, value)
  const tx = v.db.transaction('ops', 'readwrite')
  tx.objectStore('ops').put(s)
  await done(tx)
}

export async function deleteRecord(k: string): Promise<void> {
  const v = await ready()
  if (!v) return
  const tx = v.db.transaction('ops', 'readwrite')
  tx.objectStore('ops').delete(k)
  await done(tx)
}

export async function allRecords<T>(): Promise<T[]> {
  const v = await ready()
  if (!v) return []
  const rows = (await req(v.db.transaction('ops').objectStore('ops').getAll())) as Sealed[]
  const out: T[] = []
  for (const r of rows) {
    const value = await open<T>(v, 'ops', r)
    if (value !== null) out.push(value)
  }
  return out
}

/** Borra todos los almacenes locales de Pulse y el recuerdo de quién entró. */
export async function wipeVaults(): Promise<void> {
  closeVault()
  try {
    localStorage.removeItem(LAST_USER)
  } catch {
    /* nada */
  }
  if (typeof indexedDB === 'undefined') return
  const names: string[] = []
  if ('databases' in indexedDB) {
    for (const d of await indexedDB.databases()) if (d.name) names.push(d.name)
  }
  await Promise.all(
    names.map(
      (n) =>
        new Promise<void>((resolve) => {
          const r = indexedDB.deleteDatabase(n)
          r.onsuccess = r.onerror = r.onblocked = () => resolve()
        }),
    ),
  )
}
