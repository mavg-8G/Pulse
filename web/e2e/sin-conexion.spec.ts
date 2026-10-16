import type { BrowserContext, Page } from '@playwright/test'
import { apiGet, apiPost, BASE_URL, expect, expectAccessible, register, test } from './helpers'

async function controlled(page: Page) {
  await page.waitForFunction(async () => (await navigator.serviceWorker.getRegistration())?.active?.state === 'activated', null, { timeout: 15_000 })
}

// Chromium no aplica setOffline a lo que pide el service worker: además se corta la API a nivel de contexto,
// que sí intercepta esas peticiones. page.request (el "otro dispositivo") no pasa por aquí.
async function offline(context: BrowserContext) {
  await context.route('**/api/**', (r) => r.abort('internetdisconnected'))
  await context.setOffline(true)
}

async function online(context: BrowserContext) {
  await context.unroute('**/api/**')
  await context.setOffline(false)
}

async function seed(page: Page) {
  const accounts = await apiGet(page, '/api/accounts')
  const cats = await apiGet(page, '/api/categories')
  const comida = cats.find((c: any) => c.name === 'Comida').id
  const now = new Date().toISOString()
  const a = await apiPost(page, '/api/transactions', { account_id: accounts[0].id, category_id: comida, amount_cents: 1000, occurred_at: now, merchant: 'Supermaxi' })
  const b = await apiPost(page, '/api/transactions', { account_id: accounts[0].id, category_id: comida, amount_cents: 500, occurred_at: now, merchant: 'Farmacia' })
  return { a, b, account: accounts[0].id, comida }
}

const month = () => new Intl.DateTimeFormat('en-CA', { timeZone: 'America/Guayaquil', year: 'numeric', month: '2-digit' }).format(new Date())

test('sin conexión: muestra lo último guardado, edita, borra y crea; al reconectar llega una sola vez', async ({ page, context }) => {
  await register(page)
  const { a, b } = await seed(page)
  await controlled(page)
  for (const path of ['/', '/plan', '/movimientos']) {
    await page.goto(path) // en línea: se guarda cifrado en el dispositivo
    await page.waitForLoadState('networkidle')
  }
  await expect(page.getByText('Supermaxi')).toBeVisible()
  const writes: string[] = []
  page.on('response', (r) => {
    if (r.url().includes('/api/transactions') && r.request().method() !== 'GET') writes.push(`${r.request().method()} ${r.status()}`)
  })

  await offline(context)
  await page.goto('/movimientos') // página nueva sin red: la sirve el service worker con datos del almacén cifrado
  await expect(page.getByText('Supermaxi')).toBeVisible()
  await expect(page.getByTestId('sync-status')).toHaveText(/Sin conexión/)
  await expectAccessible(page, 'Movimientos sin conexión')

  await page.getByText('Supermaxi').click()
  const edit = page.getByRole('dialog', { name: 'Editar gasto' })
  await edit.getByLabel('Monto').fill('12')
  await edit.getByRole('button', { name: 'Guardar cambios' }).click()
  await expect(page.getByText(/se enviará al volver la conexión/).first()).toBeVisible()
  await expect(page.getByRole('button', { name: /Supermaxi/ }).getByText('Pendiente de enviar')).toBeVisible()

  await page.getByText('Farmacia').click()
  await page.getByRole('dialog', { name: 'Editar gasto' }).getByRole('button', { name: 'Borrar gasto' }).click()
  await page.getByRole('alertdialog').getByRole('button', { name: 'Borrar gasto' }).click()
  await expect(page.getByText('Farmacia')).toHaveCount(0)

  await page.getByRole('button', { name: 'Nuevo gasto' }).filter({ visible: true }).first().click()
  const sheet = page.getByRole('dialog', { name: 'Nuevo gasto' })
  await sheet.getByLabel('Monto').fill('3,50')
  await sheet.getByRole('button', { name: /Comida/ }).click()
  await sheet.getByLabel('Comercio').fill('Café sin red')
  await sheet.getByRole('button', { name: 'Guardar gasto' }).click()
  await expect(page.getByText('Café sin red')).toBeVisible()
  await expect(page.getByTestId('sync-status')).toHaveText('3 pendientes de enviar')
  expect(writes).toEqual([]) // nada llegó al servidor

  await online(context)
  await page.evaluate(() => window.dispatchEvent(new Event('online')))
  await expect(page.getByText('Se enviaron 3 cambios hechos sin conexión')).toBeVisible()
  await expect(page.getByTestId('sync-status')).toHaveCount(0)

  const list = await apiGet(page, `/api/transactions?month=${month()}`)
  const byName = (m: string) => list.items.filter((t: any) => t.merchant === m)
  expect(byName('Supermaxi')).toMatchObject([{ id: a.id, amount_cents: 1200, version: 2 }])
  expect(byName('Farmacia')).toEqual([])
  expect(byName('Café sin red')).toHaveLength(1)
  expect(list.items.find((t: any) => t.id === b.id)).toBeUndefined()
  expect(writes.filter((w) => w.startsWith('PATCH'))).toEqual(['PATCH 200'])
  expect(writes.filter((w) => w.startsWith('POST'))).toEqual(['POST 201'])

  // Reintentar no duplica: la cola está vacía.
  await page.reload()
  await expect(page.getByText('Café sin red')).toHaveCount(1)
})

test('conflictos: cambio y borrado en otro dispositivo se resuelven eligiendo', async ({ page, context }) => {
  await register(page)
  const { a, b } = await seed(page)
  await page.goto('/movimientos')
  await expect(page.getByText('Supermaxi')).toBeVisible()
  // "Otro dispositivo": cambia Supermaxi y borra Farmacia después de que esta pantalla los cargó.
  const other = await page.request.patch(`/api/transactions/${a.id}`, { data: { amount_cents: 1500 }, headers: { Origin: BASE_URL } })
  expect(other.ok()).toBeTruthy()
  expect((await page.request.delete(`/api/transactions/${b.id}`, { headers: { Origin: BASE_URL } })).status()).toBe(204)

  await offline(context)
  for (const [name, amount] of [['Supermaxi', '20'], ['Farmacia', '7']]) {
    await page.getByText(name).click()
    const edit = page.getByRole('dialog', { name: 'Editar gasto' })
    await edit.getByLabel('Monto').fill(amount)
    await edit.getByRole('button', { name: 'Guardar cambios' }).click()
    await expect(edit).toHaveCount(0)
  }
  await online(context)
  await page.evaluate(() => window.dispatchEvent(new Event('online')))
  const dialog = page.getByRole('dialog', { name: 'Cambios en este dispositivo' })
  await expect(dialog).toBeVisible()
  await expect(dialog.getByText('En el servidor ahora: Supermaxi · $15,00')).toBeVisible()
  await expectAccessible(page, 'Conflictos')
  await dialog.getByRole('group', { name: /Supermaxi/ }).getByRole('button', { name: 'Usar la mía' }).click()
  await dialog.getByRole('group', { name: /Farmacia/ }).getByRole('button', { name: 'Volver a crearlo con mis cambios' }).click()
  await expect(page.getByTestId('sync-status')).toHaveCount(0)

  const list = await apiGet(page, `/api/transactions?month=${month()}`)
  expect(list.items.find((t: any) => t.id === a.id)).toMatchObject({ amount_cents: 2000, version: 3 })
  const recreated = list.items.filter((t: any) => t.merchant === 'Farmacia')
  expect(recreated).toHaveLength(1)
  expect(recreated[0]).toMatchObject({ amount_cents: 700 })
  expect(recreated[0].id).not.toBe(b.id)
})

test('cerrar sesión avisa de lo pendiente y borra el almacén; otra persona no ve nada', async ({ page, context }) => {
  await register(page)
  await seed(page)
  await page.goto('/movimientos')
  await expect(page.getByText('Supermaxi')).toBeVisible()
  // La red falla solo para esta edición: queda en la cola aunque la app siga en línea.
  await context.route('**/api/transactions/*', (r) => (r.request().method() === 'PATCH' ? r.abort('internetdisconnected') : r.continue()))
  await page.getByText('Supermaxi').click()
  await page.getByRole('dialog', { name: 'Editar gasto' }).getByLabel('Monto').fill('99')
  await page.getByRole('dialog', { name: 'Editar gasto' }).getByRole('button', { name: 'Guardar cambios' }).click()
  await expect(page.getByTestId('sync-status')).toHaveText('1 pendiente de enviar')

  // Sin red no se puede cerrar sesión (seguiría abierta en el servidor).
  await page.getByRole('link', { name: 'Mi cuenta' }).filter({ visible: true }).first().click()
  await offline(context)
  await page.getByRole('button', { name: 'Cerrar sesión', exact: true }).last().click()
  await page.getByRole('alertdialog').getByRole('button', { name: 'Cerrar sesión y borrarlos' }).click()
  await expect(page.getByText('Sin conexión: conéctate para cerrar la sesión.')).toBeVisible()
  await online(context)

  await page.getByRole('button', { name: 'Cerrar sesión', exact: true }).last().click()
  const warn = page.getByRole('alertdialog', { name: /sin enviar/ })
  await expect(warn).toContainText('Están guardados solo en este dispositivo')
  await expectAccessible(page, 'Aviso de cambios sin enviar')
  await warn.getByRole('button', { name: 'Cerrar sesión y borrarlos' }).click()
  await expect(page.getByRole('heading', { name: 'Entrar' })).toBeVisible()
  const left = await page.evaluate(async () => ({
    dbs: (await indexedDB.databases()).map((d) => d.name),
    last: localStorage.getItem('pulse-last-user'),
  }))
  expect(left.dbs.filter((n) => n?.startsWith('pulse-u-'))).toEqual([])
  expect(left.last).toBeNull()
  await context.unroute('**/api/transactions/*')

  // Otra persona en el mismo navegador: tiene su propio almacén y no hereda nada de la anterior.
  await register(page)
  await page.goto('/movimientos')
  await page.waitForLoadState('networkidle')
  const mine = await page.evaluate(async () => (await indexedDB.databases()).map((d) => d.name).filter((n) => n?.startsWith('pulse-u-')))
  const me = await apiGet(page, '/api/me')
  expect(mine).toEqual([`pulse-u-${me.id}`])
  await expect(page.getByText('Supermaxi')).toHaveCount(0)
  await expect(page.getByTestId('sync-status')).toHaveCount(0)
})
