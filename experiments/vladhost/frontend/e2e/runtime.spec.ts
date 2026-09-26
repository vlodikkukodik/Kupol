import { expect, test, type Page } from '@playwright/test'
import { startFakeHelper } from './helpers'

// Кнопки-переключатели Naive UI: щёлкаем по самой кнопке, а не по скрытому радио-полю.
const pick = (page: Page, name: string) => page.locator('.n-radio-button', { hasText: name }).first().click()

test('среда выполнения: PHP, приложение, перезапуск, журнал и возврат к статике', async ({ page, request }) => {
  test.setTimeout(90_000)
  const seen: string[] = []
  const timer = startFakeHelper(seen)
  let siteId = 0
  let token = ''
  try {
    const admin = process.env.E2E_ADMIN!
    const login = await request.post('/api/auth/login', { data: { login: admin, password: process.env.E2E_ADMIN_PASSWORD } })
    token = (await login.json()).access_token as string
    const slug = `rt${Date.now().toString(36)}`.slice(0, 20)
    const created = await request.post('/api/sites', { data: { slug }, headers: { Authorization: `Bearer ${token}` } })
    expect(created.ok()).toBeTruthy()
    const id = (await created.json()).site.id as number
    siteId = id

    await page.goto('/login')
    await page.getByLabel('Email или имя').fill(admin)
    await page.getByLabel('Пароль').fill(process.env.E2E_ADMIN_PASSWORD!)
    await page.getByRole('button', { name: 'Войти' }).click()
    await expect(page.getByText(`Здравствуйте, ${admin}`)).toBeVisible()
    await page.goto(`/sites/${id}/runtime`)
    await expect(page.getByRole('heading', { name: 'Среда выполнения' })).toBeVisible()

    // Python на стенде не установлен: вариант недоступен и объяснён.
    await expect(page.getByRole('radio', { name: /Python/ })).toBeDisabled()
    await expect(page.getByText('не установлено на сервере')).toBeVisible()
    await expect(page.getByRole('button', { name: 'Применить' })).toBeDisabled()

    // PHP
    await pick(page, 'PHP')
    await expect(page.getByText('Скрипты .php выполняются')).toBeVisible()
    await page.getByRole('button', { name: 'Применить' }).click()
    await expect(page.getByText('Настройки применены').first()).toBeVisible()
    await expect(page.getByTestId('runtime-state')).toHaveText('работает')
    expect(seen).toContain('apply:php:8.3')

    // Node.js: команда обязательна, порт выдаёт сервер
    await pick(page, 'Node.js')
    await page.getByRole('textbox', { name: 'Команда запуска' }).fill('a'.repeat(501))
    await page.getByRole('button', { name: 'Применить' }).click()
    await expect(page.getByText(/Команда запуска: одна строка/)).toBeVisible()
    await page.getByRole('textbox', { name: 'Команда запуска' }).fill('node server.js')
    await page.getByRole('button', { name: 'Применить' }).click()
    await expect(page.getByText(/Порт приложения: 2\d{4}/)).toBeVisible()
    expect(seen).toContain('apply:node:')

    // Журнал и перезапуск
    await page.getByRole('button', { name: 'Показать журнал' }).click()
    await expect(page.getByTestId('runtime-logs')).toContainText('Server listening on 127.0.0.1')
    await page.getByRole('button', { name: 'Перезапустить' }).click()
    await expect(page.getByText('Перезапущено').first()).toBeVisible()
    expect(seen).toContain('restart:node:')

    // Возврат к статике
    await pick(page, 'Статика')
    await page.getByRole('button', { name: 'Применить' }).click()
    await expect(page.getByText('Настройки применены').first()).toBeVisible()
    await expect(page.getByRole('button', { name: 'Перезапустить' })).toHaveCount(0)
    expect(seen.some((s) => s.startsWith('stop:'))).toBeTruthy()
  } finally {
    // Уборка: сайт стенда удаляется (иначе лимит сайтов исчерпается для остальных тестов); исполнитель ещё отвечает.
    if (token && siteId) await request.delete(`/api/sites/${siteId}`, { headers: { Authorization: `Bearer ${token}` } })
    clearInterval(timer)
  }
})
