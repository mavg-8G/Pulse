import { apiGet, apiPost, expect, expectAccessible, freshCode, register, test } from './helpers'

test('estadísticas, líneas y proyecciones por categoría para semana y mes', async ({ page }, testInfo) => {
  await register(page)
  await page.goto('/reportes')
  await expect(page.getByText('Aún falta historial para proyectar')).toBeVisible()
  const cats = await apiGet(page, '/api/categories')
  const accounts = await apiGet(page, '/api/accounts')
  const comida = cats.find((c: any) => c.name === 'Comida').id
  const servicios = cats.find((c: any) => c.name === 'Servicios').id
  // Mediodía en la zona de la cuenta: con la fecha del runner (UTC) fallaba entre las 19:00 y las 24:00 de Guayaquil.
  const [y, m, d] = new Intl.DateTimeFormat('en-CA', { timeZone: 'America/Guayaquil' }).format(new Date()).split('-').map(Number)
  const ago = (days: number) => (days === 0 ? new Date() : new Date(Date.UTC(y, m - 1, d - days, 17))).toISOString()
  await apiPost(page, '/api/transactions', { account_id: accounts[0].id, category_id: comida, amount_cents: 14000, occurred_at: ago(14) })
  await apiPost(page, '/api/transactions', { account_id: accounts[0].id, category_id: servicios, amount_cents: 2800, occurred_at: ago(7) })
  await apiPost(page, '/api/transactions', { account_id: accounts[0].id, category_id: comida, kind: 'refund', amount_cents: 1400, occurred_at: ago(2) })
  const reportLoaded = page.waitForResponse((response) => response.url().includes('/api/reports/summary') && response.status() === 200)
  await page.reload()
  await reportLoaded
  await expect(page.getByRole('heading', { name: 'Evolución del gasto' })).toBeVisible()
  const forecast = page.getByRole('region', { name: 'Proyección de gastos' })
  await expect(forecast.getByText('Total estimado · 7 días', { exact: true })).toBeVisible()
  await expect(forecast.locator('.report-forecast-metrics').getByText('$77,00', { exact: true })).toBeVisible()
  // Rango del 95 % simulado con días reales; con 14 días aún no hay periodos pasados para elegir otro método.
  await expect(forecast.getByText(/^Rango del 95 %: \$[\d.,]+ – \$[\d.,]+$/)).toBeVisible()
  await expect(forecast.getByTestId('forecast-model')).toContainText('Promedio diario')
  // Por cuenta: total de la cuenta y, al abrirla, en qué categorías se gastó.
  const byAccount = page.getByRole('region', { name: 'Por cuenta', exact: true })
  await byAccount.getByText(accounts[0].name, { exact: true }).click()
  await expect(byAccount.getByRole('row', { name: /Comida 1 \$126,00 82%/ })).toBeVisible()
  await expect(byAccount.getByRole('row', { name: /Servicios 1 \$28,00 18%/ })).toBeVisible()
  await page.getByRole('combobox', { name: 'Comparar categoría', exact: true }).first().selectOption(comida)
  await expect(page.locator('.report-series-secondary')).toHaveCount(1)
  await page.getByText('Ver evolución como tabla', { exact: true }).click()
  await expect(page.getByRole('columnheader', { name: 'Comida', exact: true })).toBeVisible()
  await page.getByRole('combobox', { name: 'Detalle del mes', exact: true }).selectOption('0')
  await forecast.getByRole('combobox', { name: 'Comparar categoría', exact: true }).selectOption(comida)
  await expect(page.locator('.report-series-secondary')).toHaveCount(2)
  await forecast.getByRole('button', { name: 'Próximo mes', exact: true }).click()
  const summary = await apiGet(page, '/api/reports/summary')
  await expect(forecast.getByText(`Total estimado · ${summary.forecast.month_days} días`, { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '3 meses', exact: true }).click()
  await expect(page.getByRole('button', { name: '3 meses', exact: true })).toHaveAttribute('aria-pressed', 'true')
  await expect(forecast.getByText(`Total estimado · ${summary.forecast.month_days} días`, { exact: true })).toBeVisible()
  await expectAccessible(page, 'Reportes ampliados')
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBeTruthy()
  await page.screenshot({ path: testInfo.outputPath('reportes.png'), fullPage: true })
  await page.evaluate(() => document.documentElement.dataset.theme = 'dark')
  await expectAccessible(page, 'Reportes ampliados oscuro')
  await page.screenshot({ path: testInfo.outputPath('reportes-oscuro.png'), fullPage: true })
})

test('reportes, CSV con step-up, gasto fijo y conciliación', async ({ page }) => {
  const u = await register(page)
  const cats = await apiGet(page, '/api/categories')
  const accounts = await apiGet(page, '/api/accounts')
  const comida = cats.find((c: any) => c.name === 'Comida').id
  await apiPost(page, '/api/transactions', { account_id: accounts[0].id, category_id: comida, amount_cents: 4250, occurred_at: new Date().toISOString(), merchant: 'Supermaxi' })

  await page.goto('/reportes')
  await expect(page.getByRole('heading', { name: 'Reportes', level: 1 })).toBeVisible()
  await expect(page.getByText('Total $42,50')).toBeVisible()
  await expectAccessible(page, 'Reportes')

  // Exportar pide confirmar identidad y descarga el CSV
  const download = page.waitForEvent('download')
  await page.getByRole('button', { name: 'Exportar CSV' }).click()
  const dialog = page.getByRole('dialog', { name: 'Confirma que eres tú' })
  await expect(dialog).toBeVisible()
  await dialog.getByLabel('Código de tu app de autenticación').fill(await freshCode(u))
  await dialog.getByRole('button', { name: 'Verificar código' }).click()
  expect((await download).suggestedFilename()).toMatch(/^pulse-\d{4}-\d{2}-a-\d{4}-\d{2}\.csv$/)

  // Conciliación del mes pasado: sin gastos registrados, el estado dice 10 → faltan 10
  const input = page.getByLabel('Estado de cuenta').first()
  await input.fill('10')
  await input.press('Enter')
  await expect(page.getByText('Faltan $10,00 en Pulse')).toBeVisible()

  // Gasto fijo desde "Nuevo gasto"
  await page.goto('/plan')
  await page.getByRole('button', { name: 'Nuevo gasto' }).first().click()
  const sheet = page.getByRole('dialog', { name: 'Nuevo gasto' })
  await sheet.getByLabel('Monto').fill('19,99')
  await sheet.getByRole('button', { name: /Servicios/ }).click()
  await sheet.getByLabel('Comercio').fill('Netflix')
  await sheet.getByLabel(/Repetir cada mes/).check()
  await sheet.getByRole('button', { name: 'Guardar gasto' }).click()
  await expect(page.getByRole('heading', { name: 'Gastos fijos' })).toBeVisible()
  await expect(page.getByRole('region', { name: 'Gastos fijos' }).getByText('Netflix ·')).toBeVisible()
})

test('un gasto hecho sin conexión se envía al volver la red', async ({ page, context }) => {
  await register(page)
  await page.goto('/movimientos')
  await page.getByRole('button', { name: 'Nuevo gasto' }).first().click()
  const sheet = page.getByRole('dialog', { name: 'Nuevo gasto' })
  await expect(sheet.getByRole('button', { name: /Comida/ })).toBeVisible() // categorías cargadas antes de perder la red
  // El service worker hace las peticiones de la API: se cortan a nivel de contexto además de setOffline.
  await context.route('**/api/**', (r) => r.abort('internetdisconnected'))
  await context.setOffline(true)
  await sheet.getByLabel('Monto').fill('3,75')
  await sheet.getByRole('button', { name: /Comida/ }).click()
  await sheet.getByLabel('Comercio').fill('Café sin red')
  await sheet.getByRole('button', { name: 'Guardar gasto' }).click()
  await expect(page.getByText('Gasto guardado en este dispositivo: se enviará al volver la conexión')).toBeVisible()
  await context.unroute('**/api/**')
  await context.setOffline(false)
  await expect(page.getByText('Se envió un cambio hecho sin conexión')).toBeVisible()
  await expect(page.getByText('Café sin red')).toBeVisible()
  const txs = await apiGet(page, '/api/transactions')
  expect(txs.total).toBe(1)
})

test('proyección de fin de mes, comparación, día de la semana, mapa de calor y cuentas', async ({ page }, testInfo) => {
  await register(page)
  const cats = await apiGet(page, '/api/categories')
  const accounts = await apiGet(page, '/api/accounts')
  const comida = cats.find((c: any) => c.name === 'Comida').id
  // Mediodía en la zona de la cuenta: con la fecha del runner (UTC) fallaba entre las 19:00 y las 24:00 de Guayaquil.
  const [y, m, d] = new Intl.DateTimeFormat('en-CA', { timeZone: 'America/Guayaquil' }).format(new Date()).split('-').map(Number)
  const ago = (days: number) => (days === 0 ? new Date() : new Date(Date.UTC(y, m - 1, d - days, 17))).toISOString()
  const visa = await apiPost(page, '/api/accounts', { name: 'Visa', type: 'credit_card' })
  const ids = [accounts[0].id, visa.id]
  for (let d = 0; d < 70; d += 3) await apiPost(page, '/api/transactions', { account_id: ids[d % 2], category_id: comida, amount_cents: 1000 + d * 10, occurred_at: ago(d) })
  await page.goto('/reportes')
  const card = page.getByRole('region', { name: 'Proyección de fin de mes' })
  await expect(card.getByTestId('projection-value')).toHaveText(/\$/)
  const p = await apiGet(page, '/api/reports/projection')
  expect(p.projected_cents).toBeGreaterThanOrEqual(p.spent_cents)
  await expect(card.getByText(p.method === 'ritmo' ? 'Ritmo del mes (método actual)' : /Regresión|Media|Ajuste|Estacionalidad/).first()).toBeVisible()
  await card.getByText('Precisión: proyección contra el cierre real').click()
  await expect(card.getByRole('table', { name: 'Métodos comparados' })).toBeVisible()
  // La gráfica se recorre con el teclado.
  const chart = card.getByRole('group', { name: /Gráfica: detalle del día/ })
  await chart.focus()
  await page.keyboard.press('ArrowLeft')
  await expect(card.getByRole('combobox', { name: 'Detalle del día' })).toHaveValue(String(Math.max(0, p.day - 2)))

  await expect(page.getByRole('heading', { name: /^Categorías:/ })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Por día de la semana' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Evolución por cuenta' })).toBeVisible()
  const heat = page.getByRole('group', { name: /Mapa de calor/ })
  await heat.focus()
  const before = await page.getByRole('region', { name: 'Gasto por día' }).locator('[aria-live]').textContent()
  await page.keyboard.press('ArrowLeft')
  await expect(page.getByRole('region', { name: 'Gasto por día' }).locator('[aria-live]')).not.toHaveText(before!)

  await expectAccessible(page, 'Reportes con proyección')
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBeTruthy()
  await page.screenshot({ path: testInfo.outputPath('reportes-proyeccion.png'), fullPage: true })
  await page.evaluate(() => (document.documentElement.dataset.theme = 'dark'))
  await expectAccessible(page, 'Reportes con proyección (oscuro)')
  await page.screenshot({ path: testInfo.outputPath('reportes-proyeccion-oscuro.png'), fullPage: true })
})
