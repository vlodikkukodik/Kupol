import { expect, test } from '@playwright/test'
import { BULK_COUNT, canWrite, signUp } from './helpers/kupol.js'
import { openMenu } from './helpers/ui.js'

// Каталог и лента на главной работают с документами-фикстурами (см. documents.spec.js).
test.skip(!canWrite, 'нужны фикстуры и пользователи: против внешнего адреса не запускается')

const rows = (page) => page.getByTestId('registry').locator('tbody tr')
const codesOf = async (page) => (await rows(page).locator('td.code').allTextContents()).map((s) => s.trim())

// Годы фикстур: 1901 (приказ, инцидент), 1902 (30 меморандумов), 1979–1990 (объекты).

test.describe('гость', () => {
  test('реестр показывает только доступное: закрытые документы не видны ни строкой, ни счётчиком', async ({ page }) => {
    await page.goto('/catalog?type=object&from=1900&to=1999')
    await expect(rows(page).first()).toBeVisible()
    const codes = await codesOf(page)
    expect(codes).toContain('О-9001')
    for (const closed of ['О-9002', 'О-9003', 'О-9004', 'О-9007']) expect(codes).not.toContain(closed)
    const text = await page.locator('main').textContent()
    expect(text).not.toContain('СЕКРЕТ-')

    const list = await (await page.request.get('/api/documents?type=object')).text()
    expect(list).not.toContain('СЕКРЕТ-')
    expect(list).not.toContain('О-9003')
  })

  test('фильтры меняют адрес и строки; «Сбросить» возвращает всё', async ({ page }) => {
    await page.goto('/catalog?from=1900&to=1902')
    await expect(rows(page).first()).toBeVisible()

    await page.getByLabel('Тип', { exact: true }).selectOption('order')
    await expect(page).toHaveURL(/type=order/)
    await expect.poll(() => codesOf(page)).toEqual(['ПРИКАЗ-1901-91'])

    await page.getByRole('button', { name: 'Сбросить фильтры' }).first().click()
    await expect(page).not.toHaveURL(/type=/)
    await expect(page).not.toHaveURL(/from=/)

    // объекты: по классу, отделу, категории, статусу содержания
    await page.getByLabel('Тип', { exact: true }).selectOption('object')
    await page.getByLabel('Класс опасности').selectOption('3')
    await expect(page).toHaveURL(/class=3/)
    await expect.poll(() => codesOf(page)).toContain('О-9001')
    await page.getByLabel('Отдел').selectOption('ОТД-2')
    await page.getByLabel('Категория').selectOption('entity')
    await page.getByLabel('Статус содержания').selectOption('contained')
    await expect(page).toHaveURL(/dept=%D0%9E%D0%A2%D0%94-2|dept=ОТД-2/)
    await expect.poll(() => codesOf(page)).toContain('О-9001')

    await page.getByLabel('Категория').selectOption('place')
    await expect.poll(() => codesOf(page)).not.toContain('О-9001')
  })

  test('период по годам', async ({ page }) => {
    await page.goto('/catalog?type=memo')
    await page.getByLabel('Год не ранее').fill('1902')
    await page.getByLabel('Год не позднее').fill('1902')
    await page.getByLabel('Год не позднее').blur()
    await expect(page).toHaveURL(/from=1902/)
    await expect(page).toHaveURL(/to=1902/)
    await expect(page.getByRole('status').filter({ hasText: 'Найдено' })).toHaveText(`Найдено: ${BULK_COUNT}`)
  })

  test('сортировка по заголовку: aria-sort, адрес и порядок строк', async ({ page }) => {
    await page.goto('/catalog?from=1901&to=1901')
    await expect.poll(() => codesOf(page)).toEqual(['ПРИКАЗ-1901-91']) // гость: инцидент (допуск 1) не виден

    await page.goto('/catalog?type=memo&from=1902&to=1902')
    const codeHeader = page.getByRole('columnheader', { name: /№/ })
    await expect(codeHeader).toHaveAttribute('aria-sort', 'ascending')
    await expect.poll(async () => (await codesOf(page))[0]).toBe('МЕМО-9001')

    await codeHeader.getByRole('button').click()
    await expect(codeHeader).toHaveAttribute('aria-sort', 'descending')
    await expect(page).toHaveURL(/order=desc/)
    await expect.poll(async () => (await codesOf(page))[0]).toBe('МЕМО-9030')

    const title = page.getByRole('columnheader', { name: /Название/ })
    await title.getByRole('button').click()
    await expect(title).toHaveAttribute('aria-sort', 'ascending')
    await expect(codeHeader).toHaveAttribute('aria-sort', 'none')
    await expect(page).toHaveURL(/sort=title/)
    await expect.poll(async () => (await codesOf(page))[0]).toBe('МЕМО-9001')
  })

  test('естественная сортировка шифров: 9002 раньше 9010', async ({ page }) => {
    await page.goto('/catalog?type=memo&from=1902&to=1902')
    await expect.poll(async () => (await codesOf(page)).length).toBe(25)
    const codes = await codesOf(page)
    expect(codes.slice(0, 3)).toEqual(['МЕМО-9001', 'МЕМО-9002', 'МЕМО-9003'])
  })

  test('постраничная навигация: 25 на странице, «Далее» и «Назад», прямая ссылка на страницу', async ({ page }) => {
    await page.goto('/catalog?type=memo&from=1902&to=1902')
    await expect.poll(async () => (await codesOf(page)).length).toBe(25)
    const nav = page.getByRole('navigation', { name: 'Страницы каталога' })
    await expect(nav.locator('[aria-current="page"]')).toHaveText('1')
    await expect(nav.getByRole('link', { name: '← Назад' })).toHaveCount(0)

    await nav.getByRole('link', { name: 'Далее →' }).click()
    await expect(page).toHaveURL(/page=2/)
    await expect.poll(async () => (await codesOf(page)).length).toBe(BULK_COUNT - 25)
    await expect(nav.locator('[aria-current="page"]')).toHaveText('2')
    await expect(nav.getByRole('link', { name: 'Далее →' })).toHaveCount(0)

    await nav.getByRole('link', { name: '← Назад' }).click()
    await expect(page).not.toHaveURL(/page=/)
    await expect.poll(async () => (await codesOf(page)).length).toBe(25)

    await page.goto('/catalog?type=memo&from=1902&to=1902&page=2')
    await expect.poll(async () => (await codesOf(page)).length).toBe(BULK_COUNT - 25)

    // смена фильтра возвращает на первую страницу
    await page.getByLabel('Год не позднее').fill('1903')
    await page.getByLabel('Год не позднее').blur()
    await expect(page).not.toHaveURL(/page=/)
  })

  test('кнопка «назад» браузера возвращает прежние условия', async ({ page }) => {
    await page.goto('/catalog?from=1900&to=1902')
    await page.getByLabel('Тип', { exact: true }).selectOption('memo')
    await expect(page).toHaveURL(/type=memo/)
    await page.goBack()
    await expect(page).not.toHaveURL(/type=memo/)
    await expect(page.getByLabel('Тип', { exact: true })).toHaveValue('')
  })

  test('пустой результат: понятное сообщение и сброс', async ({ page }) => {
    await page.goto('/catalog?from=1950&to=1950')
    await expect(page.getByTestId('catalog-empty')).toContainText('ничего не найдено')
    await page.getByTestId('catalog-empty').getByRole('button', { name: 'Сбросить фильтры' }).click()
    await expect(page).not.toHaveURL(/from=/)
    await expect(rows(page).first()).toBeVisible()
  })

  test('мусор в адресе не ломает каталог', async ({ page }) => {
    await page.goto('/catalog?type=нет&class=99&sort=drop&order=sideways&page=-4&from=abc')
    // сервер отвечает понятной ошибкой, а не пустым экраном
    await expect(page.locator('main')).toContainText(/Каталог/)
    await expect(page.getByRole('heading', { name: 'Каталог' })).toBeVisible()
  })

  test('«Папки»: счётчики по типам и переход в реестр', async ({ page }) => {
    await page.goto('/catalog')
    await page.getByRole('button', { name: 'Папки' }).click()
    await expect(page).toHaveURL(/view=folders/)
    const folders = page.getByTestId('folders')
    await expect(folders.locator('[data-type="object"]')).toBeVisible()
    await expect(page.getByRole('button', { name: 'Папки' })).toHaveAttribute('aria-pressed', 'true')

    await folders.locator('[data-type="order"]').click()
    await expect(page).toHaveURL(/type=order/)
    await expect(page).not.toHaveURL(/view=folders/)
    await expect.poll(() => codesOf(page)).toContain('ПРИКАЗ-1901-91')
    await expect(page.getByRole('button', { name: 'Реестр' })).toHaveAttribute('aria-pressed', 'true')
  })

  test('строка реестра ведёт к документу', async ({ page }) => {
    await page.goto('/catalog?type=order&from=1901&to=1901')
    await page.getByRole('link', { name: '[e2e] Приказ об отчётности' }).click()
    await expect(page).toHaveURL(/\/doc\/PRIKAZ-1901-91$/)
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('[e2e] Приказ об отчётности')
  })

  test('главная: лента «Поступило в ЦАК» и переход в каталог', async ({ page }) => {
    await page.goto('/')
    const feed = page.getByTestId('recent-feed')
    await expect(feed).toBeVisible()
    expect(await feed.locator('li').count()).toBeLessThanOrEqual(8)
    await expect(page.getByRole('link', { name: /Весь каталог/ })).toBeVisible()

    await feed.locator('a').first().click()
    await expect(page).toHaveURL(/\/doc\//)

    await openMenu(page)
    await page.getByRole('navigation', { name: 'Основная навигация' }).getByRole('link', { name: 'Каталог' }).click()
    await expect(page).toHaveURL(/\/catalog$/)
    await expect(page.getByRole('dialog', { name: 'Меню' })).toHaveCount(0) // переход закрыл меню
    await openMenu(page)
    await expect(page.getByRole('navigation', { name: 'Основная навигация' }).getByRole('link', { name: 'Каталог' })).toHaveAttribute('aria-current', 'page')
  })
})

test.describe('вошедшие', () => {
  test('уровень 1 видит инцидент допуска 1, а уровень 0 — нет', async ({ browser }) => {
    const guest = await browser.newPage()
    await guest.goto('/catalog?from=1901&to=1901')
    await expect.poll(() => codesOf(guest)).toEqual(['ПРИКАЗ-1901-91'])
    await guest.close()

    const context = await browser.newContext()
    await signUp(context, { level: 1 })
    const page = await context.newPage()
    await page.goto('/catalog?from=1901&to=1901')
    await expect.poll(() => codesOf(page)).toEqual(['ИНЦ-1901-91', 'ПРИКАЗ-1901-91'])
    await context.close()
  })

  test('Директорат видит всё каталога, включая черновик и уровень 7, со статусом', async ({ browser }) => {
    const context = await browser.newContext()
    await signUp(context, { level: 1, directorate: true })
    const page = await context.newPage()
    await page.goto('/catalog?type=object&from=1979&to=1990')
    await expect.poll(() => codesOf(page)).toEqual(expect.arrayContaining(['О-9001', 'О-9002', 'О-9003', 'О-9004', 'О-9007']))
    await expect(rows(page).filter({ hasText: 'О-9004' })).toContainText('черновик')
    await context.close()
  })

  test('уровень 2 видит О-9002, но не О-9003 (допуск 5) и не О-9007', async ({ browser }) => {
    const context = await browser.newContext()
    await signUp(context, { level: 2 })
    const page = await context.newPage()
    await page.goto('/catalog?type=object&from=1979&to=1990')
    await expect.poll(() => codesOf(page)).toEqual(expect.arrayContaining(['О-9001', 'О-9002']))
    const codes = await codesOf(page)
    for (const closed of ['О-9003', 'О-9004', 'О-9007']) expect(codes).not.toContain(closed)
    await context.close()
  })
})

test.describe('телефон 375 px', () => {
  test.use({ viewport: { width: 375, height: 800 } })

  for (const path of ['/catalog?from=1900&to=1999', '/catalog?view=folders', '/doc/O-9001', '/doc/O-9002']) {
    test(`${path}: нет горизонтальной прокрутки страницы`, async ({ page }) => {
      await page.goto(path)
      await expect(page.locator('main h1').first()).toBeVisible()
      await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
    })
  }
})
