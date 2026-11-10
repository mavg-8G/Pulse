// Almacén local cifrado para el uso sin conexión (fase 10).
//
// Cada persona tiene su propia base de IndexedDB ("pulse-u-<id>") con su propia clave AES-GCM de 256 bits,
// generada en el navegador como NO extraíble: el código de la página puede usarla pero no leerla, y no sale
// del dispositivo. Todo lo que se guarda (lo último que se vio de Inicio, Movimientos y Plan, y los cambios
// pendientes de enviar) va cifrado y autenticado con datos asociados que lo atan a esa persona y a esa entrada,
// así que una entrada copiada a otra base o a otra clave no se descifra. Al cerrar sesión se borra todo.
//
// La clave se obtiene del servidor tras autenticar y permanece solo en memoria.
// Una recarga sin conexión no desbloquea datos; los cambios pendientes se conservan cifrados.

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
  const r = indexedDB.open(PREFIX + uid, 2)
  r.onupgradeneeded = () => {
    const db = r.result
    if (!db.objectStoreNames.contains('meta')) db.createObjectStore('meta')
    if (!db.objectStoreNames.contains('cache')) db.createObjectStore('cache', { keyPath: 'k' })
    if (!db.objectStoreNames.contains('ops')) db.createObjectStore('ops', { keyPath: 'k' })
  }
  const db = await req(r)
  db.onversionchange = () => {
    db.close() // otra pestaña está borrando la base (cierre de sesión): se suelta
    if (vault?.db === db) {
      closeVault()
      window.dispatchEvent(new Event('pulse-vault-lock'))
    }
  }
  return db
}

let generation = 0

/** Solo llamar con la clave entregada por una sesión completa del servidor. */
export async function openVault(uid: string, secret?: string): Promise<boolean> {
  if (!supported() || !ID_RE.test(uid)) return false
  if (vault?.uid === uid) return true
  if (!secret || !/^[A-Za-z0-9_-]{43}$/.test(secret)) return false
  closeVault()
  const epoch = generation
  const pending = (async (): Promise<Vault | null> => {
    const raw = Uint8Array.from(atob(secret.replace(/-/g, '+').replace(/_/g, '/') + '='), (c) => c.charCodeAt(0))
    const key = await crypto.subtle.importKey('raw', raw, 'AES-GCM', false, ['encrypt', 'decrypt'])
    raw.fill(0)
    const db = await openDB(uid)
    try {
      const v = { uid, db, key }
      // Migrar el formato antiguo sin borrar caché, preferencias ni operaciones pendientes.
      const read = db.transaction(['meta', 'cache', 'ops'])
      const complete = done(read)
      const [format, legacy, metaKeys, meta, cache, ops] = await Promise.all([
        req(read.objectStore('meta').get('format')),
        req(read.objectStore('meta').get('key')) as Promise<CryptoKey | undefined>,
        req(read.objectStore('meta').getAllKeys()), req(read.objectStore('meta').getAll()),
        req(read.objectStore('cache').getAll()) as Promise<Sealed[]>,
        req(read.objectStore('ops').getAll()) as Promise<Sealed[]>,
      ])
      await complete
      if (format === 'server-v2') {
        const check = meta[metaKeys.indexOf('keycheck')] as Sealed | undefined
        if (!check || await open<string>(v, 'meta', check) !== 'pulse-v2') throw new Error('Clave local incorrecta.')
      }
      if (format !== 'server-v2') {
        const writes: { store: string; key?: IDBValidKey; sealed: Sealed }[] = []
        const migrate = async (store: string, old: Sealed, metaKey?: IDBValidKey) => {
          if (!legacy) throw new Error('No se puede recuperar la clave local anterior.')
          const value = await open<unknown>({ uid, db, key: legacy }, store, old)
          if (value === null) throw new Error('No se pueden descifrar los cambios locales anteriores.')
          writes.push({ store, key: metaKey, sealed: await seal(v, store, old.k, value) })
        }
        for (const old of cache) await migrate('cache', old)
        for (const old of ops) await migrate('ops', old)
        for (let i = 0; i < metaKeys.length; i++) {
          if (typeof metaKeys[i] === 'string' && String(metaKeys[i]).startsWith('preference:')) await migrate('meta', meta[i] as Sealed, metaKeys[i])
        }
        const check = await seal(v, 'meta', 'keycheck', 'pulse-v2')
        const tx = db.transaction(['meta', 'cache', 'ops'], 'readwrite')
        const finished = done(tx)
        // Otra pestaña pudo completar la misma migración mientras hacíamos el cifrado.
        if (await req(tx.objectStore('meta').get('format')) !== 'server-v2') {
          for (const w of writes) {
            if (w.store === 'meta') tx.objectStore('meta').put(w.sealed, w.key)
            else tx.objectStore(w.store).put(w.sealed)
          }
          tx.objectStore('meta').put('server-v2', 'format')
          tx.objectStore('meta').put(check, 'keycheck')
          tx.objectStore('meta').delete('key')
        }
        await finished
      }
      if (epoch !== generation) { db.close(); return null }
      return v
    } catch (e) { db.close(); throw e }
  })()
  opening = pending
  try {
    const v = await pending
    if (epoch !== generation) { v?.db.close(); return false }
    vault = v
    return !!v
  } catch {
    if (epoch === generation) vault = null
    return false
  } finally {
    if (opening === pending) opening = null
  }
}

export function closeVault() {
  generation++
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
    for (const d of await indexedDB.databases()) if (d.name?.startsWith(PREFIX)) names.push(d.name)
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
