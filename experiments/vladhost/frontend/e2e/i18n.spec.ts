import { expect, test, type Page } from '@playwright/test'

// Язык по умолчанию в конфиге — русский (locale ru-RU); здесь проверяем остальные сценарии.

async function login(page: Page, login: string, password: string) {
  await page.getByLabel(/^(Email или имя|Email o nome utente)$/).fill(login)
  await page.getByLabel(/^(Пароль|Password)$/).fill(password)
  await page.getByRole('button', { name: /^(Войти|Accedi)$/ }).click()
}

test.describe('браузер с итальянским языком', () => {
  test.use({ locale: 'it-IT' })

  test('язык определяется автоматически, сервер отвечает по-итальянски', async ({ page }) => {
    await page.goto('/login')
    await expect(page.getByRole('heading', { name: 'Bentornato' })).toBeVisible()
    await expect(page.locator('html')).toHaveAttribute('lang', 'it')
    await expect(page).toHaveTitle(/hosting gratuito per sviluppatori/)

    // Ошибка сервера приходит на языке интерфейса (заголовок Accept-Language из переключателя).
    await login(page, process.env.E2E_ADMIN!, 'wrong-password')
    await expect(page.getByText('Nome utente o password errati')).toBeVisible()

    // Проверки формы на клиенте тоже переведены.
    await page.goto('/register')
    await page.getByRole('button', { name: 'Crea account' }).click()
    await expect(page.getByText('Inserisci il codice di invito')).toBeVisible()
    await expect(page.getByText('Email non valida')).toBeVisible()
  })

  test('переключатель в шапке меняет язык сразу, выбор запоминается и подхватывается сервером', async ({ page }) => {
    await page.goto('/login')
    await login(page, process.env.E2E_ADMIN!, process.env.E2E_ADMIN_PASSWORD!)
    await expect(page.getByText(`Ciao, ${process.env.E2E_ADMIN}`)).toBeVisible()
    await expect(page.getByRole('navigation', { name: 'Menu principale' }).getByText('Impostazioni')).toBeVisible()

    // Переключаем на русский без перезагрузки страницы.
    await page.getByRole('group', { name: 'Lingua' }).getByRole('button', { name: /RU/ }).click()
    await expect(page.getByText(`Здравствуйте, ${process.env.E2E_ADMIN}`)).toBeVisible()
    await expect(page.locator('html')).toHaveAttribute('lang', 'ru')
    await expect(page).toHaveTitle(/бесплатный хостинг для разработчиков/)

    // Выбор пережил перезагрузку: приоритет у сохранённого выбора, а не у языка браузера (it-IT).
    await page.reload()
    await expect(page.getByText(`Здравствуйте, ${process.env.E2E_ADMIN}`)).toBeVisible()

    // Даты и размеры форматируются по языку; на странице сайтов тоже всё по-русски.
    await page.getByRole('navigation', { name: 'Основное меню' }).getByText('Сайты').click()
    await expect(page.getByRole('heading', { name: 'Сайты', exact: true })).toBeVisible()

    // Обратно на итальянский — и сервер снова отвечает по-итальянски (создание второго сайта недоступно из-за лимита не проверяем,
    // а вот отказ по правам у обычной страницы — проверяем через ответ API ниже).
    await page.getByRole('group', { name: 'Язык' }).getByRole('button', { name: /IT/ }).click()
    await expect(page.getByRole('heading', { name: 'Siti', exact: true })).toBeVisible()
  })

  test('на странице настроек язык выбирается плиткой и подсказка про автовыбор исчезает после выбора', async ({ page }) => {
    await page.goto('/login')
    await login(page, process.env.E2E_ADMIN!, process.env.E2E_ADMIN_PASSWORD!)
    await page.getByRole('navigation', { name: 'Menu principale' }).getByText('Impostazioni').click()
    await expect(page.getByText('La lingua è stata scelta automaticamente')).toBeVisible()
    await page.getByRole('button', { name: /Русский/ }).click()
    await expect(page.getByRole('heading', { name: 'Настройки' })).toBeVisible()
    await expect(page.getByText('Язык выбран автоматически')).toHaveCount(0)
  })
})

test.describe('браузер с неподдерживаемым языком', () => {
  test.use({ locale: 'de-DE' })

  test('берётся русский по умолчанию', async ({ page }) => {
    await page.goto('/login')
    await expect(page.getByRole('heading', { name: 'С возвращением' })).toBeVisible()
    await expect(page.locator('html')).toHaveAttribute('lang', 'ru')
  })
})

test('язык, выбранный в шапке страницы входа, запоминается и применяется после входа', async ({ page }) => {
  await page.goto('/login')
  await expect(page.getByRole('heading', { name: 'С возвращением' })).toBeVisible() // ru-RU по умолчанию
  await page.getByRole('group', { name: 'Язык' }).getByRole('button', { name: /IT/ }).click()
  await expect(page.getByRole('heading', { name: 'Bentornato' })).toBeVisible()
  await login(page, process.env.E2E_ADMIN!, process.env.E2E_ADMIN_PASSWORD!)
  await expect(page.getByText(`Ciao, ${process.env.E2E_ADMIN}`)).toBeVisible()
})

test('сообщение сервера приходит на выбранном языке (через API)', async ({ request }) => {
  const bad = { login: 'nobody', password: 'wrong-password' }
  const it = await request.post('/api/auth/login', { data: bad, headers: { 'Accept-Language': 'it' } })
  expect((await it.json()).error).toMatchObject({ code: 'invalid_credentials', message: 'Nome utente o password errati' })
  const ru = await request.post('/api/auth/login', { data: bad, headers: { 'Accept-Language': 'ru' } })
  expect((await ru.json()).error).toMatchObject({ code: 'invalid_credentials', message: 'Неверный логин или пароль' })
})
