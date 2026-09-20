import { writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { resolve } from 'node:path'
import AxeBuilder from '@axe-core/playwright'
import { expect, test } from '@playwright/test'
import { canWrite, cli, signUp } from './helpers/kupol.js'
import { settle } from './helpers/ui.js'

// «Доска с нитками» (этап 4): граф связей между документами — только то, что читатель вправе видеть.
test.skip(!canWrite, 'создаёт документы и пользователей: против внешнего адреса не запускается')
// Документы общие для файла и удаляются в afterAll: параллельные воркеры удалили бы их друг у друга.
test.describe.configure({ mode: 'serial' })

const RUN = Math.random().toString(36).slice(2, 6).toUpperCase()
const C = { center: 'МЕМО-9621', near: 'МЕМО-9622', incoming: 'МЕМО-9623', far: 'МЕМО-9624', closed: 'МЕМО-9625', hiddenLink: 'МЕМО-9626' }
const FILE = resolve(tmpdir(), `kupol-e2e-graph-${RUN}.json`)

const link = (id, code, level) => ({ id, type: 'doc_link', ...(level ? { level } : {}), data: { code } })
const memo = (code, title, level, blocks) => ({ code, type: 'memo', title: `[e2e] ${title} ${RUN}`, status: 'published', level, composed: { year: 1981 }, blocks })

test.beforeAll(() => {
  writeFileSync(
    FILE,
    JSON.stringify({
      documents: [
        memo(C.center, 'Центр', 0, [link('a', C.near), link('b', C.closed)]),
        memo(C.near, 'Ближний', 0, [link('a', C.far)]),
        memo(C.incoming, 'Входящий', 0, [link('a', C.center)]),
        memo(C.far, 'Дальний', 0, [{ id: 'p', type: 'paragraph', data: { text: 'Текст' } }]),
        memo(C.closed, 'Закрытый', 5, [{ id: 'p', type: 'paragraph', data: { text: 'Текст' } }]),
        memo(C.hiddenLink, 'Закрытая нить', 0, [link('a', C.center, 4)]),
      ],
    }),
  )
  cli('doc', 'import', FILE)
})

test.afterAll(() => {
  for (const code of Object.values(C)) {
    try {
      cli('doc', 'delete', code, '--yes')
    } catch {
      // уже удалён
    }
  }
})

const card = (page, code) => page.locator(`a.card[data-code="${code}"]`)
const cardCodes = async (page) => (await page.locator('a.card').evaluateAll((els) => els.map((e) => e.getAttribute('data-code')))).sort()
const threads = async (page) => (await page.locator('path.thread').evaluateAll((els) => els.map((e) => e.getAttribute('data-edge')))).sort()

test('гость: доска показывает доступное; закрытое не появляется ни карточкой, ни нитью, ни в ответе', async ({ page }) => {
  await page.goto('/doc/MEMO-9621')
  await page.getByTestId('graph-link').click()
  await expect(page).toHaveURL(/\/graph\/MEMO-9621$/)
  await expect(page.getByTestId('board')).toBeVisible()
  // прямые связи: ближний (исходящий) и входящий; закрытый и закрытая нить — нет
  expect(await cardCodes(page)).toEqual([C.center, C.incoming, C.near].sort())
  expect(await threads(page)).toEqual([`${C.center}>${C.near}`, `${C.incoming}>${C.center}`].sort())
  await expect(card(page, C.center)).toHaveAttribute('aria-current', 'true')
  await expect(page.locator('body')).not.toContainText(C.closed)
  await expect(page.locator('body')).not.toContainText(C.hiddenLink)
  await expect(page.locator('body')).not.toContainText('Закрытый')
  const raw = await (await page.request.get('/api/graph/MEMO-9621?depth=2')).text()
  for (const secret of [C.closed, C.hiddenLink, 'Закрытый', 'Закрытая нить']) expect(raw).not.toContain(secret)

  // «через одного» добавляет дальнего
  await page.getByTestId('depth-2').click()
  await expect(page).toHaveURL(/depth=2/)
  await expect(card(page, C.far)).toBeVisible()
  expect(await cardCodes(page)).toEqual([C.center, C.far, C.incoming, C.near].sort())

  // то же списком: и документы, и ссылки (для тех, кто не видит или не может пользоваться доской)
  await expect(page.getByTestId('graph-nodes').locator('li')).toHaveCount(4)
  await expect(page.getByTestId('graph-edges').locator('li')).toHaveCount(3)
})

test('карточка ведёт к доске вокруг неё; клавиатура; центр ведёт к документу', async ({ page }) => {
  await page.goto('/graph/MEMO-9621')
  await expect(page.getByTestId('board')).toBeVisible()
  // клавиатура: карточка — ссылка, Enter переходит
  await card(page, C.near).focus()
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(/\/graph\/MEMO-9622$/)
  await expect(card(page, C.near)).toHaveAttribute('aria-current', 'true')
  expect(await cardCodes(page)).toContain(C.far)
  expect(await cardCodes(page)).toContain(C.center)
  await page.getByTestId('graph-center-link').click()
  await expect(page).toHaveURL(/\/doc\/MEMO-9622$/)
})

test('допуск расширяет доску ровно до своего уровня', async ({ browser }) => {
  const four = await browser.newContext()
  await signUp(four, { level: 4 })
  const p4 = await four.newPage()
  await p4.goto('/graph/MEMO-9621')
  await expect(p4.getByTestId('board')).toBeVisible()
  // блок-ссылка уровня 4 виден, документ уровня 5 — нет
  expect(await cardCodes(p4)).toEqual([C.center, C.hiddenLink, C.incoming, C.near].sort())
  await four.close()

  const five = await browser.newContext()
  await signUp(five, { level: 5 })
  const p5 = await five.newPage()
  await p5.goto('/graph/MEMO-9621')
  await expect(p5.getByTestId('board')).toBeVisible()
  expect(await cardCodes(p5)).toEqual([C.center, C.closed, C.hiddenLink, C.incoming, C.near].sort())
  await five.close()
})

test('закрытый или несуществующий центр — «Дело не найдено»; документ без связей — понятное сообщение', async ({ page }) => {
  await page.goto('/graph/MEMO-9625')
  await expect(page.getByText('Дело не найдено').first()).toBeVisible()
  await page.goto('/graph/MEMO-9999')
  await expect(page.getByText('Дело не найдено').first()).toBeVisible()
  await page.goto('/graph/MEMO-9624')
  await expect(page.getByTestId('graph')).toBeVisible()
  // у «Дальнего» есть входящая нить от «Ближнего»: доска не пуста
  expect(await cardCodes(page)).toEqual([C.far, C.near].sort())
})

test('телефон 375 px и доступность: доска прокручивается внутри, страница не ползёт', async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 800 })
  await page.goto('/graph/MEMO-9621?depth=2')
  await expect(page.getByTestId('board')).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBeLessThanOrEqual(0)
  await page.setViewportSize({ width: 1200, height: 900 })
  await settle(page)
  const r = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()
  expect(r.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(' ')).slice(0, 3).join(' | ')}`)).toEqual([])
})
