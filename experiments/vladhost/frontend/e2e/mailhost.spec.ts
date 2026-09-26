import { expect, test } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { startFakeHelper } from './helpers'

test.use({ actionTimeout: 10_000 })

// Почта на своих доменах: подключение домена к сайту → включение почты, DNS-записи, ящик со сгенерированным паролем,
// свой пароль, алиас, выключение и удаление. Исполнителя (root-скрипт) играет тест: он лишь подтверждает заявки, а состояние читаем из файла.
test('почта на своих доменах: домен, DNS, ящик, пароль, алиас', async ({ page, request }) => {
  test.setTimeout(120_000)
  const timer = startFakeHelper([])
  let token = ''
  let siteId = 0
  const domain = `ml${Date.now().toString(36)}.example.net`
  try {
    const admin = process.env.E2E_ADMIN!
    token = (await (await request.post('/api/auth/login', { data: { login: admin, password: process.env.E2E_ADMIN_PASSWORD } })).json()).access_token as string
    const auth = { Authorization: `Bearer ${token}` }
    const slug = `ml${Date.now().toString(36)}`.slice(0, 20)
    siteId = (await (await request.post('/api/sites', { data: { slug }, headers: auth })).json()).site.id as number
    expect((await request.post(`/api/sites/${siteId}/domains`, { data: { host: domain }, headers: auth })).status()).toBe(201)

    await page.goto('/login')
    await page.getByLabel('Email или имя').fill(admin)
    await page.getByLabel('Пароль').fill(process.env.E2E_ADMIN_PASSWORD!)
    await page.getByRole('button', { name: 'Войти' }).click()
    await expect(page.getByText(`Здравствуйте, ${admin}`)).toBeVisible()

    await page.getByRole('navigation', { name: 'Основное меню' }).getByText('Почта', { exact: true }).click()
    await expect(page.getByRole('heading', { name: 'Почта на своих доменах' })).toBeVisible()

    // Включение почты для подключённого домена
    await page.getByTestId('mail-domain-select').click()
    await page.locator('.n-base-select-option', { hasText: domain }).click()
    await page.getByTestId('mail-domain-add').click()
    const card = page.getByTestId(`mail-domain-${domain}`)
    await expect(card).toBeVisible({ timeout: 30_000 })
    for (const kind of ['mx', 'spf', 'dkim', 'dmarc']) await expect(page.getByTestId(`dns-${domain}-${kind}`)).toBeVisible()
    await expect(page.getByTestId(`dns-${domain}-mx`)).toContainText('mail.example.test')
    await expect(page.getByTestId(`dns-${domain}-dkim`)).toContainText('v=DKIM1')

    // Настройки почтовой программы и ссылка на webmail
    await expect(page.getByTestId('webmail-link')).toHaveAttribute('href', 'https://webmail.example.test')

    // Ящик со сгенерированным паролем: показывается один раз
    await card.getByRole('textbox', { name: 'Имя ящика' }).fill('Info')
    await card.getByTestId('mailbox-add').click()
    await expect(page.getByTestId('mailbox-password')).not.toBeEmpty()
    const password = (await page.getByTestId('mailbox-password').innerText()).trim()
    expect(password.length).toBeGreaterThanOrEqual(10)
    await page.getByTestId('mailbox-password-close').click()
    const box = page.getByTestId(`mailbox-info@${domain}`)
    await expect(box).toBeVisible()

    // Занятое имя и короткий пароль — понятные ошибки на полях
    await card.getByRole('textbox', { name: 'Имя ящика' }).fill('info')
    await card.getByTestId('mailbox-add').click()
    await expect(page.getByText('Такой адрес уже занят')).toBeVisible()
    await card.getByRole('textbox', { name: 'Имя ящика' }).fill('other')
    await card.getByLabel('Пароль', { exact: true }).fill('123')
    await card.getByTestId('mailbox-add').click()
    await expect(page.getByText('Пароль: от 10 до 128 знаков')).toBeVisible()
    await card.getByRole('textbox', { name: 'Имя ящика' }).fill('')

    // Сервер получил хеш, а не пароль
    const state = readFileSync('/tmp/vh-e2e-runtime/mail/state.json', 'utf8')
    expect(state).toContain(domain)
    expect(state).toContain('{SHA512-CRYPT}')
    expect(state).not.toContain(password)

    // Свой пароль и новая квота
    await box.getByRole('button', { name: 'Изменить' }).click()
    const dlg = page.getByRole('dialog')
    await dlg.getByRole('textbox', { name: 'Новый пароль' }).fill('my-own-passw0rd')
    await dlg.getByRole('button', { name: 'Сохранить' }).click()
    await expect(page.getByText('Пароль изменён').first()).toBeVisible()
    await dlg.getByRole('textbox', { name: 'Размер, МБ' }).fill('300')
    await dlg.getByRole('button', { name: 'Изменить размер' }).click()
    await expect(box).toContainText('из 300 МБ')

    // Автоответчик и пересылка
    await dlg.locator('.n-tabs-tab', { hasText: 'Автоответчик' }).click()
    await dlg.getByRole('switch', { name: 'Автоответчик' }).click()
    await dlg.getByTestId('rules-save-reply').click()
    await expect(page.getByText('Тема ответа: от 1 до 200 знаков')).toBeVisible()
    await dlg.getByRole('textbox', { name: 'Тема ответа' }).fill('Я в отпуске')
    await dlg.getByRole('textbox', { name: 'Текст ответа' }).fill('Отвечу после возвращения')
    await dlg.getByTestId('rules-save-reply').click()
    await expect(page.getByText('Настройки сохранены').first()).toBeVisible()
    await dlg.locator('.n-tabs-tab', { hasText: 'Пересылка' }).click()
    await dlg.getByRole('textbox', { name: 'Адреса' }).fill('это не адрес')
    await dlg.getByTestId('rules-save-forward').click()
    await expect(page.getByText('Некорректный адрес получателя')).toBeVisible()
    await dlg.getByRole('textbox', { name: 'Адреса' }).fill('boss@gmail.com')
    await dlg.getByRole('checkbox', { name: 'Оставлять копию в ящике' }).click() // выключаем копию
    await dlg.getByTestId('rules-save-forward').click()
    await expect(page.getByText('Настройки сохранены').first()).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(box.getByTestId('badge-autoreply')).toBeVisible()
    await expect(box.getByTestId('badge-forward')).toBeVisible()
    const withRules = readFileSync('/tmp/vh-e2e-runtime/mail/state.json', 'utf8')
    expect(withRules).toContain('"subject":"Я в отпуске"')
    expect(withRules).toContain('"keep_copy":false')

    // Алиас с пересылкой
    await card.getByRole('textbox', { name: 'Имя алиаса' }).fill('sales')
    await card.getByRole('textbox', { name: 'Куда пересылать' }).fill('boss@gmail.com, info@' + domain)
    await card.getByTestId('alias-save').click()
    const alias = page.getByTestId(`alias-sales@${domain}`)
    await expect(alias).toContainText(`boss@gmail.com, info@${domain}`)
    await card.getByRole('textbox', { name: 'Имя алиаса' }).fill('bad')
    await card.getByRole('textbox', { name: 'Куда пересылать' }).fill('это не адрес')
    await card.getByTestId('alias-save').click()
    await expect(page.getByText('Некорректный адрес получателя')).toBeVisible()

    // Журнал доставки: события приходят от исполнителя
    await card.getByTestId(`journal-load-${domain}`).click()
    const journal = page.getByTestId(`journal-${domain}`)
    await expect(journal).toContainText(`friend@else.org → info@${domain}`)
    await expect(journal).toContainText('доставлено')
    await expect(card.getByText('Очередь пуста')).toBeVisible()

    // Выключение приёма и удаление домена
    await card.getByRole('button', { name: 'Выключить приём' }).click()
    await expect(card.getByText('приём выключен')).toBeVisible()
    await card.getByRole('button', { name: 'Удалить домен' }).click()
    await page.getByRole('button', { name: 'Подтвердить' }).click()
    await expect(card).toHaveCount(0)
    await expect(page.getByText('Почтовых доменов пока нет')).toBeVisible()
  } finally {
    // Уборка не должна заслонять настоящую ошибку теста
    if (token) {
      const auth = { Authorization: `Bearer ${token}` }
      try {
        const list = await request.get('/api/mail', { headers: auth, timeout: 10_000 })
        if (list.ok()) for (const d of (await list.json()).domains as { id: number }[]) await request.delete(`/api/mail/domains/${d.id}`, { headers: auth, timeout: 10_000 })
        if (siteId) await request.delete(`/api/sites/${siteId}`, { headers: auth, timeout: 10_000 })
      } catch {
        // сервер стенда пересоздаётся на каждый прогон
      }
    }
    clearInterval(timer)
  }
})
