import { expect, test, type Page } from '@playwright/test'

test.use({ actionTimeout: 10_000 })

async function login(page: Page, name: string, password: string) {
  await page.goto('/login')
  await page.getByLabel('Email или имя').fill(name)
  await page.getByLabel('Пароль').fill(password)
  await page.getByRole('button', { name: 'Войти' }).click()
  await expect(page.getByText(`Здравствуйте, ${name}`)).toBeVisible()
}

// Журнал действий: вход, неудачные попытки, действия над сайтом и файлами, фильтр по виду, «показать ещё», язык, чужой журнал.
test('журнал действий: события аккаунта с адресом и временем', async ({ page, request }) => {
  test.setTimeout(120_000)
  const admin = process.env.E2E_ADMIN!
  const adminToken = (await (await request.post('/api/auth/login', { data: { login: admin, password: process.env.E2E_ADMIN_PASSWORD } })).json()).access_token as string
  const adminAuth = { Authorization: `Bearer ${adminToken}` }
  const invite = (await (await request.post('/api/invites', { data: {}, headers: adminAuth })).json()).invite.code as string
  const stamp = Date.now().toString(36)
  const user = `ac${stamp}`.slice(0, 20)
  const password = 'user-password-123'
  const reg = await request.post('/api/auth/register', { data: { invite, email: `${user}@example.com`, username: user, password } })
  expect(reg.ok(), await reg.text()).toBeTruthy()
  const token = (await reg.json()).access_token as string
  const auth = { Authorization: `Bearer ${token}` }

  // Неудачные попытки входа с одного адреса сливаются в одну строку
  for (let i = 0; i < 3; i++) {
    expect((await request.post('/api/auth/login', { data: { login: user, password: 'wrong-password' } })).status()).toBe(401)
  }
  // Действия через API: сайт, много правок файлов, смена настроек профиля
  const slug = `ac${stamp}`.slice(0, 20)
  const site = (await (await request.post('/api/sites', { data: { slug }, headers: auth })).json()).site as { id: number; host: string }
  for (let i = 0; i < 5; i++) {
    const r = await request.put(`/api/sites/${site.id}/file?path=f${i}.html`, { data: { content: '<p>x</p>' }, headers: auth })
    expect(r.status()).toBe(204)
  }
  expect((await request.patch('/api/me', { data: { notify_email: false }, headers: auth })).ok()).toBeTruthy()

  // Пользователь в браузере: вход тоже попадает в журнал
  await login(page, user, password)
  await page.getByRole('navigation', { name: 'Основное меню' }).getByText('Журнал действий', { exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Журнал действий', exact: true })).toBeVisible()
  const list = page.getByTestId('activity-list')
  await expect(list.getByTestId('event-auth.login')).toBeVisible()
  await expect(list.getByTestId('event-auth.register')).toBeVisible()
  const failed = list.getByTestId('event-auth.login_failed')
  await expect(failed).toContainText('Неудачная попытка входа')
  await expect(failed).toContainText('×3')
  const created = list.getByTestId('event-site.create')
  await expect(created).toContainText('Создан сайт')
  await expect(created).toContainText(site.host)
  await expect(created).toContainText('Адрес: ')
  const files = list.getByTestId('event-files.change')
  await expect(files).toContainText('Изменены файлы сайта')
  await expect(files).toContainText('×5')
  await expect(list.getByTestId('event-profile.update')).toBeVisible()

  // Фильтр по виду
  await page.getByTestId('activity-cat-sites').click()
  await expect(list.getByTestId('event-site.create')).toBeVisible()
  await expect(list.getByTestId('event-auth.login')).toHaveCount(0)
  await page.getByTestId('activity-cat-security').click()
  await expect(list.getByTestId('event-auth.login')).toBeVisible()
  await expect(list.getByTestId('event-site.create')).toHaveCount(0)
  await page.getByTestId('activity-cat-all').click()

  // Язык событий следует за языком интерфейса
  await page.getByRole('button', { name: 'IT' }).click()
  await expect(page.getByTestId('activity-list').getByTestId('event-site.create')).toContainText('Creato un sito')
  await page.getByRole('button', { name: 'RU' }).click()

  // Больше страницы: кнопка «Показать ещё» подгружает старые события
  for (let i = 0; i < 12; i++) {
    expect((await request.patch('/api/me', { data: { notify_email: i % 2 === 0 }, headers: auth })).ok()).toBeTruthy()
  }
  const many = await (await request.get('/api/activity?limit=10', { headers: auth })).json()
  expect(many.events.length).toBe(10)
  expect(many.next).toBeGreaterThan(0)
  const rest = await (await request.get(`/api/activity?limit=100&before=${many.next}`, { headers: auth })).json()
  expect(rest.events.length).toBeGreaterThan(0)
  expect(rest.events[0].id).toBeLessThan(many.next)

  // Чужой журнал недоступен: у другого пользователя своих событий нет
  const inv2 = (await (await request.post('/api/invites', { data: {}, headers: adminAuth })).json()).invite.code as string
  const other = `ao${stamp}`.slice(0, 20)
  const tok2 = (await (await request.post('/api/auth/register', { data: { invite: inv2, email: `${other}@example.com`, username: other, password } })).json()).access_token as string
  const theirs = await (await request.get('/api/activity', { headers: { Authorization: `Bearer ${tok2}` } })).json()
  expect(theirs.events.map((e: { kind: string }) => e.kind)).toEqual(['auth.register'])
  expect((await request.get('/api/activity')).status()).toBe(401)
})
