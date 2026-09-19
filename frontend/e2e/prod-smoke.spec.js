import { expect, test } from '@playwright/test'
import { openAuth } from './helpers/ui.js'

// Дымовая проверка боевого сайта с записью: регистрируется тестовый аккаунт, проверяются свойства,
// видимые только на проде (кука Secure по https, путь через nginx хостинга), и аккаунт УДАЛЯЕТСЯ —
// даже если проверка упала посередине. Запуск только явно:
//
//   KUPOL_E2E_BASE_URL=https://kupol.vladinc.ru KUPOL_E2E_SMOKE=1 npx playwright test e2e/prod-smoke.spec.js
//
// Использует одну регистрацию из лимита «3 в час на IP».
test.skip(process.env.KUPOL_E2E_SMOKE !== '1', 'запускается только явно: KUPOL_E2E_SMOKE=1')

const ANSWERS = [
  [/В каком году основан/, '1974'],
  [/Какой гриф/, 'Форма КУПОЛ-1'],
  [/Как сокращённо/, 'ЦАК'],
  [/пропущенное слово/, 'объектами'],
  [/С какого слова начинается/, 'Комитет'],
  [/Сколько букв/, '5'],
]

test('прод: регистрация, Secure-кука, вход переживает перезагрузку, удаление аккаунта', async ({ page, context, baseURL }) => {
  const login = `smoke${Math.random().toString(36).slice(2, 9)}`
  const password = 'дымовой тест 1'
  let created = false

  try {
    await page.goto('/')
    await openAuth(page, 'Регистрация')
    const question = page.locator('.captcha__question')
    await expect(question).not.toHaveText(/Загрузка|недоступен/)
    const text = await question.innerText()
    await page.getByLabel('Логин', { exact: true }).fill(login)
    await page.getByLabel('Пароль', { exact: true }).fill(password)
    await page.getByLabel('Пароль ещё раз').fill(password)
    await page.getByLabel('Ваш ответ').fill(ANSWERS.find(([re]) => re.test(text))[1])
    await page.getByRole('button', { name: 'Оформить допуск' }).click()
    await expect(page).toHaveURL(/\/backup-code$/)
    created = true

    const code = (await page.getByTestId('backup-code').innerText()).trim()
    expect(code).toMatch(/^KUPOL(-[A-HJ-NP-Z2-9]{4}){4}$/)
    await page.getByLabel(/Я сохранил/).check()
    await page.getByRole('button', { name: 'Продолжить' }).click()
    await expect(page.getByTestId('file-login')).toHaveText(login)
    await expect(page.getByTestId('file-rank')).toHaveText('Посетитель')

    // свойства куки на проде
    const cookie = (await context.cookies()).find((c) => c.name === 'kupol_session')
    expect(cookie, 'кука сессии выставлена').toBeTruthy()
    expect(cookie.secure, 'Secure по https').toBe(true)
    expect(cookie.httpOnly).toBe(true)
    expect(cookie.sameSite).toBe('Lax')
    expect(cookie.domain).toBe(new URL(baseURL).hostname) // host-only: на api-поддомен не уходит

    // сессия переживает перезагрузку (кука ходит через nginx хостинга и PHP-прокси)
    await page.reload()
    await expect(page.getByTestId('file-login')).toHaveText(login)

    // удаление аккаунта через интерфейс
    await page.getByRole('button', { name: 'Сдать дело…' }).click()
    await page.getByLabel('Пароль для подтверждения').fill(password)
    await page.getByLabel(/Я понимаю/).check()
    await page.getByRole('button', { name: 'Удалить аккаунт навсегда' }).click()
    await expect(page).toHaveURL(/\/$/)
    await expect(page.getByRole('status').filter({ hasText: 'Дело сдано в архив' })).toBeVisible()
    created = false

    // логин больше не работает
    await openAuth(page)
    await page.getByLabel('Логин', { exact: true }).fill(login)
    await page.getByLabel('Пароль', { exact: true }).fill(password)
    await page.getByRole('button', { name: 'Войти' }).click()
    await expect(page.getByRole('alert').filter({ hasText: 'Неверный логин или пароль' })).toBeVisible()
  } finally {
    if (created) {
      // проверка упала до удаления — убираем за собой напрямую через API
      const res = await page.request.delete('/api/me', { data: { password }, headers: { Origin: new URL(baseURL).origin } })
      console.log(`очистка тестового аккаунта ${login}: HTTP ${res.status()}`)
    }
  }
})
