// Cola de cambios hechos sin conexión (fase 10). Vive cifrada en el almacén local de cada persona (vault.ts) y se
// borra al cerrar sesión. Cada cambio lleva su propio id (Idempotency-Key): reenviarlo tras perder la respuesta no
// lo aplica dos veces. Las ediciones y borrados llevan la versión que se vio (If-Match): si el registro cambió en
// otro dispositivo, el servidor responde 409 con lo que hay ahora y aquí se pide elegir.
import { ApiError, api } from './api'
import { allRecords, deleteRecord, putRecord } from './vault'

export type OpKind = 'tx.create' | 'tx.update' | 'tx.delete' | 'rec.create' | 'rec.update' | 'rec.delete'

export interface Op {
  id: string // Idempotency-Key
  seq: number
  kind: OpKind
  entity: string // id del gasto o del gasto fijo
  body?: Record<string, unknown>
  version?: number // la que se vio al editar o borrar
  base?: Record<string, unknown> // el registro tal como se vio: para mostrarlo y para recrearlo si lo borraron
  label: string // texto para la persona ("Supermaxi $17,68")
  at: number
  state: 'pending' | 'conflict'
  reason?: 'changed' | 'deleted'
  current?: Record<string, unknown> | null
}

export interface FlushResult {
  sent: number
  conflicts: number
  rejected: string[] // mensajes de cambios que el servidor rechazó y se descartaron
}

// Campos que acepta el servidor al crear: una edición que se funde con una creación pendiente no puede llevar otros.
const CREATE_FIELDS: Record<string, string[]> = {
  tx: ['account_id', 'category_id', 'amount_cents', 'occurred_at', 'kind', 'merchant', 'description', 'notes'],
  rec: ['account_id', 'category_id', 'amount_cents', 'merchant', 'start_date'],
}

let ops: Op[] = []
let loaded = false
let listeners = new Set<() => void>()
let flushing: Promise<FlushResult> | null = null
let again: Promise<FlushResult> | null = null

const domain = (k: OpKind) => k.split('.')[0] as 'tx' | 'rec'
const verb = (k: OpKind) => k.split('.')[1] as 'create' | 'update' | 'delete'

function emit() {
  ops = [...ops]
  listeners.forEach((l) => l())
}

export function subscribe(l: () => void) {
  listeners.add(l)
  return () => listeners.delete(l)
}

export const snapshot = () => ops

// Cambios ya enviados o descartados en esta sesión: una lectura del almacén que empezó antes de borrarlos no los revive.
let gone = new Set<string>()

async function readQueue() {
  ops = (await allRecords<Op>()).filter((o) => !gone.has(o.id)).sort((a, b) => a.seq - b.seq)
  loaded = true
  emit()
}

/** Carga la cola de la persona cuyo almacén está abierto. Espera a que termine un envío en curso para no pisarlo. */
export async function loadOutbox() {
  while (flushing || again) await (again ?? flushing)?.catch(() => undefined)
  await readQueue()
}

/** Al cerrar sesión: la cola en memoria también se vacía (el almacén lo borra wipeVaults). */
export function resetOutbox() {
  ops = []
  loaded = false
  gone = new Set()
  emit()
}

const save = (op: Op) => putRecord(op.id, op)
const drop = async (op: Op) => {
  gone.add(op.id)
  ops = ops.filter((o) => o.id !== op.id)
  await deleteRecord(op.id)
}

function pick(body: Record<string, unknown>, fields: string[]) {
  return Object.fromEntries(Object.entries(body).filter(([k]) => fields.includes(k)))
}

/** Cambia el día de una fecha AAAA-MM-DD sin salirse del mes (31 → 30 en abril). */
export function withDay(date: string, day: number): string {
  const [y, m] = date.split('-').map(Number)
  const last = new Date(Date.UTC(y, m, 0)).getUTCDate()
  return `${date.slice(0, 8)}${String(Math.min(Math.max(1, day), last)).padStart(2, '0')}`
}

/** Para crear: los campos que acepta el servidor; en un gasto fijo, el día va dentro de start_date. */
function createBody(d: 'tx' | 'rec', body: Record<string, unknown>): Record<string, unknown> {
  const out = pick(body, CREATE_FIELDS[d])
  if (d === 'rec') {
    const start = typeof out.start_date === 'string' ? out.start_date : typeof body.next_date === 'string' ? body.next_date : ''
    if (start) out.start_date = typeof body.day === 'number' ? withDay(start, body.day) : start
  }
  return out
}

/**
 * Agrega un cambio y lo combina con lo pendiente del mismo registro: crear+editar es un solo crear,
 * crear+borrar se anula, editar+editar es una sola edición con la versión original, editar+borrar es borrar.
 */
export async function enqueue(input: Omit<Op, 'id' | 'seq' | 'at' | 'state'>, requestId = crypto.randomUUID()): Promise<void> {
  if (!loaded) await loadOutbox()
  const now = Date.now()
  const d = domain(input.kind)
  const prev = ops.find((o) => o.entity === input.entity && domain(o.kind) === d && o.state === 'pending')
  if (prev && verb(input.kind) !== 'create') {
    const pv = verb(prev.kind)
    if (pv === 'create' && verb(input.kind) === 'update') {
      prev.body = createBody(d, { ...prev.body, ...input.body })
      prev.label = input.label
      prev.at = now
      await save(prev)
      return emit()
    }
    if (pv === 'create' && verb(input.kind) === 'delete') {
      await drop(prev)
      return emit()
    }
    if (pv === 'update' && verb(input.kind) === 'update') {
      prev.body = { ...prev.body, ...input.body }
      prev.label = input.label
      prev.at = now
      await save(prev)
      return emit()
    }
    if (pv === 'update' && verb(input.kind) === 'delete') {
      Object.assign(prev, { kind: input.kind, body: undefined, label: input.label, at: now })
      await save(prev)
      return emit()
    }
  }
  const op: Op = { ...input, id: requestId, seq: Math.max(now, (ops.at(-1)?.seq ?? 0) + 1), at: now, state: 'pending' }
  await save(op)
  ops.push(op)
  emit()
}

function request(op: Op) {
  const headers: Record<string, string> = { 'Idempotency-Key': op.id }
  if (op.version) headers['If-Match'] = String(op.version)
  const base = domain(op.kind) === 'tx' ? '/api/transactions' : '/api/recurring'
  switch (verb(op.kind)) {
    case 'create':
      return api<{ version?: number }>('POST', base, { ...op.body, id: op.entity }, headers)
    case 'update':
      return api<{ version?: number }>('PATCH', `${base}/${op.entity}`, op.body, headers)
    default:
      return api<undefined>('DELETE', `${base}/${op.entity}`, undefined, headers)
  }
}

/** Envía lo pendiente en orden. Se detiene sin red, con la sesión caída o si el servidor falla, para reintentar luego. */
export function flush(): Promise<FlushResult> {
  if (flushing) {
    // Ya hay un envío en curso: se encadena otro (uno solo) para lo que llegó mientras tanto.
    if (!again) {
      again = flushing.then(
        () => {
          again = null
          return flush()
        },
        () => {
          again = null
          return flush()
        },
      )
    }
    return again
  }
  flushing = run().finally(() => {
    flushing = null
  })
  return flushing
}

async function run(): Promise<FlushResult> {
  const out: FlushResult = { sent: 0, conflicts: 0, rejected: [] }
  if (!loaded) await readQueue()
  const blocked = new Set(ops.filter((o) => o.state === 'conflict').map((o) => o.entity))
  for (const op of [...ops]) {
    if (op.state !== 'pending' || blocked.has(op.entity)) continue
    try {
      const res = await request(op)
      await drop(op)
      out.sent++
      // Si quedaba otro cambio del mismo registro, ahora espera la versión nueva.
      for (const next of ops) if (next.entity === op.entity && res?.version) next.version = res.version
    } catch (e) {
      if (!(e instanceof ApiError) || e.status === 0 || e.status === 401 || e.status === 429 || e.status >= 500) break
      if (e.status === 409 && e.code === 'version_conflict') {
        Object.assign(op, { state: 'conflict', reason: 'changed', current: (e.current as Record<string, unknown>) ?? null })
        await save(op)
        blocked.add(op.entity)
        out.conflicts++
      } else if (e.status === 404 && verb(op.kind) === 'delete') {
        await drop(op) // ya no existe: lo que se quería
        out.sent++
      } else if (e.status === 404 && verb(op.kind) === 'update') {
        Object.assign(op, { state: 'conflict', reason: 'deleted', current: null })
        await save(op)
        blocked.add(op.entity)
        out.conflicts++
      } else if (e.status === 403 && e.code === 'step_up_required') {
        break
      } else {
        await drop(op) // el servidor no lo aceptará nunca (datos inválidos): se avisa y se descarta
        out.rejected.push(`${op.label}: ${e.message}`)
      }
    }
  }
  emit()
  return out
}

/** Resolver un conflicto: "mine" reintenta sobre la versión actual del servidor, "theirs" descarta el cambio local,
 *  "recreate" (si el registro se borró en otro dispositivo) lo vuelve a crear con lo que se había editado. */
export async function resolve(opId: string, choice: 'mine' | 'theirs' | 'recreate'): Promise<void> {
  const op = ops.find((o) => o.id === opId)
  if (!op || op.state !== 'conflict') return
  if (choice === 'theirs') {
    await drop(op)
    return emit()
  }
  if (choice === 'recreate') {
    const d = domain(op.kind)
    if (op.reason !== 'deleted' || verb(op.kind) !== 'update') return
    await drop(op)
    const body = createBody(d, { ...op.base, ...op.body })
    const created: Op = { ...op, id: crypto.randomUUID(), kind: `${d}.create` as OpKind, entity: crypto.randomUUID(), body, version: undefined,
      state: 'pending', reason: undefined, current: undefined, seq: Math.max(Date.now(), (ops.at(-1)?.seq ?? 0) + 1) }
    await save(created)
    ops.push(created)
    emit()
    return
  }
  if (op.reason === 'deleted') return
  const v = Number(op.current?.version)
  Object.assign(op, { state: 'pending', reason: undefined, version: v > 0 ? v : undefined, current: undefined })
  await save(op)
  emit()
}

export const pendingCount = (list: Op[]) => list.filter((o) => o.state === 'pending').length
export const conflicts = (list: Op[]) => list.filter((o) => o.state === 'conflict')

// Solo para pruebas: deja el módulo como recién cargado.
export function __reset() {
  ops = []
  loaded = false
  listeners = new Set()
  flushing = null
  again = null
  gone = new Set()
}

/**
 * Guarda un cambio: lo envía ya si hay conexión y no queda nada pendiente de ese registro; si no hay red (o el registro
 * tiene cambios en cola, para respetar el orden), lo deja en la cola cifrada. Un conflicto en línea se lanza como error.
 */
export async function sendOrQueue(input: Omit<Op, 'id' | 'seq' | 'at' | 'state'>, requestId = crypto.randomUUID()): Promise<'sent' | 'queued'> {
  if (!loaded) await loadOutbox().catch(() => {})
  const waiting = ops.some((o) => o.entity === input.entity && domain(o.kind) === domain(input.kind))
  if (!waiting) {
    try {
      await request({ ...input, id: requestId, seq: 0, at: Date.now(), state: 'pending' })
      return 'sent'
    } catch (e) {
      if (!(e instanceof ApiError) || e.status !== 0) throw e
    }
  }
  // Tras perder la respuesta, el reenvío debe llevar la MISMA clave que llegó al servidor.
  await enqueue(input, requestId)
  flush().catch(() => {})
  return 'queued'
}
