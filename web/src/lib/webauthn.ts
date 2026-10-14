import { startAuthentication, startRegistration } from '@simplewebauthn/browser'
import { post } from './api'

export const passkeysSupported = () => typeof window !== 'undefined' && !!window.PublicKeyCredential

export async function registerPasskey(name: string) {
  const options = await post<any>('/api/auth/enroll/passkey/begin')
  const credential = await startRegistration({ optionsJSON: options })
  return post<{ state: string; recovery_codes?: string[] }>('/api/auth/enroll/passkey/finish', { name, credential })
}

export async function passkeyAssertion() {
  const options = await post<any>('/api/auth/mfa/passkey/begin')
  return startAuthentication({ optionsJSON: options })
}

export function passkeyError(e: unknown): string | null {
  if (e instanceof Error && (e.name === 'NotAllowedError' || e.name === 'AbortError')) return 'Se canceló la verificación con passkey.'
  if (e instanceof Error && e.name === 'InvalidStateError') return 'Esta passkey ya está registrada en tu cuenta.'
  return null
}

export function deviceName(): string {
  const ua = navigator.userAgent
  if (/iPhone/.test(ua)) return 'iPhone'
  if (/iPad/.test(ua)) return 'iPad'
  if (/Android/.test(ua)) return 'Android'
  if (/Mac/.test(ua)) return 'Mac'
  if (/Windows/.test(ua)) return 'Windows'
  return 'Este dispositivo'
}
