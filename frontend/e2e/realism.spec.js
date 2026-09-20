import { writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { resolve } from 'node:path'
import AxeBuilder from '@axe-core/playwright'
import { expect, test } from '@playwright/test'
import { canWrite, cli, signUp } from './helpers/kupol.js'
import { settle } from './helpers/ui.js'

// «Реалистичность архива» (этап 4): гриф по уровню, архивный шифр, номер экземпляра, лист ознакомления, зачернения разной ширины.
test.skip(!canWrite, 'нужны фикстуры и пользователи: против внешнего адреса не запускается')
// Документ для листа ознакомления общий для файла и удаляется в afterAll — параллельные воркеры мешали бы друг другу.
test.describe.configure({ mode: 'serial' })

const RUN = Math.random().toString(36).slice(2, 6).toUpperCase()
const CODE = 'МЕМО-9631'
const FILE = resolve(tmpdir(), `kupol-e2e-realism-${RUN}.json`)

test.beforeAll(() => {
  writeFileSync(
    FILE,
    JSON.stringify({
      code: CODE,
      type: 'memo',
      title: `[e2e] Лист ознакомления ${RUN}`,
      status: 'published',
      level: 0,
      composed: { year: 1984 },
      blocks: [{ id: 'a', type: 'paragraph', data: { text: 'Короткий документ для проверки листа ознакомления.' } }],
    }),
  )
  cli('doc', 'import', FILE)
})

test.afterAll(() => {
  try {
    cli('doc', 'delete', CODE, '--yes')
  } catch {
    // уже удалён
  }
})

const strip = (page, which) => page.getByTestId(`strip-${which}`)

test('гость: гриф «Несекретно» сверху и снизу, архивный шифр, «экз. б/н»', async ({ page }) => {
  await page.goto('/doc/O-9001')
  await expect(page.getByRole('heading', { level: 1 })).toContainText('Открытый объект')
  await expect(page.getByTestId('classification')).toHaveText('Несекретно')
  await expect(strip(page, 'top')).toContainText('Форма КУПОЛ-1')
  await expect(strip(page, 'bottom')).toContainText('Несекретно')
  await expect(strip(page, 'bottom')).toContainText('экз. б/н')
  const mark = await page.getByTestId('archive-mark').innerText()
  expect(mark).toMatch(/^Фонд 1 · Опись 1979 · Дело 9001 · Листов \d+ · экз\. б\/н$/i)
})

test('гриф растёт с уровнем документа; у каждого читателя свой номер экземпляра', async ({ browser }) => {
  const a = await browser.newContext()
  await signUp(a, { level: 2 })
  const pa = await a.newPage()
  await pa.goto('/doc/O-9002') // документ уровня 2
  await expect(pa.getByTestId('classification')).toHaveText('Конфиденциально')
  const copyA = /экз\. № (\d{4,})/i.exec(await pa.getByTestId('archive-mark').innerText())?.[1]
  expect(copyA).toBeTruthy()
  await expect(strip(pa, 'bottom')).toContainText(`экз. № ${copyA}`)

  const b = await browser.newContext()
  await signUp(b, { level: 2 })
  const pb = await b.newPage()
  await pb.goto('/doc/O-9002')
  const copyB = /экз\. № (\d{4,})/i.exec(await pb.getByTestId('archive-mark').innerText())?.[1]
  expect(copyB).toBeTruthy()
  expect(copyB).not.toBe(copyA)
  // тот же читатель — тот же номер
  await pa.reload()
  await expect(pa.getByTestId('archive-mark')).toContainText(`экз. № ${copyA}`)
  await a.close()
  await b.close()

  const five = await browser.newContext()
  await signUp(five, { level: 5 })
  const p5 = await five.newPage()
  await p5.goto('/doc/O-9003') // документ уровня 5
  await expect(p5.getByTestId('classification')).toHaveText('Особой важности')
  await five.close()
})

test('лист ознакомления: считает читателей без имён; гость до первого читателя раздела не видит', async ({ browser, page, baseURL }) => {
  await page.goto(`/doc/MEMO-9631`)
  await expect(page.getByRole('heading', { level: 1 })).toContainText('Лист ознакомления')
  await expect(page.getByTestId('acquaint')).toHaveCount(0)

  const readers = []
  for (let i = 0; i < 2; i++) {
    const ctx = await browser.newContext()
    const login = await signUp(ctx, { level: 1 })
    const p = await ctx.newPage()
    await p.goto('/doc/MEMO-9631')
    await expect(p.getByTestId('acquaint')).toContainText(`Ознакомлено читателей: ${i + 1}`) // сам читатель уже в счёте
    await p.reload() // повторное чтение не считается
    await expect(p.getByTestId('acquaint')).toContainText(`Ознакомлено читателей: ${i + 1}`)
    readers.push({ ctx, login })
  }
  await page.reload()
  await expect(page.getByTestId('acquaint')).toContainText('Ознакомлено читателей: 2')
  const raw = await (await page.request.get('/api/documents/MEMO-9631')).text()
  for (const { login } of readers) expect(raw).not.toContain(login)
  expect(new URL(baseURL).origin).toBeTruthy()
  for (const { ctx } of readers) await ctx.close()
})

test('зачернённые фрагменты разной ширины, но у каждого места — всегда одна и та же', async ({ page }) => {
  await page.goto('/doc/O-9001')
  const bars = page.locator('.redacted')
  await expect(bars.first()).toBeVisible()
  const widths = async () => bars.evaluateAll((els) => els.map((e) => e.style.width))
  const first = await widths()
  expect(first.length).toBeGreaterThanOrEqual(2)
  for (const w of first) expect(parseFloat(w)).toBeGreaterThanOrEqual(3.5)
  for (const w of first) expect(parseFloat(w)).toBeLessThanOrEqual(9)
  expect(new Set(first).size).toBeGreaterThan(1) // не все одинаковые
  await page.reload()
  await expect(bars.first()).toBeVisible()
  expect(await widths()).toEqual(first) // при перезагрузке ширина не «дрожит»
})

test('телефон 375 px и доступность: лист с колонтитулами и листом ознакомления', async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 800 })
  await page.goto('/doc/MEMO-9631')
  await expect(page.getByTestId('strip-bottom')).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBeLessThanOrEqual(0)
  await page.setViewportSize({ width: 1200, height: 900 })
  await settle(page)
  const r = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()
  expect(r.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(' ')).slice(0, 3).join(' | ')}`)).toEqual([])
})
