import { apiGet, apiPost, expect, register, test } from './helpers'

test('plan de gastos: presupuesto, progreso y aviso del 80 %', async ({ page }) => {
  await register(page)
  const cats = await apiGet(page, '/api/categories')
  const comida = cats.find((c: any) => c.name === 'Comida')
  const accounts = await apiGet(page, '/api/accounts')

  await page.goto('/plan')
  await page.getByRole('button', { name: 'Crear presupuesto' }).click()
  const sheet = page.getByRole('dialog', { name: 'Nuevo presupuesto' })
  await sheet.getByLabel('Categoría').selectOption({ label: '🍽️ Comida' })
  await sheet.getByLabel('Tope mensual').fill('100')
  await sheet.getByRole('button', { name: 'Guardar presupuesto' }).click()
  await expect(page.getByText('Presupuesto guardado')).toBeVisible()

  const now = new Date().toISOString()
  await apiPost(page, '/api/transactions', { account_id: accounts[0].id, category_id: comida.id, amount_cents: 8500, occurred_at: now, merchant: 'Supermaxi' })
  await page.reload()
  await expect(page.getByRole('button', { name: /Comida: 85% usado/ })).toBeVisible()

  await page.goto('/')
  await expect(page.getByText(/Llevas el 80% de/)).toBeVisible()
})
