import { expect, test } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { startFakeHelper } from './helpers'

test.use({ actionTimeout: 10_000 })

// Собственный DNS: зона своего домена, записи, проверка делегирования и подсказка в разделе «Почта». Исполнителя (root-скрипт) играет тест:
// он подтверждает заявки, а собранное панелью состояние читаем из файла.
test('DNS: зона, записи, делегирование и почта', async ({ page, request }) => {
  test.setTimeout(120_000)
  const timer = startFakeHelper([])
  let token = ''
  let siteId = 0
  const domain = `dn${Date.now().toString(36)}.example.net`
  try {
    const admin = process.env.E2E_ADMIN!
    token = (await (await request.post('/api/auth/login', { data: { login: admin, password: process.env.E2E_ADMIN_PASSWORD } })).json()).access_token as string
    const auth = { Authorization: `Bearer ${token}` }
    const slug = `dn${Date.now().toString(36)}`.slice(0, 20)
    siteId = (await (await request.post('/api/sites', { data: { slug }, headers: auth })).json()).site.id as number
    expect((await request.post(`/api/sites/${siteId}/domains`, { data: { host: domain }, headers: auth })).status()).toBe(201)

    await page.goto('/login')
    await page.getByLabel('Email или имя').fill(admin)
    await page.getByLabel('Пароль').fill(process.env.E2E_ADMIN_PASSWORD!)
    await page.getByRole('button', { name: 'Войти' }).click()
    await expect(page.getByText(`Здравствуйте, ${admin}`)).toBeVisible()

    await page.getByRole('navigation', { name: 'Основное меню' }).getByText('DNS', { exact: true }).click()
    await expect(page.getByRole('heading', { name: 'DNS', exact: true })).toBeVisible()
    await expect(page.getByTestId('dns-ns')).toContainText('ns.example.test')
    await expect(page.getByTestId('dns-ns')).toContainText('ns2.example.test')

    // Зона своего домена: сразу с записями для сайта
    await page.getByTestId('dns-domain-select').click()
    await page.locator('.n-base-select-option', { hasText: domain }).click()
    await page.getByTestId('dns-zone-add').click()
    const zone = page.getByTestId(`dns-zone-${domain}`)
    await expect(zone).toBeVisible({ timeout: 30_000 })
    await expect(zone.getByTestId('dns-record-@-A')).toContainText('203.0.113.10')
    await expect(zone.getByTestId('dns-record-www-CNAME')).toContainText(domain)

    // Запись: проверка ввода, добавление, изменение
    await zone.getByRole('textbox', { name: 'Значение' }).fill('999.1.1.1')
    await zone.getByTestId(`dns-save-${domain}`).click()
    await expect(zone.getByText('Некорректное значение записи для этого типа')).toBeVisible()
    await zone.getByRole('textbox', { name: 'Имя' }).fill('Blog')
    await zone.getByRole('textbox', { name: 'Значение' }).fill('203.0.113.50')
    await zone.getByTestId(`dns-save-${domain}`).click()
    await expect(zone.getByTestId('dns-record-blog-A')).toContainText('203.0.113.50')
    await zone.getByTestId('dns-record-blog-A').getByRole('button', { name: 'Изменить' }).click()
    await zone.getByRole('textbox', { name: 'Значение' }).fill('203.0.113.51')
    await zone.getByTestId(`dns-save-${domain}`).click()
    await expect(zone.getByTestId('dns-record-blog-A')).toContainText('203.0.113.51')
    // CNAME рядом с другими записями запрещён, а MX принимает приоритет
    await zone.getByTestId(`dns-type-${domain}`).click()
    await page.locator('.n-base-select-option', { hasText: /^MX$/ }).click()
    await zone.getByRole('textbox', { name: 'Имя' }).fill('')
    await zone.getByRole('textbox', { name: 'Значение' }).fill('mail.vladinc.ru')
    await zone.getByTestId(`dns-save-${domain}`).click()
    await expect(zone.getByTestId('dns-record--MX').or(zone.getByTestId('dns-record-@-MX'))).toContainText('10 mail.vladinc.ru')

    // Состояние панели попало на сервер файлом
    const state = readFileSync('/tmp/vh-e2e-runtime/dns/state.json', 'utf8')
    expect(state).toContain(`"name":"${domain}"`)
    expect(state).toContain('"value":"203.0.113.51"')
    expect(state).toContain('"nameservers":["ns.example.test","ns2.example.test"]')

    // Делегирование: домен пока не на наших серверах
    await zone.getByTestId(`dns-check-${domain}`).click()
    await expect(page.getByTestId(`dns-state-${domain}`)).toContainText('домен ещё не на наших серверах имён')

    // Почта: зона есть, а домен ещё не на наших серверах — кнопки нет, есть подсказка
    await page.goto('/mail')
    await expect(page.getByRole('heading', { name: 'Почта на своих доменах' })).toBeVisible()
    await page.getByTestId('mail-domain-select').click()
    await page.locator('.n-base-select-option', { hasText: domain }).click()
    await page.getByTestId('mail-domain-add').click()
    const auto = page.getByTestId(`dns-auto-${domain}`)
    await expect(auto).toContainText('Зона домена у нас уже есть', { timeout: 30_000 })
    await expect(auto).toContainText('ns.example.test, ns2.example.test')
    await expect(page.getByTestId(`dns-auto-button-${domain}`)).toHaveCount(0)

    // Удаление зоны
    await page.goto('/dns')
    const again = page.getByTestId(`dns-zone-${domain}`)
    await again.getByRole('button', { name: 'Удалить зону' }).click()
    await page.getByRole('button', { name: 'Подтвердить' }).click()
    await expect(again).toHaveCount(0)
    await expect(page.getByTestId('dns-domain-select')).toBeVisible()
  } finally {
    if (token) {
      const auth = { Authorization: `Bearer ${token}` }
      try {
        const mail = await request.get('/api/mail', { headers: auth, timeout: 10_000 })
        if (mail.ok()) for (const d of (await mail.json()).domains as { id: number }[]) await request.delete(`/api/mail/domains/${d.id}`, { headers: auth, timeout: 10_000 })
        const dns = await request.get('/api/dns', { headers: auth, timeout: 10_000 })
        if (dns.ok()) for (const z of (await dns.json()).zones as { id: number }[]) await request.delete(`/api/dns/zones/${z.id}`, { headers: auth, timeout: 10_000 })
        if (siteId) await request.delete(`/api/sites/${siteId}`, { headers: auth, timeout: 10_000 })
      } catch {
        // сервер стенда пересоздаётся на каждый прогон
      }
    }
    clearInterval(timer)
  }
})
