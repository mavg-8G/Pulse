import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { expect, expectAccessible, freshCode, register, test } from './helpers'

const DIR = process.env.PULSE_E2E_DIR || fileURLToPath(new URL('../.e2e/', import.meta.url))

test('respaldo externo: solo admin, con confirmación de identidad, cifrado y descargable', async ({ page, browser }) => {
  // Una persona sin rol de administrador no ve la sección.
  const userCtx = await browser.newContext()
  const userPage = await userCtx.newPage()
  await register(userPage)
  await userPage.goto('/ajustes')
  await expect(userPage.getByRole('heading', { name: 'Notificaciones' }).or(userPage.getByRole('heading', { name: 'Privacidad' })).first()).toBeVisible()
  await expect(userPage.getByRole('heading', { name: 'Respaldo (admin)' })).toHaveCount(0)
  expect((await userPage.request.get('/api/admin/backups')).status()).toBe(403)
  await userCtx.close()

  const u = await register(page, undefined, 'admin')
  await page.goto('/ajustes')
  const section = page.getByRole('region', { name: 'Respaldo (admin)' })
  await expect(section.getByTestId('backup-state')).toHaveText(/Aún no hay respaldos|Al día/) // el servidor E2E se comparte entre escritorio y móvil
  await section.getByRole('button', { name: 'Respaldar ahora' }).click()
  const stepUp = page.getByRole('dialog', { name: 'Confirma que eres tú' })
  await expect(stepUp).toBeVisible()
  await stepUp.getByLabel('Código de tu app de autenticación').fill(await freshCode(u))
  await stepUp.getByRole('button', { name: 'Verificar código' }).click()
  await expect(page.getByText(/Respaldo cifrado enviado/)).toBeVisible({ timeout: 30_000 })
  await expect(section.getByTestId('backup-state')).toHaveText('Al día')
  await expect(section.getByRole('cell', { name: 'Correcto' }).or(section.getByText('Correcto')).first()).toBeVisible()

  // El objeto que llegó al destino está cifrado.
  const s3 = readFileSync(join(DIR, 'fake-s3.url'), 'utf8').trim()
  const objects = await (await fetch(`${s3}/__objects`)).json()
  expect(objects.length).toBeGreaterThan(0)
  expect(objects.every((o: any) => !o.plaintext && o.key.endsWith('.db.age'))).toBeTruthy()

  // Descarga de la copia cifrada (step-up todavía vigente).
  const download = page.waitForEvent('download')
  await section.getByRole('button', { name: 'Descargar copia cifrada' }).click()
  expect((await download).suggestedFilename()).toMatch(/\.db\.age$/)
  const health = await (await page.request.get('/healthz')).json()
  expect(health.backup).toBe('ok')
  await page.goto('/ajustes/cuenta')
  await expect(page.getByText('Respaldo cifrado hecho a mano')).toBeVisible()
  await page.goto('/ajustes')
  await expect(page.getByTestId('backup-state')).toBeVisible()
  await expectAccessible(page, 'Ajustes con respaldo')
})
