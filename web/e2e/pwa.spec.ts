import { apiPost, expect, expectAccessible, register, test } from './helpers'

test('manifest y cabeceras de seguridad', async ({ page, request }) => {
  const m = await (await request.get('/manifest.webmanifest')).json()
  expect(m.display).toBe('standalone')
  expect(m.icons.some((i: any) => i.purpose === 'maskable')).toBeTruthy()
  const res = await request.get('/')
  expect(res.headers()['content-security-policy']).toContain("default-src 'self'")
  expect(res.headers()['x-frame-options']).toBe('DENY')
  await page.goto('/')
  await expect(page.getByRole('heading', { level: 1, name: 'Tu dinero tiene pulso.' })).toBeVisible()
  await page.getByRole('link', { name: 'Entrar', exact: true }).first().click()
  await expect(page).toHaveURL(/\/entrar$/)
  await expect(page.getByRole('heading', { name: 'Entrar' })).toBeVisible()
})

test('service worker: la app abre sin conexión y la API nunca se guarda en caché', async ({ page, context }) => {
  await register(page)
  await page.waitForFunction(async () => (await navigator.serviceWorker.getRegistration())?.active?.state === 'activated', null, { timeout: 15_000 })
  await page.goto('/plan') // una navegación nueva ya queda controlada por el service worker
  await page.waitForFunction(() => !!navigator.serviceWorker.controller, null, { timeout: 10_000 })
  await apiPost(page, '/api/accounts', { name: 'Banco', type: 'bank' })
  const cached = await page.evaluate(async () => {
    const out: string[] = []
    for (const k of await caches.keys()) for (const r of await (await caches.open(k)).keys()) out.push(r.url)
    return out
  })
  expect(cached.length).toBeGreaterThan(0)
  expect(cached.filter((u) => u.includes('/api/'))).toEqual([])

  expect(await page.evaluate(() => !!navigator.serviceWorker.controller)).toBeTruthy()
  // Sin red, el service worker sirve la app desde su caché.
  await context.setOffline(true)
  await page.goto('/movimientos').catch((e) => { throw new Error('no abrió sin conexión: ' + e.message) })
  await expect(page.locator('#root')).not.toBeEmpty()
  await context.setOffline(false)

  // Al cerrar sesión se borran las cachés del navegador
  await page.goto('/ajustes/cuenta')
  await page.getByRole('button', { name: 'Cerrar sesión', exact: true }).last().click()
  await expect(page.getByRole('heading', { name: 'Entrar' })).toBeVisible()
  const after = await page.evaluate(async () => (await caches.keys()).length)
  expect(after).toBe(0)
})

test('tema oscuro persiste y las pantallas son accesibles en ambos temas', async ({ page }) => {
  await register(page)
  await apiPost(page, '/api/budgets', { category_id: (await (await page.request.get('/api/categories')).json())[0].id, amount_cents: 10000 })
  for (const theme of ['Claro', 'Oscuro']) {
    await page.goto('/ajustes')
    await page.getByRole('group', { name: 'Tema' }).getByRole('button', { name: theme }).click()
    await page.reload()
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme === 'Oscuro' ? 'dark' : 'light')
    for (const path of ['/', '/movimientos', '/plan', '/ajustes', '/ajustes/cuenta', '/ajustes/cuentas']) {
      await page.goto(path)
      await page.waitForTimeout(1300) // termina la animación del pulso
      await expectAccessible(page, `${path} (${theme})`)
    }
  }
})

test('pantallas de acceso accesibles', async ({ page }) => {
  for (const path of ['/', '/registro']) {
    await page.goto(path)
    await expectAccessible(page, path)
  }
})
