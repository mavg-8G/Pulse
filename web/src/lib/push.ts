import { del, get, post } from './api'
import { deviceName } from './webauthn'

export interface NotificationPreferences {
  budgets: boolean; security: boolean; weekly: boolean; monthly: boolean
  hide_amounts: boolean; quiet_enabled: boolean; quiet_start: number; quiet_end: number
}
export interface PushDevice { id: string; device_label: string; created_at: string; current: boolean }
export const pushSupported = () => window.isSecureContext && 'serviceWorker' in navigator && 'PushManager' in window && 'Notification' in window

export async function enablePush() {
  if (!pushSupported()) throw new Error('Este navegador no admite notificaciones. En iPhone, instala Pulse en la pantalla de inicio primero.')
  // El permiso se pide desde el clic, antes de cualquier llamada a la API.
  const permission = await Notification.requestPermission()
  if (permission !== 'granted') throw new Error('Permite las notificaciones en los ajustes del navegador para activarlas.')
  const config = await get<{ enabled: boolean; public_key: string }>('/api/push/vapid-key')
  if (!config.enabled) throw new Error('Las notificaciones todavía no están configuradas en el servidor.')
  const registration = await Promise.race([
    navigator.serviceWorker.ready,
    new Promise<never>((_, reject) => setTimeout(() => reject(new Error('Abre la versión instalada o compilada de Pulse para activar las notificaciones.')), 10000)),
  ])
  const raw = atob(config.public_key.replace(/-/g, '+').replace(/_/g, '/'))
  const key = Uint8Array.from(raw, (c) => c.charCodeAt(0))
  let subscription = await registration.pushManager.getSubscription()
  if (subscription && subscription.options.applicationServerKey &&
    Array.from(new Uint8Array(subscription.options.applicationServerKey)).join(',') !== Array.from(key).join(',')) {
    await subscription.unsubscribe(); subscription = null
  }
  const created = !subscription
  subscription ??= await registration.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: key })
  try {
    await post('/api/push/subscribe', { subscription: { endpoint: subscription.endpoint, keys: subscription.toJSON().keys }, device_label: deviceName() })
  } catch (e) { if (created) await subscription.unsubscribe(); throw e }
}

export async function disablePush() {
  await del('/api/push/subscribe')
  await clearBrowserPush()
}

export async function clearBrowserPush() {
  if (!('serviceWorker' in navigator)) return
  for (const registration of await navigator.serviceWorker.getRegistrations()) {
    const subscription = await registration.pushManager?.getSubscription()
    if (subscription) await subscription.unsubscribe()
    for (const notification of await registration.getNotifications()) notification.close()
  }
}
