import { expect, test } from '@playwright/test'

/** Собирает всё, что в консоли и сети выглядит как ошибка. */
function watchProblems(page) {
  const problems = []
  page.on('pageerror', (e) => problems.push(`pageerror: ${e.message}`))
  page.on('console', (m) => {
    if (m.type() === 'error') problems.push(`console.error: ${m.text()}`)
  })
  page.on('response', (r) => {
    if (r.status() >= 400 && r.url().includes('/api/')) problems.push(`HTTP ${r.status()} ${r.url()}`)
    if (r.status() >= 400 && !r.url().includes('/api/')) problems.push(`HTTP ${r.status()} ${r.url()}`)
  })
  page.on('requestfailed', (r) => problems.push(`requestfailed: ${r.url()} ${r.failure()?.errorText}`))
  return problems
}

test('главная: «дверь» показана, связь с архивом установлена по всей цепочке, ошибок нет', async ({ page }) => {
  const problems = watchProblems(page)
  await page.goto('/')

  await expect(page.locator('h1')).toHaveText('Купол')
  await expect(page.getByText('Комитет Управления Паранормальными Объектами и Локациями').first()).toBeVisible()
  // примечание автора — только мелким шрифтом в подвале, не на самой странице
  await expect(page.locator('footer .disclaimer')).toContainText('художественный вымысел')
  await expect(page.locator('main').getByText('художественный вымысел')).toHaveCount(0)
  await expect(page).toHaveTitle('КУПОЛ — Центральный архив')

  // Индикатор загорается только после реального ответа Go API через PHP-прокси
  await expect(page.locator('.status')).toHaveText('Связь с архивом: установлена')
  expect(problems).toEqual([])

  await page.screenshot({ path: 'e2e/results/home-desktop.png', fullPage: true })
})

test('шрифты: свои файлы, кириллица PT Mono и Oswald реально загружены', async ({ page, baseURL }) => {
  const fontUrls = []
  page.on('response', (r) => {
    if (/\.woff2?$/.test(new URL(r.url()).pathname)) fontUrls.push(new URL(r.url()).host)
  })
  await page.goto('/')
  await expect(page.locator('.status')).toHaveText('Связь с архивом: установлена')

  const loaded = await page.evaluate(async () => {
    await document.fonts.ready
    return [...document.fonts].filter((f) => f.status === 'loaded').map((f) => f.family.replaceAll('"', ''))
  })
  expect(loaded).toContain('PT Mono')
  expect(loaded).toContain('Oswald')
  expect(fontUrls.length).toBeGreaterThan(0)
  expect(new Set(fontUrls)).toEqual(new Set([new URL(baseURL).host])) // только свои, никаких внешних CDN

  const families = await page.evaluate(() => ({
    body: getComputedStyle(document.body).fontFamily,
    h1: getComputedStyle(document.querySelector('h1')).fontFamily,
  }))
  expect(families.body).toContain('PT Mono')
  expect(families.h1).toContain('Oswald')
})

test('API через PHP-прокси: подпись принята Go, служебные заголовки не текут', async ({ request }) => {
  const res = await request.get('/api/health')
  expect(res.status()).toBe(200)
  const body = await res.json()
  expect(body).toMatchObject({ status: 'ok', db: 'ok', ip_source: 'proxy' })
  expect(res.headers()['x-request-id']).toMatch(/^[a-f0-9]{16}$/)
  expect(res.headers()['x-powered-by']).toBeUndefined()
  expect(res.headers()['cache-control']).toBe('no-store')
})

test('API: 404 в формате ошибки архива', async ({ request }) => {
  const res = await request.get('/api/no-such-thing')
  expect(res.status()).toBe(404)
  expect(await res.json()).toMatchObject({ error: { code: 'not_found', message: 'Дело не найдено' } })
})

test('несуществующий адрес: «Дело не найдено / изъято»', async ({ page }) => {
  await page.goto('/doc/O-999')
  await expect(page.locator('h1')).toHaveText('Дело не найдено')
  await expect(page.getByText('Изъято', { exact: true })).toBeVisible()
  await expect(page).toHaveTitle('Дело не найдено — КУПОЛ')
  await page.screenshot({ path: 'e2e/results/not-found.png' })
  await page.getByRole('link', { name: 'На главную', exact: true }).click()
  await expect(page.locator('h1')).toHaveText('Купол')
})

test('телефон 375 px: нет горизонтальной прокрутки', async ({ browser }) => {
  const ctx = await browser.newContext({ viewport: { width: 375, height: 667 }, deviceScaleFactor: 2, hasTouch: true })
  const page = await ctx.newPage()
  await page.goto('/')
  await expect(page.locator('.status')).toHaveText('Связь с архивом: установлена')
  const { scrollWidth, clientWidth } = await page.evaluate(() => ({
    scrollWidth: document.documentElement.scrollWidth,
    clientWidth: document.documentElement.clientWidth,
  }))
  expect(scrollWidth).toBeLessThanOrEqual(clientWidth)
  await page.screenshot({ path: 'e2e/results/home-mobile.png', fullPage: true })
  await ctx.close()
})

test('клавиатура: «К содержимому» появляется по Tab и переносит фокус на main', async ({ page }) => {
  await page.goto('/')
  await page.keyboard.press('Tab')
  const skip = page.locator('a.skip-link')
  await expect(skip).toBeFocused()
  expect((await skip.boundingBox()).y).toBeGreaterThanOrEqual(0)
  await page.keyboard.press('Enter')
  await expect(page.locator('main#content')).toBeFocused()
})

test('prefers-reduced-motion: анимации отключены', async ({ browser }) => {
  const ctx = await browser.newContext({ reducedMotion: 'reduce' })
  const page = await ctx.newPage()
  await page.goto('/doc/O-999')
  const stamp = page.locator('.stamp')
  await expect(stamp).toBeVisible() // страница 404 подгружается лениво
  const duration = await stamp.evaluate((el) => getComputedStyle(el).animationDuration)
  expect(parseFloat(duration)).toBeLessThan(0.001)

  // Контроль: без reduced-motion та же анимация действительно идёт (тест не пустой)
  const normal = await browser.newContext({ reducedMotion: 'no-preference' })
  const p2 = await normal.newPage()
  await p2.goto('/doc/O-999')
  const d2 = await p2.locator('.stamp').evaluate((el) => getComputedStyle(el).animationDuration)
  expect(parseFloat(d2)).toBeGreaterThan(0.3)
  await normal.close()
  await ctx.close()
})
