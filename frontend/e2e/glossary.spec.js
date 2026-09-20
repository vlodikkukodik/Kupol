import AxeBuilder from '@axe-core/playwright'
import { expect, test } from '@playwright/test'
import { canWrite } from './helpers/kupol.js'
import { newAuthor } from './helpers/team.js'
import { settle } from './helpers/ui.js'

// Глоссарий канона (этап 3.6): справочник терминов. Читают все члены команды, ведут Редактор и Архивариус.
test.skip(!canWrite, 'создаёт пользователей и записи: против внешнего адреса не запускается')

const uniq = (base) => `${base} ${Math.random().toString(36).slice(2, 8)}`
const term = (page, name) => page.locator('.term', { hasText: name })
const dialog = (page) => page.getByTestId('term-dialog')

test('Редактор ведёт глоссарий: добавляет, ищет по написанию и определению, правит, не допускает повтор, удаляет', async ({ browser }) => {
  const editor = await newAuthor(browser, ['author', 'editor'])
  const name = uniq('Тень')
  const alias = uniq('силуэт')

  await editor.page.goto('/team/glossary')
  await expect(editor.page.getByTestId('glossary')).toBeVisible()
  await expect(editor.page.getByRole('navigation', { name: 'Разделы панели команды' }).getByRole('link', { name: 'Глоссарий' })).toHaveAttribute('aria-current', 'page')

  await editor.page.getByTestId('term-new').click()
  await dialog(editor.page).getByRole('textbox', { name: 'Термин' }).fill(name)
  await dialog(editor.page).getByRole('textbox', { name: 'Определение' }).fill('Аномальное явление: тёмный силуэт без источника.')
  const aliases = dialog(editor.page).getByRole('textbox', { name: 'Другие написания' })
  await aliases.fill(alias)
  await aliases.press('Enter') // Enter в поле — новая строка, а не отправка формы
  await aliases.pressSequentially('  Тень (устар.)  ')
  await editor.page.getByTestId('term-save').click()
  await expect(editor.page.getByTestId('glossary-notice')).toContainText(`Термин «${name}» добавлен.`)

  const t = term(editor.page, name)
  await expect(t).toContainText('тёмный силуэт без источника')
  await expect(t).toContainText(`Другие написания: ${alias} · Тень (устар.)`)

  // поиск: по написанию, по определению; запрос — в адресе; несуществующее — пустое состояние
  await editor.page.getByRole('searchbox', { name: 'Найти' }).fill(alias.toUpperCase())
  await editor.page.getByRole('button', { name: 'Найти', exact: true }).click()
  await expect(editor.page).toHaveURL(/q=/)
  await expect(editor.page.getByTestId('glossary-total')).toHaveText('Найдено: 1')
  await expect(t).toBeVisible()
  await editor.page.goto('/team/glossary?q=' + encodeURIComponent('тёмный силуэт'))
  await expect(term(editor.page, name)).toBeVisible()
  await editor.page.goto('/team/glossary?q=' + encodeURIComponent(uniq('несуществующее')))
  await expect(editor.page.getByText('Ничего не найдено')).toBeVisible()
  await editor.page.getByRole('button', { name: 'Сбросить' }).click()
  await expect(editor.page).not.toHaveURL(/q=/)

  // повтор термина (регистр и пробелы не в счёт) — ошибка у поля
  await editor.page.getByTestId('term-new').click()
  await dialog(editor.page).getByRole('textbox', { name: 'Термин' }).fill(`  ${name.toUpperCase()} `)
  await dialog(editor.page).getByRole('textbox', { name: 'Определение' }).fill('Дубль.')
  await editor.page.getByTestId('term-save').click()
  await expect(dialog(editor.page).getByText('Такой термин уже есть в глоссарии')).toBeVisible()
  await editor.page.keyboard.press('Escape')

  // правка
  await term(editor.page, name).getByTestId('term-edit').click()
  await expect(dialog(editor.page).getByRole('textbox', { name: 'Термин' })).toHaveValue(name)
  await dialog(editor.page).getByRole('textbox', { name: 'Определение' }).fill('Новое определение.')
  await editor.page.getByTestId('term-save').click()
  await expect(editor.page.getByTestId('glossary-notice')).toContainText('сохранён')
  await expect(term(editor.page, name)).toContainText('Новое определение.')

  // удаление с подтверждением
  await term(editor.page, name).getByTestId('term-delete').click()
  await expect(editor.page.getByTestId('term-delete-dialog')).toContainText(name)
  await editor.page.getByTestId('term-confirm-delete').click()
  await expect(term(editor.page, name)).toHaveCount(0)
  await editor.context.close()
})

test('права: Автор читает и ищет, но не видит кнопок правки; сервер тоже не даст', async ({ browser, baseURL }) => {
  const editor = await newAuthor(browser, ['author', 'editor'])
  const author = await newAuthor(browser, ['author'])
  const name = uniq('Купол')
  const origin = { Origin: new URL(baseURL).origin }
  const made = await editor.page.request.post('/api/team/glossary', { headers: origin, data: { term: name, definition: 'Комитет.', aliases: [] } })
  expect(made.status()).toBe(201)
  const id = (await made.json()).term.id

  await author.page.goto('/team/glossary?q=' + encodeURIComponent(name))
  await expect(term(author.page, name)).toBeVisible()
  await expect(author.page.getByTestId('term-new')).toHaveCount(0)
  await expect(term(author.page, name).getByTestId('term-edit')).toHaveCount(0)
  await expect(term(author.page, name).getByTestId('term-delete')).toHaveCount(0)
  expect((await author.page.request.put(`/api/team/glossary/${id}`, { headers: origin, data: { term: 'Взлом', definition: 'х', aliases: [] } })).status()).toBe(403)
  expect((await author.page.request.delete(`/api/team/glossary/${id}`, { headers: origin })).status()).toBe(403)
  expect((await author.page.request.post('/api/team/glossary', { headers: origin, data: { term: uniq('Х'), definition: 'х', aliases: [] } })).status()).toBe(403)

  expect((await editor.page.request.delete(`/api/team/glossary/${id}`, { headers: origin })).status()).toBe(204)
  await editor.context.close()
  await author.context.close()
})

test('телефон 375 px и доступность: глоссарий и окно термина', async ({ browser, baseURL }) => {
  const editor = await newAuthor(browser, ['author', 'editor'])
  const name = uniq('Термин с очень длинным названием для проверки переноса строк на узком экране')
  const origin = { Origin: new URL(baseURL).origin }
  const made = await editor.page.request.post('/api/team/glossary', { headers: origin, data: { term: name, definition: 'Определение.', aliases: ['Первое написание', 'Второе написание'] } })
  expect(made.status()).toBe(201)
  const id = (await made.json()).term.id

  await editor.page.setViewportSize({ width: 375, height: 800 })
  await editor.page.goto('/team/glossary')
  await expect(term(editor.page, name)).toBeVisible()
  expect(await editor.page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBeLessThanOrEqual(0)

  await editor.page.setViewportSize({ width: 1200, height: 900 })
  let result = await new AxeBuilder({ page: editor.page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()
  expect(result.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(' ')).slice(0, 3).join(' | ')}`), 'глоссарий').toEqual([])

  await editor.page.getByTestId('term-new').click()
  await expect(dialog(editor.page)).toBeVisible()
  await settle(editor.page)
  result = await new AxeBuilder({ page: editor.page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()
  expect(result.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(' ')).slice(0, 3).join(' | ')}`), 'окно термина').toEqual([])

  expect((await editor.page.request.delete(`/api/team/glossary/${id}`, { headers: origin })).status()).toBe(204)
  await editor.context.close()
})
