import AxeBuilder from '@axe-core/playwright'
import { expect, test } from '@playwright/test'
import { canWrite } from './helpers/kupol.js'
import { newAuthor } from './helpers/team.js'
import { settle } from './helpers/ui.js'

// «О КУПОЛЕ» и хронология (этап 4): открытая всем страница, события с допуском, ведение хронологии в панели команды.
test.skip(!canWrite, 'создаёт пользователей и события: против внешнего адреса не запускается')
// События общие для файла и удаляются в конце: параллельные воркеры мешали бы друг другу.
test.describe.configure({ mode: 'serial' })

const RUN = Math.random().toString(36).slice(2, 7)
const T = { open: `Открытое событие ${RUN}`, closed: `Закрытое событие ${RUN}`, hidden: `Секретное событие ${RUN}` }
const created = []
const origin = (baseURL) => ({ Origin: new URL(baseURL).origin })

/** Контакты автора общие для сайта: перед началом и в конце файла возвращаем «пусто» (правит только Директорат). */
async function clearContact(browser, baseURL) {
  const boss = await newAuthor(browser, [], 6, true)
  await boss.page.request.put('/api/team/site', { headers: origin(baseURL), data: { contact: '' } })
  await boss.context.close()
}

test.beforeAll(async ({ browser, baseURL }) => clearContact(browser, baseURL))

test.afterAll(async ({ browser, baseURL }) => {
  // убираем созданное: событие по номеру удаляет тот, у кого есть право
  const editor = await newAuthor(browser, ['author', 'editor'])
  for (const id of created) await editor.page.request.delete(`/api/team/timeline/${id}`, { headers: origin(baseURL) })
  await editor.context.close()
  await clearContact(browser, baseURL)
})

test('Директорат правит контакты автора в панели («Сайт»); на странице они видны ссылками; остальные только читают', async ({ browser, baseURL, page }) => {
  await page.goto('/about')
  await expect(page.getByTestId('author-contact')).toHaveCount(0)

  const boss = await newAuthor(browser, [], 6, true)
  await boss.page.goto('/team/site')
  await expect(boss.page.getByRole('navigation', { name: 'Разделы панели команды' }).getByRole('link', { name: 'Сайт' })).toHaveAttribute('aria-current', 'page')
  await expect(boss.page.getByTestId('site-save')).toBeDisabled() // нечего сохранять
  await boss.page.getByRole('textbox', { name: 'Контакты' }).fill(`Почта: kupol-${RUN}@example.org\nСайт: https://example.org/kupol-${RUN}.\n<b>не тег</b>`)
  await boss.page.getByTestId('site-save').click()
  await expect(boss.page.getByTestId('site-notice')).toContainText('видны на странице')

  // на странице: строки, ссылки, а разметка — текстом
  await page.reload()
  const box = page.getByTestId('author-contact')
  await expect(box).toBeVisible()
  await expect(box.getByRole('link', { name: `kupol-${RUN}@example.org` })).toHaveAttribute('href', `mailto:kupol-${RUN}@example.org`)
  await expect(box.getByRole('link', { name: `https://example.org/kupol-${RUN}` })).toHaveAttribute('href', `https://example.org/kupol-${RUN}`)
  await expect(box.getByRole('link', { name: `https://example.org/kupol-${RUN}` })).toHaveAttribute('rel', /noopener/)
  await expect(box).toContainText('<b>не тег</b>')
  await expect(box.locator('b')).toHaveCount(0)

  // Редактор видит то же, но поле закрыто; сервер править не даёт
  const editor = await newAuthor(browser, ['author', 'editor'])
  await editor.page.goto('/team/site')
  await expect(editor.page.getByTestId('site-readonly')).toBeVisible()
  await expect(editor.page.getByRole('textbox', { name: 'Контакты' })).toBeDisabled()
  await expect(editor.page.getByTestId('site-save')).toHaveCount(0)
  expect((await editor.page.request.put('/api/team/site', { headers: origin(baseURL), data: { contact: 'взлом' } })).status()).toBe(403)
  await editor.context.close()

  // очистка убирает раздел
  await boss.page.getByRole('textbox', { name: 'Контакты' }).fill('')
  await boss.page.getByTestId('site-save').click()
  await expect(boss.page.getByTestId('site-notice')).toContainText('убраны')
  await page.reload()
  await expect(page.getByTestId('author-contact')).toHaveCount(0)
  await boss.context.close()
})

test('страница «О КУПОЛЕ» открыта гостю: справка, уровни, конфиденциальность; содержание ведёт по якорям', async ({ page }) => {
  await page.goto('/about')
  await expect(page.getByRole('heading', { level: 1, name: 'О КУПОЛЕ' })).toBeVisible()
  await expect(page.getByTestId('about-intro')).toContainText('вымышленный архив')
  for (const heading of ['Справка', 'Структура и отделы', 'Термины', 'Как читать архив', 'Уровни допуска', 'Хронология', 'Политика конфиденциальности', 'Об авторе и контакты']) {
    await expect(page.getByRole('heading', { level: 2, name: heading })).toBeVisible()
  }
  await expect(page.getByTestId('levels-table').locator('tbody tr')).toHaveCount(8)
  await expect(page.getByTestId('levels-table')).toContainText('Особый Совет')
  await expect(page.getByTestId('privacy')).toContainText('kupol_session')
  await expect(page.getByTestId('privacy')).toContainText('argon2id')
  // контакты автора не выдуманы: без настройки сборки раздела контактов нет
  await expect(page.getByTestId('author-contact')).toHaveCount(0)

  await page.getByRole('navigation', { name: 'Содержание страницы' }).getByRole('link', { name: 'Конфиденциальность' }).click()
  await expect(page).toHaveURL(/#privacy$/)
  await expect(page.locator('#privacy')).toBeInViewport()
  // ссылка на политику — из подвала любой страницы
  await page.goto('/catalog')
  await page.getByRole('contentinfo').getByRole('link', { name: 'Конфиденциальность' }).click()
  await expect(page).toHaveURL(/\/about#privacy$/)
  await expect(page.locator('#privacy')).toBeInViewport()
})

test('у страницы свой заголовок вкладки; отдаётся по адресу /about', async ({ page }) => {
  // «индексируется только /about» (X-Robots-Tag) решает Apache и проверяется его прогоном (scripts/e2e-apache.sh, check-site.sh)
  const about = await page.request.get('/about')
  expect(about.status()).toBe(200)
  await page.goto('/about')
  await expect(page).toHaveTitle(/О КУПОЛЕ/)
})

test('редактор ведёт хронологию, Автор её только читает; сервер не даёт править без права', async ({ browser, baseURL }) => {
  const editor = await newAuthor(browser, ['author', 'editor'])
  await editor.page.goto('/team/timeline')
  await expect(editor.page.getByTestId('team-timeline')).toBeVisible()
  await expect(editor.page.getByRole('navigation', { name: 'Разделы панели команды' }).getByRole('link', { name: 'Хронология' })).toHaveAttribute('aria-current', 'page')

  // добавление: дата, допуск, ссылка на документ
  await editor.page.getByTestId('event-new').click()
  const dlg = editor.page.getByTestId('event-dialog')
  await dlg.getByLabel('Год').fill('1974')
  await dlg.getByLabel('Месяц').selectOption('3')
  await dlg.getByLabel('День').fill('14')
  await dlg.getByRole('textbox', { name: 'Название' }).fill(T.open)
  await dlg.getByLabel('Описание').fill('Первая запись хронологии.')
  await editor.page.getByTestId('event-save').click()
  await expect(editor.page.getByTestId('timeline-notice')).toContainText(`«${T.open}» добавлено`)
  const row = editor.page.locator('li.event', { hasText: T.open })
  await expect(row).toContainText('14 марта 1974 г.')
  await expect(row).toContainText('Допуск: 0')

  // замечания сервера у полей: 30 февраля и не шифр
  await editor.page.getByTestId('event-new').click()
  await dlg.getByLabel('Год').fill('1980')
  await dlg.getByLabel('Месяц').selectOption('2')
  await dlg.getByLabel('День').fill('30')
  await dlg.getByRole('textbox', { name: 'Название' }).fill('Невозможная дата')
  await dlg.getByLabel('Документ').fill('мусор')
  await editor.page.getByTestId('event-save').click()
  await expect(dlg.getByText('в этом месяце нет такого дня')).toBeVisible()
  await expect(dlg.getByText('не шифр документа')).toBeVisible()
  await editor.page.keyboard.press('Escape')

  // правка: допуск 4
  await row.getByTestId('event-edit').click()
  await expect(dlg.getByRole('textbox', { name: 'Название' })).toHaveValue(T.open)
  await dlg.getByRole('textbox', { name: 'Название' }).fill(T.closed)
  await dlg.getByLabel('Допуск события').selectOption('4')
  await editor.page.getByTestId('event-save').click()
  await expect(editor.page.getByTestId('timeline-notice')).toContainText('сохранено')
  await expect(editor.page.locator('li.event', { hasText: T.closed })).toContainText('Допуск: 4 (Надзиратель)')

  // запомним для уборки и убедимся, что Автор видит, но не правит
  const list = await (await editor.page.request.get('/api/team/timeline')).json()
  for (const e of list.items) if ([T.open, T.closed].includes(e.title)) created.push(e.id)
  const author = await newAuthor(browser, ['author'])
  await author.page.goto('/team/timeline')
  await expect(author.page.locator('li.event', { hasText: T.closed })).toBeVisible()
  await expect(author.page.getByTestId('event-new')).toHaveCount(0)
  await expect(author.page.getByTestId('event-edit')).toHaveCount(0)
  const id = created[0]
  const headers = origin(baseURL)
  expect((await author.page.request.put(`/api/team/timeline/${id}`, { headers, data: { year: 1974, title: 'Взлом', level: 0 } })).status()).toBe(403)
  expect((await author.page.request.delete(`/api/team/timeline/${id}`, { headers })).status()).toBe(403)
  expect((await author.page.request.post('/api/team/timeline', { headers, data: { year: 1974, title: 'Взлом', level: 0 } })).status()).toBe(403)
  await author.context.close()
  await editor.context.close()
})

test('хронология на странице: каждый читатель видит события до своего допуска; закрытое не приходит вовсе', async ({ browser, baseURL, page }) => {
  // третье событие — уровня 6 (Особый Совет) и со ссылкой на закрытый документ О-9003 (уровень 5)
  const editor = await newAuthor(browser, ['author', 'editor'])
  const res = await editor.page.request.post('/api/team/timeline', {
    headers: origin(baseURL),
    data: { year: 1990, month: 6, title: T.hidden, body: 'Только для своих.', level: 6, document_code: 'О-9003' },
  })
  expect(res.status()).toBe(201)
  created.push((await res.json()).event.id)
  await editor.context.close()

  // гость: событие уровня 4 и 6 не видно ни на странице, ни в ответе; уровень 0 — если оно есть (после правки выше открытых нет)
  await page.goto('/about')
  await expect(page.getByTestId('timeline')).toBeVisible()
  await expect(page.getByTestId('timeline')).not.toContainText(RUN)
  const raw = await (await page.request.get('/api/timeline')).text()
  for (const secret of [T.closed, T.hidden, 'Только для своих', 'О-9003']) expect(raw).not.toContain(secret)

  // уровень 4: событие уровня 4 видно, уровня 6 — нет
  const four = await newAuthor(browser, ['author'], 4)
  await four.page.goto('/about')
  await expect(four.page.getByTestId('timeline-list')).toContainText(T.closed)
  await expect(four.page.getByTestId('timeline')).not.toContainText(T.hidden)
  await four.context.close()

  // Особый Совет (6): видит оба; ссылка на документ уровня 5 у него рабочая
  const council = await newAuthor(browser, ['author'], 6)
  await council.page.goto('/about')
  await expect(council.page.getByTestId('timeline-list')).toContainText(T.hidden)
  await council.page.locator('li.event', { hasText: T.hidden }).getByRole('link', { name: /О-9003/ }).click()
  await expect(council.page).toHaveURL(/\/doc\/O-9003$/)
  await council.context.close()

  // уровень 4 не видит закрытого документа в ссылке: событие уровня 4 без ссылки не меняется, событие 6 ему не показано вовсе
  const events = await (await page.request.get('/api/timeline')).json()
  expect(events.items.filter((e) => [T.closed, T.hidden].includes(e.title))).toHaveLength(0)
})

test('уведомление о cookie: показано, закрывается, не возвращается; страница читается и без него', async ({ page }) => {
  await page.goto('/about')
  const notice = page.getByTestId('cookie-notice')
  await expect(notice).toBeVisible()
  await expect(notice).toContainText('техническую cookie')
  // в потоке страницы: не перекрывает содержимое (нижний край текста выше верхнего края уведомления)
  const overlap = await page.evaluate(() => {
    const n = document.querySelector('[data-testid="cookie-notice"]').getBoundingClientRect()
    const main = document.querySelector('main').getBoundingClientRect()
    return main.bottom > n.top + 1
  })
  expect(overlap).toBe(false)
  await page.getByTestId('cookie-dismiss').click()
  await expect(notice).toHaveCount(0)
  await page.reload()
  await expect(page.getByRole('heading', { level: 1, name: 'О КУПОЛЕ' })).toBeVisible()
  await expect(page.getByTestId('cookie-notice')).toHaveCount(0)
})

test('телефон 375 px и доступность: «О КУПОЛЕ» и хронология команды', async ({ browser, page }) => {
  await page.setViewportSize({ width: 375, height: 800 })
  await page.goto('/about')
  await expect(page.getByTestId('about')).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBeLessThanOrEqual(0)
  await page.setViewportSize({ width: 1200, height: 900 })
  await settle(page)
  let r = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()
  expect(r.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(' ')).slice(0, 3).join(' | ')}`), 'О КУПОЛЕ').toEqual([])

  const editor = await newAuthor(browser, ['author', 'editor'])
  await editor.page.goto('/team/timeline')
  await expect(editor.page.getByTestId('team-timeline')).toBeVisible()
  await editor.page.getByTestId('event-new').click()
  await expect(editor.page.getByTestId('event-dialog')).toBeVisible()
  await settle(editor.page)
  r = await new AxeBuilder({ page: editor.page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()
  expect(r.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(' ')).slice(0, 3).join(' | ')}`), 'окно события').toEqual([])
  await editor.context.close()
})
