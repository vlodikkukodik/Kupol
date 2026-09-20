import AxeBuilder from '@axe-core/playwright'
import { expect, test } from '@playwright/test'
import { canWrite } from './helpers/kupol.js'
import { createDoc, newAuthor, stored } from './helpers/team.js'

// Предпросмотр «глазами уровня N» (этап 3.4): документ строит сервер, браузер ничего не скрывает сам.
test.skip(!canWrite, 'создаёт пользователей и документы: против внешнего адреса не запускается')

const para = (id, text, extra = {}) => ({ id, type: 'paragraph', data: { text: [{ text }] }, ...extra })
const SECRETS = [
  { id: 'open', type: 'paragraph', data: { text: [{ text: 'Открытый текст. ' }, { text: 'СЕКРЕТ-ФРАГМЕНТ-3', level: 3 }, { text: ' Конец.' }] } },
  para('b4', 'СЕКРЕТ-БЛОК-4', { level: 4 }),
  para('b6', 'СЕКРЕТ-БЛОК-6', { level: 6 }),
  para('tail', 'Заключение.'),
]

const sheet = (page) => page.getByTestId('preview-sheet')
const level = (page, n) => page.getByTestId(`preview-level-${n}`)
const editorOf = (page) => page.locator('.kupol-editor .ProseMirror')

/** Выбирает уровень и возвращает тело ответа сервера: то, что реально пришло в браузер. */
async function pick(page, n) {
  const response = page.waitForResponse((r) => r.url().includes('/preview') && r.request().postDataJSON()?.level === n)
  await page.locator('label', { has: level(page, n) }).click()
  return (await response).text()
}

async function openPreview(page, id, query = '') {
  await page.goto(`/team/documents/${id}?tab=preview${query}`)
  await expect(page.getByTestId('preview')).toBeVisible()
}

test('уровни 0–7: закрытое не приходит в браузер вовсе, открытое видно, подпись считает закрытое', async ({ browser, baseURL }) => {
  const { context, page } = await newAuthor(browser)
  const id = await createDoc(page, baseURL, { blocks: SECRETS })
  await openPreview(page, id)

  await expect(sheet(page)).toContainText('Открытый текст.')
  await expect(level(page, 0)).toBeChecked()

  // уровень → (фрагмент-3, блок-4, блок-6) видны; блоки разных уровней рядом не сливаются
  // уровень 0 выбран с самого начала: его ответ проверяется последним, после смены на другой
  const want = [[2, [0, 0, 0]], [3, [1, 0, 0]], [4, [1, 1, 0]], [5, [1, 1, 0]], [6, [1, 1, 1]], [7, [1, 1, 1]], [0, [0, 0, 0]]]
  for (const [n, [f, b4, b6]] of want) {
    const body = await pick(page, n)
    // ответ сервера: закрытого в нём нет вообще — не «скрыто стилями», а не передано
    expect(body.includes('СЕКРЕТ-ФРАГМЕНТ-3'), `уровень ${n}: фрагмент в ответе`).toBe(Boolean(f))
    expect(body.includes('СЕКРЕТ-БЛОК-4'), `уровень ${n}: блок 4 в ответе`).toBe(Boolean(b4))
    expect(body.includes('СЕКРЕТ-БЛОК-6'), `уровень ${n}: блок 6 в ответе`).toBe(Boolean(b6))
    // и на листе
    const text = await sheet(page).innerText()
    expect(text.includes('СЕКРЕТ-ФРАГМЕНТ-3')).toBe(Boolean(f))
    expect(text.includes('СЕКРЕТ-БЛОК-4')).toBe(Boolean(b4))
    expect(text.includes('СЕКРЕТ-БЛОК-6')).toBe(Boolean(b6))
    expect(text).toContain('Заключение.')
  }

  await expect(level(page, 0)).toBeChecked()
  await expect(page.getByTestId('preview-stats')).toHaveText('Для этого читателя закрыто: 2 блока и 1 фрагмент.')
  await pick(page, 6)
  await expect(page.getByTestId('preview-stats')).toHaveText('Для этого читателя ничего не закрыто.')
  await context.close()
})

test('несохранённые правки попадают в предпросмотр, ничего не сохраняется, редактор при возврате цел', async ({ browser, baseURL }) => {
  const { context, page } = await newAuthor(browser)
  const id = await createDoc(page, baseURL, { blocks: [para('a', 'Основа')] })
  await page.goto(`/team/documents/${id}`)
  await expect(page.getByTestId('block-editor')).toBeVisible()

  const p = editorOf(page).locator('p', { hasText: 'Основа' })
  await p.click()
  await page.waitForFunction(() => document.querySelector('.ProseMirror').editor.state.selection.$from.node(1)?.attrs.blockId === 'a')
  await page.keyboard.press('End')
  await page.keyboard.type(' — правка')
  await expect(page.getByRole('button', { name: 'Отменить правки' })).toBeVisible()

  await page.getByTestId('tab-preview').click()
  await expect(page).toHaveURL(/tab=preview/)
  await expect(sheet(page)).toContainText('Основа — правка')
  await expect(page.getByTestId('preview')).toContainText('по несохранённым правкам')
  expect((await stored(page, id)).revision).toBe(1) // предпросмотр ничего не записал
  expect((await stored(page, id)).content.blocks[0].data.text[0].text).toBe('Основа')

  // назад в «Документ»: редактор на месте, история отмены не потеряна
  await page.getByRole('button', { name: 'Документ', exact: true }).click()
  await expect(editorOf(page).locator('p', { hasText: 'Основа — правка' })).toBeVisible()
  await editorOf(page).click()
  await page.keyboard.press('Control+z')
  await expect(editorOf(page).locator('p', { hasText: 'Основа' })).toHaveText('Основа')

  // правки отменены → предпросмотр снова по сохранённому
  await page.getByTestId('tab-preview').click()
  await expect(page.getByTestId('preview')).toContainText('по сохранённому документу')
  await context.close()
})

test('закрытый документ: «Дело не найдено» и «Доступ запрещён» — как получит читатель, содержимое не отдаётся', async ({ browser, baseURL }) => {
  const { context, page } = await newAuthor(browser)
  const hidden = await createDoc(page, baseURL, { title: 'Скрытое дело', level: 3, blocks: [para('a', 'СЕКРЕТ-ДЕЛА')] })
  const denied = await createDoc(page, baseURL, { title: 'Дело с отказом', level: 3, direct_link: 'forbidden', blocks: [para('a', 'СЕКРЕТ-ОТКАЗА')] })

  await openPreview(page, hidden, '&level=2')
  await expect(page.getByTestId('preview-verdict')).toHaveAttribute('data-access', 'not_found')
  await expect(page.getByTestId('preview-verdict')).toContainText('«Дело не найдено»')
  await expect(page.getByTestId('preview-verdict')).toContainText('допуск не ниже уровня 3')
  await expect(sheet(page)).toHaveCount(0)
  const open = await pick(page, 3)
  expect(open).toContain('СЕКРЕТ-ДЕЛА')
  await expect(sheet(page)).toContainText('СЕКРЕТ-ДЕЛА')

  await openPreview(page, denied, '&level=1')
  await expect(page.getByTestId('preview-verdict')).toHaveAttribute('data-access', 'denied')
  await expect(page.getByTestId('preview-verdict')).toContainText('«Доступ запрещён»')
  await expect(page.getByTestId('preview-verdict')).toContainText('допуск не ниже уровня 3')
  const closed = await pick(page, 2)
  expect(closed).not.toContain('СЕКРЕТ-ОТКАЗА')
  await context.close()
})

test('уровень живёт в адресе и выбирается с клавиатуры', async ({ browser, baseURL }) => {
  const { context, page } = await newAuthor(browser)
  const id = await createDoc(page, baseURL, { blocks: SECRETS })
  await openPreview(page, id, '&level=4')
  await expect(level(page, 4)).toBeChecked()
  await expect(sheet(page)).toContainText('СЕКРЕТ-БЛОК-4')

  await level(page, 4).focus()
  await page.keyboard.press('ArrowRight')
  await expect(level(page, 5)).toBeChecked()
  await expect(page).toHaveURL(/level=5/)
  await page.keyboard.press('ArrowLeft')
  await page.keyboard.press('ArrowLeft')
  await expect(level(page, 3)).toBeChecked()
  await expect(sheet(page)).not.toContainText('СЕКРЕТ-БЛОК-4')

  // мусор в адресе — уровень 0
  await openPreview(page, id, '&level=abc')
  await expect(level(page, 0)).toBeChecked()
  await openPreview(page, id, '&level=99')
  await expect(level(page, 0)).toBeChecked()
  await context.close()
})

test('неготовый черновик: блок с замечанием не в предпросмотре, ссылка ведёт к нему', async ({ browser, baseURL }) => {
  const { context, page } = await newAuthor(browser)
  const id = await createDoc(page, baseURL, { blocks: [para('a', 'Готовый абзац.')] })
  await page.goto(`/team/documents/${id}`)
  await expect(page.getByTestId('editor-toolbar')).toBeVisible()
  await editorOf(page).locator('p', { hasText: 'Готовый' }).click()
  await page.getByTestId('tb-insert').selectOption('stamp') // пустой штамп: сервер такой не примет

  await page.getByTestId('tab-preview').click()
  await expect(sheet(page)).toContainText('Готовый абзац.')
  const problems = page.getByTestId('preview-problems')
  await expect(problems).toContainText('Блок 2 — Штамп')
  await expect(sheet(page)).not.toContainText('ШТАМП')

  await problems.getByRole('button', { name: /Блок 2 — Штамп/ }).click()
  await expect(page).not.toHaveURL(/tab=preview/)
  await expect(page.getByRole('group', { name: 'Штамп', exact: true }).getByLabel('Текст штампа')).toBeFocused()
  await context.close()
})

test('телефон 375 px и доступность вкладки предпросмотра', async ({ browser, baseURL }) => {
  const { context, page } = await newAuthor(browser)
  const id = await createDoc(page, baseURL, { blocks: SECRETS })
  await page.setViewportSize({ width: 375, height: 800 })
  await openPreview(page, id, '&level=3')
  await expect(sheet(page)).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBeLessThanOrEqual(0)
  const small = await page.locator('.levels__item').evaluateAll((els) => els.map((el) => el.getBoundingClientRect()).filter((r) => r.height < 43.5 || r.width < 43.5).length)
  expect(small).toBe(0)

  await page.setViewportSize({ width: 1200, height: 900 })
  for (const q of ['&level=3', '&level=0']) {
    await openPreview(page, id, q)
    await expect(sheet(page)).toBeVisible()
    const result = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()
    expect(result.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(' ')).slice(0, 3).join(' | ')}`), q).toEqual([])
  }
  await context.close()
})

test('чужой документ: предпросмотр недоступен как и сам документ', async ({ browser, baseURL }) => {
  const a = await newAuthor(browser)
  const id = await createDoc(a.page, baseURL, { blocks: [para('a', 'Личное')] })
  const b = await newAuthor(browser)
  const res = await b.page.request.post(`/api/team/documents/${id}/preview`, { headers: { Origin: new URL(baseURL).origin }, data: { level: 0 } })
  expect(res.status()).toBe(404)
  await b.page.goto(`/team/documents/${id}?tab=preview`)
  await expect(b.page.getByRole('heading', { name: 'Дело не найдено' })).toBeVisible()
  await a.context.close()
  await b.context.close()
})
