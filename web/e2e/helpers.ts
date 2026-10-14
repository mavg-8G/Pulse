import { execFileSync } from 'node:child_process'
import { test as base, expect, type Page } from '@playwright/test'
import * as OTPAuth from 'otpauth'
import AxeBuilder from '@axe-core/playwright'
import { fileURLToPath } from 'node:url'
import { join } from 'node:path'

const DIR = process.env.PULSE_E2E_DIR || fileURLToPath(new URL('../.e2e/', import.meta.url))
export const BASE_URL = `http://localhost:${process.env.PULSE_E2E_PORT || 8091}`

export function createInvite(email = ''): string {
  const env = {
    ...process.env,
    PULSE_ENV: 'development',
    PULSE_BASE_URL: BASE_URL,
    PULSE_DB_PATH: join(DIR, 'pulse.db'),
    PULSE_KEK_FILE: join(DIR, 'secrets/pulse_kek'),
  }
  const args = ['invite', 'create', ...(email ? ['--email', email] : [])]
  const out = execFileSync(join(DIR, process.platform === 'win32' ? 'pulse.exe' : 'pulse'), args, { env }).toString()
  return out.match(/PLS(-[A-Z0-9]{4}){5}/)![0]
}

export interface User {
  email: string
  password: string
  totp: OTPAuth.TOTP
  recovery: string[]
}

let counter = 0
export const uniqueEmail = (p = 'u') => `${p}${Date.now()}${counter++}@example.com`

/** Espera al siguiente período TOTP: un código no puede usarse dos veces. */
export async function freshCode(u: User): Promise<string> {
  const remaining = 30 - (Math.floor(Date.now() / 1000) % 30)
  await new Promise((r) => setTimeout(r, remaining * 1000 + 300))
  return u.totp.generate()
}

export async function register(page: Page, email = uniqueEmail()): Promise<User> {
  const password = 'caballo bateria grapa correcta'
  await page.goto('/registro')
  await page.getByLabel('Código de invitación').fill(createInvite())
  await page.getByLabel('Email').fill(email)
  await page.getByLabel('Contraseña', { exact: true }).fill(password)
  await page.getByLabel('Repite la contraseña').fill(password)
  const registration = page.waitForResponse((r) => r.url().includes('/api/auth/register') && r.request().method() === 'POST')
  await page.getByRole('button', { name: 'Crear cuenta' }).click()
  const registered = await registration
  expect(registered.status(), `Registro: ${await registered.text()}`).toBe(201)
  await expect(page.getByRole('heading', { name: 'Protege tu cuenta' })).toBeVisible()
  await page.getByRole('button', { name: 'Configurar app' }).click()
  await page.getByText('¿No puedes escanear?').click()
  const secret = (await page.getByTestId('totp-secret').textContent())!.trim()
  const totp = new OTPAuth.TOTP({ secret: OTPAuth.Secret.fromBase32(secret) })
  await page.getByLabel('Código de 6 dígitos').fill(totp.generate())
  await page.getByRole('button', { name: 'Activar' }).click()
  const codes = page.getByTestId('recovery-codes')
  await expect(codes).toBeVisible()
  const recovery = (await codes.locator('span').allTextContents()).map((s) => s.trim())
  await page.getByLabel('Ya los guardé').check()
  await page.getByRole('button', { name: 'Continuar' }).click()
  await expect(page.getByRole('heading', { name: 'Inicio' })).toBeVisible()
  return { email, password, totp, recovery }
}

/** Atajo para preparar datos: usa la sesión del navegador y el mismo origen. */
export async function apiPost(page: Page, path: string, data: unknown) {
  const r = await page.request.post(path, { data, headers: { Origin: BASE_URL } })
  expect(r.ok(), `${path}: ${await r.text()}`).toBeTruthy()
  return r.json()
}

export async function apiGet(page: Page, path: string) {
  const r = await page.request.get(path)
  expect(r.ok()).toBeTruthy()
  return r.json()
}

export async function expectAccessible(page: Page, name: string) {
  const res = await new AxeBuilder({ page: page as any }).withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze()
  const serious = res.violations.filter((v) => v.impact === 'serious' || v.impact === 'critical')
  expect(serious.map((v) => `${name}: ${v.id} — ${v.help} (${v.nodes.length})\n  ${v.nodes.map((n) => n.target.join(' ')).slice(0, 3).join('\n  ')}`)).toEqual([])
}

/** Falla el test si aparece un error de consola (incluidas violaciones de CSP). */
export const test = base.extend<{ consoleErrors: string[] }>({
  consoleErrors: [
    async ({ page }, use) => {
      const errors: string[] = []
      page.on('console', (m) => {
        const t = m.text()
        if (m.type() === 'error' && !/status of 40[0-9]|Failed to load resource/.test(t)) errors.push(t)
        if (/Content Security Policy/i.test(t)) errors.push(t)
      })
      page.on('pageerror', (e) => errors.push(e.message))
      await use(errors)
      expect(errors, 'errores en la consola del navegador').toEqual([])
    },
    { auto: true },
  ],
})
export { expect }
