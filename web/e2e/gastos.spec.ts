import { apiGet, apiPost, expect, register, test } from './helpers'

test('registrar, editar, buscar y borrar un gasto', async ({ page }) => {
  await register(page)
  await apiPost(page, '/api/accounts', { name: 'Visa Pichincha', type: 'credit_card', card_suffix: '989', institution: 'Banco Pichincha' })

  // Nuevo gasto desde el botón principal (tab bar en móvil, barra lateral en escritorio)
  await page.getByRole('button', { name: 'Nuevo gasto' }).first().click()
  const sheet = page.getByRole('dialog', { name: 'Nuevo gasto' })
  await sheet.getByLabel('Monto').fill('17,68')
  await sheet.getByRole('button', { name: /Comida/ }).click()
  await sheet.getByLabel('Cuenta').selectOption({ label: 'Visa Pichincha (…989)' })
  await sheet.getByLabel('Comercio').fill("Carl's Jr Albán Borja")
  await sheet.getByRole('button', { name: 'Guardar gasto' }).click()
  await expect(page.getByText('Gasto guardado')).toBeVisible()
  await expect(page.getByTestId('month-total')).toHaveText(/17,68/)

  // Monto inválido: no se guarda y avisa
  await page.getByRole('button', { name: 'Nuevo gasto' }).first().click()
  await sheet.getByLabel('Monto').fill('17,689,1')
  await sheet.getByRole('button', { name: /Transporte/ }).click()
  await sheet.getByRole('button', { name: 'Guardar gasto' }).click()
  await expect(sheet.getByRole('alert')).toContainText('monto válido')
  await sheet.getByRole('button', { name: 'Cerrar' }).click()

  // Movimientos: búsqueda sin tildes
  await page.goto('/movimientos')
  await page.getByLabel('Buscar').fill('alban')
  await expect(page.getByText("Carl's Jr Albán Borja")).toBeVisible()
  await page.getByLabel('Buscar').fill('netflix')
  await expect(page.getByText('Nada coincide')).toBeVisible()
  await page.getByLabel('Buscar').fill('')

  // Editar
  await page.getByText("Carl's Jr Albán Borja").click()
  const edit = page.getByRole('dialog', { name: 'Editar gasto' })
  await edit.getByLabel('Monto').fill('20')
  await edit.getByRole('button', { name: 'Guardar cambios' }).click()
  await expect(page.getByTestId('list-sum')).toHaveText(/20,00/)

  // Borrar: pide confirmación en un diálogo propio; Cancelar no borra nada.
  await page.getByText("Carl's Jr Albán Borja").click()
  await edit.getByRole('button', { name: 'Borrar gasto' }).click()
  const confirmDel = page.getByRole('alertdialog', { name: '¿Borrar este gasto?' })
  await expect(confirmDel.getByRole('button', { name: 'Cancelar' })).toBeFocused()
  await confirmDel.getByRole('button', { name: 'Cancelar' }).click()
  await expect(confirmDel).toBeHidden()
  await edit.getByRole('button', { name: 'Borrar gasto' }).click()
  await confirmDel.getByRole('button', { name: 'Borrar gasto' }).click()
  await expect(page.getByText('Gasto borrado')).toBeVisible()
  const list = await apiGet(page, '/api/transactions')
  expect(list.total).toBe(0)
})

test('las cuentas validan los últimos dígitos de la tarjeta', async ({ page }) => {
  await register(page)
  await page.goto('/ajustes/cuentas')
  await page.getByRole('button', { name: 'Agregar cuenta' }).click()
  const sheet = page.getByRole('dialog', { name: 'Nueva cuenta' })
  await sheet.getByLabel('Nombre').fill('Titanium Visa')
  await sheet.getByLabel('Banco o emisor').fill('Diners Club')
  await sheet.getByLabel('Últimos dígitos').fill('666')
  await sheet.getByRole('button', { name: 'Guardar cuenta' }).click()
  await expect(page.getByText('termina en 666')).toBeVisible()
})
