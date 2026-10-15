import { createECDH, randomBytes } from 'node:crypto'
import { apiGet, BASE_URL, expect, register, test } from './helpers'

test('notificaciones: activar, preferencias, prueba y desactivar', async ({ page, context }) => {
  await context.grantPermissions(['notifications'], { origin: BASE_URL })
  const ecdh = createECDH('prime256v1'); ecdh.generateKeys()
  const fake = {
    endpoint: `https://fcm.googleapis.com/push/test-${randomBytes(8).toString('hex')}`,
    keys: { p256dh: ecdh.getPublicKey().toString('base64url'), auth: randomBytes(16).toString('base64url') },
  }
  // El servicio externo se sustituye; el permiso, API, controles y SW son reales.
  await page.addInitScript((value) => {
    let active: PushSubscription | null = null
    Object.defineProperty(PushManager.prototype, 'getSubscription', { value: async () => active })
    Object.defineProperty(PushManager.prototype, 'subscribe', { value: async (options: PushSubscriptionOptionsInit) => {
      active = { endpoint: value.endpoint, options, toJSON: () => value, unsubscribe: async () => { active = null; return true } } as unknown as PushSubscription
      return active
    } })
  }, fake)
  await register(page)
  await page.goto('/ajustes')
  await expect(page.getByRole('heading', { name: 'Notificaciones', exact: true })).toBeVisible()
  await page.getByLabel('Horario silencioso', { exact: true }).check()
  await page.getByLabel('Silencio desde').fill('00:00')
  await page.getByLabel('Silencio hasta').fill('23:59')
  await page.getByRole('button', { name: 'Guardar notificaciones' }).click()
  await expect(page.getByRole('status')).toContainText('Preferencias guardadas')
  await page.getByRole('button', { name: 'Activar notificaciones', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Desactivar notificaciones', exact: true })).toBeVisible()
  const subs = await apiGet(page, '/api/push/subscriptions')
  expect(subs).toHaveLength(1)
  expect(subs[0].current).toBe(true)
  await page.getByLabel('Ocultar montos en las notificaciones').uncheck()
  await page.getByLabel('Resumen semanal').uncheck()
  await page.getByRole('button', { name: 'Guardar notificaciones' }).click()
  await expect.poll(async () => (await apiGet(page, '/api/push/preferences')).hide_amounts).toBe(false)
  expect((await apiGet(page, '/api/push/preferences')).weekly).toBe(false)
  await page.getByRole('button', { name: 'Enviar notificación de prueba' }).click()
  await expect(page.getByRole('status')).toContainText('Aviso en cola')
  await page.getByRole('button', { name: 'Desactivar notificaciones', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Activar notificaciones', exact: true })).toBeVisible()
  expect(await apiGet(page, '/api/push/subscriptions')).toHaveLength(0)
})

test('el service worker recibe push, abre el plan y rechaza otra cuenta', async ({ page, context }) => {
  await context.grantPermissions(['notifications'], { origin: BASE_URL })
  await register(page)
  await page.waitForFunction(async () => (await navigator.serviceWorker.getRegistration())?.active?.state === 'activated')
  await page.goto('/plan')
  await page.waitForFunction(() => !!navigator.serviceWorker.controller)
  const me = await apiGet(page, '/api/me')
  const worker = context.serviceWorkers()[0]
  // El headless shell de Windows no muestra notificaciones del sistema.
  // Se sustituye esa superficie; listener, fetch de identidad y navegación son reales.
  await worker.evaluate(() => {
    const sw = self as unknown as ServiceWorkerGlobalScope & { pulseNotifications: { title: string; options: NotificationOptions }[] }
    sw.pulseNotifications = []
    sw.registration.showNotification = async (title, options = {}) => { sw.pulseNotifications.push({ title, options }) }
    const match = sw.clients.matchAll.bind(sw.clients)
    Object.defineProperty(sw.clients, 'matchAll', { value: async (options: ClientQueryOptions) => (await match(options)).map((c) => {
      const client = c as WindowClient
      return { url: client.url, navigate: (url: string) => client.navigate(url), focus: async () => client }
    }) })
  })
  const deliver = async (userID: string, tag: string) => worker.evaluate(async ({ userID, tag }) => {
    const event = new PushEvent('push', { data: JSON.stringify({ title: 'Aviso de presupuesto', body: 'Alcanzaste el 80 %.', url: '/plan', user_id: userID, tag }) })
    let pending: Promise<unknown> = Promise.resolve()
    // Captura la vida útil de un evento sintético; se ejecuta el listener del SW real.
    Object.defineProperty(event, 'waitUntil', { value: (promise: Promise<unknown>) => { pending = promise } })
    self.dispatchEvent(event)
    await pending
  }, { userID, tag })
  await page.goto('/movimientos')
  await deliver(me.id, 'budget-e2e')
  const notifications = () => worker.evaluate(() => (self as unknown as { pulseNotifications: { options: NotificationOptions }[] }).pulseNotifications.map((n) => ({ body: n.options.body, tag: n.options.tag })))
  expect((await notifications()).map((n) => n.body)).toContain('Alcanzaste el 80 %.')
  await worker.evaluate(async () => {
    const sw = self as unknown as ServiceWorkerGlobalScope & { pulseNotifications: { options: NotificationOptions }[] }
    const n = sw.pulseNotifications.find((n) => n.options.tag === 'budget-e2e')!
    const event = new Event('notificationclick')
    Object.defineProperty(event, 'notification', { value: { data: n.options.data, close: () => { sw.pulseNotifications = [] } } })
    let pending: Promise<unknown> = Promise.resolve()
    Object.defineProperty(event, 'waitUntil', { value: (promise: Promise<unknown>) => { pending = promise } })
    sw.dispatchEvent(event)
    await pending
  })
  await expect(page).toHaveURL(/\/plan$/)
  await expect(page.getByRole('heading', { name: 'Plan de gastos' })).toBeVisible()
  await deliver('another-user', 'wrong-user')
  expect((await notifications()).map((n) => n.tag)).not.toContain('wrong-user')
  // El logout impide mostrar una entrega en vuelo de la sesión anterior.
  await page.goto('/ajustes/cuenta')
  await page.getByRole('button', { name: 'Cerrar sesión', exact: true }).last().click()
  await expect(page.getByRole('heading', { name: 'Entrar' })).toBeVisible()
  await deliver(me.id, 'after-logout')
  expect((await notifications()).map((n) => n.tag)).not.toContain('after-logout')
})
