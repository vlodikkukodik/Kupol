import AxeBuilder from '@axe-core/playwright'
import { expect, test } from '@playwright/test'
import { PASSWORD, canWrite, registerUser, signUp } from './helpers/kupol.js'
import { totpCode } from './helpers/totp.js'
import { openAuth, settle } from './helpers/ui.js'

// Код из приложения (шаг 3.7): по желанию, включается в личном деле. Коды считает независимый от сервера код (helpers/totp.js).
test.skip(!canWrite, 'создаёт пользователей: против внешнего адреса не запускается')

const dialog = (page) => page.getByTestId('totp-dialog')
const totpState = (page) => page.getByTestId('totp-state')

/** Вход через окно входа. second — код (или одноразовый код) для второго шага; без него возвращается на втором шаге. */
async function signIn(page, login, password, second) {
  await openAuth(page)
  await page.getByLabel('Логин', { exact: true }).fill(login)
  await page.getByLabel('Пароль', { exact: true }).fill(password)
  await page.getByRole('button', { name: 'Войти', exact: true }).click()
  if (second === undefined) return
  await expect(page.getByTestId('login-second-step')).toBeVisible()
  await page.getByLabel('Код из приложения', { exact: true }).fill(second)
  await page.getByRole('button', { name: 'Войти', exact: true }).click()
}

/** Подключает защиту через окно личного дела; возвращает секрет, первый код и одноразовые коды. */
async function enableThroughUi(page) {
  await page.goto('/file')
  await expect(totpState(page)).toContainText('Выключена')
  await page.getByTestId('totp-enable').click()
  await dialog(page).getByLabel('Пароль', { exact: true }).fill(PASSWORD)
  await page.getByTestId('totp-next').click()
  await expect(page.getByTestId('totp-qr')).toBeVisible()
  const secret = (await page.getByTestId('totp-secret').innerText()).replace(/\s/g, '')
  expect(secret).toMatch(/^[A-Z2-7]{32}$/)
  const first = totpCode(secret)
  await dialog(page).getByLabel('Код из приложения', { exact: true }).fill(first)
  await page.getByTestId('totp-confirm').click()
  await expect(page.getByTestId('secret-codes')).toBeVisible()
  const codes = await page.getByTestId('secret-codes').locator('li').allInnerTexts()
  return { secret, first, codes }
}

test('подключение, вход с кодом, одноразовый код, новые коды и выключение — по шагам через интерфейс', async ({ browser }) => {
  const ctx = await browser.newContext()
  const login = await signUp(ctx)
  const page = await ctx.newPage()

  // подключение: неверный первый код не включает защиту; верный — показывает десять одноразовых кодов
  await page.goto('/file')
  await expect(totpState(page)).toContainText('Выключена')
  await page.getByTestId('totp-enable').click()
  await dialog(page).getByLabel('Пароль', { exact: true }).fill('чужой пароль 12345')
  await page.getByTestId('totp-next').click()
  await expect(dialog(page).getByText('Неверный пароль')).toBeVisible()
  await dialog(page).getByLabel('Пароль', { exact: true }).fill(PASSWORD)
  await page.getByTestId('totp-next').click()
  await expect(page.getByTestId('totp-qr')).toBeVisible()
  const secret = (await page.getByTestId('totp-secret').innerText()).replace(/\s/g, '')
  await dialog(page).getByLabel('Код из приложения', { exact: true }).fill('000000')
  await page.getByTestId('totp-confirm').click()
  await expect(dialog(page).getByText('Неверный или уже использованный код')).toBeVisible()
  const first = totpCode(secret)
  await dialog(page).getByLabel('Код из приложения', { exact: true }).fill(`${first.slice(0, 3)} ${first.slice(3)}`) // «123 456», как показывает приложение
  await page.getByTestId('totp-confirm').click()
  await expect(page.getByTestId('secret-codes')).toBeVisible()
  const codes = await page.getByTestId('secret-codes').locator('li').allInnerTexts()
  expect(codes).toHaveLength(10)
  for (const c of codes) expect(c).toMatch(/^[a-z2-9]{5}-[a-z2-9]{5}$/)

  // окно с кодами не закрывается, пока их не сохранили
  await expect(page.getByTestId('totp-done')).toBeDisabled()
  await page.keyboard.press('Escape')
  await expect(page.getByTestId('totp-close-hint')).toBeVisible()
  await expect(dialog(page)).toBeVisible()
  await dialog(page).getByLabel('Я сохранил(а) коды в надёжном месте').check()
  await page.getByTestId('totp-done').click()
  await expect(dialog(page)).toHaveCount(0)
  await expect(totpState(page)).toContainText('Включена')
  await expect(page.getByTestId('totp-left')).toContainText('осталось: 10')
  expect((await (await ctx.request.get('/api/auth/session')).json()).user.totp_enabled).toBe(true)

  // вход: пароль → второй шаг; повтор прежнего кода и чужой код не пускают; код следующего шага — пускает
  await page.getByRole('button', { name: 'Выйти' }).click()
  await signIn(page, login, PASSWORD)
  await expect(page.getByTestId('login-second-step')).toContainText('Пароль верный')
  await page.getByLabel('Код из приложения', { exact: true }).fill(first) // этот код уже использован при подключении
  await page.getByRole('button', { name: 'Войти', exact: true }).click()
  await expect(page.getByText('Неверный или уже использованный код')).toBeVisible()
  await page.getByLabel('Код из приложения', { exact: true }).fill(totpCode(secret, 1))
  await page.getByRole('button', { name: 'Войти', exact: true }).click()
  await expect(page).toHaveURL(/\/file$/)
  await expect(totpState(page)).toContainText('Включена')

  // назад со второго шага возвращает к паролю
  await page.getByRole('button', { name: 'Выйти' }).click()
  await openAuth(page)
  await page.getByLabel('Логин', { exact: true }).fill(login)
  await page.getByLabel('Пароль', { exact: true }).fill(PASSWORD)
  await page.getByRole('button', { name: 'Войти', exact: true }).click()
  await expect(page.getByTestId('login-second-step')).toBeVisible()
  await page.getByTestId('login-back').click()
  await expect(page.getByLabel('Пароль', { exact: true })).toBeVisible()
  await expect(page.getByLabel('Пароль', { exact: true })).toHaveValue('')
  await page.keyboard.press('Escape')

  // одноразовый код заменяет код из приложения, но работает один раз
  await signIn(page, login, PASSWORD, codes[0].toUpperCase())
  await expect(page).toHaveURL(/\/file$/)
  await expect(page.getByTestId('totp-left')).toContainText('осталось: 9')
  await page.getByRole('button', { name: 'Выйти' }).click()
  await signIn(page, login, PASSWORD, codes[0])
  await expect(page.getByText('Неверный или уже использованный код')).toBeVisible()
  await page.keyboard.press('Escape')

  // новые коды: прежние перестают действовать
  await signIn(page, login, PASSWORD, codes[1])
  await expect(page).toHaveURL(/\/file$/)
  await page.getByTestId('totp-renew').click()
  await dialog(page).getByLabel('Пароль', { exact: true }).fill(PASSWORD)
  await dialog(page).getByLabel('Код из приложения или одноразовый код').fill(codes[2])
  await page.getByTestId('totp-confirm').click()
  await expect(page.getByTestId('secret-codes')).toBeVisible()
  const fresh = await page.getByTestId('secret-codes').locator('li').allInnerTexts()
  expect(fresh).toHaveLength(10)
  expect(fresh).not.toContain(codes[3])
  await dialog(page).getByLabel('Я сохранил(а) коды в надёжном месте').check()
  await page.getByTestId('totp-done').click()
  await expect(page.getByTestId('totp-left')).toContainText('осталось: 10')
  await page.getByRole('button', { name: 'Выйти' }).click()
  await signIn(page, login, PASSWORD, codes[3]) // выдан прежним набором
  await expect(page.getByText('Неверный или уже использованный код')).toBeVisible()
  await page.getByLabel('Код из приложения', { exact: true }).fill(fresh[0])
  await page.getByRole('button', { name: 'Войти', exact: true }).click()
  await expect(page).toHaveURL(/\/file$/)

  // выключение: пароль и код; после него вход одним паролем
  await page.getByTestId('totp-disable').click()
  await dialog(page).getByLabel('Пароль', { exact: true }).fill(PASSWORD)
  await dialog(page).getByLabel('Код из приложения или одноразовый код').fill('123456')
  await page.getByTestId('totp-confirm').click()
  await expect(dialog(page).getByText('Неверный или уже использованный код')).toBeVisible()
  await dialog(page).getByLabel('Код из приложения или одноразовый код').fill(fresh[1])
  await page.getByTestId('totp-confirm').click()
  await expect(dialog(page)).toHaveCount(0)
  await expect(page.getByTestId('totp-notice')).toContainText('выключен')
  await expect(totpState(page)).toContainText('Выключена')
  await page.getByRole('button', { name: 'Выйти' }).click()
  await signIn(page, login, PASSWORD)
  await expect(page).toHaveURL(/\/file$/)
  await ctx.close()
})

test('«Забыли пароль?»: с включённым кодом восстановление меняет пароль, но сессии не даёт — вход по-прежнему с кодом', async ({ browser, baseURL }) => {
  // подготовка API-запросами: человек с включённой защитой и его резервный код
  const setupCtx = await browser.newContext()
  const { login, backupCode } = await registerUser(setupCtx)
  const origin = { Origin: new URL(baseURL).origin }
  const setup = await (await setupCtx.request.post('/api/me/totp/setup', { headers: origin, data: { password: PASSWORD } })).json()
  const enabled = await setupCtx.request.post('/api/me/totp/enable', { headers: origin, data: { code: totpCode(setup.secret) } })
  expect(enabled.status()).toBe(200)
  const recovery = (await enabled.json()).recovery_codes
  await setupCtx.close()

  const ctx = await browser.newContext()
  const page = await ctx.newPage()
  await page.goto('/restore')
  await page.getByLabel('Логин', { exact: true }).fill(login)
  await page.getByLabel('Резервный код').fill(backupCode)
  await page.getByLabel('Новый пароль', { exact: true }).fill('новый пароль 2026')
  await page.getByLabel('Новый пароль ещё раз').fill('новый пароль 2026')
  await page.getByRole('button', { name: 'Восстановить доступ' }).click()

  // сессии нет; новый резервный код показан здесь же и его нужно сохранить
  await expect(page.getByTestId('restore-done')).toBeVisible()
  const newBackup = (await page.getByTestId('secret-codes').locator('li').innerText()).trim()
  expect(newBackup).toMatch(/^KUPOL-/)
  expect(newBackup).not.toBe(backupCode)
  expect((await (await ctx.request.get('/api/auth/session')).json()).user).toBeNull()
  await expect(page.getByTestId('restore-sign-in')).toBeDisabled()
  await page.getByLabel('Я сохранил(а) код в надёжном месте').check()
  await page.getByTestId('restore-sign-in').click()

  // вход новым паролем без кода не завершается, с одноразовым кодом — да
  const modal = page.getByTestId('auth-modal')
  await expect(modal).toBeVisible()
  await settle(page)
  await modal.getByLabel('Логин', { exact: true }).fill(login)
  await modal.getByLabel('Пароль', { exact: true }).fill('новый пароль 2026')
  await modal.getByRole('button', { name: 'Войти', exact: true }).click()
  await expect(page.getByTestId('login-second-step')).toBeVisible()
  await modal.getByLabel('Код из приложения', { exact: true }).fill(recovery[0])
  await modal.getByRole('button', { name: 'Войти', exact: true }).click()
  await expect(page).toHaveURL(/\/file$/)
  await ctx.close()
})

test('телефон 375 px и доступность: подключение (QR, коды), второй шаг входа и выключение', async ({ browser }) => {
  const ctx = await browser.newContext()
  const login = await signUp(ctx)
  const page = await ctx.newPage()
  const axe = async (what) => {
    await settle(page)
    const r = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()
    expect(r.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(' ')).slice(0, 3).join(' | ')}`), what).toEqual([])
  }
  const noOverflow = async (what) =>
    expect(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth), what).toBeLessThanOrEqual(0)

  await page.setViewportSize({ width: 375, height: 800 })
  await page.goto('/file')
  await expect(totpState(page)).toBeVisible()
  await noOverflow('личное дело')
  await page.getByTestId('totp-enable').click()
  await dialog(page).getByLabel('Пароль', { exact: true }).fill(PASSWORD)
  await page.getByTestId('totp-next').click()
  await expect(page.getByTestId('totp-qr')).toBeVisible()
  await noOverflow('окно с QR')
  const box = await page.getByTestId('totp-qr').boundingBox()
  expect(box.width).toBeGreaterThan(150) // QR не сжат до нечитаемого размера
  await page.setViewportSize({ width: 1200, height: 900 })
  await axe('окно с QR-кодом')

  const secret = (await page.getByTestId('totp-secret').innerText()).replace(/\s/g, '')
  await dialog(page).getByLabel('Код из приложения', { exact: true }).fill(totpCode(secret))
  await page.getByTestId('totp-confirm').click()
  await expect(page.getByTestId('secret-codes')).toBeVisible()
  await axe('окно с одноразовыми кодами')
  await page.setViewportSize({ width: 375, height: 800 })
  await noOverflow('окно с кодами')
  await dialog(page).getByLabel('Я сохранил(а) коды в надёжном месте').check()
  await page.getByTestId('totp-done').click()
  await page.setViewportSize({ width: 1200, height: 900 })
  await axe('личное дело с включённой защитой')

  await page.getByRole('button', { name: 'Выйти' }).click()
  await signIn(page, login, PASSWORD)
  await expect(page.getByTestId('login-second-step')).toBeVisible()
  await axe('окно входа, второй шаг')
  await page.setViewportSize({ width: 375, height: 800 })
  await noOverflow('окно входа, второй шаг')
  await ctx.close()
})

test('неверные коды считаются неудачами входа: после пяти вход блокируется, даже с верным кодом', async ({ browser }) => {
  const ctx = await browser.newContext()
  const login = await signUp(ctx)
  const page = await ctx.newPage()
  const { secret } = await enableThroughUi(page)
  await page.getByTestId('totp-dialog').getByLabel('Я сохранил(а) коды в надёжном месте').check()
  await page.getByTestId('totp-done').click()

  const anon = await browser.newContext()
  const origin = { Origin: new URL(page.url()).origin }
  const attempt = (totp) => anon.request.post('/api/auth/login', { headers: origin, data: { login, password: PASSWORD, ...(totp ? { totp } : {}) } })
  for (let i = 0; i < 5; i++) expect((await attempt('000000')).status(), `неверный код №${i + 1}`).toBe(422)
  const blocked = await attempt(totpCode(secret, 1))
  expect(blocked.status()).toBe(429)
  await anon.close()
  await ctx.close()
})
