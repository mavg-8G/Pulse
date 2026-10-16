import { readFileSync } from 'node:fs'
import { execFileSync } from 'node:child_process'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { expect, expectAccessible, register, test } from './helpers'

const DIR = process.env.PULSE_E2E_DIR || fileURLToPath(new URL('../.e2e/', import.meta.url))
// Keep server-response simulations on the page network; the real PWA is covered
// by pwa.spec.ts with service workers enabled.
test.use({ serviceWorkers: 'block' })

test('el footer muestra la versión del servidor en hora local y detecta actualizaciones', async ({ page }, info) => {
  const expectedBuild = JSON.parse(readFileSync(join(DIR, 'build-info.json'), 'utf8'))
  const binary = join(DIR, process.platform === 'win32' ? 'pulse.exe' : 'pulse')
  expect(JSON.parse(execFileSync(binary, ['version']).toString())).toEqual(expectedBuild)
  const actual = await page.request.get('/api/version')
  expect(actual.ok()).toBeTruthy()
  expect(actual.headers()['cache-control']).toBe('no-store')
  expect(await actual.json()).toEqual(expectedBuild)

  let serverVersion = { built_at: '2026-10-16T06:18:00Z', revision: '0123456789abcdef0123456789abcdef01234567' }
  let unreachable = false
  await page.route('**/api/version', (route) => unreachable
    ? route.abort('failed')
    : route.fulfill({ json: serverVersion, headers: { 'Cache-Control': 'no-store' } }))
  await register(page)
  const footer = page.locator('.workspace-footer')
  const version = footer.getByTestId('server-version')
  await expect(footer).toContainText('Un gasto a la vez.')
  await expect(version).toContainText('V2026-10-16 01:18:00')
  await expect(version).toContainText('0123456')
  await expect(version).toHaveAttribute('title', /UTC: 2026-10-16T06:18:00Z/)
  await expect(version.locator('time')).toHaveAttribute('datetime', serverVersion.built_at)

  // Returning to the page consults the server again, without reloading the PWA.
  serverVersion = { built_at: '2026-10-16T07:19:02Z', revision: 'abcdef0123456789abcdef0123456789abcdef01' }
  const refreshed = page.waitForResponse((r) => r.url().endsWith('/api/version') && r.ok())
  await page.evaluate(() => window.dispatchEvent(new Event('visibilitychange')))
  await refreshed
  await expect(version).toContainText('V2026-10-16 02:19:02')
  await expect(version).toContainText('abcdef0')

  unreachable = true
  await page.evaluate(() => window.dispatchEvent(new Event('visibilitychange')))
  await expect(version).toContainText('sin verificar')
  await expect(version).toContainText('V2026-10-16 02:19:02')
  if (info.project.name === 'movil') await page.setViewportSize({ width: 320, height: 740 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBeTruthy()
  await expectAccessible(page, 'Versión del servidor en el footer')
  await page.screenshot({ path: info.outputPath('version-footer.png'), fullPage: true })
  await footer.screenshot({ path: info.outputPath('version-footer-detail.png') })
  await page.evaluate(() => document.documentElement.dataset.theme = 'dark')
  await expectAccessible(page, 'Versión del servidor en oscuro')
})
