import { expect, expectAccessible, register, test } from './helpers'

const pages = [
  { path: '/privacidad', title: 'Política de privacidad' },
  { path: '/terminos', title: 'Términos y condiciones' },
  { path: '/privacy', title: 'Privacy Policy' },
  { path: '/terms', title: 'Terms of Service' },
  { path: '/about', title: 'Pulse — your personal expense tracker' },
  { path: '/acerca', title: 'Pulse — tu registro personal de gastos' },
]

test('páginas legales públicas: HTML estático, con CSP y sin sesión', async ({ request }) => {
  for (const p of pages) {
    const res = await request.get(p.path)
    expect(res.status(), p.path).toBe(200)
    expect(res.headers()['content-type']).toContain('text/html')
    expect(res.headers()['content-security-policy']).toContain("script-src 'self'")
    const html = await res.text()
    // El contenido está en el HTML (sin depender de JavaScript): lo puede leer el revisor de Google.
    expect(html).toContain(`<h1>${p.title}</h1>`)
    expect(html).not.toContain('id="root"')
  }
  const priv = await (await request.get('/privacidad')).text()
  expect(priv).toContain('Limited Use requirements') // declaración exigida por Google para gmail.readonly
  expect(priv).toContain('12 meses') // debe coincidir con store.AuditRetention
  const en = await (await request.get('/privacy')).text()
  for (const t of ['Limited Use requirements', 'gmail.readonly', 'Data accessed', 'How the data is used', 'How the data is stored', 'How the data is shared', 'Retention and deletion', '12 months'])
    expect(en, t).toContain(t)
  // La portada pública explica el propósito y enlaza la política (requisito de Google).
  const about = await (await request.get('/about')).text()
  expect(about).toContain('href="/privacy"')
  expect(about).toContain('Why Pulse asks for access to Gmail')
})

test('páginas legales accesibles en ambos temas y enlazadas desde el acceso', async ({ page }) => {
  for (const scheme of ['light', 'dark'] as const) {
    await page.emulateMedia({ colorScheme: scheme })
    for (const p of pages) {
      await page.goto(p.path)
      await expect(page.getByRole('heading', { level: 1, name: p.title })).toBeVisible()
      await expectAccessible(page, `${p.path} (${scheme})`)
    }
  }
  await page.emulateMedia({ colorScheme: 'light' })
  await page.goto('/entrar')
  await page.getByRole('link', { name: 'Qué es Pulse' }).click()
  await expect(page.getByRole('heading', { level: 1, name: 'Tu dinero tiene pulso.' })).toBeVisible()
  await page.goto('/entrar')
  await page.getByRole('link', { name: 'Privacidad' }).click()
  await expect(page).toHaveURL(/\/privacidad$/)
  await page.getByRole('link', { name: 'Términos y condiciones' }).click()
  await expect(page.getByRole('heading', { level: 1, name: 'Términos y condiciones' })).toBeVisible()
})

test('con el service worker activo, /privacidad no se reemplaza por la app', async ({ page }) => {
  await register(page)
  await page.waitForFunction(async () => (await navigator.serviceWorker.getRegistration())?.active?.state === 'activated', null, { timeout: 15_000 })
  await page.goto('/ajustes')
  await page.waitForFunction(() => !!navigator.serviceWorker.controller, null, { timeout: 10_000 })
  await page.getByRole('link', { name: /Política de privacidad/ }).click()
  await expect(page.getByRole('heading', { level: 1, name: 'Política de privacidad' })).toBeVisible()
  await expect(page.locator('#root')).toHaveCount(0)
})
