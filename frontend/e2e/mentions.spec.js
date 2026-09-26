import { writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { resolve } from 'node:path'
import AxeBuilder from '@axe-core/playwright'
import { expect, test } from '@playwright/test'
import { canWrite, cli, signUp } from './helpers/kupol.js'
import { settle } from './helpers/ui.js'

// «Упоминается в» (этап 4): кто ссылается на документ и кто это видит — документ-источник и сам блок-ссылка проверяются отдельно.
test.skip(!canWrite, 'создаёт документы и пользователей: против внешнего адреса не запускается')
// Документы общие для файла и удаляются в afterAll: параллельные воркеры удалили бы их друг у друга.
test.describe.configure({ mode: 'serial' })

const RUN = Math.random().toString(36).slice(2, 6).toUpperCase()
const CODES = { target: 'МЕМО-9611', open: 'МЕМО-9612', closed: 'МЕМО-9613', hidden: 'МЕМО-9614', draft: 'МЕМО-9615' }
const FILE = resolve(tmpdir(), `kupol-e2e-mentions-${RUN}.json`)

const memo = (code, title, status, level, blocks) => ({ code, type: 'memo', title, status, level, composed: { year: 1980 }, blocks })
const link = (level) => ({ id: 'l', type: 'doc_link', ...(level ? { level } : {}), data: { code: CODES.target, note: 'см. также' } })

test.beforeAll(() => {
  writeFileSync(
    FILE,
    JSON.stringify({
      documents: [
        memo(CODES.target, `[e2e] Цель ссылок ${RUN}`, 'published', 0, [{ id: 'a', type: 'paragraph', data: { text: 'Цель.' } }]),
        memo(CODES.open, `[e2e] Открытый источник ${RUN}`, 'published', 0, [link(0)]),
        memo(CODES.closed, `[e2e] Закрытый источник ${RUN}`, 'published', 5, [link(0)]),
        memo(CODES.hidden, `[e2e] Закрытая ссылка ${RUN}`, 'published', 0, [link(4)]),
        memo(CODES.draft, `[e2e] Черновик-источник ${RUN}`, 'draft', 0, [link(0)]),
      ],
    }),
  )
  cli('doc', 'import', FILE)
})

test.afterAll(() => {
  for (const code of Object.values(CODES)) {
    try {
      cli('doc', 'delete', code, '--yes')
    } catch {
      // уже удалён
    }
  }
})

const mentions = (page) => page.getByTestId('mentions')
const mentionCodes = async (page) => (await mentions(page).locator('li a').allTextContents()).map((t) => t.split(' — ')[0].trim())

async function open(page) {
  await page.goto('/doc/MEMO-9611')
  await expect(page.getByRole('heading', { level: 1 })).toContainText('Цель ссылок')
}

test('гость видит только открытые источники; закрытое не упоминается ни в списке, ни в ответе сервера', async ({ page }) => {
  await open(page)
  await expect(mentions(page).getByRole('heading', { name: 'Упоминается в' })).toBeVisible()
  expect(await mentionCodes(page)).toEqual([CODES.open])
  await expect(page.locator('body')).not.toContainText(CODES.closed)
  await expect(page.locator('body')).not.toContainText(CODES.hidden)
  await expect(page.locator('body')).not.toContainText(CODES.draft)

  const raw = await (await page.request.get('/api/documents/MEMO-9611')).text()
  for (const secret of [CODES.closed, CODES.hidden, CODES.draft, 'Закрытый источник', 'Закрытая ссылка', 'Черновик-источник']) expect(raw).not.toContain(secret)

  // ссылка ведёт к источнику
  await mentions(page).getByRole('link', { name: new RegExp(CODES.open) }).click()
  await expect(page).toHaveURL(/\/doc\/MEMO-9612$/)
})

test('допуск читателя раскрывает упоминания ровно до его уровня; Директорат видит и черновик', async ({ browser }) => {
  const cases = [
    { level: 3, want: [CODES.open] },
    { level: 4, want: [CODES.open, CODES.hidden] }, // блок-ссылка уровня 4 виден, документ уровня 5 — нет
    { level: 5, want: [CODES.open, CODES.closed, CODES.hidden] },
  ]
  for (const { level, want } of cases) {
    const ctx = await browser.newContext()
    await signUp(ctx, { level })
    const page = await ctx.newPage()
    await open(page)
    expect(await mentionCodes(page), `уровень ${level}`).toEqual(want)
    await ctx.close()
  }
  const boss = await browser.newContext()
  await signUp(boss, { level: 6, directorate: true })
  const page = await boss.newPage()
  await open(page)
  expect(await mentionCodes(page)).toEqual([CODES.open, CODES.closed, CODES.hidden, CODES.draft])
  await boss.close()
})

test('документ без входящих ссылок не показывает раздел; телефон 375 px и доступность', async ({ page }) => {
  await page.goto('/doc/MEMO-9612')
  await expect(page.getByRole('heading', { level: 1 })).toBeVisible()
  await expect(mentions(page)).toHaveCount(0)

  await page.setViewportSize({ width: 375, height: 800 })
  await open(page)
  await expect(mentions(page)).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBeLessThanOrEqual(0)
  await page.setViewportSize({ width: 1200, height: 900 })
  await settle(page)
  const r = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()
  expect(r.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(' ')).slice(0, 3).join(' | ')}`)).toEqual([])
})
