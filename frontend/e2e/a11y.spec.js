import { expect, test } from '@playwright/test'
import AxeBuilder from '@axe-core/playwright'
import { canWrite, signUp } from './helpers/kupol.js'
import { openAuth, openMenu } from './helpers/ui.js'

// Автоматическая проверка доступности (WCAG 2.1 A/AA) на всех экранах. Экраны для вошедших требуют
// регистрации, поэтому идут только там, где разрешена запись (как и остальные сценарии аккаунтов).

const TAGS = ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']

async function expectAccessible(page, what) {
  const results = await new AxeBuilder({ page }).withTags(TAGS).analyze()
  const summary = results.violations.map((v) => `${v.id} (${v.impact}): ${v.help}\n  ${v.nodes.map((n) => n.target.join(' ')).join('\n  ')}`)
  expect(summary, `нарушения доступности: ${what}`).toEqual([])
}

const uniq = () => `ax${Math.random().toString(36).slice(2, 10)}`

test('гость: главная', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Купол', level: 1 })).toBeVisible()
  await expectAccessible(page, 'главная')
})

test('гость: боковое меню', async ({ page }) => {
  await page.goto('/')
  await openMenu(page)
  await expectAccessible(page, 'боковое меню')
})

test('гость: окно входа', async ({ page }) => {
  await page.goto('/')
  await openAuth(page)
  await expect(page.getByRole('tab', { name: 'Вход' })).toBeVisible()
  await expectAccessible(page, 'окно входа')
})

test('гость: окно регистрации', async ({ page }) => {
  await page.goto('/')
  await openAuth(page, 'Регистрация')
  await expect(page.locator('.captcha-question')).not.toHaveText(/Загрузка/)
  await expectAccessible(page, 'окно регистрации')
})

test('гость: формы с показанными ошибками', async ({ page }) => {
  await page.goto('/')
  await openAuth(page)
  await page.getByRole('button', { name: 'Войти', exact: true }).click()
  await expect(page.getByText('Введите логин')).toBeVisible()
  await expectAccessible(page, 'вход с ошибками')

  await page.getByRole('tab', { name: 'Регистрация' }).click()
  await expect(page.locator('.captcha-question')).not.toHaveText(/Загрузка/)
  await page.getByRole('button', { name: 'Оформить допуск' }).click()
  await expect(page.getByText('Логин: от 3 до 24 символов')).toBeVisible()
  await expectAccessible(page, 'регистрация с ошибками')
})

test('гость: восстановление доступа', async ({ page }) => {
  await page.goto('/restore')
  await expect(page.getByRole('heading', { name: 'Восстановление доступа' })).toBeVisible()
  await page.getByRole('button', { name: 'Восстановить доступ' }).click()
  await expect(page.getByText('Введите резервный код')).toBeVisible()
  await expectAccessible(page, 'восстановление с ошибками')
})

test('страница «Дело не найдено»', async ({ page }) => {
  await page.goto('/no/such/page')
  await expect(page.getByRole('heading', { name: 'Дело не найдено' })).toBeVisible()
  await expectAccessible(page, '404')
})

test('вошедший: резервный код, личное дело, форма удаления', async ({ page }) => {
  test.skip(!canWrite, 'создаёт пользователя')
  await page.goto('/')
  await openAuth(page, 'Регистрация')
  await expect(page.locator('.captcha-question')).not.toHaveText(/Загрузка/)
  const q = await page.locator('.captcha-question').innerText()
  const answers = [
    [/В каком году основан/, '1974'], [/Какой гриф/, 'Форма КУПОЛ-1'], [/Как сокращённо/, 'ЦАК'],
    [/пропущенное слово/, 'объектами'], [/С какого слова начинается/, 'Комитет'], [/Сколько букв/, '5'],
  ]
  await page.getByLabel('Логин', { exact: true }).fill(uniq())
  await page.getByLabel('Пароль', { exact: true }).fill('секретный пароль 1')
  await page.getByLabel('Пароль ещё раз').fill('секретный пароль 1')
  await page.getByLabel('Ваш ответ').fill(answers.find(([re]) => re.test(q))[1])
  await page.getByRole('button', { name: 'Оформить допуск' }).click()

  await expect(page.getByRole('heading', { name: 'Резервный код' })).toBeVisible()
  await expectAccessible(page, 'резервный код')

  await page.getByLabel(/Я сохранил/).check()
  await page.getByRole('button', { name: 'Продолжить' }).click()
  await expect(page.getByRole('heading', { name: 'Личное дело' })).toBeVisible()
  await expectAccessible(page, 'личное дело')

  await page.getByRole('button', { name: 'Сдать дело…' }).click()
  await page.getByRole('button', { name: 'Удалить аккаунт навсегда' }).click()
  await expect(page.getByText('Введите пароль для подтверждения')).toBeVisible()
  await expectAccessible(page, 'удаление аккаунта с ошибками')
})

// Документы: фикстуры загружает global-setup (нужна запись в БД).
test.describe('документы и каталог', () => {
  test.skip(!canWrite, 'нужны фикстуры документов')

  test('главная с лентой «Поступило в ЦАК»', async ({ page }) => {
    await page.goto('/')
    await expect(page.getByTestId('recent-feed')).toBeVisible()
    await expectAccessible(page, 'главная с лентой')
  })

  test('каталог: реестр, фильтры, пустой результат, папки', async ({ page }) => {
    await page.goto('/catalog?from=1900&to=1999')
    await expect(page.getByTestId('registry')).toBeVisible()
    await expectAccessible(page, 'каталог, реестр')

    await page.goto('/catalog?type=memo&from=1902&to=1902')
    await expect(page.getByRole('navigation', { name: 'Страницы каталога' })).toBeVisible()
    await expectAccessible(page, 'каталог, страницы')

    await page.goto('/catalog?from=1950&to=1950')
    await expect(page.getByTestId('catalog-empty')).toBeVisible()
    await expectAccessible(page, 'каталог, ничего не найдено')

    await page.goto('/catalog?view=folders')
    await expect(page.getByTestId('folders')).toBeVisible()
    await expectAccessible(page, 'каталог, папки')
  })

  test('документ гостю: все типы блоков, плашки, зачернённые фрагменты', async ({ page }) => {
    await page.goto('/doc/O-9001')
    await expect(page.locator('article.dossier .plate').first()).toBeVisible()
    await expectAccessible(page, 'документ О-9001 (гость)')

    await page.goto('/doc/PRIKAZ-1901-91')
    await expect(page.getByRole('heading', { level: 1 })).toBeVisible()
    await expectAccessible(page, 'приказ')
  })

  test('«Доступ запрещён»', async ({ page }) => {
    await page.goto('/doc/O-9002')
    await expect(page.getByTestId('access-denied')).toBeVisible()
    await expectAccessible(page, 'доступ запрещён')
  })

  test('Директорат: документ без плашек, каталог со статусами', async ({ browser }) => {
    const context = await browser.newContext()
    await signUp(context, { level: 1, directorate: true })
    const page = await context.newPage()
    await page.goto('/doc/O-9001')
    await expect(page.locator('article.dossier')).toBeVisible()
    await expectAccessible(page, 'документ О-9001 (Директорат)')
    await page.goto('/catalog?type=object&from=1979&to=1990')
    await expect(page.getByTestId('registry')).toBeVisible()
    await expectAccessible(page, 'каталог (Директорат)')
    await context.close()
  })
})
