import { expect, test, uniqueEmail, createInvite } from './helpers'

// Autenticador WebAuthn virtual de Chromium: simula Touch ID / Windows Hello.
test('registro con passkey e inicio de sesión con ella', async ({ page, context, browserName }) => {
  test.skip(browserName !== 'chromium', 'El autenticador virtual solo existe en Chromium')
  const cdp = await context.newCDPSession(page)
  await cdp.send('WebAuthn.enable')
  await cdp.send('WebAuthn.addVirtualAuthenticator', {
    options: { protocol: 'ctap2', transport: 'internal', hasResidentKey: true, hasUserVerification: true, isUserVerified: true, automaticPresenceSimulation: true },
  })

  const email = uniqueEmail('pk')
  const password = 'caballo bateria grapa correcta'
  await page.goto('/registro')
  await page.getByLabel('Código de invitación').fill(createInvite())
  await page.getByLabel('Email').fill(email)
  await page.getByLabel('Contraseña', { exact: true }).fill(password)
  await page.getByLabel('Repite la contraseña').fill(password)
  await page.getByRole('button', { name: 'Crear cuenta' }).click()
  await page.getByRole('button', { name: 'Crear passkey' }).click()
  await expect(page.getByTestId('recovery-codes')).toBeVisible()
  await page.getByLabel('Ya los guardé').check()
  await page.getByRole('button', { name: 'Continuar' }).click()
  await expect(page.getByRole('heading', { name: 'Inicio' })).toBeVisible()

  await page.goto('/ajustes')
  await expect(page.getByText(/^Passkey: /)).toBeVisible()

  await context.clearCookies()
  await page.goto('/entrar')
  await page.getByLabel('Email').fill(email)
  await page.getByLabel('Contraseña').fill(password)
  await page.getByRole('button', { name: 'Continuar' }).click()
  await page.getByRole('button', { name: 'Usar mi passkey' }).click()
  await expect(page.getByRole('heading', { name: 'Inicio' })).toBeVisible()

  // Cambiar la contraseña exige confirmar con la passkey (step-up)
  await page.goto('/ajustes')
  await page.getByRole('button', { name: 'Cambiar' }).last().click()
  await page.getByLabel('Contraseña actual').fill(password)
  await page.getByLabel('Contraseña nueva').fill('otra frase bastante larga')
  await page.getByRole('button', { name: 'Cambiar contraseña' }).click()
  await expect(page.getByRole('dialog', { name: 'Confirma que eres tú' })).toBeVisible()
  await page.getByRole('button', { name: 'Usar mi passkey' }).click()
  await expect(page.getByText('Contraseña cambiada. Cerramos tus otras sesiones.')).toBeVisible()
})
