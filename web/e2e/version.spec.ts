import { expect, test } from '@playwright/test'
import { register } from './helpers'

test('el pie y Ajustes muestran la versión que corre en el servidor', async ({ page }) => {
  await register(page)
  await page.goto('/')
  await expect(page.locator('.workspace-footer .build-version')).toHaveText(/^V\d{4}-\d\d-\d\d \d\d:\d\d:\d\d$/)
  await page.goto('/ajustes')
  await expect(page.getByRole('region', { name: 'Versión' })).toContainText(/V\d{4}-\d\d-\d\d \d\d:\d\d:\d\d/)
})
