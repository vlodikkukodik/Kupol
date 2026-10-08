import { expect, test } from '@playwright/test'

// Планировщик: создание задачи, проверка расписания и адреса, запуск, журнал, отключение, удаление.
test('планировщик: задача по адресу, журнал и защита внутренней сети', async ({ page }) => {
  const admin = process.env.E2E_ADMIN!
  await page.goto('/login')
  await page.getByLabel('Email или имя').fill(admin)
  await page.getByLabel('Пароль').fill(process.env.E2E_ADMIN_PASSWORD!)
  await page.getByRole('button', { name: 'Войти' }).click()
  await page.getByRole('navigation', { name: 'Основное меню' }).getByText('Планировщик').click()
  await expect(page.getByRole('heading', { name: 'Планировщик' })).toBeVisible()

  const name = `job${Date.now().toString(36)}`
  await page.getByRole('button', { name: 'Новая задача' }).click()
  const dialog = page.getByRole('dialog')

  // Команды на стенде выключены: этот вид недоступен, а причина объяснена.
  await expect(dialog.getByText('Команды на этом сервере не включены')).toBeVisible()
  await expect(dialog.getByRole('radio', { name: 'Команда на сервере' })).toBeDisabled()

  await dialog.getByRole('textbox', { name: 'Название' }).fill(name)
  await dialog.getByRole('textbox', { name: 'Адрес' }).fill('http://127.0.0.1:9/x')
  await dialog.getByRole('textbox', { name: 'Расписание' }).fill('* * * * *')
  await dialog.getByRole('button', { name: 'Создать', exact: true }).click()
  await expect(dialog.getByText('не чаще раза в 5 мин')).toBeVisible()

  await dialog.getByRole('button', { name: 'Каждые 5 минут' }).click()
  await dialog.getByRole('button', { name: 'Создать', exact: true }).click()
  await expect(dialog.getByText('не во внутреннюю сеть')).toBeVisible()

  await dialog.getByRole('textbox', { name: 'Адрес' }).fill('https://example.com/cron')
  await dialog.getByRole('button', { name: 'Создать', exact: true }).click()
  const card = page.getByTestId(`job-${name}`)
  await expect(card).toBeVisible()
  await expect(card.getByText('*/5 * * * *')).toBeVisible()
  await expect(card.getByText('Ещё не запускалась')).toBeVisible()

  // Ручной запуск и журнал (сам запрос уходит в интернет, поэтому исход не проверяем — только что запуск записан).
  await card.getByRole('button', { name: 'Запустить сейчас' }).click()
  await expect(page.getByText('Запуск начат')).toBeVisible()
  await card.getByRole('button', { name: 'Журнал' }).click()
  const log = page.getByRole('dialog')
  await expect(log.getByText(/Журнал:/)).toBeVisible()
  await expect(log.locator('.run').first()).toBeVisible()
  await page.keyboard.press('Escape')

  // Отключение и включение.
  const sw = card.getByRole('switch', { name: new RegExp(`Включена: ${name}`) })
  await sw.click()
  await expect(card.getByText('выключена')).toBeVisible()
  await expect(card.getByText('Следующего запуска нет')).toBeVisible()

  // Удаление.
  await card.getByRole('button', { name: 'Удалить' }).click()
  await page.getByRole('button', { name: 'Подтвердить' }).click()
  await expect(card).toHaveCount(0)
})
