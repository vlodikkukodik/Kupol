import { expect, test, type Page } from '@playwright/test'
import { mkdirSync } from 'node:fs'
import { makeZip } from './zip'

// Скриншоты всех экранов на обоих языках (десктоп и телефон) для визуальной проверки дизайна.
// Запуск: SHOTS=1 npx playwright test --project=shots  → картинки в /tmp/vh-shots
test.skip(!process.env.SHOTS, 'только по запросу: SHOTS=1')
test.setTimeout(240_000) // много скриншотов с ожиданием анимаций

const OUT = '/tmp/vh-shots'
mkdirSync(OUT, { recursive: true })

async function shot(page: Page, name: string) {
  await page.waitForTimeout(700) // дождаться анимаций появления
  await page.screenshot({ path: `${OUT}/${name}.png`, fullPage: true })
}

for (const loc of ['ru', 'it'] as const) {
  test(`экраны ${loc}: десктоп`, async ({ browser }) => {
    const ctx = await browser.newContext({ baseURL: 'http://127.0.0.1:5174', locale: loc === 'ru' ? 'ru-RU' : 'it-IT', viewport: { width: 1440, height: 900 } })
    const page = await ctx.newPage()
    const nav = page.getByRole('navigation').first()

    await page.goto('/login')
    await expect(page.locator('html')).toHaveAttribute('lang', loc)
    await shot(page, `${loc}-1-login`)
    await page.goto('/register?invite=DEMO')
    await page.locator('input').nth(1).fill('bad')
    await page.getByRole('button').last().click()
    await shot(page, `${loc}-2-register-errors`)

    await page.goto('/login')
    await page.locator('input').nth(0).fill(process.env.E2E_ADMIN!)
    await page.locator('input').nth(1).fill(process.env.E2E_ADMIN_PASSWORD!)
    await page.getByRole('button', { name: /Войти|Accedi/ }).click()
    await page.waitForURL('**/')
    await shot(page, `${loc}-3-dashboard-empty`)

    await nav.locator('a').nth(1).click() // сайты
    await page.waitForTimeout(600)
    // Сценарий можно гонять повторно: у админа лимит в один сайт, поэтому старый убираем.
    const del = page.getByRole('button', { name: /Удалить|Elimina/ })
    if (await del.count()) {
      await del.first().click()
      await page.locator('.n-popconfirm__action .n-button--primary-type').click()
      await page.waitForTimeout(600)
    }
    await page.locator('input[type=text]').first().fill(`demo${loc}`)
    await page.getByRole('button', { name: /Создать|Crea/ }).last().click()
    await page.waitForTimeout(500)
    await shot(page, `${loc}-4-sites-new`)

    await page.locator('input[type=file]').first().setInputFiles({
      name: 'site.zip',
      mimeType: 'application/zip',
      buffer: makeZip({ 'index.html': '<h1>Demo</h1>', 'css/style.css': 'body{margin:0}', 'js/app.js': 'console.log(1)', 'logo.svg': '<svg/>', 'data.json': '{}' }),
    })
    await page.waitForTimeout(800)
    await page.getByRole('button', { name: /Включить FTP|Attiva FTP/ }).click()
    await page.waitForTimeout(700)
    await shot(page, `${loc}-5-ftp-dialog`)
    await page.keyboard.press('Escape')
    await shot(page, `${loc}-6-sites-live`)

    await page.getByRole('button', { name: /Файлы|File/ }).first().click()
    await page.waitForTimeout(500)
    await page.getByRole('link', { name: 'index.html' }).click()
    await page.waitForTimeout(600)
    await shot(page, `${loc}-7-files-editor`)

    await nav.locator('a').nth(0).click()
    await shot(page, `${loc}-8-dashboard-data`)
    await nav.locator('a').nth(2).click()
    await page.getByRole('button', { name: /Создать инвайт|Crea invito/ }).click()
    await page.waitForTimeout(700)
    await shot(page, `${loc}-9-settings`)
    await ctx.close()
  })
}

test('экраны: телефон', async ({ browser }) => {
  const ctx = await browser.newContext({ baseURL: 'http://127.0.0.1:5174', locale: 'it-IT', viewport: { width: 390, height: 844 }, deviceScaleFactor: 2 })
  const page = await ctx.newPage()
  await page.goto('/login')
  await shot(page, 'm-1-login')
  await page.locator('input').nth(0).fill(process.env.E2E_ADMIN!)
  await page.locator('input').nth(1).fill(process.env.E2E_ADMIN_PASSWORD!)
  await page.getByRole('button', { name: 'Accedi' }).click()
  await page.waitForURL('**/')
  await shot(page, 'm-2-dashboard')
  await page.getByRole('navigation').first().locator('a').nth(1).click()
  await shot(page, 'm-3-sites')
  await ctx.close()
})
