import { apiGet, apiPost, expect, expectAccessible, register, test } from './helpers'

test('vistas públicas y acceso: diseño adaptable y accesible en ambos temas', async ({ page }, info) => {
  test.setTimeout(120_000)
  const views = [
    ['/', 'Tu dinero tiene pulso.', 'portada'],
    ['/entrar', 'Entrar', 'acceso'],
    ['/registro', 'Crear cuenta', 'registro'],
    ['/privacidad', 'Política de privacidad', 'privacidad'],
  ]
  for (const theme of ['light', 'dark'] as const) {
    await page.emulateMedia({ colorScheme: theme, reducedMotion: 'reduce' })
    for (const [path, heading, name] of views) {
      await page.goto(path)
      await expect(page.getByRole('heading', { level: 1, name: heading, exact: true })).toBeVisible()
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), `${name}, ${theme}: ancho`).toBeTruthy()
      await expectAccessible(page, `${name}, ${theme}`)
      await page.screenshot({ path: info.outputPath(`${name}-${theme}.png`), fullPage: true, animations: 'disabled' })
    }
  }
})

test('todas las vistas privadas conservan legibilidad, navegación y formularios', async ({ page }, info) => {
  test.setTimeout(180_000)
  await register(page)
  const cats = await apiGet(page, '/api/categories')
  const accounts = await apiGet(page, '/api/accounts')
  const food = cats.find((c: any) => c.name === 'Comida').id
  const transport = cats.find((c: any) => c.name === 'Transporte').id
  await apiPost(page, '/api/accounts', { name: 'Visa personal', type: 'credit_card', institution: 'Banco Pichincha', card_suffix: '989' })
  await apiPost(page, '/api/budgets', { category_id: food, amount_cents: 40000, thresholds: '50,80,100' })
  await apiPost(page, '/api/budgets', { category_id: transport, amount_cents: 12000, thresholds: '50,80,100' })
  for (const [merchant, cents, category, daysAgo] of [['Supermercado', 6840, food, 14], ['Café de la mañana', 450, food, 1], ['Viaje al trabajo', 820, transport, 1]] as const) {
    const date = new Date()
    date.setDate(date.getDate() - daysAgo)
    date.setHours(12, 0, 0, 0)
    await apiPost(page, '/api/transactions', { account_id: accounts[0].id, category_id: category, amount_cents: cents, occurred_at: date.toISOString(), merchant })
  }
  const views = [
    ['/', 'Inicio', 'inicio', '.dashboard'],
    ['/movimientos', 'Movimientos', 'movimientos', '.totals-line'],
    ['/plan', 'Plan de gastos', 'plan', '.plan-total'],
    ['/reportes', 'Reportes', 'reportes', '.report-metrics'],
    ['/ajustes/cuentas', 'Cuentas y tarjetas', 'cuentas', '.account-collection .tx'],
    ['/ajustes/categorias', 'Categorías', 'categorias', '.category-collection .tx'],
    ['/ajustes', 'Ajustes', 'ajustes', '#h-look'],
    ['/ajustes/cuenta', 'Mi cuenta', 'mi-cuenta', '.account-card strong'],
    ['/ajustes/gmail?error=access_denied', 'Conectar Gmail', 'conexion', '[role="alert"]'],
  ]
  const initialLoad = page.waitForResponse((response) => response.url().endsWith('/api/auth/state') && response.ok())
  await page.goto('/')
  await initialLoad
  for (const theme of ['light', 'dark'] as const) {
    await page.emulateMedia({ colorScheme: theme, reducedMotion: 'reduce' })
    for (const [path, heading, name, ready] of views) {
      if (path.includes('?')) {
        const authReady = page.waitForResponse((response) => response.url().endsWith('/api/auth/state') && response.ok())
        await page.goto(path)
        await authReady
      } else if (new URL(page.url()).pathname !== path) {
        // Exercise the app's own navigation, including organization links in
        // Settings on mobile, without restarting the PWA for every screenshot.
        const link = page.locator(`a[href="${path}"]:visible`).first()
        if (await link.count() === 0) {
          await page.locator('a[href="/ajustes"]:visible').first().click()
          await expect(page.getByRole('heading', { name: 'Ajustes', level: 1, exact: true })).toBeVisible()
        }
        await link.click()
      }
      await expect(page.getByRole('heading', { level: 1, name: heading, exact: true })).toBeVisible()
      // Wait for the actual panel before judging layout or taking screenshots;
      // a successful API read can take longer than the default assertion window.
      await expect(page.locator(ready).first()).toBeVisible({ timeout: 30_000 })
      await expect(page.getByText('Cargando…', { exact: true })).toHaveCount(0)
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), `${name}, ${theme}: ancho`).toBeTruthy()
      await expectAccessible(page, `${name}, ${theme}`)
      await page.screenshot({ path: info.outputPath(`${name}-${theme}.png`), fullPage: true, animations: 'disabled' })
    }
  }
  await page.locator('a[href="/"]:visible').first().click()
  await page.getByRole('button', { name: 'Nuevo gasto', exact: true }).first().click()
  const sheet = page.getByRole('dialog', { name: 'Nuevo gasto', exact: true })
  await expect(sheet).toBeVisible()
  await expectAccessible(page, 'Formulario de gasto')
  await page.screenshot({ path: info.outputPath('nuevo-gasto.png'), fullPage: true, animations: 'disabled' })
  await sheet.getByRole('button', { name: 'Cerrar', exact: true }).click()
  const navigation = page.getByRole('navigation', { name: 'Principal', exact: true })
  await navigation.getByRole('link', { name: 'Reportes', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Reportes', level: 1 })).toBeVisible()
})
