import AxeBuilder from '@axe-core/playwright'
import { expect, test } from '@playwright/test'
import { canWrite, signUp } from './helpers/kupol.js'
import { openMenu } from './helpers/ui.js'

// Панель команды: роли, права и экран «Команда». Нужны настоящие пользователи и команды сервера.
test.skip(!canWrite, 'создаёт пользователей и выдаёт роли: против внешнего адреса не запускается')

const menu = (page) => page.getByRole('dialog', { name: 'Меню' })
const notFound = (page) => page.getByRole('heading', { name: 'Дело не найдено' })
const row = (page, login) => page.locator(`tr[data-login="${login}"]`)

async function menuLinks(page) {
  await openMenu(page)
  const links = await menu(page).getByRole('navigation', { name: 'Основная навигация' }).getByRole('link').allTextContents()
  await page.keyboard.press('Escape')
  return links.map((s) => s.trim())
}

async function newActor(browser, opts) {
  const context = await browser.newContext()
  const login = await signUp(context, opts)
  const page = await context.newPage()
  return { context, login, page }
}

test('гость: /team ведёт на главную и открывает окно входа с возвратом на панель', async ({ page }) => {
  await page.goto('/team')
  await expect(page).toHaveURL(/\/\?next=(%2F|\/)team$/)
  await expect(page.getByTestId('auth-modal')).toBeVisible()
})

test('пользователь без ролей: панели нет ни в меню, ни по адресу, ни в API — даже с высоким допуском', async ({ browser }) => {
  const { context, page } = await newActor(browser, { level: 6 })
  await page.goto('/')
  expect(await menuLinks(page)).not.toContain('Панель команды')

  for (const path of ['/team', '/team/members']) {
    await page.goto(path)
    await expect(notFound(page)).toBeVisible()
    await expect(page).toHaveURL(new RegExp(`${path}$`))
    await expect(page).toHaveTitle('Дело не найдено — КУПОЛ')
  }
  for (const [method, url] of [['get', '/api/team/roles'], ['get', '/api/team/members'], ['put', '/api/team/members/x/roles/editor']]) {
    const res = await page.request[method](url, method === 'put' ? { headers: { Origin: new URL(page.url()).origin } } : {})
    expect(res.status(), url).toBe(403)
    expect((await res.json()).error.code).toBe('forbidden')
  }
  await context.close()
})

test('Автор: панель есть, права показаны честно, экрана «Команда» нет', async ({ browser }) => {
  const { context, page } = await newActor(browser, { roles: ['author'] })
  await page.goto('/')
  expect(await menuLinks(page)).toContain('Панель команды')

  await page.goto('/team')
  await expect(page.getByRole('heading', { name: 'Панель команды', level: 1 })).toBeVisible()
  await expect(page.getByTestId('my-roles')).toHaveText('Автор')
  const rights = page.getByTestId('my-rights')
  await expect(rights.getByText('Создавать документы и править черновики')).not.toHaveClass(/off/)
  await expect(rights.locator('li', { hasText: 'Публиковать проверенные документы' })).toHaveClass(/off/)
  await expect(rights.locator('li', { hasText: 'Выдавать и снимать роли' })).toHaveClass(/off/)
  await expect(page.getByRole('navigation', { name: 'Разделы панели команды' }).getByRole('link')).toHaveText(['Роли и права', 'Документы'])

  // таблица «что даёт роль»: Автор не публикует, Редактор публикует
  const matrix = page.getByTestId('roles-matrix')
  const publish = matrix.locator('tr', { hasText: 'Публиковать проверенные документы' })
  await expect(publish.locator('td')).toHaveText(['нет', 'да', 'нет', 'нет', 'да'].map((s) => new RegExp(`${s}|—|✓`)))

  await page.goto('/team/members')
  await expect(notFound(page)).toBeVisible()

  // роли видны и в личном деле
  await page.goto('/file')
  await expect(page.getByTestId('file-roles')).toHaveText('Автор')
  await context.close()
})

test('Директорат выдаёт и снимает роль; получатель видит изменение без повторного входа', async ({ browser }) => {
  const dir = await newActor(browser, { directorate: true })
  const worker = await newActor(browser, {})
  const { login } = worker

  // у получателя открыт сайт: панели ещё нет
  await worker.page.goto('/catalog')
  await worker.page.goto('/team')
  await expect(notFound(worker.page)).toBeVisible()

  await dir.page.goto('/team/members')
  await dir.page.getByLabel('Логин', { exact: true }).fill(login)
  await dir.page.getByRole('button', { name: 'Найти' }).click()
  await expect(dir.page).toHaveURL(new RegExp(`q=${login}`))
  await expect(dir.page.getByTestId('members-total')).toHaveText('Найдено: 1')

  const editor = row(dir.page, login).getByRole('button', { name: 'Редактор' })
  await expect(editor).toHaveAttribute('aria-pressed', 'false')
  await editor.click()
  await expect(editor).toHaveAttribute('aria-pressed', 'true')
  await expect(dir.page.getByRole('status').filter({ hasText: `Роль «Редактор» выдана: ${login}` })).toBeAttached()

  // получатель: та же сессия, обычный переход — панель открывается
  await worker.page.goto('/team')
  await expect(worker.page.getByTestId('my-roles')).toHaveText('Редактор')
  await expect(worker.page.getByTestId('my-rights').locator('li', { hasText: 'Публиковать проверенные документы' })).not.toHaveClass(/off/)
  expect(await menuLinks(worker.page)).toContain('Панель команды')
  await worker.page.goto('/file')
  await expect(worker.page.getByTestId('file-roles')).toHaveText('Редактор')

  // ещё одна роль: обе видны, порядок — как в таблице ролей
  await row(dir.page, login).getByRole('button', { name: 'Автор' }).click()
  await expect(row(dir.page, login).getByRole('button', { name: 'Автор' })).toHaveAttribute('aria-pressed', 'true')
  await dir.page.reload()
  await expect(row(dir.page, login).locator('button[aria-pressed="true"]')).toHaveText(['Автор', 'Редактор'])

  // снять обе — панель закрывается сразу
  await row(dir.page, login).getByRole('button', { name: 'Редактор' }).click()
  await row(dir.page, login).getByRole('button', { name: 'Автор' }).click()
  await expect(row(dir.page, login).locator('button[aria-pressed="true"]')).toHaveCount(0)
  await worker.page.goto('/team')
  await expect(notFound(worker.page)).toBeVisible()
  await worker.page.goto('/file')
  await expect(worker.page.getByTestId('file-roles')).toHaveCount(0)

  await dir.context.close()
  await worker.context.close()
})

test('«Команда»: поиск и фильтры живут в адресе; Директорат показан флагом, без переключателей', async ({ browser }) => {
  const dir = await newActor(browser, { directorate: true })
  const staff = await newActor(browser, { roles: ['archivist', 'moderator'] })
  const plain = await newActor(browser, {})

  await dir.page.goto(`/team/members?q=${staff.login}`)
  await expect(dir.page.getByTestId('members-total')).toHaveText('Найдено: 1')
  await expect(row(dir.page, staff.login).locator('button[aria-pressed="true"]')).toHaveText(['Модератор', 'Архивариус'])

  // только команда: обычный пользователь исчезает из выдачи
  await dir.page.goto(`/team/members?q=${plain.login}`)
  await expect(row(dir.page, plain.login)).toBeVisible()
  await dir.page.getByLabel('Только команда').check()
  await expect(dir.page).toHaveURL(/staff=1/)
  await expect(dir.page.getByTestId('members-empty')).toBeVisible()
  await dir.page.getByRole('button', { name: 'Сбросить' }).click()
  await expect(dir.page).not.toHaveURL(/staff=1/)

  // фильтр по роли
  await dir.page.goto(`/team/members?role=archivist&q=${staff.login}`)
  await expect(row(dir.page, staff.login)).toBeVisible()
  await expect(dir.page.getByLabel('Роль', { exact: true })).toHaveValue('archivist')
  await dir.page.getByLabel('Роль', { exact: true }).selectOption('editor')
  await expect(dir.page.getByTestId('members-empty')).toBeVisible()

  // Директорат в списке: флаг вместо кнопок
  await dir.page.goto(`/team/members?q=${dir.login}`)
  await expect(row(dir.page, dir.login)).toContainText('Директорат: все роли и права')
  await expect(row(dir.page, dir.login).getByRole('button')).toHaveCount(0)

  // «_» и «%» в поиске — обычные символы, а не шаблон
  await dir.page.goto('/team/members?q=%25')
  await expect(dir.page.getByTestId('members-empty')).toBeVisible()

  // «назад» возвращает прежние условия
  await dir.page.goto('/team/members')
  await dir.page.getByLabel('Только команда').check()
  await expect(dir.page).toHaveURL(/staff=1/)
  await dir.page.goBack()
  await expect(dir.page).not.toHaveURL(/staff=1/)
  await expect(dir.page.getByLabel('Только команда')).not.toBeChecked()

  for (const a of [dir, staff, plain]) await a.context.close()
})

test('выдача роли без права отклоняется сервером, даже если запрос подделан руками', async ({ browser }) => {
  const editor = await newActor(browser, { roles: ['editor'] })
  const victim = await newActor(browser, {})
  await editor.page.goto('/team')
  const origin = new URL(editor.page.url()).origin
  const res = await editor.page.request.put(`/api/team/members/${victim.login}/roles/editor`, { headers: { Origin: origin } })
  expect(res.status()).toBe(403)
  await victim.page.goto('/team')
  await expect(notFound(victim.page)).toBeVisible() // роль не выдана

  // самовыдача — тоже нельзя
  const self = await editor.page.request.put(`/api/team/members/${editor.login}/roles/author`, { headers: { Origin: origin } })
  expect(self.status()).toBe(403)
  await editor.context.close()
  await victim.context.close()
})

test('телефон 375 px: панель и «Команда» без горизонтальной прокрутки, кнопки ролей не мельче 44 px', async ({ browser }) => {
  const context = await browser.newContext({ viewport: { width: 375, height: 700 }, hasTouch: true })
  await signUp(context, { directorate: true })
  const page = await context.newPage()
  for (const path of ['/team', '/team/members']) {
    await page.goto(path)
    await expect(page.getByRole('heading', { name: 'Панель команды', level: 1 })).toBeVisible()
    // если страница вылезает вширь, в сообщении будет видно, какие элементы (внутри своей прокрутки таблицы не считаются)
    await expect
      .poll(() =>
        page.evaluate(() => {
          if (document.documentElement.scrollWidth <= window.innerWidth) return []
          const scrolls = (el) => ['auto', 'scroll', 'hidden'].includes(getComputedStyle(el).overflowX)
          const inScroller = (el) => {
            for (let p = el.parentElement; p && p !== document.body; p = p.parentElement) if (scrolls(p)) return true
            return false
          }
          return [...document.querySelectorAll('body *')]
            .filter((el) => el.getBoundingClientRect().right > window.innerWidth + 1 && !inScroller(el))
            .slice(0, 6)
            .map((el) => `${el.tagName.toLowerCase()}.${el.className} right=${Math.round(el.getBoundingClientRect().right)}`)
        }),
      )
      .toEqual([])
  }
  await page.goto('/team/members')
  const toggle = page.locator('button.toggle').first()
  await expect(toggle).toBeVisible()
  expect((await toggle.boundingBox()).height).toBeGreaterThanOrEqual(44)
  await page.screenshot({ path: 'e2e/results/team-members-mobile.png' })
  await context.close()
})

test('доступность: панель, «Команда» и таблица ролей (WCAG 2.1 AA)', async ({ browser }) => {
  const dir = await newActor(browser, { directorate: true })
  const failed = [] // неудачные ответы API — чтобы при сбое видеть код и адрес, а не только «элемент не найден»
  dir.page.on('response', (r) => {
    if (r.url().includes('/api/') && r.status() >= 400) failed.push(`${r.status()} ${new URL(r.url()).pathname}${new URL(r.url()).search}`)
  })
  for (const path of ['/team', '/team/members']) {
    await dir.page.goto(path)
    await expect(dir.page.getByRole('heading', { name: 'Панель команды', level: 1 })).toBeVisible()
    if (path.endsWith('members')) await expect(dir.page.getByTestId('members-total')).toBeVisible({ timeout: 10_000 }).catch(() => {})
    expect(failed).toEqual([])
    if (path.endsWith('members')) await expect(dir.page.getByTestId('members')).toBeVisible()
    else await expect(dir.page.getByTestId('roles-matrix')).toBeVisible()
    const results = await new AxeBuilder({ page: dir.page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()
    const summary = results.violations.map((v) => `${v.id}: ${v.help}\n  ${v.nodes.map((n) => n.target.join(' ')).join('\n  ')}`)
    expect(summary, `нарушения доступности: ${path}`).toEqual([])
    await dir.page.screenshot({ path: `e2e/results/team${path.endsWith('members') ? '-members' : ''}.png`, fullPage: !path.endsWith('members') })
  }
  await dir.context.close()
})
