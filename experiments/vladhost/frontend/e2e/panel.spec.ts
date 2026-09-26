import { expect, test, type Page } from '@playwright/test'
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs'
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
  // После создания открывается отдельный кабинет сайта со своим меню, без общего меню панели.
  const siteMenu = p.getByRole('navigation', { name: 'Меню сайта' })
  await expect(siteMenu).toBeVisible()
  await expect(nav(p)).toHaveCount(0)
  await expect(p.getByRole('link', { name: host })).toBeVisible()
  await p.getByRole('link', { name: 'Все сайты' }).click()
  await expect(p.getByText('Достигнут лимит')).toBeVisible()
  // Сводка аккаунта: пустой сайт требует внимания, события собраны из данных сайта.
  await p.getByRole('link', { name: 'Обзор' }).click()
  await expect(p.getByRole('heading', { name: 'Требует внимания' })).toBeVisible()
  await expect(p.getByText(`На ${host} ещё не загружены файлы`)).toBeVisible()
  await expect(p.getByText(`Создан сайт ${host}`)).toBeVisible()
  await p.getByRole('link', { name: 'Сайты', exact: true }).click()
  await p.locator('a.site', { hasText: host }).click() // сайт выбирается из списка
  await expect(siteMenu).toBeVisible()

  // Деплой zip.
  await p.locator('input[type=file]').setInputFiles({
    name: 'site.zip',
    mimeType: 'application/zip',
    buffer: makeZip({ 'index.html': '<h1>Привет</h1>', 'css/a.css': 'body{}', '.htaccess': 'Options -Indexes\nphp_value memory_limit 64M\n' }),
  })
  await expect(p.getByRole('main').getByText('опубликован')).toBeVisible()
  const pub = `/tmp/vh-e2e-sites/${host}/public`
  expect(readFileSync(`${pub}/index.html`, 'utf8')).toBe('<h1>Привет</h1>')

  // FTP: выдача пароля (виден один раз), смена, отключение.
  await siteMenu.getByText('FTP', { exact: true }).click()
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

  // Дополнительные FTP-аккаунты: свой логин, папка, режим «только чтение», отключение, новый пароль, удаление.
  await expect(p.getByText('Дополнительных аккаунтов пока нет')).toBeVisible()
  await p.getByLabel('Имя аккаунта').fill('a.b')
  await p.getByRole('button', { name: 'Создать аккаунт' }).click()
  await expect(p.getByText('Имя: 1–24 символа')).toBeVisible()
  await p.getByLabel('Имя аккаунта').fill('Deploy')
  await expect(p.getByText(`Логин: deploy.blog.${user}`)).toBeVisible()
  await p.getByLabel('Папка').fill('../x')
  await p.getByRole('button', { name: 'Создать аккаунт' }).click()
  await expect(p.getByText('Недопустимая папка')).toBeVisible() // ошибку сервера видно у поля
  await p.getByLabel('Папка').fill('app/dist')
  await p.getByRole('button', { name: 'Создать аккаунт' }).click()
  const acctDialog = p.getByRole('dialog')
  await expect(acctDialog.locator('code', { hasText: `deploy.blog.${user}` }).first()).toBeVisible()
  const acctPassword = (await acctDialog.getByTestId('ftp-password').innerText()).trim()
  expect(acctPassword).toMatch(/^[A-Za-z0-9]{20}$/)
  await p.keyboard.press('Escape')
  await expect(p.getByText(acctPassword)).toHaveCount(0)
  expect(existsSync(`/tmp/vh-e2e-sites/${host}/public/app/dist`)).toBe(true) // папка аккаунта создана
  const acct = p.locator('.acc', { hasText: `deploy.blog.${user}` })
  await expect(acct).toContainText('папка app/dist')
  await expect(acct).toContainText('чтение и запись')

  await acct.getByRole('button', { name: 'Изменить' }).click()
  await p.getByRole('dialog').getByLabel('Папка').fill('docs')
  await p.getByRole('dialog').getByRole('switch', { name: 'Только чтение' }).click()
  await p.getByRole('dialog').getByRole('button', { name: 'Сохранить' }).click()
  await expect(acct).toContainText('папка docs')
  await expect(acct).toContainText('только чтение')

  await acct.getByRole('switch').click() // отключить
  await expect(acct).toContainText('отключён')
  await acct.getByRole('switch').click() // включить обратно
  await expect(acct).not.toContainText('отключён')

  await acct.getByRole('button', { name: 'Новый пароль' }).click()
  await p.getByRole('button', { name: 'Подтвердить' }).click()
  const acctPassword2 = (await p.getByRole('dialog').getByTestId('ftp-password').innerText()).trim()
  expect(acctPassword2).not.toBe(acctPassword)
  await p.keyboard.press('Escape')

  await p.reload() // список аккаунтов приходит с сервера, пароля в нём нет
  await expect(p.getByText(acctPassword2)).toHaveCount(0)
  await expect(p.locator('.acc', { hasText: `deploy.blog.${user}` })).toContainText('папка docs')
  await p.locator('.acc', { hasText: `deploy.blog.${user}` }).getByRole('button', { name: 'Удалить' }).click()
  await p.getByRole('button', { name: 'Подтвердить' }).click()
  await expect(p.getByText('Дополнительных аккаунтов пока нет')).toBeVisible()

  // Журналы: шлюз в этом стенде не запущен, поэтому кладём в каталог журналов то, что записал бы он.
  const at = (s: number) => new Date(Date.now() + s * 1000).toISOString()
  const line = (o: object) => `${JSON.stringify(o)}\n`
  mkdirSync(`/tmp/vh-e2e-logs/${host}`, { recursive: true })
  writeFileSync(
    `/tmp/vh-e2e-logs/${host}/access.log`,
    line({ t: at(1), ip: '203.0.113.7', host, m: 'GET', p: '/index.html', s: 200, b: 15, ms: 2, ua: 'e2e' }) +
      line({ t: at(2), ip: '203.0.113.8', host, m: 'GET', p: '/no-such-page', s: 404, b: 300, ms: 1 }),
  )
  writeFileSync(`/tmp/vh-e2e-logs/${host}/error.log`, line({ t: at(2), ip: '203.0.113.8', host, p: '/no-such-page', code: 'not_found', d: '/no-such-page' }))
  await siteMenu.getByText('Журналы').click()
  await expect(p.getByRole('cell', { name: '/index.html' })).toBeVisible()
  await expect(p.getByRole('cell', { name: '/no-such-page' })).toBeVisible()
  await p.getByLabel('Поиск в журнале').fill('no-such')
  await expect(p.getByRole('cell', { name: '/index.html' })).toHaveCount(0)
  await p.getByLabel('Поиск в журнале').fill('')
  await p.getByText('4xx', { exact: true }).click()
  await expect(p.getByRole('cell', { name: '/index.html' })).toHaveCount(0)
  await expect(p.getByRole('cell', { name: '/no-such-page' })).toBeVisible()
  await p.getByText('Ошибки', { exact: true }).click()
  await expect(p.getByText('Файл не найден')).toBeVisible()
  const [dl] = await Promise.all([p.waitForEvent('download'), p.getByRole('button', { name: 'Скачать' }).click()])
  expect(readFileSync(await dl.path()!, 'utf8')).toContain('[not_found]')

  // Статистика: счётчики за сутки сохраняет шлюз; здесь кладём готовый файл, как это сделал бы он.
  const today = new Date().toISOString().slice(0, 10)
  writeFileSync(
    `/tmp/vh-e2e-logs/${host}/stats.json`,
    JSON.stringify({
      salt: 'e2e',
      days: { [today]: { h: 12, bt: 2, pg: 6, v: 4, b: 2048, s2: 9, s3: 1, s4: 2, s5: 0, paths: { '/': 4, '/pricing.html': 2 }, refs: { 'news.example.org': 3 } } },
    }),
  )
  await siteMenu.getByText('Статистика').click()
  await expect(p.getByRole('heading', { name: 'Статистика' })).toBeVisible()
  const tile = (name: string) => p.locator('.tile', { hasText: name })
  await expect(tile('Посетители')).toContainText('4')
  await expect(tile('Просмотры страниц')).toContainText('6')
  await expect(tile('Запросы')).toContainText('из них ботов и программ: 2')
  await expect(p.getByText('/pricing.html')).toBeVisible()
  await expect(p.getByText('news.example.org')).toBeVisible()
  await expect(p.locator('.col')).toHaveCount(30)
  await p.locator('.col').last().hover() // подсказка столбца показывает все показатели дня
  await expect(p.getByRole('status')).toContainText('Просмотры страниц')
  await p.getByRole('button', { name: 'Таблица' }).click()
  await expect(p.getByRole('cell', { name: today })).toBeVisible()
  await p.getByText('7 дней', { exact: true }).click()
  await p.getByRole('button', { name: 'График' }).click()
  await expect(p.locator('.col')).toHaveCount(7)

  // Поддомены сайта: имя проверяется, папка создаётся, папку можно сменить, поддомен можно убрать.
  await siteMenu.getByText('Домены').click()
  await expect(p.getByText('Поддоменов пока нет')).toBeVisible()
  await p.getByLabel('Имя поддомена').fill('a--b')
  await p.getByRole('button', { name: 'Добавить' }).click()
  await expect(p.getByText('Имя: 1–32 символа')).toBeVisible()
  await p.getByLabel('Имя поддомена').fill('Docs')
  await expect(p.getByText(`Адрес: docs.${host}`)).toBeVisible()
  await p.getByLabel('Папка', { exact: true }).fill('../x')
  await p.getByRole('button', { name: 'Добавить' }).click()
  await expect(p.getByText('Недопустимая папка')).toBeVisible()
  await p.getByLabel('Папка', { exact: true }).fill('docs')
  await p.getByRole('button', { name: 'Добавить' }).click()
  const subRow = p.getByTestId('subdomains').locator('li', { hasText: `docs.${host}` })
  await expect(subRow).toContainText('папка docs')
  expect(existsSync(`/tmp/vh-e2e-sites/${host}/public/docs`)).toBe(true)
  await subRow.getByRole('button', { name: 'Папка', exact: true }).click()
  await p.getByRole('dialog').getByLabel('Папка').fill('site2/v1')
  await p.getByRole('dialog').getByRole('button', { name: 'Сохранить' }).click()
  await expect(subRow).toContainText('папка site2/v1')
  expect(existsSync(`/tmp/vh-e2e-sites/${host}/public/site2/v1`)).toBe(true)
  await p.getByLabel('Имя поддомена').fill('docs')
  await p.getByRole('button', { name: 'Добавить' }).click()
  await expect(p.getByText('Такой поддомен уже есть')).toBeVisible()
  await subRow.getByRole('button', { name: 'Отвязать' }).click()
  await p.getByRole('button', { name: 'Подтвердить' }).click()
  await expect(p.getByText('Поддоменов пока нет')).toBeVisible()

  // Свои домены: инструкция с IP сервера, проверка ввода, отвязка.
  const domain = `vh-${Date.now().toString(36)}.example.net`
  await p.getByLabel('Домен', { exact: true }).fill('это не домен')
  await p.getByRole('button', { name: 'Подключить' }).click()
  await expect(p.getByText('Введите домен вида example.com')).toBeVisible()
  await p.getByLabel('Домен', { exact: true }).fill(domain.toUpperCase())
  await p.getByRole('button', { name: 'Подключить' }).click()
  await expect(p.locator('.dom-host', { hasText: domain })).toBeVisible()
  await expect(p.getByText('ждём A-запись')).toBeVisible()
  await expect(p.getByText(`A-запись: ${domain} → 203.0.113.10`)).toBeVisible()
  await p.getByLabel('Домен', { exact: true }).fill(domain)
  await p.getByRole('button', { name: 'Подключить' }).click()
  await expect(p.getByText('Этот домен уже подключён к сайту')).toBeVisible()
  await p.getByRole('button', { name: 'Отвязать' }).click()
  await p.getByRole('button', { name: 'Подтвердить' }).click()
  await expect(p.getByText('Своих доменов пока нет')).toBeVisible()

  // Файлы: открыть index.html и изменить в CodeMirror.
  await siteMenu.getByText('Файлы').click()
  await expect(p.getByRole('link', { name: 'css' })).toBeVisible()

  // Проверка .htaccess: неподдерживаемая директива показана с номером строки.
  await p.getByRole('button', { name: 'Проверить .htaccess' }).click()
  const hta = p.getByRole('dialog')
  await expect(hta.getByText('Директива php_value не поддерживается и будет проигнорирована')).toBeVisible()
  await expect(hta.getByText('строка 2')).toBeVisible()
  await p.keyboard.press('Escape')
  await expect(hta).toHaveCount(0)
  await p.getByRole('link', { name: 'index.html' }).click()
  const editor = p.locator('.cm-content')
  await expect(editor).toContainText('<h1>Привет</h1>')
  await editor.click()
  await p.keyboard.press('ControlOrMeta+A')
  // Обычный текст: html-режим сам закрывает теги, набранные вручную (это нормальное поведение редактора).
  await p.keyboard.type('Изменено', { delay: 25 }) // с паузой: под нагрузкой редактор иначе путает порядок букв
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

  // Настройки сайта: корневая папка, индексные файлы, листинг, страницы ошибок, www, HSTS.
  await siteMenu.getByText('Настройки').click()
  await p.getByLabel('Корневая папка').fill('../x')
  await p.getByRole('button', { name: 'Сохранить' }).click()
  await expect(p.getByText('Недопустимая корневая папка')).toBeVisible()
  await p.getByLabel('Корневая папка').fill('dist')
  await p.getByLabel('Индексные файлы').fill('home.html, index.html')
  await p.getByRole('switch', { name: 'Показывать список файлов' }).click()
  await p.getByLabel('404 — Файл не найден').fill('nope.html')
  await p.getByRole('button', { name: 'Сохранить' }).click()
  await expect(p.getByText('Файл страницы для кода 404 не найден')).toBeVisible()
  writeFileSync(`/tmp/vh-e2e-sites/${host}/public/dist/404.html`, '<h1>нет такой страницы</h1>')
  await p.getByLabel('404 — Файл не найден').fill('404.html')
  await p.getByText('Добавлять www', { exact: true }).click()
  await p.getByRole('switch', { name: 'HSTS' }).click()
  await p.getByRole('button', { name: 'Сохранить' }).click()
  await expect(p.getByText('Настройки сохранены')).toBeVisible()
  const saved = JSON.parse(readFileSync(`/tmp/vh-e2e-sites/${host}/settings.json`, 'utf8'))
  expect(saved).toMatchObject({ root_dir: 'dist', index: ['home.html', 'index.html'], autoindex: true, error_pages: { '404': '404.html' }, www: 'add', hsts: true })
  await p.reload() // значения приходят с сервера
  await expect(p.getByLabel('Корневая папка')).toHaveValue('dist')
  await expect(p.getByLabel('Индексные файлы')).toHaveValue('home.html, index.html')
  await expect(p.getByLabel('404 — Файл не найден')).toHaveValue('404.html')
  await expect(p.getByRole('switch', { name: 'HSTS' })).toBeChecked()

  // Обычный пользователь не видит инвайтов.
  await p.goto('/settings')
  await expect(p.getByRole('heading', { name: 'Приглашения' })).toHaveCount(0)

  // Смена пароля: проверки на клиенте и на сервере, затем вход с новым паролем.
  await p.getByLabel('Текущий пароль').fill('password-123')
  await p.getByLabel('Новый пароль', { exact: true }).fill('new-password-456')
  await p.getByLabel('Повторите новый пароль').fill('другой-пароль')
  await p.getByRole('button', { name: 'Сменить пароль' }).click()
  await expect(p.getByText('Пароли не совпадают')).toBeVisible()
  await p.getByLabel('Текущий пароль').fill('неверный-текущий')
  await p.getByLabel('Повторите новый пароль').fill('new-password-456')
  await p.getByRole('button', { name: 'Сменить пароль' }).click()
  await expect(p.getByText('Текущий пароль указан неверно')).toBeVisible()
  await p.getByLabel('Текущий пароль').fill('password-123')
  await p.getByRole('button', { name: 'Сменить пароль' }).click()
  await expect(p.getByText('Пароль изменён, остальные сессии закрыты')).toBeVisible()
  await p.reload() // сессия этого устройства осталась рабочей
  await expect(p.getByRole('heading', { name: 'Настройки' })).toBeVisible()

  // Выход закрывает сессию: перезагрузка ведёт на вход.
  await p.locator('button.user').click()
  await p.getByText('Выйти').click()
  await expect(p.getByRole('heading', { name: 'С возвращением' })).toBeVisible()
  await p.goto('/')
  await expect(p.getByRole('heading', { name: 'С возвращением' })).toBeVisible()
  // Старый пароль не работает, новый — да.
  await login(p, user, 'password-123')
  await expect(p.getByText('Неверный логин или пароль')).toBeVisible()
  await login(p, user, 'new-password-456')
  await expect(p.getByText(`Здравствуйте, ${user}`)).toBeVisible()
  await ctx.close()
})

test('неверный пароль показывает ошибку и не пускает', async ({ page }) => {
  await login(page, process.env.E2E_ADMIN!, 'wrong-password')
  await expect(page.getByText('неверный логин или пароль')).toBeVisible()
  await page.goto('/settings')
  await expect(page.getByRole('heading', { name: 'С возвращением' })).toBeVisible()
})
