// Cliente HTTP. Toda petición va al mismo origen con la cookie de sesión (HttpOnly):
// el frontend nunca ve ni guarda tokens.

export class ApiError extends Error {
  /** En un conflicto de versión (409 version_conflict), lo que hay ahora en el servidor. */
  current?: unknown
  constructor(public status: number, public code: string, message: string) {
    super(message)
  }
}

type StepUpHandler = () => Promise<boolean>
let stepUpHandler: StepUpHandler | null = null
let unauthorizedHandler: (() => void) | null = null

/** La app registra aquí el diálogo de re-autenticación (passkey/TOTP). */
export function onStepUpRequired(h: StepUpHandler | null) {
  stepUpHandler = h
}

export function onUnauthorized(h: (() => void) | null) {
  unauthorizedHandler = h
}

async function raw<T>(method: string, path: string, body?: unknown, headers: Record<string, string> = {}, signal?: AbortSignal): Promise<T> {
  const init: RequestInit = { method, credentials: 'same-origin', headers: { ...headers }, signal }
  if (body !== undefined) {
    init.body = JSON.stringify(body)
    ;(init.headers as Record<string, string>)['Content-Type'] = 'application/json'
  }
  let res: Response
  try {
    res = await fetch(path, init)
  } catch {
    if (signal?.aborted) throw new DOMException('Consulta cancelada', 'AbortError')
    throw new ApiError(0, 'network', 'Sin conexión. Revisa tu internet e inténtalo de nuevo.')
  }
  if (res.status === 204) return undefined as T
  const text = await res.text()
  let data: any = undefined
  try {
    data = text ? JSON.parse(text) : undefined
  } catch {
    /* respuesta no JSON */
  }
  if (!res.ok) {
    const e = data?.error
    const err = new ApiError(res.status, e?.code ?? 'error', e?.message ?? 'Ocurrió un error inesperado.')
    if (data?.current) err.current = data.current
    throw err
  }
  return (data ?? text) as T // CSV y otras respuestas que no son JSON llegan como texto
}

export async function api<T>(method: string, path: string, body?: unknown, headers?: Record<string, string>, signal?: AbortSignal): Promise<T> {
  try {
    return await raw<T>(method, path, body, headers, signal)
  } catch (err) {
    if (err instanceof ApiError) {
      if (err.code === 'step_up_required' && stepUpHandler) {
        if (await stepUpHandler()) return raw<T>(method, path, body, headers, signal)
        throw new ApiError(403, 'step_up_cancelled', 'Confirmación cancelada. Tus cambios siguen aquí.')
      }
      if (err.status === 401 && ['unauthorized', 'mfa_required', 'enrollment_required'].includes(err.code)) {
        unauthorizedHandler?.()
      }
    }
    throw err
  }
}

/** Descarga binaria (p. ej. la copia cifrada del respaldo) con el mismo manejo de errores y step-up. */
export async function getBlob(path: string, retried = false): Promise<Blob> {
  let res: Response
  try {
    res = await fetch(path, { credentials: 'same-origin' })
  } catch {
    throw new ApiError(0, 'network', 'Sin conexión. Revisa tu internet e inténtalo de nuevo.')
  }
  if (res.ok) return res.blob()
  let e: any
  try {
    e = (await res.json())?.error
  } catch {
    /* sin cuerpo JSON */
  }
  const err = new ApiError(res.status, e?.code ?? 'error', e?.message ?? 'Ocurrió un error inesperado.')
  if (err.code === 'step_up_required' && stepUpHandler && !retried) {
    if (await stepUpHandler()) return getBlob(path, true)
    throw new ApiError(403, 'step_up_cancelled', 'Confirmación cancelada.')
  }
  if (err.status === 401) unauthorizedHandler?.()
  throw err
}

export const get = <T>(path: string, signal?: AbortSignal) => api<T>('GET', path, undefined, undefined, signal)
export const post = <T>(path: string, body: unknown = {}) => api<T>('POST', path, body)
export const patch = <T>(path: string, body: unknown) => api<T>('PATCH', path, body)
export const del = <T = void>(path: string) => api<T>('DELETE', path)

export function errorMessage(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.code === 'step_up_cancelled') return e.message
    if (e.status === 0) return 'Sin conexión. Revisa tu internet e inténtalo de nuevo.'
    if (e.status === 401 && ['unauthorized', 'mfa_required', 'enrollment_required'].includes(e.code)) return 'Tu sesión venció. Vuelve a entrar para continuar.'
    if (e.status === 409) return 'Este registro cambió en otro dispositivo. Revisa la versión actual antes de volver a guardar.'
    if (e.status === 429) return 'Espera un momento antes de volver a intentarlo.'
    if (e.status >= 500) return 'No se pudo completar la operación. Inténtalo de nuevo en unos momentos.'
    return e.message
  }
  return 'No se pudo completar la operación. Inténtalo de nuevo.'
}
