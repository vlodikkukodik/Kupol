import AxeBuilder from '@axe-core/playwright'
import { expect, test } from '@playwright/test'
import { canWrite } from './helpers/kupol.js'
import { settle } from './helpers/ui.js'

// «Картотека» — третий вид каталога (этап 4): те же документы и то же правило видимости, что в реестре, но карточками.
test.skip(!canWrite, 'нужны фикстуры: против внешнего адреса не запускается')

const cards = (page) => page.getByTestId('cards').locator('li.card')
const codesOf = async (page) => page.getByTestId('cards').locator('li.card').evaluateAll((els) => els.map((e) => e.getAttribute('data-code')))

test('картотека: карточки вместо таблицы, вид и порядок — в адресе, закрытого нет', async ({ page }) => {
  await page.goto('/catalog?type=object&from=1900&to=1999')
  await page.getByTestId('view-cards').click()
  await expect(page).toHaveURL(/view=cards/)
  await expect(cards(page).first()).toBeVisible()
  await expect(page.getByTestId('registry')).toHaveCount(0)

  const codes = await codesOf(page)
  expect(codes).toContain('О-9001')
  for (const closed of ['О-9002', 'О-9003', 'О-9004', 'О-9007']) expect(codes).not.toContain(closed)
  await expect(page.locator('body')).not.toContainText('СЕКРЕТ-')
  const card = page.locator('li.card[data-code="О-9001"]')
  await expect(card).toContainText('Объект')
  await expect(card.getByRole('link')).toContainText('Открытый объект со всеми блоками')
  await card.getByRole('link').click()
  await expect(page).toHaveURL(/\/doc\/O-9001$/)

  // порядок карточек: по году, сначала новые — меняет адрес и порядок; возврат к шифру убирает параметры
  await page.goto('/catalog?view=cards&type=memo&from=1901&to=1902')
  await expect(cards(page).first()).toBeVisible()
  await page.getByLabel('Порядок карточек').selectOption('year:desc')
  await expect(page).toHaveURL(/sort=year/)
  await expect(page).toHaveURL(/order=desc/)
  await page.getByLabel('Порядок карточек').selectOption('code')
  await expect(page).not.toHaveURL(/sort=/)
  await expect(page).not.toHaveURL(/order=/)

  // обратно в реестр: вид сбрасывается
  await page.getByRole('button', { name: 'Реестр' }).click()
  await expect(page).not.toHaveURL(/view=/)
  await expect(page.getByTestId('registry')).toBeVisible()
})

test('картотека: телефон 375 px и доступность', async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 800 })
  await page.goto('/catalog?view=cards')
  await expect(cards(page).first()).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBeLessThanOrEqual(0)
  await page.setViewportSize({ width: 1200, height: 900 })
  await settle(page)
  const r = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()
  expect(r.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(' ')).slice(0, 3).join(' | ')}`)).toEqual([])
})
