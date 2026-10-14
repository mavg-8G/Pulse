import { apiGet, createInvite, expect, freshCode, register, test, uniqueEmail } from './helpers'

test.describe('registro e inicio de sesión', () => {
  test('sin invitación válida no se puede crear cuenta', async ({ page }) => {
    await page.goto('/registro')
    await page.getByLabel('Código de invitación').fill('PLS-AAAA-BBBB-CCCC-DDDD-EEEE')
    await page.getByLabel('Email').fill(uniqueEmail())
    await page.getByLabel('Contraseña', { exact: true }).fill('caballo bateria grapa correcta')
    await page.getByLabel('Repite la contraseña').fill('caballo bateria grapa correcta')
    await page.getByRole('button', { name: 'Crear cuenta' }).click()
    await expect(page.getByRole('alert')).toContainText('invitación')
  })

  test('registro con TOTP, cierre de sesión y vuelta a entrar', async ({ page }) => {
    const u = await register(page)
    expect(u.recovery).toHaveLength(10)

    await page.goto('/ajustes')
    await page.getByRole('button', { name: 'Cerrar sesión', exact: true }).last().click()
    await expect(page.getByRole('heading', { name: 'Entrar' })).toBeVisible()

    // Contraseña incorrecta: mensaje claro, sin revelar si la cuenta existe
    await page.getByLabel('Email').fill(u.email)
    await page.getByLabel('Contraseña').fill('no-es-la-clave-123')
    await page.getByRole('button', { name: 'Continuar' }).click()
    await expect(page.getByRole('alert')).toContainText('incorrectos')

    await page.getByLabel('Contraseña').fill(u.password)
    await page.getByRole('button', { name: 'Continuar' }).click()
    await expect(page.getByRole('heading', { name: 'Confirma que eres tú' })).toBeVisible()
    // Sin el segundo factor la API no entrega datos
    const r = await page.request.get('/api/me')
    expect(r.status()).toBe(401)

    await page.getByLabel('Código de tu app de autenticación').fill(await freshCode(u))
    await page.getByRole('button', { name: 'Verificar' }).click()
    await expect(page.getByRole('heading', { name: 'Inicio' })).toBeVisible()
    const me = await apiGet(page, '/api/me')
    expect(me.email).toBe(u.email)
  })

  test('entrar con un código de recuperación (un solo uso)', async ({ page, context }) => {
    const u = await register(page)
    await context.clearCookies()
    await page.goto('/')
    await page.getByLabel('Email').fill(u.email)
    await page.getByLabel('Contraseña').fill(u.password)
    await page.getByRole('button', { name: 'Continuar' }).click()
    await page.getByRole('button', { name: 'Usar código de recuperación' }).click()
    await page.getByLabel('Código de recuperación').fill(u.recovery[0])
    await page.getByRole('button', { name: 'Verificar' }).click()
    await expect(page.getByRole('heading', { name: 'Inicio' })).toBeVisible()

    await context.clearCookies()
    await page.goto('/')
    await page.getByLabel('Email').fill(u.email)
    await page.getByLabel('Contraseña').fill(u.password)
    await page.getByRole('button', { name: 'Continuar' }).click()
    await page.getByRole('button', { name: 'Usar código de recuperación' }).click()
    await page.getByLabel('Código de recuperación').fill(u.recovery[0])
    await page.getByRole('button', { name: 'Verificar' }).click()
    await expect(page.getByRole('alert')).toBeVisible()
  })

  test('invitación atada a otro email', async ({ page }) => {
    await page.goto('/registro')
    await page.getByLabel('Código de invitación').fill(createInvite('alguien@example.com'))
    await page.getByLabel('Email').fill(uniqueEmail())
    await page.getByLabel('Contraseña', { exact: true }).fill('caballo bateria grapa correcta')
    await page.getByLabel('Repite la contraseña').fill('caballo bateria grapa correcta')
    await page.getByRole('button', { name: 'Crear cuenta' }).click()
    await expect(page.getByRole('alert')).toContainText('invitación')
  })
})
