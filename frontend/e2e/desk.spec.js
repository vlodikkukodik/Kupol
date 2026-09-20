import AxeBuilder from '@axe-core/playwright'
import { expect, test } from '@playwright/test'
import { canWrite } from './helpers/kupol.js'
import { createDoc, newAuthor } from './helpers/team.js'

// Рабочий стол команды (этап 3.6): что ждёт человека — его документы, возвращённое на доработку, очередь на проверку.
test.skip(!canWrite, 'создаёт пользователей и документы: против внешнего адреса не запускается')

const para = (id, text) => ({ id, type: 'paragraph', data: { text: [{ text }] } })
const desk = (page) => page.getByTestId('desk')
const post = (page, baseURL, path, data) => page.request.post(path, { headers: { Origin: new URL(baseURL).origin }, data })

test('автор: пустой стол, затем счётчики, возвращённое с причиной и замечаниями, ждущее проверки', async ({ browser, baseURL }) => {
  const author = await newAuthor(browser, ['author'])
  const editor = await newAuthor(browser, ['author', 'editor'])

  await author.page.goto('/team')
  await expect(desk(author.page)).toBeVisible()
  await expect(desk(author.page).getByText('У вас пока нет документов')).toBeVisible()
  await expect(author.page.getByTestId('desk-new')).toBeVisible()
  await expect(author.page.getByTestId('desk-queue')).toHaveCount(0) // очередь — только рецензентам
  await expect(author.page.getByRole('navigation', { name: 'Разделы панели команды' }).getByRole('link', { name: 'Рабочий стол' })).toHaveAttribute('aria-current', 'page')

  const plain = await createDoc(author.page, baseURL, { title: 'Обычный черновик', blocks: [para('a', 'Текст.')] })
  const returned = await createDoc(author.page, baseURL, { title: 'Вернули автору', blocks: [para('a', 'Текст.')] })
  const waiting = await createDoc(author.page, baseURL, { title: 'Ждёт проверки', blocks: [para('a', 'Текст.')] })
  for (const id of [returned, waiting]) expect((await post(author.page, baseURL, `/api/team/documents/${id}/submit`, { base_revision: 1 })).status()).toBe(200)
  expect((await post(editor.page, baseURL, `/api/team/documents/${returned}/comments`, { block_id: 'a', body: 'Уточните дату.' })).status()).toBe(201)
  expect((await post(editor.page, baseURL, `/api/team/documents/${returned}/comments`, { body: 'И место.' })).status()).toBe(201)
  expect((await post(editor.page, baseURL, `/api/team/documents/${returned}/verdict`, { verdict: 'return', comment: 'Мало сведений об объекте.', base_revision: 1 })).status()).toBe(200)

  await author.page.reload()
  await expect(author.page.getByTestId('desk-count-draft')).toContainText('2')
  await expect(author.page.getByTestId('desk-count-review')).toContainText('1')
  await expect(author.page.getByTestId('desk-count-published')).toContainText('0')

  const back = author.page.getByTestId('desk-returned')
  await expect(back).toContainText('Вернули автору')
  await expect(back).toContainText(`${editor.login}: Мало сведений об объекте.`)
  await expect(back).toContainText('2 замечания к исправлению')
  await expect(author.page.getByTestId('desk-drafts')).toContainText('Обычный черновик')
  await expect(author.page.getByTestId('desk-drafts')).not.toContainText('Вернули автору') // возвращённый — в своём списке
  await expect(author.page.getByTestId('desk-in-review')).toContainText('Ждёт проверки')

  // «2 замечания» ведут прямо на вкладку рецензии документа
  await back.getByRole('link', { name: /2 замечания/ }).click()
  await expect(author.page).toHaveURL(new RegExp(`/team/documents/${returned}\\?tab=review`))
  await expect(author.page.getByTestId('review-panel')).toContainText('Уточните дату.')

  // карточка-счётчик открывает список документов с фильтром
  await author.page.goto('/team')
  await author.page.getByTestId('desk-count-draft').click()
  await expect(author.page).toHaveURL(/status=draft/)
  await expect(author.page).toHaveURL(/mine=1/)
  expect(plain).toBeGreaterThan(0)
  await author.context.close()
  await editor.context.close()
})

test('рецензент: очередь на проверку — чужие документы, давно ждущие сверху; свои в очередь не попадают', async ({ browser, baseURL }) => {
  const author = await newAuthor(browser, ['author'])
  const editor = await newAuthor(browser, ['author', 'editor'])

  const first = await createDoc(author.page, baseURL, { title: 'Первым отправлен', blocks: [para('a', 'Текст.')] })
  const second = await createDoc(author.page, baseURL, { title: 'Вторым отправлен', blocks: [para('a', 'Текст.')] })
  const own = await createDoc(editor.page, baseURL, { title: 'Свой документ Редактора', blocks: [para('a', 'Текст.')] })
  for (const [page, id] of [[author.page, first], [author.page, second], [editor.page, own]]) {
    expect((await post(page, baseURL, `/api/team/documents/${id}/submit`, { base_revision: 1 })).status()).toBe(200)
    await page.waitForTimeout(1100) // отправки в разные секунды: порядок очереди — по времени отправки
  }

  await editor.page.goto('/team')
  const queue = editor.page.getByTestId('desk-queue')
  await expect(queue).toBeVisible()
  // очередь делят все тесты: проверяем своё — оба документа на месте, давний выше, свой документ Редактора отсутствует
  const row = (id) => queue.locator(`li[data-doc="${id}"]`)
  await expect(row(first)).toContainText('Первым отправлен')
  await expect(row(second)).toContainText('Вторым отправлен')
  await expect(row(first)).toContainText(`автор ${author.login}`)
  const order = await queue.locator('li[data-doc]').evaluateAll((els) => els.map((el) => Number(el.dataset.doc)))
  expect(order.indexOf(first)).toBeGreaterThanOrEqual(0)
  expect(order.indexOf(first)).toBeLessThan(order.indexOf(second))
  expect(order).not.toContain(own)
  expect(Number(await editor.page.getByTestId('desk-queue-total').innerText())).toBeGreaterThanOrEqual(2)
  await expect(editor.page.getByTestId('desk-in-review')).toContainText('Свой документ Редактора') // свой — в «ждут проверки»

  // принял первый — очередь сокращается, у автора он опубликован
  expect((await post(editor.page, baseURL, `/api/team/documents/${first}/verdict`, { verdict: 'approve', base_revision: 1 })).status()).toBe(200)
  await editor.page.reload()
  await expect(row(second)).toBeVisible()
  await expect(row(first)).toHaveCount(0) // принятый из очереди ушёл
  await author.page.goto('/team')
  await expect(author.page.getByTestId('desk-count-published')).toContainText('1')

  await queue.getByRole('link', { name: 'Вторым отправлен' }).click()
  await expect(editor.page).toHaveURL(new RegExp(`/team/documents/${second}$`))
  await author.context.close()
  await editor.context.close()
})

test('Директорат видит в очереди и свой документ; модератор без документов — пустой стол без очереди', async ({ browser, baseURL }) => {
  const director = await newAuthor(browser, [], 6, true)
  const moderator = await newAuthor(browser, ['moderator'])
  const own = await createDoc(director.page, baseURL, { title: 'Документ Директората', blocks: [para('a', 'Текст.')] })
  expect((await post(director.page, baseURL, `/api/team/documents/${own}/submit`, { base_revision: 1 })).status()).toBe(200)

  await director.page.goto('/team')
  await expect(director.page.getByTestId('desk-queue').locator(`li[data-doc="${own}"]`)).toContainText('Документ Директората') // свой документ Директорат проверять вправе
  await moderator.page.goto('/team')
  await expect(moderator.page.getByTestId('desk-new')).toHaveCount(0) // писать документы Модератор не вправе
  await expect(desk(moderator.page)).toContainText('У вас пока нет документов')
  await expect(moderator.page.getByTestId('desk-queue')).toHaveCount(0)
  await director.context.close()
  await moderator.context.close()
})

test('телефон 375 px и доступность рабочего стола с содержимым', async ({ browser, baseURL }) => {
  const author = await newAuthor(browser, ['author'])
  const editor = await newAuthor(browser, ['author', 'editor'])
  const id = await createDoc(author.page, baseURL, { title: 'Возвращённый документ с очень длинным названием для проверки переноса строк на узком экране', blocks: [para('a', 'Текст.')] })
  expect((await post(author.page, baseURL, `/api/team/documents/${id}/submit`, { base_revision: 1 })).status()).toBe(200)
  expect((await post(editor.page, baseURL, `/api/team/documents/${id}/verdict`, { verdict: 'return', comment: 'Причина возврата достаточно длинная, чтобы занять несколько строк на телефоне.', base_revision: 1 })).status()).toBe(200)
  await createDoc(author.page, baseURL, { title: 'Ещё черновик', blocks: [para('a', 'Текст.')] })

  for (const page of [author.page, editor.page]) {
    await page.setViewportSize({ width: 375, height: 800 })
    await page.goto('/team')
    await expect(desk(page)).toBeVisible()
    expect(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBeLessThanOrEqual(0)
    await page.setViewportSize({ width: 1200, height: 900 })
    const result = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()
    expect(result.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(' ')).slice(0, 3).join(' | ')}`)).toEqual([])
  }
  await author.context.close()
  await editor.context.close()
})
