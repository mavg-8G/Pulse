import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { apiGet, expect, expectAccessible, freshCode, register, test } from './helpers'

const DIR = process.env.PULSE_E2E_DIR || fileURLToPath(new URL('../.e2e/', import.meta.url))
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
  await stepUp.getByRole('button', { name: 'Verificar código' }).click()

  // Google (falso) acepta y vuelve a /ajustes/gmail; Pulse canjea el código y limpia la URL.
  await expect(page.getByText(`Gmail conectado: ${gmailAddress}`)).toBeVisible()
  await expect(page).toHaveURL(/\/ajustes$/)
  await expect(section.getByText(gmailAddress)).toBeVisible()
  await expect(section.getByText('Conectado', { exact: true })).toBeVisible()
  await expect(section.getByLabel(/Modo sombra/)).toBeChecked()
  await expectAccessible(page, 'Ajustes con Gmail')

  await page.request.get(`${google()}/__deliver?to=${encodeURIComponent(gmailAddress)}&fixture=produbanco-consumo`)
  await page.request.get(`${google()}/__deliver?to=${encodeURIComponent(gmailAddress)}&fixture=pichincha-pago`)
  await section.getByRole('button', { name: 'Sincronizar ahora' }).click()
  await expect(page.getByText('1 gasto nuevo. Revísalos en Movimientos.')).toBeVisible()
  await expect(section.getByTestId('gmail-sync-info')).toContainText('1 importado')

  const pending = await apiGet(page, '/api/transactions?status=pending_review&month=2026-10')
  expect(pending.total).toBe(1)
  expect(pending.items[0].source).toBe('gmail')
  await page.goto('/movimientos?month=2026-10')
  await expect(page.getByText('Pedidosya Groceries')).toBeVisible()
  await expect(page.getByText('Por revisar')).toBeVisible()

  // Desconectar revoca el acceso en Google y conserva los gastos.
  await page.goto('/ajustes')
  page.once('dialog', (d) => d.accept())
  await section.getByRole('button', { name: 'Desconectar' }).click()
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
