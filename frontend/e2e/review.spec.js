import AxeBuilder from '@axe-core/playwright'
import { expect, test } from '@playwright/test'
import { canWrite } from './helpers/kupol.js'
import { createDoc, newAuthor, stored } from './helpers/team.js'

// Рецензия и публикация (этап 3.5): два настоящих человека, настоящий сервер. Автор отправляет документ, Редактор
// комментирует и выносит вердикт, читатель видит опубликованное; линтер канона не пропускает битые ссылки.
test.skip(!canWrite, 'создаёт пользователей и документы: против внешнего адреса не запускается')

const para = (id, text) => ({ id, type: 'paragraph', data: { text: [{ text }] } })
const status = (page) => page.getByTestId('doc-status')
const wf = (page, name) => page.getByTestId(`wf-${name}`)
const panel = (page) => page.getByTestId('review-panel')

async function open(page, id, query = '') {
  await page.goto(`/team/documents/${id}${query}`)
  await expect(page.getByTestId('workflow')).toBeVisible()
}

/** Ждёт, пока сообщение об итоге появится на странице (оно есть и для скринридера, и на экране). */
const said = (page, text) => expect(page.getByText(text).first()).toBeVisible()

async function guestGet(browser, baseURL, path) {
  const context = await browser.newContext()
  const res = await context.request.get(path)
  const out = { status: res.status(), body: res.status() === 200 ? await res.json() : null }
  await context.close()
  return out
}

test('от черновика до публикации: отправка, комментарий к блоку, возврат, исправление, принятие, архив', async ({ browser, baseURL }) => {
  const author = await newAuthor(browser, ['author'])
  const editor = await newAuthor(browser, ['author', 'editor'])
  const id = await createDoc(author.page, baseURL, { title: 'Объект для проверки', blocks: [para('p1', 'Первый абзац.'), para('p2', 'Второй абзац.')] })

  // ——— автор: черновик
  await open(author.page, id)
  await expect(status(author.page)).toHaveText('Черновик')
  await expect(author.page.getByTestId('workflow-hint')).toContainText('Когда текст готов')
  await expect(wf(author.page, 'submit')).toBeEnabled()
  await expect(wf(author.page, 'verdict')).toHaveCount(0)

  // несохранённые правки: на проверку уходит только сохранённое, кнопка ждёт «Сохранить»
  await author.page.locator('.kupol-editor .ProseMirror p', { hasText: 'Первый' }).click()
  await author.page.waitForFunction(() => document.querySelector('.ProseMirror').editor.state.selection.$from.node(1)?.attrs.blockId === 'p1')
  await author.page.keyboard.press('End')
  await author.page.waitForFunction(() => {
    const { $from } = document.querySelector('.ProseMirror').editor.state.selection
    return $from.parentOffset === $from.parent.content.size
  })
  await author.page.keyboard.type(' Дополнено.')
  await expect(wf(author.page, 'submit')).toBeDisabled()
  await expect(author.page.getByTestId('wf-dirty-note')).toBeVisible()
  await author.page.getByRole('button', { name: 'Сохранить' }).click()
  await said(author.page, 'Сохранено: редакция 2.')
  await expect(wf(author.page, 'submit')).toBeEnabled()

  // ——— отправка
  await wf(author.page, 'submit').click()
  await said(author.page, 'Документ отправлен на проверку')
  await expect(status(author.page)).toHaveText('На проверке')
  await expect(wf(author.page, 'submit')).toHaveCount(0)
  await expect(wf(author.page, 'withdraw')).toBeVisible()
  expect((await stored(author.page, id)).status).toBe('review')

  // ——— редактор: читает, комментирует блок, не может вынести вердикт без причины
  await open(editor.page, id)
  await expect(status(editor.page)).toHaveText('На проверке')
  await expect(wf(editor.page, 'verdict')).toBeVisible()
  await editor.page.getByTestId('tab-review').click()
  await expect(panel(editor.page)).toBeVisible()
  await expect(editor.page.getByTestId('lint-summary')).toContainText('Предупреждений: 1') // у объекта нет шапки досье: только предупреждение
  await expect(editor.page.getByTestId('lint-issues')).toContainText('Шапка досье')
  await expect(editor.page.getByTestId('comments-empty')).toBeVisible()
  await panel(editor.page).getByLabel('К чему комментарий').selectOption('p2') // «Блок 2 — Абзац»
  await panel(editor.page).getByLabel('Комментарий', { exact: true }).fill('Уточните дату события.')
  await editor.page.getByTestId('comment-add').click()
  await expect(panel(editor.page).locator('.comment')).toContainText('Уточните дату события.')
  await expect(panel(editor.page).locator('.comment')).toContainText('Блок 2 — Абзац')

  await wf(editor.page, 'verdict').click()
  const dialog = editor.page.getByTestId('wf-dialog')
  await expect(dialog).toBeVisible()
  await dialog.getByTestId('verdict-return').check()
  await dialog.getByTestId('wf-confirm').click()
  await expect(dialog.getByText('Укажите причину: автору нужно знать, что исправить.')).toBeVisible() // клиент не отправляет
  await dialog.getByRole('textbox', { name: 'Причина' }).fill('См. комментарий к блоку 2.')
  await dialog.getByTestId('wf-confirm').click()
  await said(editor.page, 'Документ возвращён автору на доработку.')
  await expect(dialog).toHaveCount(0)

  // ——— автор: причина и комментарий на месте, исправляет и отправляет снова
  await open(author.page, id)
  await expect(status(author.page)).toHaveText('Черновик')
  await expect(author.page.getByTestId('tab-review-count')).toHaveText('1')
  await author.page.getByTestId('tab-review').click()
  await expect(author.page.getByTestId('events')).toContainText('Возвращён на доработку')
  await expect(author.page.getByTestId('events')).toContainText('См. комментарий к блоку 2.')
  await expect(author.page.getByTestId('events')).toContainText(editor.login) // кто вернул
  await author.page.getByRole('button', { name: 'Блок 2 — Абзац' }).click() // к блоку в редакторе
  await expect(author.page).not.toHaveURL(/tab=review/)
  await expect(author.page.getByRole('button', { name: 'Отменить правки' })).toHaveCount(0)

  await author.page.getByTestId('tab-review').click()
  await author.page.getByTestId('comment-resolve').click()
  await expect(author.page.locator('.comment.is-resolved')).toContainText('Исправлено')
  await expect(author.page.getByTestId('tab-review-count')).toHaveCount(0)
  await wf(author.page, 'submit').click()
  await said(author.page, 'Документ отправлен на проверку')

  // ——— редактор: принимает — документ публикуется и получает номер
  await open(editor.page, id)
  await wf(editor.page, 'verdict').click()
  await expect(editor.page.getByTestId('wf-dialog').getByText('номер О-№ присвоится')).toBeVisible()
  await editor.page.getByTestId('verdict-approve').check()
  await editor.page.getByTestId('wf-confirm').click()
  await said(editor.page, /Документ опубликован как О-\d+\./)
  await expect(status(editor.page)).toHaveText('Опубликован')
  const code = (await stored(editor.page, id)).code
  expect(code).toMatch(/^О-\d+$/)

  // читатель видит опубликованное сразу и по присвоенному номеру; закрытых от него блоков в ответе нет
  const guest = await guestGet(browser, baseURL, `/api/documents/${code}`)
  expect(guest.status).toBe(200)
  expect(guest.body.document.title).toBe('Объект для проверки')
  expect(JSON.stringify(guest.body)).toContain('Первый абзац. Дополнено.')

  // автору опубликованный документ править уже нельзя, а убрать в архив он не вправе
  await open(author.page, id)
  await expect(status(author.page)).toHaveText('Опубликован')
  await expect(wf(author.page, 'archive')).toHaveCount(0)
  await expect(author.page.getByTestId('readonly-banner')).toBeVisible()

  // ——— архив и обратно
  await wf(editor.page, 'archive').click()
  await editor.page.getByTestId('wf-dialog').getByLabel('Пояснение (необязательно)').fill('Устарело.')
  await editor.page.getByTestId('wf-confirm').click()
  await said(editor.page, 'Документ убран в архив')
  await expect(status(editor.page)).toHaveText('Архив')
  expect((await guestGet(browser, baseURL, `/api/documents/${code}`)).status).toBe(404)
  await wf(editor.page, 'unarchive').click()
  await editor.page.getByTestId('wf-confirm').click()
  await said(editor.page, 'Документ снова опубликован.')
  expect((await guestGet(browser, baseURL, `/api/documents/${code}`)).status).toBe(200)

  await author.context.close()
  await editor.context.close()
})

test('линтер канона: битая ссылка не даёт отправить документ, предупреждения не мешают, «К блоку» ведёт к ссылке', async ({ browser, baseURL }) => {
  const author = await newAuthor(browser, ['author'])
  const link = { id: 'lnk', type: 'doc_link', data: { code: 'О-9998' } }
  const id = await createDoc(author.page, baseURL, { title: 'Со ссылкой', blocks: [para('p1', 'Текст.'), link] })

  await open(author.page, id)
  await wf(author.page, 'submit').click()
  await expect(author.page).toHaveURL(/tab=review/) // причина — на вкладке «Рецензия»
  await expect(author.page.getByText(/не прошёл проверку канона/).first()).toBeVisible()
  await expect(status(author.page)).toHaveText('Черновик')
  const issues = author.page.getByTestId('lint-issues')
  await expect(issues.locator('[data-severity="error"]')).toContainText('О-9998')
  await expect(issues.locator('[data-severity="warning"]')).toContainText('Шапка досье') // предупреждение рядом с ошибкой
  await expect(author.page.getByTestId('lint-summary')).toContainText('нельзя отправить на проверку')
  expect((await stored(author.page, id)).status).toBe('draft')

  await issues.getByRole('button', { name: 'К блоку 2' }).click()
  await expect(author.page).not.toHaveURL(/tab=review/)
  await expect(author.page.getByRole('group', { name: 'Ссылка на документ', exact: true }).getByLabel('Шифр документа')).toBeFocused()

  // автор исправляет ссылку и сохраняет — проверка проходит, остаётся только предупреждение
  const field = author.page.getByRole('group', { name: 'Ссылка на документ', exact: true }).getByLabel('Шифр документа')
  await field.fill('О-9997')
  await author.page.getByRole('button', { name: 'Сохранить' }).click()
  await said(author.page, 'Сохранено: редакция 2.')
  await author.page.getByTestId('tab-review').click()
  await author.page.getByTestId('lint-refresh').click()
  await expect(author.page.getByTestId('lint-issues').locator('[data-severity="error"]')).toContainText('О-9997') // такого документа тоже нет
  await author.context.close()
})

test('права видны в интерфейсе: свой документ Редактор не проверяет, Директорат проверяет любой', async ({ browser, baseURL }) => {
  const editor = await newAuthor(browser, ['author', 'editor'])
  const director = await newAuthor(browser, [], 6, true)
  const id = await createDoc(editor.page, baseURL, { title: 'Свой документ Редактора', blocks: [para('p1', 'Текст.')] })

  await open(editor.page, id)
  await wf(editor.page, 'submit').click()
  await said(editor.page, 'Документ отправлен на проверку')
  await expect(wf(editor.page, 'verdict')).toHaveCount(0) // вердикт по своему — не его
  await expect(wf(editor.page, 'withdraw')).toBeVisible()
  await editor.page.getByTestId('tab-review').click()
  await expect(panel(editor.page).getByRole('form', { name: 'Новый комментарий' })).toHaveCount(0)
  // и сервер тоже не даст
  const res = await editor.page.request.post(`/api/team/documents/${id}/verdict`, { headers: { Origin: new URL(baseURL).origin }, data: { verdict: 'approve', base_revision: 1 } })
  expect(res.status()).toBe(403)
  expect((await res.json()).error.code).toBe('self_review')

  await open(director.page, id)
  await expect(wf(director.page, 'verdict')).toBeVisible()
  await wf(director.page, 'verdict').click()
  await director.page.getByTestId('verdict-reject').check()
  await director.page.getByTestId('wf-dialog').getByRole('textbox', { name: 'Причина' }).fill('Дубль существующего дела.')
  await director.page.getByTestId('wf-confirm').click()
  await said(director.page, 'Документ отклонён и убран в архив.')
  await expect(status(director.page)).toHaveText('Архив')
  await editor.context.close()
  await director.context.close()
})

test('пока рецензент читал, автор поправил документ: вердикт по прочитанному не выносится', async ({ browser, baseURL }) => {
  const author = await newAuthor(browser, ['author'])
  const editor = await newAuthor(browser, ['author', 'editor'])
  const id = await createDoc(author.page, baseURL, { title: 'Меняется', blocks: [para('p1', 'Текст.')] })

  await open(author.page, id)
  await wf(author.page, 'submit').click()
  await said(author.page, 'Документ отправлен на проверку')

  await open(editor.page, id) // рецензент открыл редакцию 1
  // автор берёт документ обратно (через API: интерфейс забирает тем же запросом), правит и снова отправляет
  const origin = { Origin: new URL(baseURL).origin }
  expect((await author.page.request.post(`/api/team/documents/${id}/withdraw`, { headers: origin, data: {} })).status()).toBe(200)
  const put = await author.page.request.put(`/api/team/documents/${id}`, {
    headers: origin,
    data: { base_revision: 1, content: { title: 'Меняется', composed: { year: 1979 }, blocks: [para('p1', 'Текст изменён.')] } },
  })
  expect(put.status()).toBe(200)
  expect((await author.page.request.post(`/api/team/documents/${id}/submit`, { headers: origin, data: { base_revision: 2 } })).status()).toBe(200)

  await wf(editor.page, 'verdict').click()
  await editor.page.getByTestId('verdict-approve').check()
  await editor.page.getByTestId('wf-confirm').click()
  await expect(editor.page.getByTestId('conflict-banner')).toBeVisible()
  expect((await stored(author.page, id)).status).toBe('review') // ничего не опубликовано
  await author.context.close()
  await editor.context.close()
})

test('телефон 375 px и доступность: панель хода, вкладка «Рецензия», окно вердикта', async ({ browser, baseURL }) => {
  const author = await newAuthor(browser, ['author'])
  const editor = await newAuthor(browser, ['author', 'editor'])
  const id = await createDoc(author.page, baseURL, { title: 'Для проверки вёрстки', blocks: [para('p1', 'Текст.'), para('p2', 'Ещё текст.')] })
  const origin = { Origin: new URL(baseURL).origin }
  expect((await author.page.request.post(`/api/team/documents/${id}/submit`, { headers: origin, data: { base_revision: 1 } })).status()).toBe(200)
  const comment = await editor.page.request.post(`/api/team/documents/${id}/comments`, { headers: origin, data: { block_id: 'p2', body: 'Проверьте формулировку.' } })
  expect(comment.status()).toBe(201)

  await editor.page.setViewportSize({ width: 375, height: 800 })
  await open(editor.page, id, '?tab=review')
  await expect(panel(editor.page)).toContainText('Проверьте формулировку.')
  expect(await editor.page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBeLessThanOrEqual(0)
  const small = await editor.page.locator('.flow button:visible').evaluateAll((els) => els.filter((el) => el.getBoundingClientRect().height < 43.5).length)
  expect(small).toBe(0)

  await editor.page.setViewportSize({ width: 1200, height: 900 })
  let result = await new AxeBuilder({ page: editor.page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()
  expect(result.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(' ')).slice(0, 3).join(' | ')}`), 'вкладка «Рецензия»').toEqual([])

  await wf(editor.page, 'verdict').click()
  await expect(editor.page.getByTestId('wf-dialog')).toBeVisible()
  result = await new AxeBuilder({ page: editor.page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()
  expect(result.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(' ')).slice(0, 3).join(' | ')}`), 'окно вердикта').toEqual([])
  await editor.page.keyboard.press('Escape')
  await expect(editor.page.getByTestId('wf-dialog')).toHaveCount(0)
  await author.context.close()
  await editor.context.close()
})
