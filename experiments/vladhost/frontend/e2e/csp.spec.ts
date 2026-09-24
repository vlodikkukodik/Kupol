import { expect, test } from '@playwright/test'

// Собранное приложение под тем же строгим CSP, что стоит на боевом nginx (deploy/nginx/vladhost-https.conf).
// Если что-то в сборке требует eval или внешних ресурсов, на бою оно бы молча не заработало — здесь это видно.
test('production-сборка работает под CSP без нарушений', async ({ page }) => {
  const violations: string[] = []
  page.on('console', (m) => {
    if (/Content Security Policy|Refused to/i.test(m.text())) violations.push(m.text())
  })
  page.on('pageerror', (e) => violations.push(`pageerror: ${e.message}`))
  page.on('requestfailed', (r) => violations.push(`requestfailed: ${r.url()}`))

  await page.goto('/login')
  await expect(page.getByRole('heading', { name: 'С возвращением' })).toBeVisible()

  // Переключение языка и перерисовка — самое «динамичное» место i18n.
  await page.getByRole('group', { name: 'Язык' }).getByRole('button', { name: /IT/ }).click()
  await expect(page.getByRole('heading', { name: 'Bentornato' })).toBeVisible()

  // Вход и обход страниц: редактор CodeMirror, модальные окна и таблицы тоже под CSP.
  await page.getByLabel('Email o nome utente').fill(process.env.E2E_ADMIN!)
  await page.getByLabel('Password').fill(process.env.E2E_ADMIN_PASSWORD!)
  await page.getByRole('button', { name: 'Accedi' }).click()
  await expect(page.getByText(`Ciao, ${process.env.E2E_ADMIN}`)).toBeVisible()
  const nav = page.getByRole('navigation', { name: 'Menu principale' })
  await nav.getByText('Siti').click()
  await expect(page.getByRole('heading', { name: 'Siti', exact: true })).toBeVisible()
  await nav.getByText('Impostazioni').click()
  await expect(page.getByRole('heading', { name: 'Impostazioni' })).toBeVisible()

  // Шрифт Inter загружен со своего origin (внешние ресурсы CSP запретил бы).
  const fontOk = await page.evaluate(() => document.fonts.check("16px 'Inter Variable'"))
  expect(fontOk).toBe(true)

  expect(violations).toEqual([])
})
