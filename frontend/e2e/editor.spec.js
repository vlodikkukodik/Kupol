import AxeBuilder from '@axe-core/playwright'
import { expect, test } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { canWrite } from './helpers/kupol.js'
import { caretToEnd, createDoc, newAuthor, stored } from './helpers/team.js'

// Редактор документов (этап 3.3): настоящий Chromium -> Vite -> PHP-прокси -> Go -> PostgreSQL.
// Проверяется то, что видит и делает автор: набор, оформление, закрытие фрагментов, блоки, таблица, замечания сервера.
test.skip(!canWrite, 'создаёт пользователей и документы: против внешнего адреса не запускается')

const BLOCKS = JSON.parse(readFileSync(new URL('./fixtures/editor-blocks.json', import.meta.url), 'utf8'))
const para = (id, text, extra = {}) => ({ id, type: 'paragraph', data: { text: [{ text }] }, ...extra })

const editorOf = (page) => page.locator('.kupol-editor .ProseMirror')
const saveButton = (page) => page.getByRole('button', { name: 'Сохранить', exact: true }) // не «Сохранить как шаблон»
const dirtyMark = (page) => page.getByRole('button', { name: 'Отменить правки' })
const paragraphs = async (page) => (await editorOf(page).locator('p').allTextContents()).map((t) => t.trim()).filter(Boolean)

async function open(page, id) {
  await page.goto(`/team/documents/${id}`)
  await expect(page.getByTestId('block-editor')).toBeVisible()
  await expect(page.getByTestId('editor-toolbar')).toBeVisible()
}

/**
 * Щелчок в текст блока и ожидание, пока редактор примет выделение: он сверяется с браузером не мгновенно,
 * а клавиши, нажатые в этот промежуток, ушли бы в прежнее место. Живому человеку эта пауза незаметна.
 */
async function clickIn(page, locator) {
  const id = await locator.evaluate((el) => el.closest('[data-block-id]').dataset.blockId)
  await locator.click()
  await page.waitForFunction((blockId) => document.querySelector('.ProseMirror').editor.state.selection.$from.node(1)?.attrs.blockId === blockId, id)
}

/** Размер выделения в редакторе (редактор узнаёт о выделении браузера не мгновенно, поэтому его ждут). */
const selectionSize = (page, expected) =>
  page.waitForFunction((count) => {
    const { from, to } = document.querySelector('.ProseMirror').editor.state.selection
    return to - from === count
  }, expected)

/** Выделяет первые n знаков абзаца клавиатурой: одинаково во всех браузерах, без двойного щелчка. */
async function selectStart(page, paragraph, n) {
  await clickIn(page, paragraph)
  await page.keyboard.press('Home')
  for (let i = 1; i <= n; i++) {
    await page.keyboard.press('Shift+ArrowRight')
    await selectionSize(page, i)
  }
}

test('документ со всеми 15 видами блоков открывается без ложных правок', async ({ browser, baseURL }) => {
  const { context, page } = await newAuthor(browser)
  const id = await createDoc(page, baseURL, { blocks: BLOCKS })
  const errors = []
  page.on('pageerror', (e) => errors.push(e.message))
  await open(page, id)

  for (const name of [
    'Шапка досье', 'Заголовок', 'Список', 'Цитата', 'Журнал эксперимента', 'Штамп', 'Бланк (записка, приказ, письмо)',
    'Вырезка / расшифровка', 'Таблица', 'Ссылка на документ', 'Разделитель', 'Разрыв страницы', 'Сноска', 'Приложение',
  ]) {
    await expect(page.getByRole('group', { name, exact: true }).first(), name).toBeVisible()
  }
  await expect(editorOf(page).locator('p', { hasText: 'Обычный' })).toBeVisible()
  await expect(editorOf(page).locator('.pm-redact').first()).toHaveAttribute('data-redact', '3')

  // Автор ничего не трогал: нет «Отменить правки», нет автосохранения, а «Сохранить» отвечает «изменений нет»
  await expect(dirtyMark(page)).toHaveCount(0)
  await expect(page.getByTestId('autosave-state')).toHaveText('')
  await saveButton(page).click()
  await expect(page.getByText('Изменений нет — сохранять нечего.').first()).toBeVisible()
  expect((await stored(page, id)).revision).toBe(1)
  expect(errors).toEqual([])
  await context.close()
})

test('набор текста: правка сохраняется, дописанное после закрытого фрагмента — открытый текст', async ({ browser, baseURL }) => {
  const { context, page } = await newAuthor(browser)
  const id = await createDoc(page, baseURL, { blocks: BLOCKS })
  await open(page, id)

  await clickIn(page, editorOf(page).locator('p', { hasText: 'Обычный' }))
  await caretToEnd(page)
  await page.keyboard.type(' — дописано')
  await expect(dirtyMark(page)).toBeVisible()
  await expect(page.getByTestId('autosave-state')).toHaveAttribute('data-state', /pending|saving|saved/)

  await saveButton(page).click()
  await expect(page.getByText('Сохранено: редакция 2.').first()).toBeVisible()
  await expect(dirtyMark(page)).toHaveCount(0)

  const p = (await stored(page, id)).content.blocks.find((b) => b.id === 'paragraph1')
  expect(p.level).toBe(2) // допуск блока не потерян
  expect(p.data.text.slice(-2)).toEqual([{ text: 'СЕКРЕТ', level: 3 }, { text: ' — дописано' }])

  await page.reload()
  await expect(editorOf(page).locator('p', { hasText: '— дописано' })).toBeVisible()
  await context.close()
})

test('жирный, курсив и закрытие до уровня N: панель, сочетания клавиш, сохранённые данные', async ({ browser, baseURL }) => {
  const { context, page } = await newAuthor(browser)
  const id = await createDoc(page, baseURL, { blocks: [para('a', 'Обычный текст абзаца'), para('b', 'Второй абзац')] })
  await open(page, id)
  const first = editorOf(page).locator('p', { hasText: 'Обычный' })

  await selectStart(page, first, 7)
  await page.getByTestId('tb-bold').click()
  await expect(page.getByTestId('tb-bold')).toHaveAttribute('aria-pressed', 'true')
  await page.getByTestId('tb-redact').selectOption('3')
  await expect(editorOf(page).locator('.pm-redact')).toHaveText(/Обычный/)
  await expect(editorOf(page).locator('.pm-redact')).toHaveAttribute('data-redact', '3')

  const second = editorOf(page).locator('p', { hasText: 'Второй' })
  await selectStart(page, second, 6)
  await page.keyboard.press('Control+i')
  await expect(page.getByTestId('tb-italic')).toHaveAttribute('aria-pressed', 'true')

  await saveButton(page).click()
  await expect(page.getByText('Сохранено: редакция 2.').first()).toBeVisible()
  const blocks = (await stored(page, id)).content.blocks
  expect(blocks.find((b) => b.id === 'a').data.text).toEqual([{ text: 'Обычный', bold: true, level: 3 }, { text: ' текст абзаца' }])
  expect(blocks.find((b) => b.id === 'b').data.text).toEqual([{ text: 'Второй', italic: true }, { text: ' абзац' }])

  // Закрытие снимается той же панелью
  await selectStart(page, editorOf(page).locator('p', { hasText: 'Обычный' }), 7)
  await expect(page.getByTestId('tb-redact')).toHaveValue('3')
  await page.getByTestId('tb-redact').selectOption('0')
  await expect(editorOf(page).locator('.pm-redact')).toHaveCount(0)
  await context.close()
})

test('заголовок — простая строка: оформление и закрытие в нём недоступны', async ({ browser, baseURL }) => {
  const { context, page } = await newAuthor(browser)
  const id = await createDoc(page, baseURL, { blocks: [{ id: 'h', type: 'heading', data: { depth: 2, text: 'Заголовок' } }, para('a', 'Текст')] })
  await open(page, id)
  await clickIn(page, page.locator('.pm-heading .pm-content'))
  await expect(page.getByTestId('tb-bold')).toBeDisabled()
  await expect(page.getByTestId('tb-redact')).toBeDisabled()
  await clickIn(page, editorOf(page).locator('p', { hasText: 'Текст' }))
  await expect(page.getByTestId('tb-bold')).toBeEnabled()
  await context.close()
})

test('вставка блока: штамп, поля в шапке, сохранение; шапка досье — только одна', async ({ browser, baseURL }) => {
  const { context, page } = await newAuthor(browser)
  const id = await createDoc(page, baseURL, { blocks: [para('a', 'Опорный абзац')] })
  await open(page, id)
  await clickIn(page, editorOf(page).locator('p', { hasText: 'Опорный' }))

  await page.getByTestId('tb-insert').selectOption('stamp')
  const stamp = page.getByRole('group', { name: 'Штамп', exact: true })
  await expect(stamp).toBeVisible()
  await expect(stamp.getByLabel('Текст штампа')).toBeFocused() // фокус — в первое поле нового блока
  await page.keyboard.type('Тест')
  await stamp.getByLabel('Цвет').selectOption('ink')
  await stamp.getByLabel('Наклон, градусы').fill('-8')
  await expect(stamp.locator('.ui-stamp')).toHaveText('Тест')

  await page.getByTestId('tb-insert').selectOption('dossier_header')
  await expect(page.getByTestId('tb-insert').locator('option[value="dossier_header"]')).toHaveAttribute('disabled', '')

  await saveButton(page).click()
  await expect(page.getByText('Сохранено: редакция 2.').first()).toBeVisible()
  const blocks = (await stored(page, id)).content.blocks
  expect(blocks.map((b) => b.type)).toEqual(['paragraph', 'stamp', 'dossier_header'])
  expect(blocks[1].data).toEqual({ text: 'Тест', tone: 'ink', tilt: -8 })
  expect(blocks.every((b) => /^[a-z0-9][a-z0-9_-]{0,31}$/.test(b.id))).toBe(true)
  expect(new Set(blocks.map((b) => b.id)).size).toBe(3)
  await context.close()
})

test('замечания сервера: пустой штамп не проходит, блок отмечен, ссылка ведёт к нему; после исправления всё чисто', async ({ browser, baseURL }) => {
  const { context, page } = await newAuthor(browser)
  const id = await createDoc(page, baseURL, { blocks: [para('a', 'Опорный абзац')] })
  await open(page, id)
  await clickIn(page, editorOf(page).locator('p', { hasText: 'Опорный' }))
  await page.getByTestId('tb-insert').selectOption('stamp')

  await saveButton(page).click()
  const problems = page.getByTestId('problems')
  await expect(problems).toBeVisible()
  await expect(problems).toContainText('Блок 2 — Штамп')
  await expect(page.locator('.kupol-editor .has-problem')).toHaveCount(1)
  await expect(page.locator('.kupol-editor .has-problem')).toHaveAttribute('aria-invalid', 'true')

  await problems.getByRole('button', { name: /Блок 2 — Штамп/ }).click()
  await expect(page.getByRole('group', { name: 'Штамп', exact: true }).getByLabel('Текст штампа')).toBeFocused()

  await page.keyboard.type('Принято')
  await saveButton(page).click()
  await expect(page.getByText('Сохранено: редакция 2.').first()).toBeVisible()
  await expect(page.getByTestId('problems')).toHaveCount(0)
  await expect(page.locator('.kupol-editor .has-problem')).toHaveCount(0)
  await context.close()
})

test('блоки: вверх, вниз, допуск блока, удаление', async ({ browser, baseURL }) => {
  const { context, page } = await newAuthor(browser)
  const id = await createDoc(page, baseURL, { blocks: [para('a', 'Альфа'), para('b', 'Бета'), para('c', 'Гамма')] })
  await open(page, id)

  await clickIn(page, editorOf(page).locator('p', { hasText: 'Бета' }))
  await page.getByTestId('tb-up').click()
  expect(await paragraphs(page)).toEqual(['Бета', 'Альфа', 'Гамма'])
  await page.getByTestId('tb-down').click()
  await page.getByTestId('tb-down').click()
  expect(await paragraphs(page)).toEqual(['Альфа', 'Гамма', 'Бета'])
  await expect(page.getByTestId('tb-down')).toBeDisabled() // последний блок ниже не идёт

  await page.getByTestId('tb-block-level').selectOption('5')
  await expect(editorOf(page).locator('p[data-block-level="5"]')).toHaveText('Бета')

  await clickIn(page, editorOf(page).locator('p', { hasText: 'Гамма' }))
  await page.getByTestId('tb-remove').click()
  expect(await paragraphs(page)).toEqual(['Альфа', 'Бета'])

  await saveButton(page).click()
  await expect(page.getByText('Сохранено: редакция 2.').first()).toBeVisible()
  const blocks = (await stored(page, id)).content.blocks
  expect(blocks.map((b) => [b.id, b.level])).toEqual([['a', null], ['b', 5]])
  await context.close()
})

test('таблица: заголовки и ячейки набираются в таблице, Tab переходит по ячейкам, строку заголовков удалить нельзя', async ({ browser, baseURL }) => {
  const { context, page } = await newAuthor(browser)
  const id = await createDoc(page, baseURL, { blocks: [para('a', 'Опорный абзац')] })
  await open(page, id)
  await clickIn(page, editorOf(page).locator('p', { hasText: 'Опорный' }))
  await page.getByTestId('tb-insert').selectOption('table')

  const table = page.getByRole('group', { name: 'Таблица', exact: true })
  await table.getByLabel('Подпись к таблице').fill('Замеры')
  await clickIn(page, table.locator('th').first())
  await expect(page.getByTestId('tb-row-del')).toBeDisabled()
  await page.keyboard.type('Дата')
  await page.keyboard.press('Tab')
  await page.keyboard.type('Итог')
  await page.keyboard.press('Tab') // в первую ячейку следующей строки
  await page.keyboard.type('01.04')
  await page.keyboard.press('Tab')
  await page.keyboard.type('12')
  await expect(page.getByTestId('tb-row-del')).toBeEnabled()

  await page.getByTestId('tb-row-add').click()
  await clickIn(page, table.locator('tr').nth(2).locator('td').first()) // курсор остаётся в прежней ячейке: переходим в новую строку
  await page.keyboard.type('02.04')
  await page.getByTestId('tb-col-add').click()
  await expect(table.locator('tr').first().locator('th')).toHaveCount(3)

  await clickIn(page, table.locator('th').nth(1)) // столбец добавляется правее текущего
  await page.keyboard.type('Прим.')
  await saveButton(page).click()
  await expect(page.getByText('Сохранено: редакция 2.').first()).toBeVisible()

  const t = (await stored(page, id)).content.blocks.find((b) => b.type === 'table').data
  expect(t.caption).toBe('Замеры')
  expect(t.columns).toEqual(['Дата', 'Прим.', 'Итог'])
  expect(t.rows).toHaveLength(2)
  expect(t.rows.every((r) => r.length === 3)).toBe(true)
  expect(t.rows[0][0]).toEqual([{ text: '01.04' }])
  expect(t.rows[1][0]).toEqual([{ text: '02.04' }])
  await context.close()
})

test('список: Enter — новый пункт, Enter на пустом пункте — выход из списка', async ({ browser, baseURL }) => {
  const { context, page } = await newAuthor(browser)
  const id = await createDoc(page, baseURL, { blocks: [para('a', 'Опорный абзац')] })
  await open(page, id)
  await clickIn(page, editorOf(page).locator('p', { hasText: 'Опорный' }))
  await page.getByTestId('tb-insert').selectOption('list')
  await page.keyboard.type('один')
  await page.keyboard.press('Enter')
  await page.keyboard.type('два')
  await page.keyboard.press('Enter')
  await page.keyboard.press('Enter') // пустой пункт: выход из списка
  await page.keyboard.type('после списка')

  await saveButton(page).click()
  await expect(page.getByText('Сохранено: редакция 2.').first()).toBeVisible()
  const blocks = (await stored(page, id)).content.blocks
  expect(blocks.map((b) => b.type)).toEqual(['paragraph', 'list', 'paragraph'])
  expect(blocks[1].data.items).toEqual([[{ text: 'один' }], [{ text: 'два' }]])
  expect(blocks[2].data.text).toEqual([{ text: 'после списка' }])
  await context.close()
})

test('цитата и сноска: Enter — перенос строки внутри блока, а не новый блок', async ({ browser, baseURL }) => {
  const { context, page } = await newAuthor(browser)
  const id = await createDoc(page, baseURL, { blocks: [{ id: 'q', type: 'quote', data: { text: [{ text: 'первая' }] } }] })
  await open(page, id)
  await clickIn(page, page.locator('.pm-quote .pm-content'))
  await caretToEnd(page)
  await page.keyboard.press('Enter')
  await page.keyboard.type('вторая')
  await saveButton(page).click()
  await expect(page.getByText('Сохранено: редакция 2.').first()).toBeVisible()
  const blocks = (await stored(page, id)).content.blocks
  expect(blocks).toHaveLength(1)
  expect(blocks[0].data.text).toEqual([{ text: 'первая\nвторая' }])
  await context.close()
})

test('отмена и возврат: панель и Ctrl+Z; «Отменить правки» возвращает сохранённое', async ({ browser, baseURL }) => {
  const { context, page } = await newAuthor(browser)
  const id = await createDoc(page, baseURL, { blocks: [para('a', 'Исходный')] })
  await open(page, id)
  const p = editorOf(page).locator('p', { hasText: 'Исходный' })
  await expect(page.getByTestId('tb-undo')).toBeDisabled()

  await clickIn(page, p)
  await caretToEnd(page)
  await page.keyboard.type(' плюс')
  await expect(p).toHaveText('Исходный плюс')
  await page.getByTestId('tb-undo').click()
  await expect(editorOf(page).locator('p', { hasText: 'Исходный' })).toHaveText('Исходный')
  await page.getByTestId('tb-redo').click()
  await expect(editorOf(page).locator('p', { hasText: 'Исходный' })).toHaveText('Исходный плюс')
  await page.keyboard.press('Control+z')
  await expect(editorOf(page).locator('p', { hasText: 'Исходный' })).toHaveText('Исходный')

  await page.keyboard.type('!')
  await expect(dirtyMark(page)).toBeVisible()
  await dirtyMark(page).click()
  await expect(editorOf(page).locator('p', { hasText: 'Исходный' })).toHaveText('Исходный')
  await expect(dirtyMark(page)).toHaveCount(0)
  await context.close()
})

test('Ctrl+S сохраняет документ; несохранённое автосохраняется и предлагается после перезагрузки', async ({ browser, baseURL }) => {
  const { context, page } = await newAuthor(browser)
  const id = await createDoc(page, baseURL, { blocks: [para('a', 'Основа')] })
  await open(page, id)
  const p = editorOf(page).locator('p', { hasText: 'Основа' })
  await clickIn(page, p)
  await caretToEnd(page)
  await page.keyboard.type(' раз')
  await page.keyboard.press('Control+s')
  await expect(page.getByText('Сохранено: редакция 2.').first()).toBeVisible()

  await page.keyboard.type(' два')
  await expect(page.getByTestId('autosave-state')).toHaveAttribute('data-state', 'saved', { timeout: 15_000 })
  await page.reload()
  await expect(page.getByTestId('draft-banner')).toBeVisible()
  await page.getByRole('button', { name: 'Продолжить с ними' }).click()
  await expect(editorOf(page).locator('p', { hasText: 'Основа' })).toHaveText('Основа раз два')
  await expect(dirtyMark(page)).toBeVisible()
  await context.close()
})

test('поля блока: Enter в поле не отправляет форму, список строк принимает «Enter» внутри поля', async ({ browser, baseURL }) => {
  const { context, page } = await newAuthor(browser)
  const id = await createDoc(page, baseURL, { blocks: [{ id: 'm', type: 'memo', data: { kind: 'memo', body: [[{ text: 'Текст' }]] } }] })
  await open(page, id)
  const memo = page.getByRole('group', { name: 'Бланк (записка, приказ, письмо)', exact: true })
  await memo.getByLabel('Номер').fill('МЕМО-1')
  await page.keyboard.press('Enter')
  await expect(page.getByText(/Сохранено|Изменений нет/)).toHaveCount(0) // форма не ушла

  const to = memo.getByLabel('Кому')
  await to.click()
  await page.keyboard.type('Директорат')
  await page.keyboard.press('Enter')
  await page.keyboard.type('Смит, Дж.')
  await memo.getByLabel('Тема').click()
  await saveButton(page).click()
  await expect(page.getByText('Сохранено: редакция 2.').first()).toBeVisible()
  const data = (await stored(page, id)).content.blocks[0].data
  expect(data.number).toBe('МЕМО-1')
  expect(data.to).toEqual(['Директорат', 'Смит, Дж.']) // запятая в имени не делит его
  await context.close()
})

test('документ, который правит другой сотрудник: только чтение — текст, панель и поля блоков', async ({ browser, baseURL }) => {
  const a = await newAuthor(browser, ['author', 'editor'])
  const id = await createDoc(a.page, baseURL, { blocks: [para('a', 'Основа'), { id: 's', type: 'stamp', data: { text: 'Штамп', tone: 'red' } }] })
  await open(a.page, id)
  await clickIn(a.page, editorOf(a.page).locator('p', { hasText: 'Основа' }))
  await a.page.keyboard.type('!')
  await expect(a.page.getByTestId('autosave-state')).toHaveAttribute('data-state', 'saved', { timeout: 15_000 })

  const b = await newAuthor(browser, [], 6, true) // Директорат видит чужой черновик, но занятый документ ему открыт только для чтения
  await open(b.page, id)
  await expect(b.page.getByTestId('lock-banner')).toBeVisible()
  await expect(editorOf(b.page)).toHaveAttribute('contenteditable', 'false')
  await expect(b.page.getByTestId('tb-insert')).toBeDisabled()
  await expect(b.page.getByTestId('tb-bold')).toBeDisabled()
  await expect(b.page.getByRole('group', { name: 'Штамп', exact: true }).getByLabel('Текст штампа')).toBeDisabled()
  await expect(saveButton(b.page)).toBeDisabled()
  await a.context.close()
  await b.context.close()
})

test('неизвестный вид блока (из более новой версии сервера) показан и сохраняется без изменений', async ({ browser, baseURL }) => {
  const { context, page } = await newAuthor(browser)
  const id = await createDoc(page, baseURL, { blocks: [para('a', 'Основа')] })
  // Автосохранение принимает любое содержимое: кладём в черновик блок вида, которого редактор не знает
  const content = { title: '[e2e] Редактор', blocks: [para('a', 'Основа'), { id: 'x', type: 'hologram', level: 3, data: { any: ['thing', 1] } }] }
  const res = await page.request.put(`/api/team/documents/${id}/draft`, { headers: { Origin: new URL(baseURL).origin }, data: content })
  expect(res.ok()).toBeTruthy()
  await open(page, id)
  await page.getByRole('button', { name: 'Продолжить с ними' }).click()
  await expect(page.getByRole('group', { name: 'Неизвестный блок' })).toContainText('hologram')
  await clickIn(page, editorOf(page).locator('p', { hasText: 'Основа' }))
  await page.keyboard.type('!')
  await expect(page.getByTestId('autosave-state')).toHaveAttribute('data-state', 'saved', { timeout: 15_000 })
  const draft = (await stored(page, id)).draft.content.blocks
  expect(draft[1]).toEqual({ id: 'x', type: 'hologram', level: 3, data: { any: ['thing', 1] } })
  await context.close()
})

test('телефон 375 px: страница не ширится, панель прокручивается внутри, кнопки не мельче 44 px', async ({ browser, baseURL }) => {
  const { context, page } = await newAuthor(browser)
  const id = await createDoc(page, baseURL, { blocks: BLOCKS })
  await page.setViewportSize({ width: 375, height: 800 })
  await open(page, id)
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)
  expect(overflow).toBeLessThanOrEqual(0)
  const small = await page.getByTestId('editor-toolbar').locator('button:visible, select:visible').evaluateAll((els) =>
    els.map((el) => ({ label: el.getAttribute('aria-label') || el.textContent.trim(), h: el.getBoundingClientRect().height, w: el.getBoundingClientRect().width })).filter((e) => e.h < 43.5 || e.w < 43.5),
  )
  expect(small).toEqual([])
  await context.close()
})

test('доступность: страница редактора, все виды блоков, WCAG 2.1 AA', async ({ browser, baseURL }) => {
  const { context, page } = await newAuthor(browser)
  const id = await createDoc(page, baseURL, { blocks: BLOCKS })
  await open(page, id)
  const result = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()
  expect(result.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(' ')).slice(0, 3).join(' | ')}`)).toEqual([])
  await context.close()
})
