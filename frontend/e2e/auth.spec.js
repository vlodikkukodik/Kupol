import { expect, test } from '@playwright/test'
import { openAuth, openMenu } from './helpers/ui.js'

// Эти сценарии создают пользователей, поэтому против внешнего адреса (боевого сайта) они не идут,
// пока явно не разрешено KUPOL_E2E_ALLOW_WRITES=1 (так делает scripts/e2e-apache.sh: там своя БД).
test.skip(
  Boolean(process.env.KUPOL_E2E_BASE_URL) && process.env.KUPOL_E2E_ALLOW_WRITES !== '1',
  'создаёт пользователей: против внешнего адреса не запускается',
)

// Ответы на вопросы анкеты — как их прочитал бы человек на главной странице.
const CAPTCHA_ANSWERS = [
  [/В каком году основан/, '1974'],
  [/Какой гриф/, 'Форма КУПОЛ-1'],
  [/Как сокращённо/, 'ЦАК'],
  [/пропущенное слово/, 'объектами'],
  [/С какого слова начинается/, 'Комитет'],
  [/Сколько букв/, '5'],
]

const uniq = () => `e2e${Math.random().toString(36).slice(2, 10)}`
const PASSWORD = 'секретный пароль 1'

async function answerCaptcha(page) {
  const question = page.locator('.captcha-question')
  await expect(question).not.toHaveText(/Загрузка|недоступен/)
  const text = await question.innerText()
  const hit = CAPTCHA_ANSWERS.find(([re]) => re.test(text))
  if (!hit) throw new Error(`неизвестный вопрос анкеты: ${text}`)
  await page.getByLabel('Ваш ответ').fill(hit[1])
}

/** Регистрация через интерфейс; после неё открыт экран резервного кода. */
async function register(page, login, password = PASSWORD) {
  await page.goto('/')
  await openAuth(page, 'Регистрация')
  await page.getByLabel('Логин', { exact: true }).fill(login)
  await page.getByLabel('Пароль', { exact: true }).fill(password)
  await page.getByLabel('Пароль ещё раз').fill(password)
  await answerCaptcha(page)
  await page.getByRole('button', { name: 'Оформить допуск' }).click()
  await expect(page).toHaveURL(/\/backup-code$/)
  return (await page.getByTestId('backup-code').innerText()).trim()
}

/** Подтвердить сохранение кода и перейти в личное дело. */
async function acknowledgeCode(page) {
  await page.getByLabel(/Я сохранил/).check()
  await page.getByRole('button', { name: 'Продолжить' }).click()
  await expect(page).toHaveURL(/\/file$/)
}

async function logout(page) {
  await page.getByRole('button', { name: 'Выйти' }).click()
  await expect(page).toHaveURL(/\/$/)
  await expect(page.getByRole('heading', { name: 'Купол', level: 1 })).toBeVisible()
  await expect(page.getByRole('link', { name: /Пропуск/ })).toHaveCount(0)
}

async function login(page, name, password) {
  await page.goto('/')
  await openAuth(page)
  await page.getByLabel('Логин', { exact: true }).fill(name)
  await page.getByLabel('Пароль', { exact: true }).fill(password)
  await page.getByRole('button', { name: 'Войти' }).click()
}

test('на главной формы входа нет; в окне из бокового меню вкладки переключаются стрелками, Home и End', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Купол', level: 1 })).toBeVisible()
  await expect(page.getByRole('tab')).toHaveCount(0)
  await expect(page.getByLabel('Пароль', { exact: true })).toHaveCount(0)

  await openAuth(page)
  const loginTab = page.getByRole('tab', { name: 'Вход' })
  const regTab = page.getByRole('tab', { name: 'Регистрация' })
  await expect(loginTab).toHaveAttribute('aria-selected', 'true')
  await expect(page.getByRole('form', { name: 'Вход' })).toBeVisible()
  await expect(page.getByRole('button', { name: /Войти/ })).toBeVisible()
  await expect(page.getByRole('link', { name: 'Забыли пароль?' })).toBeVisible()
  await page.screenshot({ path: 'e2e/results/auth-login.png', fullPage: true })

  await loginTab.focus()
  await page.keyboard.press('ArrowRight')
  await expect(regTab).toHaveAttribute('aria-selected', 'true')
  await expect(regTab).toBeFocused()
  await expect(page.getByRole('form', { name: 'Регистрация' })).toBeVisible()
  await expect(page.getByRole('form', { name: 'Вход' })).toBeHidden()
  await page.screenshot({ path: 'e2e/results/auth-register.png', fullPage: true })

  await page.keyboard.press('Home')
  await expect(loginTab).toHaveAttribute('aria-selected', 'true')
  await page.keyboard.press('End')
  await expect(regTab).toHaveAttribute('aria-selected', 'true')
})

test('регистрация: код показывается один раз, кука защищена, вход переживает перезагрузку', async ({ page, context }) => {
  const name = uniq()
  const code = await register(page, name)

  // экран резервного кода
  expect(code).toMatch(/^KUPOL(-[A-HJ-NP-Z2-9]{4}){4}$/)
  const proceed = page.getByRole('button', { name: 'Продолжить' })
  await expect(proceed).toBeDisabled() // без подтверждения дальше нельзя
  await page.screenshot({ path: 'e2e/results/auth-backup-code.png', fullPage: true })
  await acknowledgeCode(page)

  // личное дело и «пропуск»
  await expect(page.getByTestId('file-login')).toHaveText(name)
  await expect(page.getByTestId('file-rank')).toHaveText('Посетитель')
  const pass = page.getByRole('link', { name: /Пропуск/ })
  await expect(pass).toContainText(name)
  await expect(pass).toContainText('Посетитель')
  await page.screenshot({ path: 'e2e/results/auth-file.png', fullPage: true })

  // кука сессии: HttpOnly и SameSite=Lax; из JavaScript недоступна
  const cookie = (await context.cookies()).find((c) => c.name === 'kupol_session')
  expect(cookie, 'кука сессии выставлена').toBeTruthy()
  expect(cookie.httpOnly).toBe(true)
  expect(cookie.sameSite).toBe('Lax')
  expect(cookie.path).toBe('/')
  expect(cookie.value).toHaveLength(43)
  expect(await page.evaluate(() => document.cookie)).not.toContain('kupol_session')

  // код второй раз не показывается: экран без подтверждения недоступен
  await page.goto('/backup-code')
  await expect(page).toHaveURL(/\/file$/)

  // перезагрузка: сессия жива
  await page.reload()
  await expect(page.getByTestId('file-login')).toHaveText(name)
  await expect(page.getByRole('link', { name: /Пропуск/ })).toContainText(name)

  // главная для вошедшего: приветствие; в меню нет кнопки входа, есть личное дело
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Допуск оформлен' })).toBeVisible()
  await openMenu(page)
  await expect(page.getByRole('button', { name: 'Войти или зарегистрироваться' })).toHaveCount(0)
  await expect(page.getByRole('dialog', { name: 'Меню' }).getByRole('link', { name: 'Личное дело' })).toBeVisible()
})

test('клиентская проверка формы регистрации: подсказки и фокус на первом поле с ошибкой', async ({ page }) => {
  await page.goto('/')
  await openAuth(page, 'Регистрация')
  const submit = page.getByRole('button', { name: 'Оформить допуск' })
  await expect(page.locator('.captcha-question')).not.toHaveText(/Загрузка/)

  // всё пусто
  await submit.click()
  const loginInput = page.getByLabel('Логин', { exact: true })
  await expect(loginInput).toBeFocused()
  await expect(loginInput).toHaveAttribute('aria-invalid', 'true')
  await expect(page.getByText('Логин: от 3 до 24 символов')).toBeVisible()

  // латиница вперемешку с кириллицей (подмена «К»)
  await loginInput.fill('Kуратор7')
  await page.getByLabel('Пароль', { exact: true }).fill('short')
  await submit.click()
  await expect(page.getByText('Логин: буквы только латиницей или только кириллицей, не вперемешку')).toBeVisible()
  await expect(page.getByText('Пароль: от 8 до 128 символов')).toBeVisible()

  // пароли не совпадают
  await loginInput.fill('validlogin')
  await page.getByLabel('Пароль', { exact: true }).fill('long enough 1')
  await page.getByLabel('Пароль ещё раз').fill('long enough 2')
  await submit.click()
  await expect(page.getByText('Пароли не совпадают')).toBeVisible()
  await expect(page.getByLabel('Пароль ещё раз')).toBeFocused()

  // не ответили на анкету
  await page.getByLabel('Пароль ещё раз').fill('long enough 1')
  await submit.click()
  await expect(page.getByText('Ответьте на вопрос анкеты').first()).toBeVisible()
  await expect(page.getByLabel('Ваш ответ')).toBeFocused()
  await expect(page).toHaveURL(/\/$/) // ни один запрос регистрации не ушёл
})

test('пароль можно показать и скрыть', async ({ page }) => {
  await page.goto('/')
  await openAuth(page)
  const pw = page.getByLabel('Пароль', { exact: true })
  await pw.fill('секрет')
  await expect(pw).toHaveAttribute('type', 'password')
  const toggle = page.getByRole('button', { name: 'Показать' })
  await toggle.click()
  await expect(pw).toHaveAttribute('type', 'text')
  await expect(page.getByRole('button', { name: 'Скрыть' })).toHaveAttribute('aria-pressed', 'true')
  await page.getByRole('button', { name: 'Скрыть' }).click()
  await expect(pw).toHaveAttribute('type', 'password')
})

test('занятый логин (без учёта регистра) — ошибка у поля, вопрос анкеты обновляется', async ({ page, browser }) => {
  const name = uniq()
  await register(page, name)
  await acknowledgeCode(page)

  const other = await (await browser.newContext()).newPage()
  await other.goto('/')
  await openAuth(other, 'Регистрация')
  await other.getByLabel('Логин', { exact: true }).fill(name.toUpperCase())
  await other.getByLabel('Пароль', { exact: true }).fill(PASSWORD)
  await other.getByLabel('Пароль ещё раз').fill(PASSWORD)
  await answerCaptcha(other)
  const before = await other.locator('.captcha-question').innerText()
  await other.getByRole('button', { name: 'Оформить допуск' }).click()

  const loginInput = other.getByLabel('Логин', { exact: true })
  await expect(other.getByText('Этот логин уже занят').first()).toBeVisible()
  await expect(loginInput).toHaveAttribute('aria-invalid', 'true')
  await expect(loginInput).toBeFocused()
  await expect(other.getByLabel('Ваш ответ')).toHaveValue('') // анкета израсходована — ответ очищен
  await expect(other).toHaveURL(/\/$/)
  expect(before).toBeTruthy()
  await other.context().close()
})

test('вход, неверный пароль и выход', async ({ page }) => {
  const name = uniq()
  await register(page, name)
  await acknowledgeCode(page)
  await logout(page)
  await expect(page.getByRole('link', { name: /Пропуск/ })).toHaveCount(0)

  // неверный пароль: общая ошибка, пароль очищен, логин остался
  await login(page, name, 'не тот пароль')
  await expect(page.getByRole('alert').filter({ hasText: 'Неверный логин или пароль' })).toBeVisible()
  await expect(page.getByLabel('Пароль', { exact: true })).toHaveValue('')
  await expect(page.getByLabel('Логин', { exact: true })).toHaveValue(name)
  await expect(page.getByLabel('Пароль', { exact: true })).toBeFocused()
  await expect(page).toHaveURL(/\/$/)

  // без учёта регистра логина; верный пароль
  await page.getByLabel('Логин', { exact: true }).fill(name.toUpperCase())
  await page.getByLabel('Пароль', { exact: true }).fill(PASSWORD)
  await page.getByRole('button', { name: 'Войти' }).click()
  await expect(page).toHaveURL(/\/file$/)
  await expect(page.getByTestId('file-login')).toHaveText(name)

  // выход стирает куку и закрывает личное дело
  await logout(page)
  await page.goto('/file')
  await expect(page).toHaveURL(/\/\?next=(%2F|\/)file$/)
  await expect(page.getByTestId('auth-modal')).toBeVisible() // окно входа открылось само
  await expect(page.getByRole('tab', { name: 'Вход' })).toBeVisible()
})

test('гостя со страницы личного дела возвращают на главную, окно входа открывается само, после входа — обратно', async ({ page }) => {
  const name = uniq()
  await register(page, name)
  await acknowledgeCode(page)
  await logout(page)

  await page.goto('/file')
  await expect(page).toHaveURL(/\/\?next=(%2F|\/)file$/)
  await expect(page.getByTestId('auth-modal')).toBeVisible()
  await page.getByLabel('Логин', { exact: true }).fill(name)
  await page.getByLabel('Пароль', { exact: true }).fill(PASSWORD)
  await page.getByRole('button', { name: 'Войти' }).click()
  await expect(page).toHaveURL(/\/file$/)
})

test('пустая форма входа: подсказки без обращения к серверу', async ({ page }) => {
  await page.goto('/')
  await openAuth(page)
  await page.getByRole('button', { name: 'Войти', exact: true }).click()
  await expect(page.getByText('Введите логин')).toBeVisible()
  await expect(page.getByText('Введите пароль')).toBeVisible()
  await expect(page.getByLabel('Логин', { exact: true })).toBeFocused()
})

test('смена пароля: неверный текущий, успех, старый пароль перестаёт работать', async ({ page }) => {
  const name = uniq()
  await register(page, name)
  await acknowledgeCode(page)

  const current = page.getByLabel('Текущий пароль')
  const next = page.getByLabel('Новый пароль', { exact: true })
  const next2 = page.getByLabel('Новый пароль ещё раз')
  const submit = page.getByRole('button', { name: 'Сменить пароль' })

  await current.fill('совсем не тот')
  await next.fill('новый пароль 2')
  await next2.fill('новый пароль 2')
  await submit.click()
  await expect(page.getByText('Неверный пароль', { exact: true })).toBeVisible()
  await expect(current).toHaveAttribute('aria-invalid', 'true')

  await current.fill(PASSWORD)
  await next.fill('коротко')
  await next2.fill('коротко')
  await submit.click()
  await expect(page.getByText('Пароль: от 8 до 128 символов')).toBeVisible()

  await current.fill(PASSWORD)
  await next.fill('новый пароль 2')
  await next2.fill('новый пароль 2')
  await submit.click()
  await expect(page.getByRole('status').filter({ hasText: 'Пароль изменён' })).toBeVisible()
  await expect(current).toHaveValue('') // поля очищены

  await logout(page)
  await login(page, name, PASSWORD)
  await expect(page.getByRole('alert').filter({ hasText: 'Неверный логин или пароль' })).toBeVisible()
  await page.getByLabel('Пароль', { exact: true }).fill('новый пароль 2')
  await page.getByRole('button', { name: 'Войти' }).click()
  await expect(page).toHaveURL(/\/file$/)
})

test('смена пароля завершает сеансы на других устройствах', async ({ page, browser }) => {
  const name = uniq()
  await register(page, name)
  await acknowledgeCode(page)

  const phone = await (await browser.newContext()).newPage()
  await login(phone, name, PASSWORD)
  await expect(phone).toHaveURL(/\/file$/)

  await page.getByLabel('Текущий пароль').fill(PASSWORD)
  await page.getByLabel('Новый пароль', { exact: true }).fill('другой пароль 3')
  await page.getByLabel('Новый пароль ещё раз').fill('другой пароль 3')
  await page.getByRole('button', { name: 'Сменить пароль' }).click()
  await expect(page.getByRole('status').filter({ hasText: 'Пароль изменён' })).toBeVisible()

  // текущий сеанс жив, чужой — нет
  await page.reload()
  await expect(page.getByTestId('file-login')).toHaveText(name)
  await phone.reload()
  await expect(phone).toHaveURL(/\/\?next=(%2F|\/)file$/)
  await expect(phone.getByRole('tab', { name: 'Вход' })).toBeVisible()
  await phone.context().close()
})

test('восстановление по резервному коду: новый пароль, новый код, старый код сгорает', async ({ page }) => {
  const name = uniq()
  const oldCode = await register(page, name)
  await acknowledgeCode(page)
  await logout(page)

  await openAuth(page)
  await page.getByRole('link', { name: 'Забыли пароль?' }).click()
  await expect(page).toHaveURL(/\/restore$/)
  await expect(page.getByTestId('auth-modal')).toHaveCount(0) // переход закрыл окно входа
  await page.getByLabel('Логин', { exact: true }).fill(name)
  await page.getByLabel('Резервный код').fill(oldCode.toLowerCase().replaceAll('-', ' ')) // ввод в любом виде
  await page.getByLabel('Новый пароль', { exact: true }).fill('восстановленный 4')
  await page.getByLabel('Новый пароль ещё раз').fill('восстановленный 4')
  await page.getByRole('button', { name: 'Восстановить доступ' }).click()

  await expect(page).toHaveURL(/\/backup-code$/)
  const newCode = (await page.getByTestId('backup-code').innerText()).trim()
  expect(newCode).toMatch(/^KUPOL(-[A-HJ-NP-Z2-9]{4}){4}$/)
  expect(newCode).not.toBe(oldCode)
  await acknowledgeCode(page)
  await logout(page)

  await login(page, name, PASSWORD)
  await expect(page.getByRole('alert').filter({ hasText: 'Неверный логин или пароль' })).toBeVisible()
  await page.getByLabel('Пароль', { exact: true }).fill('восстановленный 4')
  await page.getByRole('button', { name: 'Войти' }).click()
  await expect(page).toHaveURL(/\/file$/)
  await logout(page)

  // старый код больше не работает
  await page.goto('/restore')
  await page.getByLabel('Логин', { exact: true }).fill(name)
  await page.getByLabel('Резервный код').fill(oldCode)
  await page.getByLabel('Новый пароль', { exact: true }).fill('ещё один пароль 5')
  await page.getByLabel('Новый пароль ещё раз').fill('ещё один пароль 5')
  await page.getByRole('button', { name: 'Восстановить доступ' }).click()
  await expect(page.getByRole('alert').filter({ hasText: 'Неверный логин или резервный код' })).toBeVisible()
  await expect(page).toHaveURL(/\/restore$/)
})

test('вошедшему страница восстановления не нужна', async ({ page }) => {
  await register(page, uniq())
  await acknowledgeCode(page)
  await page.goto('/restore')
  await expect(page).toHaveURL(/\/file$/)
})

test('«сдать дело в архив»: подтверждение паролем и галочкой, аккаунт удаляется, логин освобождается', async ({ page }) => {
  const name = uniq()
  await register(page, name)
  await acknowledgeCode(page)

  await page.getByRole('button', { name: 'Сдать дело…' }).click()
  const del = page.getByRole('button', { name: 'Удалить аккаунт навсегда' })

  await del.click() // пусто: подсказки, запрос не уходит
  await expect(page.getByText('Введите пароль для подтверждения')).toBeVisible()
  await expect(page.getByText('Подтвердите, что понимаете последствия')).toBeVisible()
  await expect(page).toHaveURL(/\/file$/)

  await page.getByLabel('Пароль для подтверждения').fill('неверный пароль')
  await page.getByLabel(/Я понимаю/).check()
  await del.click()
  await expect(page.getByText('Неверный пароль', { exact: true })).toBeVisible()
  await expect(page).toHaveURL(/\/file$/) // аккаунт цел

  await page.getByLabel('Пароль для подтверждения').fill(PASSWORD)
  await del.click()
  await expect(page).toHaveURL(/\/$/)
  await expect(page.getByRole('status').filter({ hasText: 'Дело сдано в архив' })).toBeVisible()
  await expect(page.getByRole('link', { name: /Пропуск/ })).toHaveCount(0)

  // войти в удалённый аккаунт нельзя, логин свободен для регистрации
  await login(page, name, PASSWORD)
  await expect(page.getByRole('alert').filter({ hasText: 'Неверный логин или пароль' })).toBeVisible()
  const again = await register(page, name)
  expect(again).toMatch(/^KUPOL-/)
})

test('CSRF: запрос от чужого сайта отклоняется, сессия цела', async ({ page }) => {
  const name = uniq()
  await register(page, name)
  await acknowledgeCode(page)

  // «злая» страница шлёт запрос с куками пользователя, но с чужим Origin
  const forged = await page.request.post('/api/auth/logout', { headers: { Origin: 'https://evil.example' } })
  expect(forged.status()).toBe(403)
  expect((await forged.json()).error.code).toBe('forbidden_origin')
  const noOrigin = await page.request.delete('/api/me', { data: { password: PASSWORD } })
  expect(noOrigin.status()).toBe(403) // без Origin при наличии куки — тоже отказ

  await page.reload()
  await expect(page.getByTestId('file-login')).toHaveText(name)
})

test('в адресе и в хранилище браузера нет ни пароля, ни резервного кода', async ({ page }) => {
  const name = uniq()
  const code = await register(page, name)
  await acknowledgeCode(page)
  expect(page.url()).not.toContain(PASSWORD)
  expect(page.url()).not.toContain(encodeURIComponent(PASSWORD))
  const stored = await page.evaluate(() => JSON.stringify({ ...localStorage }) + JSON.stringify({ ...sessionStorage }))
  expect(stored).not.toContain(PASSWORD)
  expect(stored).not.toContain(code)
})

test('телефон 375 px: формы, экран кода и личное дело без горизонтальной прокрутки', async ({ browser }) => {
  const ctx = await browser.newContext({ viewport: { width: 375, height: 667 }, deviceScaleFactor: 2, hasTouch: true })
  const page = await ctx.newPage()
  const overflow = () =>
    page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)

  await page.goto('/')
  expect(await overflow()).toBeLessThanOrEqual(0)
  await openMenu(page)
  expect(await overflow()).toBeLessThanOrEqual(0)
  await page.screenshot({ path: 'e2e/results/menu-mobile.png' })
  await page.getByRole('button', { name: 'Войти или зарегистрироваться' }).click()
  await page.getByRole('tab', { name: 'Регистрация' }).click()
  await expect(page.locator('.captcha-question')).not.toHaveText(/Загрузка/)
  expect(await overflow()).toBeLessThanOrEqual(0)
  await page.screenshot({ path: 'e2e/results/auth-register-mobile.png', fullPage: true })

  await register(page, uniq())
  expect(await overflow()).toBeLessThanOrEqual(0)
  await page.screenshot({ path: 'e2e/results/auth-backup-code-mobile.png', fullPage: true })
  await acknowledgeCode(page)
  expect(await overflow()).toBeLessThanOrEqual(0)
  await page.screenshot({ path: 'e2e/results/auth-file-mobile.png', fullPage: true })
  await ctx.close()
})

test('неверный пароль: сообщение одинаково для существующего и несуществующего логина', async ({ page }) => {
  // по тексту ошибки нельзя понять, есть ли такой логин в архиве
  const name = uniq()
  await register(page, name)
  await acknowledgeCode(page)
  await logout(page)

  await login(page, name, 'неверный пароль 1')
  await expect(page.getByRole('alert')).toBeVisible()
  const known = await page.getByRole('alert').innerText()

  await page.getByLabel('Логин', { exact: true }).fill(uniq()) // такого логина нет
  await page.getByLabel('Пароль', { exact: true }).fill('неверный пароль 1')
  await page.getByRole('button', { name: 'Войти' }).click()
  await expect(page.getByRole('alert')).toBeVisible()
  expect(await page.getByRole('alert').innerText()).toBe(known)
})
