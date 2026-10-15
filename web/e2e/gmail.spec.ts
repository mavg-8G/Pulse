import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { apiGet, expect, expectAccessible, freshCode, register, test } from './helpers'

const DIR = process.env.PULSE_E2E_DIR || fileURLToPath(new URL('../.e2e/', import.meta.url))
// Las mismas credenciales que acepta el Google falso (fake-google.mjs).
const CLIENT_ID = 'pulse-e2e.apps.googleusercontent.com'
const CLIENT_SECRET = 'secreto-e2e'
const google = () => readFileSync(join(DIR, 'fake-google.url'), 'utf8').trim()

test('conectar Gmail con step-up, sincronizar un consumo y desconectar', async ({ page }) => {
  test.setTimeout(120_000)
  const u = await register(page)
  const gmailAddress = `p${Date.now()}@gmail.com`
  await page.request.get(`${google()}/__login?email=${encodeURIComponent(gmailAddress)}`)

  await page.goto('/ajustes')
  const section = page.getByRole('region', { name: 'Correo del banco (Gmail)' })
  await expect(section.getByText('servicios@tarjetasbancopichincha.com', { exact: false })).toBeVisible()
  await section.getByRole('button', { name: 'Conectar Gmail' }).click()

  // Conectar una fuente de datos bancarios exige confirmar identidad.
  const stepUp = page.getByRole('dialog', { name: 'Confirma que eres tú' })
  await expect(stepUp).toBeVisible()
  await stepUp.getByLabel('Código de tu app de autenticación').fill(await freshCode(u))
  const verified = page.waitForResponse((r) => r.url().endsWith('/api/auth/step-up/totp') && r.request().method() === 'POST')
  const connected = page.waitForResponse((r) => r.url().endsWith('/api/gmail/callback') && r.request().method() === 'POST')
  const connectionStatus = page.waitForResponse((r) => r.url().endsWith('/api/gmail') && r.request().method() === 'GET' && r.ok(), { timeout: 30_000 })
  await stepUp.getByRole('button', { name: 'Verificar código' }).click()
  expect((await verified).ok()).toBeTruthy()
  expect((await connected).ok()).toBeTruthy()

  // Google (falso) acepta y vuelve a /ajustes/gmail; Pulse canjea el código y limpia la URL.
  await expect(page.getByText(`Gmail conectado: ${gmailAddress}`)).toBeVisible()
  await expect(page).toHaveURL(/\/ajustes$/)
  await connectionStatus
  await expect(section.getByText(gmailAddress)).toBeVisible()
  await expect(section.getByText('Conectado', { exact: true })).toBeVisible()
  await expect(section.getByLabel(/Modo sombra/)).toBeChecked()
  await expectAccessible(page, 'Ajustes con Gmail')

  await page.request.get(`${google()}/__deliver?to=${encodeURIComponent(gmailAddress)}&fixture=produbanco-consumo-mes-dia-15`)
  await page.request.get(`${google()}/__deliver?to=${encodeURIComponent(gmailAddress)}&fixture=pichincha-pago`)
  await page.request.get(`${google()}/__deliver?to=${encodeURIComponent(gmailAddress)}&fixture=produbanco-promocion`)
  await page.request.get(`${google()}/__deliver?to=${encodeURIComponent(gmailAddress)}&fixture=diners-pago`)
  await page.request.get(`${google()}/__deliver?to=${encodeURIComponent(gmailAddress)}&fixture=pichincha-plantilla-nueva`)
  await section.getByRole('button', { name: 'Sincronizar ahora' }).click()
  await expect(page.getByText('1 gasto nuevo. Revísalos en Movimientos. 1 correo sigue sin reconocer; consulta los pendientes abajo.')).toBeVisible()
  await expect(section.getByTestId('gmail-sync-info')).toContainText('1 importado')
  await expect(section.getByRole('list', { name: 'Correos no reconocidos' }).getByRole('listitem')).toHaveCount(1)

  // Un formato que sigue siendo desconocido permanece visible al reintentarlo.
  await expect(section.getByRole('status')).toContainText('Hay un correo bancario pendiente de lectura')
  await expect(section.getByRole('list', { name: 'Correos no reconocidos' })).toContainText('Detectado')
  const retried = page.waitForResponse((r) => r.url().includes('/api/gmail/sync?retry_unrecognized=true') && r.request().method() === 'POST')
  await section.getByRole('button', { name: 'Reintentar correos pendientes' }).click()
  expect((await retried).ok()).toBeTruthy()
  await expect(page.getByText('Reintento terminado. No hay gastos nuevos. 1 correo sigue sin reconocer; consulta los pendientes abajo.')).toBeVisible()
  await expect(section.getByTestId('gmail-sync-info')).toContainText('1 importado')
  await expectAccessible(page, 'Ajustes con correo pendiente')

  const pending = await apiGet(page, '/api/transactions?status=pending_review&month=2026-10')
  expect(pending.total).toBe(1)
  expect(pending.items[0].source).toBe('gmail')
  expect(pending.items[0].amount_cents).toBe(197)
  await page.goto('/movimientos?month=2026-10')
  await expect(page.getByText('Viaje de Prueba')).toBeVisible()
  await expect(page.getByText('Por revisar')).toBeVisible()

  // Desconectar revoca el acceso en Google y conserva los gastos.
  await page.goto('/ajustes')
  await section.getByRole('button', { name: 'Desconectar' }).click()
  await page.getByRole('alertdialog', { name: '¿Desconectar Gmail?' }).getByRole('button', { name: 'Desconectar Gmail' }).click()
  await expect(page.getByText('Gmail desconectado')).toBeVisible()
  await expect(section.getByRole('button', { name: 'Conectar Gmail' })).toBeVisible()
  const revoked = await (await page.request.get(`${google()}/__revoked`)).json()
  expect(revoked.some((t: string) => t.startsWith('rt-'))).toBeTruthy()
  expect((await apiGet(page, '/api/transactions?month=2026-10')).total).toBe(1)
  await expect(page.getByText('Gmail desconectado').first()).toBeVisible()
})

test('si se cancela el permiso en Google, Pulse lo explica', async ({ page }) => {
  await register(page)
  await page.goto('/ajustes/gmail?error=access_denied&state=x')
  await expect(page.getByRole('alert')).toHaveText('Cancelaste el permiso en Google. Gmail no se conectó.')
  await expect(page).toHaveURL(/\/ajustes\/gmail$/)
  await page.getByRole('link', { name: 'Volver a Ajustes' }).click()
  await expect(page.getByRole('heading', { name: 'Ajustes' })).toBeVisible()
})

test('un admin configura las credenciales de Google desde Ajustes y se aplican sin reiniciar', async ({ page }) => {
  test.setTimeout(120_000)
  const u = await register(page, undefined, 'admin')
  await page.goto('/ajustes')
  const section = page.getByRole('region', { name: 'Correo del banco (Gmail)' })
  await section.getByText('Configuración del servidor (admin)').click()
  const form = section.getByRole('form', { name: 'Credenciales de Google' })
  await expect(form.getByText('variables de entorno', { exact: false })).toBeVisible()
  await expect(form.getByTestId('gmail-redirect-uri')).toHaveText(/\/ajustes\/gmail$/)

  await form.getByLabel('ID de cliente').fill(CLIENT_ID)
  await form.getByLabel('Secreto del cliente').fill(CLIENT_SECRET)
  await form.getByRole('button', { name: 'Guardar credenciales' }).click()
  const stepUp = page.getByRole('dialog', { name: 'Confirma que eres tú' })
  await stepUp.getByLabel('Código de tu app de autenticación').fill(await freshCode(u))
  await stepUp.getByRole('button', { name: 'Verificar código' }).click()
  await expect(page.getByText('Credenciales de Google guardadas')).toBeVisible()
  await expect(form.getByText('guardadas aquí, cifradas', { exact: false })).toBeVisible()
  await expect(form.getByLabel('Secreto del cliente')).toHaveValue('')
  await expectAccessible(page, 'Ajustes de Gmail del servidor')

  // Ya con las credenciales de Ajustes, conectar Gmail funciona (step-up todavía vigente).
  const gmailAddress = `a${Date.now()}@gmail.com`
  await page.request.get(`${google()}/__login?email=${encodeURIComponent(gmailAddress)}`)
  const connected = page.waitForResponse((r) => r.url().endsWith('/api/gmail/callback') && r.request().method() === 'POST')
  await section.getByRole('button', { name: 'Conectar Gmail' }).click()
  expect((await connected).ok()).toBeTruthy()
  await expect(page.getByText(`Gmail conectado: ${gmailAddress}`)).toBeVisible()

  // Borrar vuelve a la configuración por variables de entorno (al volver de Google el bloque está plegado).
  await section.getByText('Configuración del servidor (admin)').click()
  await section.getByRole('button', { name: 'Borrar', exact: true }).click()
  await page.getByRole('alertdialog', { name: '¿Borrar las credenciales de Google?' }).getByRole('button', { name: 'Borrar credenciales' }).click()
  await expect(page.getByText('Credenciales borradas')).toBeVisible()
  await expect(form.getByText('variables de entorno', { exact: false })).toBeVisible()
})

test('una persona sin rol admin no ve la configuración del servidor', async ({ page }) => {
  await register(page)
  await page.goto('/ajustes')
  await expect(page.getByRole('region', { name: 'Correo del banco (Gmail)' })).toBeVisible()
  await expect(page.getByText('Configuración del servidor (admin)')).toHaveCount(0)
})
