import { expect, test, type Page } from '@playwright/test'

test.use({ actionTimeout: 10_000 })

async function login(page: Page, name: string, password: string) {
  await page.goto('/login')
  await page.getByLabel('Email или имя').fill(name)
  await page.getByLabel('Пароль').fill(password)
  await page.getByRole('button', { name: 'Войти' }).click()
  await expect(page.getByText(`Здравствуйте, ${name}`)).toBeVisible()
}

const menu = (page: Page, name: string) => page.getByRole('navigation', { name: 'Основное меню' }).getByText(name, { exact: true })

// Обращения в поддержку: пользователь пишет, администратор отвечает в очереди, пользователь видит ответ и закрывает обращение.
test('поддержка: обращение, ответ администратора, закрытие и повторное открытие', async ({ page, browser, request }) => {
  test.setTimeout(120_000)
  const admin = process.env.E2E_ADMIN!
  const adminPassword = process.env.E2E_ADMIN_PASSWORD!
  const adminToken = (await (await request.post('/api/auth/login', { data: { login: admin, password: adminPassword } })).json()).access_token as string
  const adminAuth = { Authorization: `Bearer ${adminToken}` }
  const invite = (await (await request.post('/api/invites', { data: {}, headers: adminAuth })).json()).invite.code as string
  const stamp = Date.now().toString(36)
  const user = `sp${stamp}`.slice(0, 20)
  const password = 'user-password-123'
  const reg = await request.post('/api/auth/register', { data: { invite, email: `${user}@example.com`, username: user, password } })
  expect(reg.ok(), await reg.text()).toBeTruthy()

  // Пользователь: форма с проверкой ввода и отправка
  await login(page, user, password)
  await menu(page, 'Поддержка').click()
  await expect(page.getByRole('heading', { name: 'Поддержка', exact: true })).toBeVisible()
  await expect(page.getByTestId('tickets-empty')).toBeVisible()
  await page.getByTestId('ticket-send').click()
  await expect(page.getByText('Тема: от 3 до 120 знаков')).toBeVisible()
  await expect(page.getByText('Выберите тему обращения')).toBeVisible()
  await expect(page.getByText('Введите сообщение')).toBeVisible()
  const subject = `Не открывается сайт ${stamp}`
  await page.getByRole('textbox', { name: 'Тема' }).fill(subject)
  await page.getByTestId('ticket-category').click()
  await page.locator('.n-base-select-option', { hasText: 'Сайты и файлы' }).click()
  await page.getByRole('textbox', { name: 'Сообщение' }).fill('Сайт отдаёт ошибку 502.\nАдрес: example.vladinc.ru')
  await page.getByTestId('ticket-send').click()
  await expect(page.getByTestId('ticket-subject')).toHaveText(subject)
  await expect(page.getByTestId('ticket-status')).toHaveText('ждёт ответа')
  await expect(page.getByTestId('ticket-thread')).toContainText('Сайт отдаёт ошибку 502.')
  const ticketUrl = page.url()
  const id = ticketUrl.split('/').pop()!

  // Администратор в своём окне: видит обращение в очереди, отвечает
  const adminCtx = await browser.newContext({ baseURL: page.url().split('/support')[0] })
  const adminPage = await adminCtx.newPage()
  await login(adminPage, admin, adminPassword)
  await menu(adminPage, 'Поддержка').click()
  await expect(adminPage.getByTestId('tickets-queue')).toContainText(subject)
  await expect(adminPage.getByTestId(`queue-${id}`)).toContainText(`От: ${user}`)
  await adminPage.getByTestId(`queue-${id}`).click()
  await expect(adminPage.getByTestId('ticket-subject')).toHaveText(subject)
  await expect(adminPage.getByText(`${user}@example.com`)).toBeVisible()
  await adminPage.getByRole('textbox', { name: 'Ответить' }).fill('Посмотрите журнал ошибок на вкладке «Журналы».')
  await adminPage.getByTestId('ticket-reply').click()
  await expect(adminPage.getByTestId('ticket-status')).toHaveText('есть ответ')
  await adminCtx.close()

  // Пользователь: значок на пункте меню, ответ без имени администратора, своё сообщение
  await page.goto('/support')
  await expect(page.getByTestId('support-badge')).toHaveText('1')
  await page.getByTestId(`ticket-${id}`).click()
  await expect(page.getByTestId('ticket-status')).toHaveText('есть ответ')
  const thread = page.getByTestId('ticket-thread')
  await expect(thread).toContainText('Посмотрите журнал ошибок')
  await expect(thread).toContainText('Поддержка')
  await expect(thread).not.toContainText(admin)
  await page.getByRole('textbox', { name: 'Ответить' }).fill('Спасибо, помогло!')
  await page.getByTestId('ticket-reply').click()
  await expect(thread).toContainText('Спасибо, помогло!')
  await expect(page.getByTestId('ticket-status')).toHaveText('ждёт ответа')

  // Закрытие и повторное открытие
  await page.getByTestId('ticket-close').click()
  await page.getByRole('button', { name: 'Подтвердить' }).click()
  await expect(page.getByTestId('ticket-status')).toHaveText('закрыто')
  await expect(page.getByText('Обращение закрыто. Новое сообщение откроет его снова.')).toBeVisible()
  await page.getByTestId('ticket-reopen').click()
  await expect(page.getByTestId('ticket-status')).toHaveText('ждёт ответа')

  // Чужое обращение недоступно: у второго пользователя оно не открывается
  const other = `so${stamp}`.slice(0, 20)
  const inv2 = (await (await request.post('/api/invites', { data: {}, headers: adminAuth })).json()).invite.code as string
  const tok2 = (await (await request.post('/api/auth/register', { data: { invite: inv2, email: `${other}@example.com`, username: other, password } })).json()).access_token as string
  expect((await request.get(`/api/tickets/${id}`, { headers: { Authorization: `Bearer ${tok2}` } })).status()).toBe(404)
  expect((await request.get('/api/admin/tickets', { headers: { Authorization: `Bearer ${tok2}` } })).status()).toBe(403)
})
