import { fileURLToPath } from 'node:url'
import { apiGet, expect, register, test } from './helpers'

const fixtures = fileURLToPath(new URL('../../internal/mailparse/testdata/', import.meta.url))

test('importar un correo del banco, revisarlo, aprender la categoría y confirmarlo', async ({ page }) => {
  await register(page)
  await page.goto('/movimientos')

  // Diners del 20 de junio de 2026
  await page.getByLabel('Archivo .eml del banco').setInputFiles(fixtures + 'diners-compra-columnas.eml')
  await expect(page.getByText(/Agregado para revisar: Uber Eats en Comida \(cuenta nueva: Titanium \(Diners Club\) …666\)/)).toBeVisible()
  // Reimportar el mismo correo no lo duplica
  await page.getByLabel('Archivo .eml del banco').setInputFiles(fixtures + 'diners-compra-columnas.eml')
  await expect(page.getByText('Ese correo ya estaba importado')).toBeVisible()
  // Un correo que no es gasto se explica
  await page.getByLabel('Archivo .eml del banco').setInputFiles(fixtures + 'pichincha-pago.eml')
  await expect(page.getByText('No es un gasto: pago de la tarjeta')).toBeVisible()

  const pending = await apiGet(page, '/api/transactions?status=pending_review')
  expect(pending.total).toBe(1)

  for (let i = 0; i < 24; i++) {
    const label = await page.locator('.month-switch strong').textContent()
    if (label?.startsWith('Junio de 2026')) break
    await page.getByRole('button', { name: 'Mes anterior' }).click()
    await expect(page.locator('.month-switch strong')).not.toHaveText(label!)
  }
  await expect(page.getByText('Por revisar')).toBeVisible()
  await page.getByText('Uber Eats').click()
  const sheet = page.getByRole('dialog', { name: 'Editar gasto' })
  await expect(sheet.getByText('Este gasto llegó por correo')).toBeVisible()
  await sheet.getByRole('button', { name: /Ocio/ }).click()
  await sheet.getByLabel('Usar siempre Ocio para “Uber Eats”').check()
  await sheet.getByRole('button', { name: 'Confirmar gasto' }).click()
  await expect(page.getByText('Gasto confirmado')).toBeVisible()
  await expect(page.getByText('Por revisar')).toHaveCount(0)

  const rules = await apiGet(page, '/api/merchant-rules')
  expect(rules.map((r: any) => r.pattern)).toEqual(['uber eats'])
  await page.goto('/ajustes/categorias')
  await expect(page.getByText('“uber eats” va a')).toBeVisible()
})
