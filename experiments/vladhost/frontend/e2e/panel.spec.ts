import { expect, test, type Page } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { makeZip } from './zip'

const nav = (page: Page) => page.getByRole('navigation', { name: 'Основное меню' })

async function login(page: Page, login: string, password: string) {
  await page.goto('/login')
  await page.getByLabel('Email или имя').fill(login)
  await page.getByLabel('Пароль').fill(password)
  await page.getByRole('button', { name: 'Войти' }).click()
}

test('приглашение → регистрация → сайт → деплой → правка файла → выход', async ({ page, browser }) => {
  test.setTimeout(120_000) // длинный сценарий: bcrypt, выпуск токенов, анимации
  const adminName = process.env.E2E_ADMIN!
  const adminPass = process.env.E2E_ADMIN_PASSWORD!
  const user = `u${Date.now().toString(36)}`

  // Админ создаёт инвайт.
  await login(page, adminName, adminPass)
  await expect(page.getByText(`Здравствуйте, ${adminName}`)).toBeVisible()
  await nav(page).getByText('Настройки').click()
  // В списке лежат и старые инвайты прошлых прогонов: ждём, пока появится новый, а не читаем первую строку.
  const codes = page.locator('td code')
  await expect(page.getByRole('button', { name: /Создать инвайт/ })).toBeEnabled()
  const before = await codes.count()
  await page.getByRole('button', { name: /Создать инвайт/ }).click()
  await expect(codes).toHaveCount(before + 1)
  const code = (await codes.first().innerText()).trim()
  expect(code.length).toBeGreaterThan(10)

  // Новый пользователь регистрируется в отдельном контексте (свои cookie).
  const ctx = await browser.newContext({ baseURL: 'http://127.0.0.1:5174', locale: 'ru-RU' })
  const p = await ctx.newPage()
  // Для диагностики: неожиданные ошибки API попадают в вывод теста.
  p.on('response', (r) => {
    if (r.url().includes('/api/') && r.status() >= 400 && r.status() !== 401 && r.status() !== 422) {
      console.log(`API ${r.status()} ${r.request().method()} ${r.url()}`)
    }
  })

  // Без инвайта — отказ.
  await p.goto('/register?invite=wrong')
  await p.getByLabel('Email', { exact: true }).fill(`${user}@example.com`)
  await p.getByLabel('Имя пользователя').fill(user)
  await p.getByLabel('Пароль').fill('password-123')
  await p.getByRole('button', { name: 'Создать аккаунт' }).click()
  await expect(p.getByText('инвайт недействителен')).toBeVisible()

  // Ошибка валидации показывается до отправки.
  await p.getByLabel('Имя пользователя').fill('a--b')
  await p.getByRole('button', { name: 'Создать аккаунт' }).click()
  await expect(p.getByText('Два дефиса подряд недопустимы')).toBeVisible()

  await p.goto(`/register?invite=${code}`)
  await p.getByLabel('Email', { exact: true }).fill(`${user}@example.com`)
  await p.getByLabel('Имя пользователя').fill(user)
  await p.getByLabel('Пароль').fill('password-123')
  await p.getByRole('button', { name: 'Создать аккаунт' }).click()
  await expect(p.getByText(`Здравствуйте, ${user}`)).toBeVisible()

  // Сессия переживает перезагрузку (refresh-cookie).
  await p.reload()
  await expect(p.getByText(`Здравствуйте, ${user}`)).toBeVisible()

  // Сайт.
  await nav(p).getByText('Сайты').click()
  await p.getByLabel('Имя сайта').fill('blog')
  await p.getByRole('button', { name: 'Создать', exact: true }).click()
  const host = `blog.${user}.vladinc.ru`
  await expect(p.getByRole('link', { name: host })).toBeVisible()
  await expect(p.getByText('Достигнут лимит')).toBeVisible()

  // Деплой zip.
  await p.locator('input[type=file]').setInputFiles({
    name: 'site.zip',
    mimeType: 'application/zip',
    buffer: makeZip({ 'index.html': '<h1>Привет</h1>', 'css/a.css': 'body{}' }),
  })
  await expect(p.getByText('опубликован')).toBeVisible()
  const pub = `/tmp/vh-e2e-sites/${host}/public`
  expect(readFileSync(`${pub}/index.html`, 'utf8')).toBe('<h1>Привет</h1>')

  // FTP: выдача пароля (виден один раз), смена, отключение.
  await p.getByRole('button', { name: 'Включить FTP' }).click()
  const dialog = p.getByRole('dialog')
  await expect(dialog.getByText('Доступ по FTP')).toBeVisible()
  await expect(dialog.locator('code', { hasText: `blog.${user}` })).toBeVisible()
  const ftpPassword = (await dialog.getByTestId('ftp-password').innerText()).trim()
  expect(ftpPassword).toMatch(/^[A-Za-z0-9]{20}$/)
  await p.keyboard.press('Escape')
  await expect(p.getByText(`логин blog.${user}`)).toBeVisible()
  await expect(p.getByText(ftpPassword)).toHaveCount(0) // после закрытия пароль нигде не виден
  await p.reload()
  await expect(p.getByText(ftpPassword)).toHaveCount(0) // и не возвращается с сервера
  await p.getByRole('button', { name: 'Новый FTP-пароль' }).click()
  await p.getByRole('button', { name: 'Подтвердить' }).click()
  const newPassword = (await p.getByRole('dialog').getByTestId('ftp-password').innerText()).trim()
  expect(newPassword).not.toBe(ftpPassword)
  await p.keyboard.press('Escape')
  await p.getByRole('button', { name: 'Отключить FTP' }).click()
  await expect(p.getByRole('button', { name: 'Включить FTP' })).toBeVisible()

  // Файлы: открыть index.html и изменить в CodeMirror.
  await p.getByRole('button', { name: 'Файлы' }).click()
  await expect(p.getByRole('link', { name: 'css' })).toBeVisible()
  await p.getByRole('link', { name: 'index.html' }).click()
  const editor = p.locator('.cm-content')
  await expect(editor).toContainText('<h1>Привет</h1>')
  await editor.click()
  await p.keyboard.press('ControlOrMeta+A')
  // Обычный текст: html-режим сам закрывает теги, набранные вручную (это нормальное поведение редактора).
  await p.keyboard.type('Изменено')
  await expect(p.getByText('не сохранён')).toBeVisible()
  await p.keyboard.press('ControlOrMeta+S')
  await expect(p.getByText('Сохранено')).toBeVisible()
  expect(readFileSync(`${pub}/index.html`, 'utf8')).toBe('Изменено')

  // Новая папка и переход в неё.
  await p.getByRole('button', { name: 'Новая папка' }).click()
  await p.getByRole('dialog').getByRole('textbox').fill('img')
  await p.getByRole('button', { name: 'Готово' }).click()
  // Ссылка папки открывается с клавиатуры (focus + Enter): заодно проверяет, что список доступен без мыши.
  await p.getByRole('link', { name: 'img' }).focus()
  await p.keyboard.press('Enter')
  await expect(p.getByText('Папка пуста')).toBeVisible()

  // Обычный пользователь не видит инвайтов.
  await nav(p).getByText('Настройки').click()
  await expect(p.getByText('Приглашения')).toHaveCount(0)

  // Выход закрывает сессию: перезагрузка ведёт на вход.
  await p.locator('button.user').click()
  await p.getByText('Выйти').click()
  await expect(p.getByRole('heading', { name: 'С возвращением' })).toBeVisible()
  await p.goto('/')
  await expect(p.getByRole('heading', { name: 'С возвращением' })).toBeVisible()
  await ctx.close()
})

test('неверный пароль показывает ошибку и не пускает', async ({ page }) => {
  await login(page, process.env.E2E_ADMIN!, 'wrong-password')
  await expect(page.getByText('неверный логин или пароль')).toBeVisible()
  await page.goto('/settings')
  await expect(page.getByRole('heading', { name: 'С возвращением' })).toBeVisible()
})
