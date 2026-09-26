import { expect, test } from '@playwright/test'

// Раздел «Базы данных»: создание в обоих СУБД, одноразовый пароль, лимит, удаление.
test('базы данных: создание, пароль, проверка размера, удаление', async ({ page }) => {
  const admin = process.env.E2E_ADMIN!
  await page.goto('/login')
  await page.getByLabel('Email или имя').fill(admin)
  await page.getByLabel('Пароль').fill(process.env.E2E_ADMIN_PASSWORD!)
  await page.getByRole('button', { name: 'Войти' }).click()
  await page.getByRole('navigation', { name: 'Основное меню' }).getByText('Базы данных').click()
  await expect(page.getByRole('heading', { name: 'Базы данных' })).toBeVisible()

  const base = `e${Date.now().toString(36)}`.slice(0, 12)
  const names = { postgres: `${base}p`, mariadb: `${base}m` }

  // Ошибка проверки формы видна до отправки.
  await page.getByLabel('Имя базы').fill('Bad-Name')
  await page.getByRole('button', { name: 'Создать базу' }).click()
  await expect(page.getByText('Только латинские буквы в нижнем регистре и цифры, до 20 символов')).toBeVisible()

  for (const [engine, name] of Object.entries(names)) {
    await page.getByRole('radio', { name: engine === 'postgres' ? 'PostgreSQL' : 'MariaDB' }).check({ force: true })
    await page.getByLabel('Имя базы').fill(name)
    await page.getByRole('button', { name: 'Создать базу' }).click()
    const dialog = page.getByRole('dialog')
    await expect(dialog.getByTestId('db-login')).toHaveText(`${admin}_${name}`)
    expect((await dialog.getByTestId('db-password').innerText()).length).toBe(24)
    await page.keyboard.press('Escape')
    await expect(dialog).toBeHidden()
    await expect(page.getByTestId(`db-${admin}_${name}`)).toBeVisible()
  }

  // Проверка размера и новый пароль.
  const card = page.getByTestId(`db-${admin}_${names.postgres}`)
  await card.getByRole('button', { name: 'Проверить размер' }).click()
  await expect(page.getByText('Размер обновлён')).toBeVisible()
  await expect(async () => {
    await card.getByRole('button', { name: 'Новый пароль' }).click()
    await expect(page.getByRole('button', { name: 'Подтвердить' })).toBeVisible({ timeout: 1500 })
  }).toPass({ timeout: 15_000 })
  await page.getByRole('button', { name: 'Подтвердить' }).click()
  await expect(page.getByTestId('db-password')).toBeVisible()
  await page.keyboard.press('Escape')

  // Удаление обеих баз.
  for (const name of Object.values(names)) {
    const c = page.getByTestId(`db-${admin}_${name}`)
    // Список перерисовывается после предыдущих действий и может закрыть окно подтверждения: открываем его, пока не появится.
    await expect(async () => {
      await c.getByRole('button', { name: 'Удалить' }).click()
      await expect(page.getByRole('button', { name: 'Подтвердить' })).toBeVisible({ timeout: 1500 })
    }).toPass({ timeout: 15_000 })
    await page.getByRole('button', { name: 'Подтвердить' }).click()
    await expect(c).toHaveCount(0, { timeout: 20_000 }) // удаление базы на настоящем сервере может занять несколько секунд
  }
})
