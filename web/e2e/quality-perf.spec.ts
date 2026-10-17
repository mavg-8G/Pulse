import { mkdirSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { apiGet, apiPost, expect, register, test } from './helpers'

// Ejecución explícita: PULSE_PERF_LABEL=before|after. Mismo Chromium, producción, datos y red.
test('medición reproducible de recorridos', async ({ page, browserName }, info) => {
  test.skip(!process.env.PULSE_PERF_LABEL, 'Medición optativa; no añade carga a la suite ordinaria')
  test.skip(info.project.name !== 'escritorio', 'La referencia usa el perfil de escritorio')
  test.setTimeout(300_000)
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.addInitScript(() => {
    ;(window as any).__shifts = 0
    new PerformanceObserver((list) => { for (const e of list.getEntries()) if (!(e as any).hadRecentInput) (window as any).__shifts += (e as any).value }).observe({ type: 'layout-shift', buffered: true })
  })
  const samples: unknown[] = []
  for (const count of [0, 100, 5000]) {
    await register(page)
    const a = await apiPost(page, '/api/accounts', { name: 'Cuenta de medición', type: 'cash' })
    const cats = await apiGet(page, '/api/categories')
    const me = await apiGet(page, '/api/me')
    const month = new Intl.DateTimeFormat('en-CA', { timeZone: me.timezone, year: 'numeric', month: '2-digit' }).format(new Date())
    for (let start = 0; start < count; start += 25) await Promise.all(Array.from({ length: Math.min(25, count - start) }, (_, i) => apiPost(page, '/api/transactions', {
      account_id: a.id, category_id: cats.find((c: any) => !c.parent_id).id, amount_cents: 100 + i, occurred_at: `${month}-01T12:00`, merchant: `Medición ${start + i}`,
    })))
    for (const [path, selector] of [['/', '[data-testid="month-total"]'], ['/movimientos', count ? '.tx' : '.empty'], ['/plan', '#h-breakdown'], ['/reportes', '#resumen']] as const) {
      for (let run = 0; run < 5; run++) {
        let requests = 0
        const record = (r: any) => { if (new URL(r.url()).pathname.startsWith('/api/')) requests++ }
        page.on('request', record)
        const start = Date.now()
        await page.goto(path)
        await expect(page.locator(selector).first()).toBeVisible({ timeout: 60_000 })
        const elapsed = Date.now() - start
        await page.waitForLoadState('networkidle')
        const cls = await page.evaluate(() => (window as any).__shifts)
        page.off('request', record)
        samples.push({ count, path, run, elapsed, requests, cls })
      }
    }
    await page.request.post('/api/auth/logout', { headers: { Origin: new URL(page.url()).origin } })
  }
  const dir = join(process.cwd(), '..', 'artifacts', 'quality')
  mkdirSync(dir, { recursive: true })
  writeFileSync(join(dir, `perf-${process.env.PULSE_PERF_LABEL}.json`), JSON.stringify({ cls_collectors: 1, browserName, viewport: page.viewportSize(), network: 'local sin limitación', motion: 'reduce', samples }, null, 2))
})
