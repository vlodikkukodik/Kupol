import { expect, test } from '@playwright/test'

// Интерфейс SSL проверяется на подставленных ответах списка сайтов: в стенде выпуск сертификатов выключен
// (иначе адрес сайта был бы недоступен, пока «выпускается», и сломал бы остальные сценарии), а реальная логика
// перевыпуска и сроков покрыта тестами бэкенда.
test('раздел SSL: сроки, предупреждения, перевыпуск и сводка', async ({ page }) => {
  test.setTimeout(90_000)
  const admin = process.env.E2E_ADMIN!
  await page.goto('/login')
  await page.getByLabel('Email или имя').fill(admin)
  await page.getByLabel('Пароль').fill(process.env.E2E_ADMIN_PASSWORD!)
  await page.getByRole('button', { name: 'Войти' }).click()
  await expect(page.getByText(`Здравствуйте, ${admin}`)).toBeVisible()

  // Без выпускателя сертификатов раздел честно об этом говорит.
  await page.goto('/sites')
  await page.getByLabel('Имя сайта').fill('ssltest')
  await page.getByRole('button', { name: 'Создать', exact: true }).click()
  const menu = page.getByRole('navigation', { name: 'Меню сайта' })
  await expect(menu).toBeVisible()
  await menu.getByText('SSL', { exact: true }).click()
  await expect(page.getByText('Выпуск сертификатов на этом сервере выключен')).toBeVisible()

  // Подставляем состояние с сертификатами.
  const day = 86_400_000
  const iso = (offset: number) => new Date(Date.now() + offset).toISOString()
  const cert = (daysLeft: number) => ({ issuer: "Let's Encrypt (R11)", not_before: iso(-(90 - daysLeft) * day), not_after: iso(daysLeft * day + day / 2), names: ['names.example'] })
  const host = `ssltest.${admin}.vladinc.ru`
  await page.route('**/api/sites', async (route) => {
    if (route.request().method() !== 'GET') return route.continue()
    const res = await route.fetch()
    const body = await res.json()
    const s = body.sites[0]
    s.cert_status = 'active'
    s.cert = cert(9)
    s.cert_renew_at = null
    const dom = (id: number, name: string, over: object) => ({
      id, host: name, kind: 'custom', dir: '', status: 'active', problem: '', found: [], error: '', verified_at: null,
      created_at: iso(-day), cert: cert(60), cert_renew_at: null, ...over,
    })
    s.domains = [
      dom(9001, 'shop.example.com', { error: 'rate limited', cert_renew_at: iso(2 * day) }),
      dom(9002, 'broken.example.com', { status: 'failed', error: 'DNS problem', cert: null }),
      dom(9003, `docs.${host}`, { kind: 'sub', cert: cert(-2) }),
    ]
    await route.fulfill({ response: res, json: body })
  })
  await page.reload()
  await expect(page.getByRole('heading', { name: 'SSL' })).toBeVisible()

  if (process.env.SHOTS) {
    await page.waitForTimeout(1500)
    await page.screenshot({ path: '/tmp/vh-shots/ssl.png', fullPage: true })
  }
  const row = (name: string) => page.locator('.cert', { hasText: name }).first()
  const own = page.locator('.cert').first()
  await expect(own).toContainText(host)
  await expect(own).toContainText('HTTPS работает')
  await expect(own).toContainText('осталось 9 дн.')
  await expect(own).toContainText("Let's Encrypt (R11)")
  await expect(own).toContainText('Продлевается автоматически за 30 дней до окончания')

  const shop = row('shop.example.com')
  await expect(shop).toContainText('осталось 60 дн.')
  await expect(shop).toContainText('Последний перевыпуск не удался: rate limited')
  await expect(shop).toContainText('Следующий ручной перевыпуск будет доступен после')
  await expect(shop.getByRole('button', { name: 'Перевыпустить' })).toBeDisabled()

  const broken = row('broken.example.com')
  await expect(broken).toContainText('не выпущен')
  await expect(broken).toContainText('DNS problem')
  await expect(broken.getByRole('button', { name: 'Повторить' })).toBeVisible()
  await expect(broken.getByRole('button', { name: 'Перевыпустить' })).toHaveCount(0)

  const expired = row(`docs.${host}`)
  await expect(expired).toContainText('поддомен')
  await expect(expired).toContainText('срок истёк')

  // Перевыпуск: запрос уходит с именем, пользователь видит подтверждение.
  let posted: unknown = null
  await page.route('**/certs/renew', async (route) => {
    posted = route.request().postDataJSON()
    await route.fulfill({ status: 202 })
  })
  await own.getByRole('button', { name: 'Перевыпустить' }).click()
  await page.getByRole('button', { name: 'Подтвердить' }).click()
  await expect(page.getByText('Сертификат перевыпускается')).toBeVisible()
  expect(posted).toEqual({ host })

  // Сводка предупреждает о скором окончании и уже истёкшем сертификате.
  await page.goto('/')
  await expect(page.getByText(`Сертификат ${host} истекает через 9 дн.`)).toBeVisible()
  await expect(page.getByText(`Сертификат docs.${host} истёк`)).toBeVisible()
  await page.getByText(`Сертификат ${host} истекает`).click()
  await expect(page.getByRole('heading', { name: 'SSL' })).toBeVisible() // пункт ведёт прямо в раздел
})
