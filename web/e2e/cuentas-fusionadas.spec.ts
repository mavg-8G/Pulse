import { apiGet, apiPost, expect, expectAccessible, register, test } from './helpers'

test('fusionar dos cuentas que son la misma tarjeta y separarlas', async ({ page }) => {
  await register(page)
  const main = await apiPost(page, '/api/accounts', { name: 'Titanium (Diners Club)', type: 'credit_card', institution: 'Diners Club' })
  const alias = await apiPost(page, '/api/accounts', { name: 'Titanium (Diners Club) …666', type: 'credit_card', institution: 'Diners Club', card_suffix: '666' })
  const cats: { id: string; name: string }[] = await apiGet(page, '/api/categories')
  const comida = cats.find((c) => c.name === 'Comida')!.id
  const today = new Date().toLocaleString('sv-SE', { timeZone: 'America/Guayaquil' }).slice(0, 10)
  await apiPost(page, '/api/transactions', { account_id: main.id, category_id: comida, amount_cents: 1000, occurred_at: `${today}T08:00`, merchant: 'Café sin dígitos' })
  await apiPost(page, '/api/transactions', { account_id: alias.id, category_id: comida, amount_cents: 2500, occurred_at: `${today}T09:00`, merchant: 'Almuerzo con dígitos' })

  await page.goto('/ajustes/cuentas')
  await page.getByRole('button', { name: /^Titanium \(Diners Club\) …666/ }).click()
  const sheet = page.getByRole('dialog', { name: 'Editar cuenta' })
  await sheet.getByLabel('Fusionar con…').selectOption({ label: 'Titanium (Diners Club)' })
  await expectAccessible(page, 'fusionar cuentas')
  await sheet.getByRole('button', { name: 'Fusionar', exact: true }).click()
  const confirm = page.getByRole('alertdialog', { name: /¿Fusionar Titanium/ })
  await expect(confirm).toContainText('Ninguna se borra')
  await confirm.getByRole('button', { name: 'Fusionar' }).click()
  await expect(page.getByText('ahora se ve junto con Titanium (Diners Club)')).toBeVisible()
  await expect(page.getByText('Incluye Titanium (Diners Club) …666')).toBeVisible()
  expect((await apiGet(page, '/api/accounts')).length).toBe(3) // nada se borró (Efectivo + las dos)

  // Movimientos: la cuenta fusionada ya no aparece aparte y su filtro trae los gastos de ambas.
  await page.goto('/movimientos')
  const filter = page.getByLabel('Cuenta', { exact: true })
  await expect(filter.locator('option', { hasText: '…666' })).toHaveCount(0)
  await filter.selectOption({ label: 'Titanium (Diners Club)' })
  await expect(page.getByText('Almuerzo con dígitos')).toBeVisible()
  await expect(page.getByText('Café sin dígitos')).toBeVisible()
  await expect(page.getByTestId('list-sum')).toHaveText(/35,00/)

  // Separar: cada una vuelve a sus propios gastos.
  await page.goto('/ajustes/cuentas')
  await page.getByRole('button', { name: /^Titanium \(Diners Club\) …666/ }).click()
  await sheet.getByRole('button', { name: 'Separar de Titanium (Diners Club)' }).click()
  await expect(page.getByText('Cuentas separadas')).toBeVisible()
  const page1 = await apiGet(page, `/api/transactions?account_id=${main.id}`)
  expect(page1.total).toBe(1)
})
