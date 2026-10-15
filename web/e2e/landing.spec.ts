import { expect, expectAccessible, register, test } from './helpers'

test('sin sesión, / es la landing pública y lleva a entrar y a registrarse', async ({ page }) => {
  for (const scheme of ['light', 'dark'] as const) {
    await page.emulateMedia({ colorScheme: scheme })
    await page.goto('/')
    await expect(page.getByRole('heading', { level: 1, name: 'Tu dinero tiene pulso.' })).toBeVisible()
    await expect(page.getByRole('heading', { name: 'Tu banco ya te avisa de cada compra' })).toBeVisible()
    await expectAccessible(page, `Landing (${scheme})`)
  }
  await page.getByRole('link', { name: 'Tengo una invitación' }).click()
  await expect(page).toHaveURL(/\/registro$/)
  await expect(page.getByRole('heading', { name: 'Crear cuenta' })).toBeVisible()
  await page.getByRole('link', { name: 'Pulse, volver al inicio' }).click()
  await expect(page).toHaveURL(/\/$/)
  await page.getByRole('link', { name: 'Entrar a Pulse' }).click()
  await expect(page.getByRole('heading', { name: 'Entrar' })).toBeVisible()
  await expect(page.getByLabel('Email')).toHaveAttribute('spellcheck', 'false')
})

test('con sesión, / es el panel y la landing no aparece', async ({ page }) => {
  await register(page)
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Inicio' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Tu dinero tiene pulso.' })).toHaveCount(0)
})

test('con movimiento reducido no hay animaciones en la landing', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('/')
  const running = await page.evaluate(() => document.getAnimations().length)
  expect(running).toBe(0)
})
