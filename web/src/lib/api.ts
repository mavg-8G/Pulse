// Cliente HTTP. Toda petición va al mismo origen con la cookie de sesión (HttpOnly):
// el frontend nunca ve ni guarda tokens.

export class ApiError extends Error {
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

async function raw<T>(method: string, path: string, body?: unknown): Promise<T> {
  const init: RequestInit = { method, credentials: 'same-origin', headers: {} }
  if (body !== undefined) {
    init.body = JSON.stringify(body)
    ;(init.headers as Record<string, string>)['Content-Type'] = 'application/json'
  }
  let res: Response
  try {
    res = await fetch(path, init)
  } catch {
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
    throw new ApiError(res.status, e?.code ?? 'error', e?.message ?? 'Ocurrió un error inesperado.')
  }
  return (data ?? text) as T // CSV y otras respuestas que no son JSON llegan como texto
}

export async function api<T>(method: string, path: string, body?: unknown): Promise<T> {
  try {
    return await raw<T>(method, path, body)
  } catch (err) {
    if (err instanceof ApiError) {
      if (err.code === 'step_up_required' && stepUpHandler) {
        if (await stepUpHandler()) return raw<T>(method, path, body)
      }
      if (err.status === 401 && ['unauthorized', 'mfa_required', 'enrollment_required'].includes(err.code)) {
        unauthorizedHandler?.()
      }
    }
    throw err
  }
}

export const get = <T>(path: string) => api<T>('GET', path)
export const post = <T>(path: string, body: unknown = {}) => api<T>('POST', path, body)
export const patch = <T>(path: string, body: unknown) => api<T>('PATCH', path, body)
export const del = <T = void>(path: string) => api<T>('DELETE', path)

export function errorMessage(e: unknown): string {
  if (e instanceof ApiError) return e.message
  if (e instanceof Error) return e.message
  return 'Ocurrió un error inesperado.'
}
