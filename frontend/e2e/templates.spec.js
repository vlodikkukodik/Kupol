import AxeBuilder from '@axe-core/playwright'
import { expect, test } from '@playwright/test'
import { canWrite } from './helpers/kupol.js'
import { createDoc, newAuthor, stored } from './helpers/team.js'

// Шаблоны и наборы блоков (этап 3.6): сохранить из документа, создать документ по шаблону, вставить набор в редактор.
test.skip(!canWrite, 'создаёт пользователей и документы: против внешнего адреса не запускается')

const para = (id, text, extra = {}) => ({ id, type: 'paragraph', data: { text: [{ text }] }, ...extra })
const uniq = (base) => `${base} ${Math.random().toString(36).slice(2, 8)}`
const card = (page, name) => page.locator('article.tpl', { hasText: name })
const dialog = (page) => page.getByTestId('tpl-save-dialog')

const SOURCE = [
  { id: 'head', type: 'dossier_header', data: {} },
  { id: 'h', type: 'heading', data: { depth: 2, text: 'Общие сведения' } },
  para('p1', 'Открытый абзац.'),
  para('p2', 'Закрытый абзац.', { level: 4 }),
]

async function openDoc(page, id) {
  await page.goto(`/team/documents/${id}`)
  await expect(page.getByTestId('block-editor')).toBeVisible()
}

test('шаблон документа: сохраняется из документа и создаёт новый документ с теми же блоками, допуском и грифом', async ({ browser, baseURL }) => {
  const editor = await newAuthor(browser, ['author', 'editor'])
  const name = uniq('Типовой объект')
  const id = await createDoc(editor.page, baseURL, { title: 'Исходный объект', level: 2, grif: 'СЛУЖЕБНОЕ', blocks: SOURCE })

  await openDoc(editor.page, id)
  await editor.page.getByTestId('save-as-template').click()
  await expect(dialog(editor.page)).toBeVisible()
  await expect(dialog(editor.page).getByLabel('Название шаблона')).toHaveValue('Исходный объект') // по умолчанию — название документа
  await dialog(editor.page).getByLabel('Название шаблона').fill(name)
  await dialog(editor.page).getByLabel('Описание (необязательно)').fill('Для новых объектов')
  await editor.page.getByTestId('tpl-submit').click()
  await expect(editor.page.getByText(`Шаблон «${name}» сохранён`).first()).toBeVisible()

  // в разделе «Шаблоны» — карточка с составом
  await editor.page.goto('/team/templates')
  const c = card(editor.page, name)
  await expect(c).toContainText('Объект · 4 блока')
  await expect(c).toContainText('Для новых объектов')
  await expect(c).toContainText(`автор ${editor.login}`)
  await c.getByTestId('tpl-toggle').click()
  const detail = c.getByTestId('tpl-detail')
  await expect(detail).toContainText('Допуск')
  await expect(detail).toContainText('СЛУЖЕБНОЕ')
  await expect(detail.locator('li')).toHaveCount(4)
  await expect(detail.locator('li').nth(3)).toContainText('допуск 4')

  // документ по шаблону: тип, название и блоки подставились; год — сам
  await c.getByTestId('tpl-use').click()
  await expect(editor.page).toHaveURL(/\/team\/documents\/new\?template=\d+/)
  await expect(editor.page.getByTestId('nd-template-note')).toContainText(`Подставлено из шаблона «${name}»`)
  await expect(editor.page.getByLabel('Тип документа')).toHaveValue('object')
  await expect(editor.page.getByLabel('Название', { exact: true })).toHaveValue('Исходный объект')
  await editor.page.getByLabel('Название', { exact: true }).fill('Новый объект по шаблону')
  await editor.page.getByLabel('Год').fill('1979')
  await editor.page.getByRole('button', { name: 'Завести черновик' }).click()
  await expect(editor.page).toHaveURL(/\/team\/documents\/\d+$/)
  const newId = Number(new URL(editor.page.url()).pathname.split('/').pop())
  const created = await stored(editor.page, newId)
  expect(created.content.title).toBe('Новый объект по шаблону')
  expect(created.content.level).toBe(2)
  expect(created.content.grif).toBe('СЛУЖЕБНОЕ')
  expect(created.content.blocks.map((b) => b.type)).toEqual(['dossier_header', 'heading', 'paragraph', 'paragraph'])
  expect(created.content.blocks[3].level).toBe(4)
  await expect(editor.page.locator('.kupol-editor .ProseMirror p', { hasText: 'Открытый абзац.' })).toBeVisible()

  // шаблон удаляют с подтверждением; созданный по нему документ остаётся
  await editor.page.goto('/team/templates')
  await card(editor.page, name).getByTestId('tpl-delete').click()
  await expect(editor.page.getByTestId('tpl-delete-dialog')).toContainText('Документы, созданные по нему')
  await editor.page.getByTestId('tpl-confirm-delete').click()
  await expect(card(editor.page, name)).toHaveCount(0)
  expect((await stored(editor.page, newId)).content.blocks).toHaveLength(4)
  await editor.context.close()
})

test('набор блоков: диапазон блоков сохраняется и вставляется в другой документ с новыми идентификаторами', async ({ browser, baseURL }) => {
  const editor = await newAuthor(browser, ['author', 'editor'])
  const name = uniq('Шапка и сведения')
  const source = await createDoc(editor.page, baseURL, { title: 'Источник набора', blocks: SOURCE })
  const target = await createDoc(editor.page, baseURL, { title: 'Приёмник', blocks: [para('t1', 'Первый абзац приёмника.'), para('t2', 'Второй абзац приёмника.')] })

  await openDoc(editor.page, source)
  await editor.page.getByTestId('save-as-template').click()
  await dialog(editor.page).getByTestId('tpl-kind-blockset').check()
  await dialog(editor.page).getByLabel('С блока').selectOption('2')
  await dialog(editor.page).getByLabel('По блок').selectOption('4')
  await expect(editor.page.getByTestId('tpl-count')).toHaveText('В набор войдёт блоков: 3.')
  await dialog(editor.page).getByLabel('Название шаблона').fill(name)
  await editor.page.getByTestId('tpl-submit').click()
  await expect(editor.page.getByText(`Шаблон «${name}» сохранён`).first()).toBeVisible()

  await editor.page.goto('/team/templates?kind=blockset')
  await expect(card(editor.page, name)).toContainText('3 блока')
  await expect(card(editor.page, name).getByTestId('tpl-use')).toHaveCount(0) // набор в новый документ «создать» нельзя — он вставляется

  // вставка в приёмник после первого абзаца
  await openDoc(editor.page, target)
  const first = editor.page.locator('.kupol-editor .ProseMirror p', { hasText: 'Первый абзац приёмника.' })
  await first.click()
  await editor.page.waitForFunction(() => document.querySelector('.ProseMirror').editor.state.selection.$from.node(1)?.attrs.blockId === 't1')
  await editor.page.getByTestId('tb-insert-set').selectOption({ label: `${name} — 3` })
  await expect(editor.page.getByText(`Вставлен набор «${name}»: блоков 3.`).first()).toBeAttached()
  await expect(editor.page.locator('.pm-heading .pm-content', { hasText: 'Общие сведения' })).toBeVisible()
  await editor.page.getByRole('button', { name: 'Сохранить', exact: true }).click()
  await expect(editor.page.getByText('Сохранено: редакция 2.').first()).toBeVisible()

  const blocks = (await stored(editor.page, target)).content.blocks
  expect(blocks.map((b) => b.type)).toEqual(['paragraph', 'heading', 'paragraph', 'paragraph', 'paragraph'])
  expect(blocks[3].level).toBe(4) // допуск блока из набора сохранился
  expect(new Set(blocks.map((b) => b.id)).size).toBe(5)
  expect(['h', 'p1', 'p2'].some((old) => blocks.map((b) => b.id).includes(old))).toBe(false) // идентификаторы новые

  // тот же набор второй раз — тоже без повторов
  await editor.page.getByTestId('tb-insert-set').selectOption({ label: `${name} — 3` })
  await editor.page.getByRole('button', { name: 'Сохранить', exact: true }).click()
  await expect(editor.page.getByText('Сохранено: редакция 3.').first()).toBeVisible()
  const twice = (await stored(editor.page, target)).content.blocks
  expect(twice).toHaveLength(8)
  expect(new Set(twice.map((b) => b.id)).size).toBe(8)

  await editor.page.goto('/team/templates?kind=blockset')
  await card(editor.page, name).getByTestId('tpl-delete').click()
  await editor.page.getByTestId('tpl-confirm-delete').click()
  await expect(card(editor.page, name)).toHaveCount(0)
  await editor.context.close()
})

test('права: Автор читает шаблоны и создаёт по ним документы, но не заводит, не переименовывает и не удаляет', async ({ browser, baseURL }) => {
  const editor = await newAuthor(browser, ['author', 'editor'])
  const author = await newAuthor(browser, ['author'])
  const name = uniq('Общий шаблон')
  const id = await createDoc(editor.page, baseURL, { title: 'Образец', blocks: SOURCE })
  const origin = { Origin: new URL(baseURL).origin }
  const made = await editor.page.request.post('/api/team/templates', {
    headers: origin,
    data: { kind: 'document', name, description: '', doc_type: 'object', content: { title: 'Образец', blocks: SOURCE } },
  })
  expect(made.status()).toBe(201)
  const tplId = (await made.json()).template.id
  expect(id).toBeGreaterThan(0)

  await author.page.goto('/team/templates')
  const c = card(author.page, name)
  await expect(c).toBeVisible()
  await expect(c.getByTestId('tpl-use')).toBeVisible()
  await expect(c.getByTestId('tpl-edit')).toHaveCount(0)
  await expect(c.getByTestId('tpl-delete')).toHaveCount(0)
  // и сервер не даст, даже если запрос подделан руками
  expect((await author.page.request.put(`/api/team/templates/${tplId}`, { headers: origin, data: { name: 'Взлом', description: '' } })).status()).toBe(403)
  expect((await author.page.request.delete(`/api/team/templates/${tplId}`, { headers: origin })).status()).toBe(403)
  expect((await author.page.request.post('/api/team/templates', { headers: origin, data: { kind: 'blockset', name: uniq('Х'), content: { blocks: SOURCE } } })).status()).toBe(403)

  // на странице своего документа кнопки «Сохранить как шаблон» у Автора нет
  const own = await createDoc(author.page, baseURL, { title: 'Мой документ', blocks: [para('a', 'Текст.')] })
  await openDoc(author.page, own)
  await expect(author.page.getByTestId('save-as-template')).toHaveCount(0)

  // Редактор: переименование, занятое название — ошибка у поля
  await editor.page.goto('/team/templates')
  const other = uniq('Другой шаблон')
  const second = await editor.page.request.post('/api/team/templates', { headers: origin, data: { kind: 'document', name: other, doc_type: 'object', content: { blocks: SOURCE } } })
  expect(second.status()).toBe(201)
  await editor.page.reload()
  await card(editor.page, other).getByTestId('tpl-edit').click()
  const edit = editor.page.getByTestId('tpl-edit-dialog')
  const nameField = edit.getByRole('textbox', { name: 'Название' })
  await nameField.fill(`  ${name.toUpperCase()}  `)
  await edit.getByTestId('tpl-save').click()
  await expect(edit.getByText('Такое название уже занято: выберите другое')).toBeVisible()
  await nameField.fill(`${other} (новое)`)
  await edit.getByTestId('tpl-save').click()
  await expect(editor.page.getByTestId('templates-notice')).toContainText('сохранён')
  await expect(card(editor.page, `${other} (новое)`)).toBeVisible()

  for (const n of [name, `${other} (новое)`]) {
    await card(editor.page, n).getByTestId('tpl-delete').click()
    await editor.page.getByTestId('tpl-confirm-delete').click()
    await expect(card(editor.page, n)).toHaveCount(0)
  }
  await editor.context.close()
  await author.context.close()
})

test('шаблон не сохраняется из неготового документа: причины названы по блокам', async ({ browser, baseURL }) => {
  const editor = await newAuthor(browser, ['author', 'editor'])
  const id = await createDoc(editor.page, baseURL, { title: 'Черновик со штампом', blocks: [para('a', 'Готовый абзац.')] })
  await openDoc(editor.page, id)
  await editor.page.locator('.kupol-editor .ProseMirror p', { hasText: 'Готовый' }).click()
  await editor.page.waitForFunction(() => document.querySelector('.ProseMirror').editor.state.selection.$from.node(1)?.attrs.blockId === 'a')
  await editor.page.getByTestId('tb-insert').selectOption('stamp') // пустой штамп

  await editor.page.getByTestId('save-as-template').click()
  await dialog(editor.page).getByLabel('Название шаблона').fill(uniq('Со штампом'))
  await editor.page.getByTestId('tpl-submit').click()
  const problems = editor.page.getByTestId('tpl-problems')
  await expect(problems).toContainText('Блок 2')
  await expect(problems).toContainText('не может быть пустым')
  await expect(dialog(editor.page)).toBeVisible() // окно осталось: можно передумать и выбрать набор из блока 1

  await dialog(editor.page).getByTestId('tpl-kind-blockset').check()
  await dialog(editor.page).getByLabel('По блок').selectOption('1')
  const name = uniq('Только готовое')
  await dialog(editor.page).getByLabel('Название шаблона').fill(name)
  await editor.page.getByTestId('tpl-submit').click()
  await expect(editor.page.getByText(`Шаблон «${name}» сохранён`).first()).toBeVisible()
  await editor.page.goto('/team/templates?kind=blockset')
  await card(editor.page, name).getByTestId('tpl-delete').click()
  await editor.page.getByTestId('tpl-confirm-delete').click()
  await editor.context.close()
})

test('телефон 375 px и доступность: раздел «Шаблоны» и окно «Сохранить как шаблон»', async ({ browser, baseURL }) => {
  const editor = await newAuthor(browser, ['author', 'editor'])
  const name = uniq('Шаблон для вёрстки с очень длинным названием, чтобы проверить перенос строк на узком экране')
  const origin = { Origin: new URL(baseURL).origin }
  const made = await editor.page.request.post('/api/team/templates', { headers: origin, data: { kind: 'document', name, description: 'Описание шаблона.', doc_type: 'object', content: { title: 'Х', blocks: SOURCE } } })
  expect(made.status()).toBe(201)
  const id = await createDoc(editor.page, baseURL, { title: 'Для окна', blocks: SOURCE })

  await editor.page.setViewportSize({ width: 375, height: 800 })
  await editor.page.goto('/team/templates')
  await expect(card(editor.page, name)).toBeVisible()
  expect(await editor.page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBeLessThanOrEqual(0)

  await editor.page.setViewportSize({ width: 1200, height: 900 })
  let result = await new AxeBuilder({ page: editor.page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()
  expect(result.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(' ')).slice(0, 3).join(' | ')}`), 'раздел «Шаблоны»').toEqual([])

  await openDoc(editor.page, id)
  await editor.page.getByTestId('save-as-template').click()
  await expect(dialog(editor.page)).toBeVisible()
  await dialog(editor.page).getByTestId('tpl-kind-blockset').check()
  await editor.page.evaluate(async () => {
    await new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r)))
    await Promise.all(document.getAnimations().map((a) => a.finished.catch(() => {})))
  })
  result = await new AxeBuilder({ page: editor.page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()
  expect(result.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(' ')).slice(0, 3).join(' | ')}`), 'окно шаблона').toEqual([])
  await editor.page.keyboard.press('Escape')

  await editor.page.goto('/team/templates')
  await card(editor.page, name).getByTestId('tpl-delete').click()
  await editor.page.getByTestId('tpl-confirm-delete').click()
  await editor.context.close()
})
