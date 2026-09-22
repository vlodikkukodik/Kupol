import { expect, test } from '@playwright/test'
import { canWrite, signUp } from './helpers/kupol.js'

// «Пометки на полях» (шаг 5.2): нужны фикстуры (О-9001, открытый документ) и настоящие пользователи/роли.
test.skip(!canWrite, 'нужны фикстуры и пользователи: против внешнего адреса не запускается')

async function newActor(browser, opts) {
  const context = await browser.newContext()
  const login = await signUp(context, opts)
  const page = await context.newPage()
  return { context, login, page }
}

const uniq = () => Math.random().toString(36).slice(2, 10)

test('гость видит пометки, но не форму; вошедший пишет, отвечает, жалуется; модератор удаляет', async ({ page, browser }) => {
  const reader1 = await newActor(browser, {})
  const reader2 = await newActor(browser, {})
  const moderator = await newActor(browser, { roles: ['moderator'] })
  const mark = uniq()
  const rootText = `Первая пометка от e2e ${mark}`
  const replyText = `Ответ от второго читателя ${mark}`

  await page.goto('/doc/O-9001')
  await expect(page.getByRole('heading', { name: 'Пометки на полях' })).toBeVisible()
  await expect(page.getByText('Чтобы оставить пометку, нужно войти.')).toBeVisible()

  await reader1.page.goto('/doc/O-9001')
  await reader1.page.getByPlaceholder('Ваша пометка…').fill(rootText)
  await reader1.page.getByRole('button', { name: 'Оставить пометку' }).click()
  const firstRemark = reader1.page.locator('li.remark', { hasText: rootText })
  await expect(firstRemark).toBeVisible()

  await reader2.page.goto('/doc/O-9001')
  const remarkOnReader2 = reader2.page.locator('li.remark', { hasText: rootText })
  await remarkOnReader2.getByRole('button', { name: 'Ответить' }).click()
  await remarkOnReader2.getByPlaceholder('Ваш ответ…').fill(replyText)
  await remarkOnReader2.getByRole('button', { name: 'Отправить ответ' }).click()
  await expect(reader2.page.locator('ul.remark__children li.remark', { hasText: replyText })).toBeVisible()

  // перезагрузка страницы гостем: тред виден целиком, с веткой
  await page.reload()
  await expect(page.locator('li.remark', { hasText: rootText })).toBeVisible()
  await expect(page.locator('ul.remark__children li.remark', { hasText: replyText })).toBeVisible()

  // жалоба на первую пометку (getByRole искал бы и кнопку вложенного ответа — берём только свою панель действий корня)
  await reader2.page.reload()
  const rootActions = remarkOnReader2.locator(':scope > .remark__actions')
  await rootActions.getByRole('button', { name: 'Жалоба' }).click()
  await expect(rootActions.getByText('Жалоба отправлена')).toBeVisible()

  // модератор видит жалобу в панели команды и удаляет пометку (вместе с веткой)
  await moderator.page.goto('/team/reports')
  await expect(moderator.page.getByRole('heading', { name: 'Жалобы' })).toBeVisible()
  const reportRow = moderator.page.locator('li', { hasText: rootText })
  await expect(reportRow).toBeVisible()
  await reportRow.getByTestId('report-delete').click()
  await moderator.page.getByTestId('report-confirm-delete').click()
  await expect(moderator.page.getByText(rootText)).toHaveCount(0)

  await page.reload()
  await expect(page.getByText(rootText)).toHaveCount(0)

  await reader1.context.close()
  await reader2.context.close()
  await moderator.context.close()
})
