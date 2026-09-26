import AxeBuilder from '@axe-core/playwright'
import { expect, test } from '@playwright/test'
import { canWrite } from './helpers/kupol.js'
import { newAuthor } from './helpers/team.js'
import { settle } from './helpers/ui.js'

// Два языка интерфейса — русский и итальянский (этап i18n). Переключатель в шапке, выбор запоминается, язык браузера
// определяет начальный, сервер отвечает на языке запроса, данные документов остаются как написаны.
test.skip(!canWrite, 'заводит пользователей: против внешнего адреса не запускается')

const nav = (page) => page.getByRole('navigation', { name: /Разделы|Sezioni/ })

test('по умолчанию русский; переключатель в шапке переводит интерфейс, <html lang> и заголовок вкладки', async ({ page }) => {
  await page.goto('/')
  await expect(page.locator('html')).toHaveAttribute('lang', 'ru')
  await expect(page).toHaveTitle('КУПОЛ — Центральный архив')
  await expect(page.getByTestId('lang-switcher').getByRole('button', { name: /RU/ })).toHaveAttribute('aria-pressed', 'true')

  await page.getByTestId('lang-it').click()
  await expect(page.locator('html')).toHaveAttribute('lang', 'it')
  await expect(page).toHaveTitle('KUPOL — Archivio centrale')
  await expect(nav(page).getByRole('link', { name: 'Catalogo' })).toBeVisible()
  await expect(page.getByTestId('lang-it')).toHaveAttribute('aria-pressed', 'true')
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Kupol')
  await expect(page.getByText('Modulo KUPOL-1').first()).toBeVisible()
  // ни одного русского слова в интерфейсе шапки, подвала и главной, кроме данных архива
  const chrome = (await page.locator('header, footer, main h1, main h2').allInnerTexts()).join(' ')
  expect(chrome).not.toMatch(/[А-Яа-яЁё]/)

  // обратно
  await page.getByTestId('lang-ru').click()
  await expect(page.locator('html')).toHaveAttribute('lang', 'ru')
  await expect(nav(page).getByRole('link', { name: 'Каталог' })).toBeVisible()
})

test('выбор запоминается: после перезагрузки и на других страницах язык тот же', async ({ page }) => {
  await page.goto('/')
  await page.getByTestId('lang-it').click()
  await page.goto('/about')
  await expect(page.locator('html')).toHaveAttribute('lang', 'it')
  await expect(page.getByRole('heading', { level: 1, name: 'SU KUPOL' })).toBeVisible()
  await expect(page).toHaveTitle('SU KUPOL — l’archivio immaginario KUPOL')
  await expect(page.getByTestId('levels-table')).toContainText('Cittadino')
  await expect(page.getByTestId('levels-table')).toContainText('Direttorato')
  await page.reload()
  await expect(page.locator('html')).toHaveAttribute('lang', 'it')
  await page.goto('/catalog')
  await expect(page.getByRole('heading', { level: 1, name: 'Catalogo' })).toBeVisible()
  await expect(page.getByLabel('Tipo')).toBeVisible()
  await expect(page).toHaveTitle('Catalogo — KUPOL')
})

test('?lang=it в ссылке открывает итальянскую версию и запоминает выбор', async ({ page }) => {
  await page.goto('/about?lang=it')
  await expect(page.locator('html')).toHaveAttribute('lang', 'it')
  await page.goto('/catalog')
  await expect(page.getByRole('heading', { level: 1, name: 'Catalogo' })).toBeVisible()
})

test.describe('язык браузера', () => {
  test.use({ locale: 'it-IT' })
  test('итальянский браузер сразу получает итальянский интерфейс', async ({ page }) => {
    await page.goto('/')
    await expect(page.locator('html')).toHaveAttribute('lang', 'it')
    await expect(nav(page).getByRole('link', { name: 'Ricerca' })).toBeVisible()
  })
})

test.describe('другой язык браузера — русский', () => {
  test.use({ locale: 'de-DE' })
  test('незнакомый язык — основной, русский', async ({ page }) => {
    await page.goto('/')
    await expect(page.locator('html')).toHaveAttribute('lang', 'ru')
  })
})

test('сообщения сервера приходят по-итальянски: вход с ошибкой, справочник, звание, документ гостю', async ({ page, browser }) => {
  await page.goto('/')
  await page.getByTestId('lang-it').click()

  // подсказки формы — из интерфейса, ответ сервера — с сервера; язык запроса им обоим один
  await page.getByRole('button', { name: 'Menu', exact: true }).click()
  await expect(page.getByRole('dialog', { name: 'Menu' })).toBeVisible()
  await settle(page)
  await page.getByRole('button', { name: 'Accedi o registrati' }).click()
  const modal = page.getByTestId('auth-modal')
  await expect(modal).toBeVisible()
  await settle(page)
  await modal.getByRole('button', { name: 'Accedi' }).click()
  await expect(modal.getByText('Inserisci il login')).toBeVisible()
  await modal.getByLabel('Login').fill('nessuno_qui')
  await modal.getByLabel('Password').fill('sbagliata123')
  await modal.getByRole('button', { name: 'Accedi' }).click()
  await expect(modal.getByRole('alert').first()).toContainText('Login o password errati')

  // регистрация: вопрос анкеты по-итальянски, на итальянском же принимается ответ
  await modal.getByRole('tab', { name: 'Registrazione' }).click()
  await expect(modal.getByText(/KUPOL|Kupol|comitato|Comitato/).first()).toBeVisible()
  const question = await modal.locator('.captcha__question').innerText()
  expect(question).not.toMatch(/[А-Яа-яЁё]/)

  // документ гостю: закрытый — «Accesso negato» и уровень словами
  const res = await page.request.get('/api/documents/O-9002', { headers: { 'Accept-Language': 'it' } })
  const body = await res.json()
  expect([403, 404]).toContain(res.status())
  if (res.status() === 403) expect(body.error.required_level_name).toMatch(/^[A-Za-z ]+$/)

  // Директорат: звание в пропуске и в «Личном деле» — по-итальянски
  const boss = await newAuthor(browser, [], 6, true)
  await boss.page.goto('/file?lang=it')
  await expect(boss.page.getByTestId('file-rank')).toHaveText('Direttorato')
  await boss.page.goto('/team/roles')
  await expect(boss.page.getByTestId('my-roles')).toContainText('Direttorato')
  await expect(boss.page.getByRole('heading', { name: 'I tuoi ruoli' })).toBeVisible()
  await boss.context.close()
})

test('смена языка обновляет данные с сервера без перезагрузки страницы: названия типов, звание в пропуске', async ({ browser }) => {
  const boss = await newAuthor(browser, [], 6, true)
  const page = boss.page
  await page.goto('/catalog?from=1900&to=1999')
  await expect(page.getByTestId('registry').locator('tbody tr').first()).toBeVisible()
  // название типа — строка под названием документа (само название — данные)
  const kind = page.getByTestId('registry').locator('.sub').first()
  await expect(kind).toContainText(/Инцидент|Объект|Приказ|Меморандум/)
  await expect(page.locator('.masthead__pass, header a[href="/file"]').first()).toContainText('Директорат')

  await page.getByTestId('lang-it').click()
  await expect(kind).toContainText(/Incidente|Oggetto|Ordine|Memorandum/)
  await expect(kind).not.toContainText(/Инцидент|Объект|Приказ|Меморандум/)
  await expect(page.locator('header a[href="/file"]').first()).toContainText('Direttorato')
  // названия документов — данные, они не переводятся
  await expect(page.getByTestId('registry').first()).toContainText('О-9001')
  await boss.context.close()
})

test('панель команды: разделы, форма документа и сообщения об ошибке — по-итальянски', async ({ browser, baseURL }) => {
  const author = await newAuthor(browser, ['author'])
  const page = author.page
  await page.goto('/team?lang=it')
  await expect(page.getByRole('navigation', { name: 'Sezioni del pannello della squadra' }).getByRole('link', { name: 'Documenti' })).toBeVisible()
  await page.goto('/team/documents/new')
  await expect(page.getByRole('heading', { name: 'Nuovo documento' })).toBeVisible()
  await page.getByLabel('Tipo di documento').selectOption({ label: 'Memorandum' })
  await page.getByRole('button', { name: 'Crea la bozza' }).click()
  // пустые поля: сообщения приходят и от интерфейса (год), и от сервера (шифр, название)
  await expect(page.locator('.new-doc [role="alert"], .new-doc .error, .new-doc [aria-invalid="true"]').first()).toBeVisible()
  const text = await page.locator('.new-doc').innerText()
  expect(text).not.toMatch(/Введите|не может быть пустым|не указан/)

  // документ создаётся и открывается: интерфейс редактора и его подсказки итальянские, данные — как написаны
  const res = await page.request.post('/api/team/documents', {
    headers: { Origin: new URL(baseURL).origin, 'Accept-Language': 'it' },
    data: { type: 'memo', code: `МЕМО-${9400 + Math.floor(Math.random() * 500)}`, title: 'Служебная записка', composed: { year: 1979 }, blocks: [{ id: 'b1', type: 'paragraph', data: { text: 'Текст записки.' } }] },
  })
  expect(res.status()).toBe(201)
  const { document } = await res.json()
  await page.goto(`/team/documents/${document.id}`)
  await expect(page.getByTestId('block-editor')).toBeVisible()
  await expect(page.getByRole('toolbar', { name: 'Modifica del documento' })).toBeVisible()
  await expect(page.getByRole('group', { name: 'Sezione del documento' })).toBeVisible()
  await expect(page.getByTestId('doc-status')).toContainText('Bozza')
  await expect(page.getByRole('heading', { level: 2, name: 'Служебная записка' })).toBeVisible()
  await author.context.close()
})

test('доступность: итальянские главная, «SU KUPOL», каталог и переключатель (WCAG 2.1 AA), мобильный экран без горизонтальной прокрутки', async ({ page }) => {
  for (const path of ['/?lang=it', '/about', '/catalog']) {
    await page.goto(path)
    await settle(page)
    const results = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()
    expect(results.violations.map((v) => `${v.id}: ${v.nodes.length}`), path).toEqual([])
  }
  await page.setViewportSize({ width: 375, height: 800 })
  await page.goto('/about')
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)
  expect(overflow).toBeLessThanOrEqual(0)
  // переключатель доступен на телефоне
  await expect(page.getByTestId('lang-switcher')).toBeVisible()
})
