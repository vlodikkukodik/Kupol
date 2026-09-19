import { expect, test } from '@playwright/test'
import { PASSWORD, canWrite, signUp } from './helpers/kupol.js'
import { openAuth, openMenu, settle } from './helpers/ui.js'

// Бургер, боковое меню и окно входа: открытие и закрытие, фокус, клавиатура, недоступность страницы под ними.

const burger = (page) => page.getByRole('button', { name: 'Меню', exact: true })
const menu = (page) => page.getByRole('dialog', { name: 'Меню' })
const modal = (page) => page.getByTestId('auth-modal')

const insideDialog = (page, selector) => page.evaluate((s) => Boolean(document.activeElement?.closest(s)), selector)

test.describe('боковое меню', () => {
  test('бургер открывает меню, повторное нажатие и «Закрыть меню» закрывают; aria-expanded следует за состоянием', async ({ page }) => {
    await page.goto('/')
    await expect(burger(page)).toHaveAttribute('aria-expanded', 'false')
    await expect(menu(page)).toHaveCount(0)

    await burger(page).click()
    await expect(menu(page)).toBeVisible()
    await expect(burger(page)).toHaveAttribute('aria-expanded', 'true')
    await expect(menu(page).getByRole('link', { name: 'Главная' })).toBeVisible()
    await expect(menu(page).getByRole('link', { name: 'Каталог' })).toBeVisible()
    await expect(menu(page).getByRole('link', { name: 'Личное дело' })).toHaveCount(0) // гостю личного дела нет
    await expect(menu(page).getByRole('button', { name: 'Войти или зарегистрироваться' })).toBeVisible()
    await settle(page)
    await page.screenshot({ path: 'e2e/results/menu-desktop.png' })

    await menu(page).getByRole('button', { name: 'Закрыть меню' }).click()
    await expect(menu(page)).toHaveCount(0)
    await expect(burger(page)).toHaveAttribute('aria-expanded', 'false')
    await expect(burger(page)).toBeFocused() // фокус вернулся туда, откуда открыли
  })

  test('Esc и щелчок по затемнению закрывают меню; фокус возвращается на бургер', async ({ page }) => {
    await page.goto('/')
    await openMenu(page)
    await page.keyboard.press('Escape')
    await expect(menu(page)).toHaveCount(0)
    await expect(burger(page)).toBeFocused()

    await openMenu(page)
    await page.getByTestId('menu-scrim').click({ position: { x: 1000, y: 300 } })
    await expect(menu(page)).toHaveCount(0)
    await expect(burger(page)).toBeFocused()
  })

  test('пока меню открыто, страница под ним недоступна и не прокручивается', async ({ page }) => {
    await page.goto('/catalog')
    await openMenu(page)
    await expect(page.locator('.page')).toHaveAttribute('inert', '')
    await expect(page.locator('html')).toHaveClass(/scroll-locked/)
    expect(await page.evaluate(() => getComputedStyle(document.documentElement).overflow)).toBe('hidden')

    await page.keyboard.press('Escape')
    await expect(page.locator('.page')).not.toHaveAttribute('inert', /.*/)
    await expect(page.locator('html')).not.toHaveClass(/scroll-locked/)
  })

  test('фокус не выходит из меню: Tab и Shift+Tab ходят по кругу', async ({ page }) => {
    await page.goto('/')
    await openMenu(page)
    expect(await insideDialog(page, '#site-menu')).toBe(true) // фокус сразу внутри

    for (let i = 0; i < 12; i++) {
      await page.keyboard.press('Tab')
      expect(await insideDialog(page, '#site-menu'), `Tab №${i + 1}`).toBe(true)
    }
    for (let i = 0; i < 12; i++) {
      await page.keyboard.press('Shift+Tab')
      expect(await insideDialog(page, '#site-menu'), `Shift+Tab №${i + 1}`).toBe(true)
    }
  })

  test('переход по ссылке закрывает меню; текущая страница помечена', async ({ page }) => {
    await page.goto('/')
    await openMenu(page)
    await expect(menu(page).getByRole('link', { name: 'Главная' })).toHaveAttribute('aria-current', 'page')
    await menu(page).getByRole('link', { name: 'Каталог' }).click()
    await expect(page).toHaveURL(/\/catalog$/)
    await expect(menu(page)).toHaveCount(0)

    // ссылка на ту же страницу перехода не даёт, но меню всё равно закрывается
    await openMenu(page)
    await menu(page).getByRole('link', { name: 'Каталог' }).click()
    await expect(menu(page)).toHaveCount(0)
  })

  test('кнопка «назад» браузера закрывает меню вместе с переходом', async ({ page }) => {
    await page.goto('/')
    await openMenu(page)
    await menu(page).getByRole('link', { name: 'Каталог' }).click()
    await expect(page).toHaveURL(/\/catalog$/)
    await openMenu(page)
    await page.goBack()
    await expect(page).toHaveURL(/\/$/)
    await expect(menu(page)).toHaveCount(0)
  })

  test('телефон 375 px: меню помещается, страница не ползёт вширь', async ({ browser }) => {
    const ctx = await browser.newContext({ viewport: { width: 375, height: 667 }, hasTouch: true })
    const page = await ctx.newPage()
    await page.goto('/')
    await openMenu(page)
    const box = await menu(page).boundingBox()
    expect(box.width).toBeLessThanOrEqual(375 * 0.9)
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
    await page.screenshot({ path: 'e2e/results/menu-mobile.png' })
    await ctx.close()
  })
})

test.describe('окно входа', () => {
  test('кнопка в меню закрывает меню, открывает окно и ставит фокус в поле логина', async ({ page }) => {
    await page.goto('/')
    await openMenu(page)
    await menu(page).getByRole('button', { name: 'Войти или зарегистрироваться' }).click()
    await expect(modal(page)).toBeVisible()
    await expect(menu(page)).toHaveCount(0)
    await expect(modal(page)).toHaveAttribute('aria-modal', 'true')
    await expect(page.getByRole('dialog', { name: 'Допуск в архив' })).toBeVisible()
    await expect(page.getByLabel('Логин', { exact: true })).toBeFocused()
    await expect(page.locator('.page')).toHaveAttribute('inert', '')
  })

  test('Esc закрывает окно, фокус — на бургере', async ({ page }) => {
    await page.goto('/')
    await openAuth(page)
    await page.keyboard.press('Escape')
    await expect(modal(page)).toHaveCount(0)
    await expect(burger(page)).toBeFocused()
    await expect(page.locator('.page')).not.toHaveAttribute('inert', /.*/)
    await expect(page.locator('html')).not.toHaveClass(/scroll-locked/)
  })

  test('крестик и щелчок вне окна закрывают его, щелчок внутри — нет', async ({ page }) => {
    await page.goto('/')
    await openAuth(page)
    await modal(page).getByRole('heading', { name: 'Допуск в архив' }).click()
    await expect(modal(page)).toBeVisible()

    await modal(page).getByRole('button', { name: 'Закрыть окно входа' }).click()
    await expect(modal(page)).toHaveCount(0)

    await openAuth(page)
    await page.mouse.click(5, 5)
    await expect(modal(page)).toHaveCount(0)
  })

  test('фокус не выходит из окна; вкладки по-прежнему переключаются стрелками', async ({ page }) => {
    await page.goto('/')
    await openAuth(page)
    for (let i = 0; i < 14; i++) {
      await page.keyboard.press('Tab')
      expect(await insideDialog(page, '[data-testid="auth-modal"]'), `Tab №${i + 1}`).toBe(true)
    }
    for (let i = 0; i < 14; i++) {
      await page.keyboard.press('Shift+Tab')
      expect(await insideDialog(page, '[data-testid="auth-modal"]'), `Shift+Tab №${i + 1}`).toBe(true)
    }
    await page.getByRole('tab', { name: 'Вход' }).focus()
    await page.keyboard.press('ArrowRight')
    await expect(page.getByRole('tab', { name: 'Регистрация' })).toHaveAttribute('aria-selected', 'true')
  })

  test('окно открывается заново пустым: введённое не остаётся после закрытия', async ({ page }) => {
    await page.goto('/')
    await openAuth(page)
    await page.getByLabel('Логин', { exact: true }).fill('кто-то')
    await page.getByLabel('Пароль', { exact: true }).fill('секрет-которого-не-должно-остаться')
    await page.keyboard.press('Escape')
    await openAuth(page)
    await expect(page.getByLabel('Логин', { exact: true })).toHaveValue('')
    await expect(page.getByLabel('Пароль', { exact: true })).toHaveValue('')
  })

  test('телефон 375 px: окно регистрации выше экрана прокручивается, крестик и форма достижимы', async ({ browser }) => {
    const ctx = await browser.newContext({ viewport: { width: 375, height: 560 }, hasTouch: true })
    const page = await ctx.newPage()
    await page.goto('/')
    await openAuth(page, 'Регистрация')
    await expect(page.locator('.captcha-question')).not.toHaveText(/Загрузка/)
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
    const submit = page.getByRole('button', { name: 'Оформить допуск' })
    await submit.scrollIntoViewIfNeeded()
    await expect(submit).toBeInViewport()
    await page.screenshot({ path: 'e2e/results/auth-modal-mobile.png' })
    await ctx.close()
  })
})

test.describe('вход из окна', () => {
  test.skip(!canWrite, 'создаёт пользователей: против внешнего адреса не запускается')

  test('вошедшему в меню — личное дело и его пропуск, кнопки входа нет', async ({ browser }) => {
    const ctx = await browser.newContext()
    const login = await signUp(ctx, { level: 1 })
    const page = await ctx.newPage()
    await page.goto('/catalog')
    await openMenu(page)
    await expect(menu(page)).toContainText(login)
    await expect(menu(page).getByRole('button', { name: 'Войти или зарегистрироваться' })).toHaveCount(0)
    await menu(page).getByRole('link', { name: 'Личное дело' }).click()
    await expect(page).toHaveURL(/\/file$/)
    await ctx.close()
  })

  test('«Доступ запрещён»: вход прямо из окна возвращает на тот же документ уже с новым допуском', async ({ browser }) => {
    const donor = await browser.newContext()
    const login = await signUp(donor, { level: 2 })
    await donor.close()

    const ctx = await browser.newContext()
    const page = await ctx.newPage()
    await page.goto('/doc/O-9002')
    await expect(page.getByTestId('access-denied')).toBeVisible()
    await page.getByTestId('access-denied').getByRole('button', { name: 'Войти или зарегистрироваться' }).click()
    await expect(modal(page)).toBeVisible()

    await page.getByLabel('Логин', { exact: true }).fill(login)
    await page.getByLabel('Пароль', { exact: true }).fill(PASSWORD)
    await page.getByRole('button', { name: 'Войти', exact: true }).click()

    await expect(modal(page)).toHaveCount(0)
    await expect(page).toHaveURL(/\/doc\/O-9002$/)
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('[e2e] Объект допуска 2')
    await ctx.close()
  })
})
