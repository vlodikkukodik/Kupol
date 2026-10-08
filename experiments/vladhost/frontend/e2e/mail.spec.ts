import { expect, test, type APIRequestContext, type Page } from '@playwright/test'
import { existsSync, readdirSync, readFileSync } from 'node:fs'

const SPOOL = '/tmp/vh-e2e-mail'

// Разбирает quoted-printable: мягкие переносы строк и байты вида =D0.
function decodeQP(s: string): string {
  const bytes: number[] = []
  const t = s.replace(/=\r?\n/g, '')
  for (let i = 0; i < t.length; i++) {
    if (t[i] === '=' && /^[0-9A-F]{2}$/i.test(t.slice(i + 1, i + 3))) {
      bytes.push(parseInt(t.slice(i + 1, i + 3), 16))
      i += 2
    } else bytes.push(...Buffer.from(t[i]!, 'utf8'))
  }
  return Buffer.from(bytes).toString('utf8')
}

interface Mail {
  to: string
  subject: string
  text: string
}

// Письма стенда: в папке лежат .eml, которые ушли бы по SMTP. Текстовая часть — первая в multipart/alternative.
function mails(to: string): Mail[] {
  if (!existsSync(SPOOL)) return []
  return readdirSync(SPOOL)
    .sort()
    .map((f) => readFileSync(`${SPOOL}/${f}`, 'utf8'))
    .filter((raw) => new RegExp(`^To: .*${to.replace('.', '\\.')}`, 'im').test(raw))
    .map((raw) => {
      const subject = /^Subject: (.*)$/im.exec(raw)?.[1] ?? ''
      const body = raw.split(/\r?\n\r?\n/).slice(1).join('\n\n')
      const plain = body.split(/--[0-9a-f]{20,}/)[1] ?? body
      return { to, subject, text: decodeQP(plain.split(/\r?\n\r?\n/).slice(1).join('\n\n')) }
    })
}

async function link(to: string, subjectPart: string, nth = 0): Promise<string> {
  let found = ''
  await expect
    .poll(
      () => {
        const m = mails(to).filter((x) => x.subject.length > 0 && decodeSubject(x.subject).includes(subjectPart))[nth]
        found = m ? (/https?:\/\/\S+/.exec(m.text)?.[0] ?? '') : ''
        return found
      },
      { timeout: 40_000, message: `письмо «${subjectPart}» для ${to}` },
    )
    .not.toBe('')
  return found
}

// Ждёт письмо с такой темой (у письма без ссылки, например о смене пароля, проверять нечего, кроме самого факта).
async function mailArrives(to: string, subjectPart: string): Promise<void> {
  await expect
    .poll(() => mails(to).some((x) => decodeSubject(x.subject).includes(subjectPart)), { timeout: 40_000, message: `письмо «${subjectPart}» для ${to}` })
    .toBe(true)
}

// Тема закодирована как =?utf-8?q?...?= (может быть в нескольких кусках).
function decodeSubject(s: string): string {
  return s.replace(/=\?utf-8\?q\?([^?]*)\?=\s*/gi, (_m, enc: string) => decodeQP(enc.replace(/_/g, ' '))).trim()
}

async function adminInvite(request: APIRequestContext): Promise<string> {
  const login = await request.post('/api/auth/login', { data: { login: process.env.E2E_ADMIN, password: process.env.E2E_ADMIN_PASSWORD } })
  const token = (await login.json()).access_token as string
  const inv = await request.post('/api/invites', { headers: { Authorization: `Bearer ${token}` } })
  return (await inv.json()).invite.code as string
}

async function fillLogin(page: Page, login: string, password: string) {
  await page.goto('/login')
  await page.getByLabel('Email или имя').fill(login)
  await page.getByLabel('Пароль').fill(password)
  await page.getByRole('button', { name: 'Войти' }).click()
}

test('почта: подтверждение адреса, сброс пароля и уведомления', async ({ page, request }) => {
  test.setTimeout(150_000)
  const name = `m${Date.now().toString(36)}`
  const email = `${name}@example.com`
  const invite = await adminInvite(request)

  // Регистрация: письмо с подтверждением уходит само, а в панели видно предупреждение.
  await page.goto(`/register?invite=${invite}`)
  await page.getByLabel('Email', { exact: true }).fill(email)
  await page.getByLabel('Имя пользователя').fill(name)
  await page.getByLabel('Пароль').fill('password-123')
  await page.getByRole('button', { name: 'Создать аккаунт' }).click()
  await expect(page.getByText(`Здравствуйте, ${name}`)).toBeVisible()
  const banner = page.getByTestId('verify-banner')
  await expect(banner).toContainText(`Подтвердите адрес почты ${email}`)
  if (process.env.SHOTS) {
    await page.waitForTimeout(1200)
    await page.screenshot({ path: '/tmp/vh-shots/mail-banner.png' })
  }

  // Повторная отправка: сразу — рано (пауза между письмами), об этом говорит понятное сообщение.
  await banner.getByRole('button', { name: 'Отправить письмо ещё раз' }).click()
  await expect(page.getByText('Письмо уже отправлено недавно')).toBeVisible()

  // Ссылка из письма подтверждает адрес (открываем в той же вкладке, где вход выполнен) — баннер пропадает.
  const verifyLink = await link(email, 'Подтвердите адрес почты')
  expect(verifyLink).toContain('/verify-email?token=')
  await page.goto(verifyLink)
  await expect(page.getByText('Адрес почты подтверждён')).toBeVisible()
  await page.getByRole('button', { name: 'В панель' }).click()
  await expect(page.getByText(`Здравствуйте, ${name}`)).toBeVisible()
  await expect(page.getByTestId('verify-banner')).toHaveCount(0)
  // Использованная ссылка второй раз не работает.
  await page.goto(verifyLink)
  await expect(page.getByText('Ссылка недействительна, устарела или уже использована')).toBeVisible()

  // Настройки: уведомления включены, адрес подтверждён; выключатель сохраняется.
  await page.goto('/settings')
  const notify = page.getByRole('switch', { name: 'Присылать уведомления' })
  await expect(notify).toBeChecked()
  await expect(page.getByText('адрес подтверждён')).toBeVisible()
  await notify.click()
  await expect(page.getByText('Настройки уведомлений сохранены')).toBeVisible()
  await page.reload()
  await expect(page.getByRole('switch', { name: 'Присылать уведомления' })).not.toBeChecked()

  // Сброс пароля: выходим, просим ссылку (ответ одинаков для любого адреса), задаём новый пароль.
  await page.locator('button.user').click()
  await page.getByText('Выйти').click()
  await expect(page.getByRole('heading', { name: 'С возвращением' })).toBeVisible()
  await page.getByRole('link', { name: 'Забыли пароль?' }).click()
  await expect(page.getByRole('heading', { name: 'Забыли пароль?' })).toBeVisible() // страница сменилась, а не только ссылка нажата
  if (process.env.SHOTS) {
    await page.waitForTimeout(1000)
    await page.screenshot({ path: '/tmp/vh-shots/mail-forgot.png' })
  }
  await page.getByLabel('Email', { exact: true }).fill('no-such-user@example.com')
  await page.getByRole('button', { name: 'Прислать ссылку' }).click()
  await expect(page.getByText('Если такой адрес есть в системе')).toBeVisible()
  await page.goto('/forgot')
  await page.getByLabel('Email', { exact: true }).fill(email)
  await page.getByRole('button', { name: 'Прислать ссылку' }).click()
  await expect(page.getByText('Если такой адрес есть в системе')).toBeVisible()
  expect(mails('no-such-user@example.com')).toHaveLength(0) // несуществующему адресу письмо не уходит

  const resetLink = await link(email, 'Сброс пароля')
  await page.goto(resetLink)
  if (process.env.SHOTS) {
    await page.waitForTimeout(1000)
    await page.screenshot({ path: '/tmp/vh-shots/mail-reset.png' })
  }
  await page.getByLabel('Новый пароль').fill('short')
  await page.getByLabel('Повторите пароль').fill('short')
  await page.getByRole('button', { name: 'Сменить пароль' }).click()
  await expect(page.getByText('Пароль короче 8 символов')).toBeVisible()
  await page.getByLabel('Новый пароль').fill('brand-new-pass-1')
  await page.getByLabel('Повторите пароль').fill('другой-пароль-1')
  await page.getByRole('button', { name: 'Сменить пароль' }).click()
  await expect(page.getByText('Пароли не совпадают')).toBeVisible()
  await page.getByLabel('Повторите пароль').fill('brand-new-pass-1')
  await page.getByRole('button', { name: 'Сменить пароль' }).click()
  await expect(page.getByText('Пароль изменён. Теперь можно войти')).toBeVisible()

  // Ссылка одноразовая; старый пароль не подходит, новый — да; о смене приходит письмо.
  await page.goto(resetLink)
  await page.getByLabel('Новый пароль').fill('another-pass-123')
  await page.getByLabel('Повторите пароль').fill('another-pass-123')
  await page.getByRole('button', { name: 'Сменить пароль' }).click()
  await expect(page.getByText('Ссылка недействительна, устарела или уже использована')).toBeVisible()
  await fillLogin(page, name, 'password-123')
  await expect(page.getByText('Неверный логин или пароль')).toBeVisible()
  await fillLogin(page, name, 'brand-new-pass-1')
  await expect(page.getByText(`Здравствуйте, ${name}`)).toBeVisible()
  await mailArrives(email, 'Пароль от аккаунта изменён')
})

test('почта: письма на языке интерфейса', async ({ page, request }) => {
  test.setTimeout(90_000)
  const name = `i${Date.now().toString(36)}`
  const email = `${name}@example.com`
  const invite = await adminInvite(request)
  await page.context().addInitScript(() => localStorage.setItem('vh-lang', 'it'))
  await page.goto(`/register?invite=${invite}`)
  await page.getByLabel('Email', { exact: true }).fill(email)
  await page.getByLabel('Nome utente').fill(name)
  await page.getByLabel('Password').fill('password-123')
  await page.getByRole('button', { name: 'Crea account' }).click()
  await expect(page.getByText(`Ciao, ${name}`)).toBeVisible()
  await expect(page.getByTestId('verify-banner')).toContainText('Conferma l\'indirizzo email')
  const verifyLink = await link(email, 'Conferma l\'indirizzo email')
  expect(verifyLink).toContain('/verify-email?token=')
})
