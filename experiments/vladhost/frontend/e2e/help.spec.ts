import { expect, test } from '@playwright/test'

test.use({ actionTimeout: 10_000 })

// Справка: список статей, поиск, разделы, статья со ссылками, переключение языка и переход в поддержку.
test('справка: поиск, разделы, статья и язык', async ({ page }) => {
  const admin = process.env.E2E_ADMIN!
  await page.goto('/login')
  await page.getByLabel('Email или имя').fill(admin)
  await page.getByLabel('Пароль').fill(process.env.E2E_ADMIN_PASSWORD!)
  await page.getByRole('button', { name: 'Войти' }).click()
  await expect(page.getByText(`Здравствуйте, ${admin}`)).toBeVisible()

  await page.getByRole('navigation', { name: 'Основное меню' }).getByText('Справка', { exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Справка', exact: true })).toBeVisible()
  await expect(page.getByTestId('help-item-01-quick-start')).toBeVisible()
  await expect(page.getByTestId('help-item-06-mail')).toBeVisible()

  // Поиск: подходящие статьи остаются, остальные скрываются
  const search = page.getByTestId('help-search').getByRole('textbox')
  await search.fill('сертификат')
  await expect(page.getByTestId('help-item-04-https')).toBeVisible()
  await expect(page.getByTestId('help-item-12-cron')).toHaveCount(0)
  await search.fill('слово которого нет ни в одной статье qwertyzxc')
  await expect(page.getByTestId('help-empty')).toBeVisible()
  await search.fill('')

  // Раздел
  await page.getByTestId('help-cat-mail').click()
  await expect(page.getByTestId('help-item-06-mail')).toBeVisible()
  await expect(page.getByTestId('help-item-02-ftp')).toHaveCount(0)
  await page.getByRole('button', { name: 'Все разделы' }).click()

  // Статья: заголовок, внутренняя ссылка ведёт в панель
  await page.getByTestId('help-item-01-quick-start').click()
  const article = page.getByTestId('help-article')
  await expect(article.getByRole('heading', { name: 'Быстрый старт: первый сайт за пять минут' })).toBeVisible()
  await expect(article).toContainText('Что получится')
  await article.getByRole('link', { name: 'Сайты' }).click()
  await expect(page).toHaveURL(/\/sites$/)

  // Язык статьи следует за языком интерфейса
  await page.goto('/help/01-quick-start')
  await page.getByRole('button', { name: 'IT' }).click()
  await expect(page.getByTestId('help-article').getByRole('heading', { name: 'Guida rapida: il primo sito in cinque minuti' })).toBeVisible()
  await expect(page.getByTestId('help-article')).toContainText('Cosa otterrai')
  await page.getByRole('link', { name: 'Scrivi al supporto' }).click()
  await expect(page).toHaveURL(/\/support$/)

  // Несуществующая статья
  await page.getByRole('button', { name: 'RU' }).click()
  await page.goto('/help/no-such-article')
  await expect(page.getByTestId('help-missing')).toBeVisible()
})
