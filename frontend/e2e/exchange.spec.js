import { readFile } from 'node:fs/promises'
import AxeBuilder from '@axe-core/playwright'
import { expect, test } from '@playwright/test'
import { canWrite } from './helpers/kupol.js'
import { createDoc, newAuthor, stored } from './helpers/team.js'
import { settle } from './helpers/ui.js'

// Экспорт документа в файл и загрузка из файла (этап 3.6): круг «скачать JSON → загрузить» и Markdown для чтения.
test.skip(!canWrite, 'создаёт пользователей и документы: против внешнего адреса не запускается')

const para = (id, text, extra = {}) => ({ id, type: 'paragraph', data: { text: [{ text }] }, ...extra })
const uniq = (base) => `${base} ${Math.random().toString(36).slice(2, 8)}`
const BLOCKS = [
  { id: 'head', type: 'dossier_header', data: {} },
  { id: 'h', type: 'heading', data: { depth: 2, text: 'Общие сведения' } },
  para('p1', 'Открытый абзац.'),
  para('p2', 'Закрытый абзац.', { level: 4 }),
]
const dialog = (page) => page.getByTestId('import-dialog')
const pick = (page, name, text) => page.getByTestId('import-file').setInputFiles({ name, mimeType: 'application/json', buffer: Buffer.from(text) })

async function download(page, testid) {
  const [file] = await Promise.all([page.waitForEvent('download'), page.getByTestId(testid).click()])
  return { name: file.suggestedFilename(), text: await readFile(await file.path(), 'utf8') }
}

test('скачать JSON и загрузить его обратно: получается черновик за загрузившим, с теми же блоками и допуском', async ({ browser, baseURL }) => {
  const author = await newAuthor(browser, ['author'])
  const title = uniq('Объект для выгрузки')
  const id = await createDoc(author.page, baseURL, { title, level: 2, grif: 'СЛУЖЕБНОЕ', blocks: BLOCKS })

  await author.page.goto(`/team/documents/${id}`)
  await expect(author.page.getByTestId('block-editor')).toBeVisible()
  const json = await download(author.page, 'export-json')
  expect(json.name).toBe(`object-${id}.json`)
  const file = JSON.parse(json.text)
  expect(file).toMatchObject({ type: 'object', title, status: 'draft', level: 2, grif: 'СЛУЖЕБНОЕ' })
  expect(file.blocks.map((b) => b.id)).toEqual(['head', 'h', 'p1', 'p2'])

  const md = await download(author.page, 'export-md')
  expect(md.name).toBe(`object-${id}.md`)
  expect(md.text).toContain(`# ${title}`)
  expect(md.text).toContain('## Общие сведения')
  expect(md.text).toContain('<!-- допуск блока: 4 -->\nЗакрытый абзац.')

  // несохранённые правки в файл не попадают — об этом сказано у кнопок
  await expect(author.page.locator('#export-hint')).toHaveCount(0)
  await author.page.getByLabel('Название', { exact: true }).fill(title + ' (правка)')
  await expect(author.page.locator('#export-hint')).toContainText('сохранённая версия')
  expect(JSON.parse((await download(author.page, 'export-json')).text).title).toBe(title)

  // загрузка: файл сначала проверяется, статус из файла не берётся
  await author.page.goto('/team/documents')
  await author.page.getByTestId('import-open').click()
  await expect(dialog(author.page)).toBeVisible()
  await expect(author.page.getByTestId('import-create')).toBeDisabled()
  await pick(author.page, 'doc.json', JSON.stringify({ ...file, status: 'published', title: title + ' (копия)' }))
  await expect(author.page.getByTestId('import-summary')).toContainText(title + ' (копия)')
  await expect(author.page.getByTestId('import-summary')).toContainText('Объект')
  await expect(author.page.getByTestId('import-summary')).toContainText('номер присвоится при публикации')
  await expect(author.page.getByTestId('import-summary')).toContainText(/Блоков\s*4/)
  await expect(author.page.getByTestId('import-status-note')).toContainText('«published»')
  await author.page.getByTestId('import-create').click()

  await expect(author.page).toHaveURL(/\/team\/documents\/\d+$/)
  const copyId = Number(new URL(author.page.url()).pathname.split('/').pop())
  expect(copyId).not.toBe(id)
  await expect(author.page.getByTestId('doc-status')).toContainText('Черновик')
  await expect(author.page.getByLabel('Название', { exact: true })).toHaveValue(title + ' (копия)')
  const copy = await stored(author.page, copyId)
  expect(copy).toMatchObject({ status: 'draft', author: author.login, revision: 1, content: { level: 2, grif: 'СЛУЖЕБНОЕ' } })
  expect(copy.content.blocks.map((b) => [b.id, b.level ?? null])).toEqual([['head', null], ['h', null], ['p1', null], ['p2', 4]])
  await author.context.close()
})

test('плохой файл: не JSON, замечания с путями, пакет, занятый шифр — черновик не создаётся', async ({ browser, baseURL }) => {
  const author = await newAuthor(browser, ['author'])
  const origin = { Origin: new URL(baseURL).origin }
  const code = `МЕМО-${Math.floor(Math.random() * 9000) + 1000}`
  const memo = { type: 'memo', code, title: uniq('Служебная записка'), composed: { year: 1979 }, blocks: [] }
  expect((await author.page.request.post('/api/team/documents', { headers: origin, data: memo })).status()).toBe(201)
  const before = (await (await author.page.request.get('/api/team/documents?mine=1&per_page=1')).json()).total

  await author.page.goto('/team/documents')
  await author.page.getByTestId('import-open').click()

  await pick(author.page, 'doc.md', '# Это Markdown, а не JSON')
  await expect(author.page.getByTestId('import-failure')).toContainText('не разобран как JSON')
  await expect(author.page.getByTestId('import-create')).toBeDisabled()

  await pick(author.page, 'bad.json', JSON.stringify({ ...memo, code: 'МЕМО-9', title: '', blocks: [{ id: 'b1', type: 'heading', data: { depth: 9, text: 'Х' } }] }))
  const list = author.page.getByTestId('import-problems')
  await expect(list).toContainText('Файл не прошёл проверку')
  await expect(list).toContainText('title')
  await expect(list).toContainText('blocks[0]')
  await expect(author.page.getByTestId('import-create')).toBeDisabled()
  await expect(author.page.getByTestId('import-summary')).toHaveCount(0) // прежний итог не остаётся после плохого файла

  await pick(author.page, 'batch.json', JSON.stringify({ documents: [memo] }))
  await expect(author.page.getByTestId('import-problems')).toContainText('загружайте по одному документу')

  await pick(author.page, 'taken.json', JSON.stringify(memo))
  await expect(author.page.getByTestId('import-failure')).toContainText('шифр уже занят')
  await expect(author.page.getByTestId('import-create')).toBeDisabled()

  await pick(author.page, 'typo.json', JSON.stringify({ ...memo, code: 'МЕМО-9001', titel: 'опечатка' }))
  await expect(author.page.getByTestId('import-problems')).toContainText('titel')

  expect((await (await author.page.request.get('/api/team/documents?mine=1&per_page=1')).json()).total).toBe(before)
  await author.context.close()
})

test('права: без права писать нет кнопки загрузки, а сервер отказывает; скачать чужой черновик нельзя', async ({ browser, baseURL }) => {
  const author = await newAuthor(browser, ['author'])
  const moderator = await newAuthor(browser, ['moderator'])
  const origin = { Origin: new URL(baseURL).origin }
  const id = await createDoc(author.page, baseURL, { title: uniq('Личный черновик'), blocks: BLOCKS })

  await moderator.page.goto('/team/documents')
  await expect(moderator.page.getByRole('heading', { name: 'Документы', level: 2 })).toBeVisible()
  await expect(moderator.page.getByTestId('import-open')).toHaveCount(0)
  const body = { type: 'memo', code: 'МЕМО-9002', title: 'Х', composed: { year: 1979 }, blocks: [] }
  expect((await moderator.page.request.post('/api/team/documents/import', { headers: origin, data: body })).status()).toBe(403)
  expect((await moderator.page.request.get(`/api/team/documents/${id}/export`)).status()).toBe(404)
  expect((await author.page.request.get(`/api/team/documents/${id}/export?format=pdf`)).status()).toBe(400)
  await author.context.close()
  await moderator.context.close()
})

test('телефон 375 px и доступность: окно загрузки с замечаниями и кнопки экспорта', async ({ browser, baseURL }) => {
  const author = await newAuthor(browser, ['author'])
  const id = await createDoc(author.page, baseURL, { title: uniq('Объект с очень длинным названием для проверки переноса строк на узком экране'), blocks: BLOCKS })

  await author.page.setViewportSize({ width: 375, height: 800 })
  await author.page.goto(`/team/documents/${id}`)
  await expect(author.page.getByTestId('export')).toBeVisible()
  expect(await author.page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBeLessThanOrEqual(0)

  await author.page.goto('/team/documents')
  await author.page.getByTestId('import-open').click()
  await pick(author.page, 'bad.json', JSON.stringify({ type: 'memo', code: 'МЕМО-9', title: '', composed: { year: 1979 }, blocks: [] }))
  await expect(author.page.getByTestId('import-problems')).toBeVisible()
  await settle(author.page)
  expect(await author.page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBeLessThanOrEqual(0)

  await author.page.setViewportSize({ width: 1200, height: 900 })
  let result = await new AxeBuilder({ page: author.page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()
  expect(result.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(' ')).slice(0, 3).join(' | ')}`), 'окно загрузки с замечаниями').toEqual([])

  await pick(author.page, 'ok.json', JSON.stringify({ type: 'object', title: 'Проверка', composed: { year: 1979 }, status: 'published', blocks: [] }))
  await expect(author.page.getByTestId('import-summary')).toBeVisible()
  await settle(author.page)
  result = await new AxeBuilder({ page: author.page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()
  expect(result.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(' ')).slice(0, 3).join(' | ')}`), 'окно загрузки с итогом').toEqual([])
  await author.context.close()
})
