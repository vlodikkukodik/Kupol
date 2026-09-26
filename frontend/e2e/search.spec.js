import { writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { resolve } from 'node:path'
import AxeBuilder from '@axe-core/playwright'
import { expect, test } from '@playwright/test'
import { canWrite, cli, signUp } from './helpers/kupol.js'
import { settle } from './helpers/ui.js'

// Поиск по архиву (этап 4): что находит каждый читатель и чего не должен видеть ни в выдаче, ни в ответе сервера.
test.skip(!canWrite, 'создаёт документы и пользователей: против внешнего адреса не запускается')
// Документы общие для всех тестов файла и удаляются в afterAll: при fullyParallel (на CI два воркера) beforeAll/afterAll
// шли бы у каждого воркера свои, и один удалил бы документы из-под другого. Serial держит файл в одном воркере.
test.describe.configure({ mode: 'serial' })

// Уникальный «корень» запроса: другие тесты и прогоны с этими документами не пересекаются.
const RUN = Math.random().toString(36).slice(2, 8)
// Слово из одной кириллицы: латиница с цифрами разбилась бы на части и морфология считала бы их по-своему
const WORD = [...RUN].map((ch) => 'бвгджзклмнпрстфхцчш'[ch.charCodeAt(0) % 19]).join('') + 'ак'
const SECRET4 = `zsecret4${RUN}`
const SECRET5 = `zsecret5${RUN}`
const CODES = { open: 'МЕМО-9601', closed: 'МЕМО-9602', draft: 'МЕМО-9603' }
const FILE = resolve(tmpdir(), `kupol-e2e-search-${RUN}.json`)

const para = (id, ...runs) => ({ id, type: 'paragraph', data: { text: runs.map((r) => (typeof r === 'string' ? { text: r } : r)) } })

test.beforeAll(() => {
  writeFileSync(
    FILE,
    JSON.stringify({
      documents: [
        {
          code: CODES.open,
          type: 'memo',
          title: `[e2e] Записка про ${WORD}`,
          status: 'published',
          level: 0,
          composed: { year: 1979 },
          blocks: [
            para('intro', `Сотрудников ${WORD} проводили опыты в подвале. `),
            para('mix', 'Открытое начало ', { text: SECRET4, level: 4 }, ' и открытый хвост.'),
            { id: 'closed-block', type: 'paragraph', level: 5, data: { text: `Закрытый блок ${SECRET5}` } },
          ],
        },
        {
          code: CODES.closed,
          type: 'memo',
          title: `[e2e] Закрытый ${SECRET5}`,
          status: 'published',
          level: 5,
          composed: { year: 1985 },
          blocks: [para('a', `Текст закрытого документа про ${WORD}`)],
        },
        {
          code: CODES.draft,
          type: 'memo',
          title: `[e2e] Черновик про ${WORD}`,
          status: 'draft',
          level: 0,
          composed: { year: 1990 },
          blocks: [para('a', 'Черновик')],
        },
      ],
    }),
  )
  cli('doc', 'import', FILE)
})

test.afterAll(() => {
  for (const code of Object.values(CODES)) {
    try {
      cli('doc', 'delete', code, '--yes')
    } catch {
      // уже удалён
    }
  }
})

const hits = (page) => page.getByTestId('search-hits').locator('li.hit')
const hit = (page, code) => page.getByTestId('search-hits').locator(`li.hit[data-code="${code}"]`)

async function search(page, q, extra = '') {
  await page.goto(`/search?q=${encodeURIComponent(q)}${extra}`)
  await expect(page.getByTestId('search-total').or(page.getByTestId('search-bad')).or(page.getByText('слишком короткий'))).toBeVisible()
}

test('гость: находит открытое по морфологии, подсвечивает совпадение и ведёт к блоку', async ({ page }) => {
  await page.goto('/')
  await page.getByRole('button', { name: 'Меню', exact: true }).click()
  await page.getByRole('dialog', { name: 'Меню' }).getByRole('link', { name: 'Поиск' }).click()
  await expect(page).toHaveURL(/\/search$/)
  await expect(page.getByTestId('search-empty-query')).toBeVisible()

  // морфология: «сотрудник» находит «сотрудников» рядом с редким словом
  await page.getByRole('searchbox', { name: 'Найти в архиве' }).fill(`сотрудник ${WORD}`)
  await page.getByRole('button', { name: 'Найти', exact: true }).click()
  await expect(page).toHaveURL(new RegExp(`/search\\?q=`))
  await expect(page.getByTestId('search-total')).toHaveText('Найдено документов: 1')
  await expect(hits(page)).toHaveCount(1)
  const h = hit(page, CODES.open)
  await expect(h).toContainText(`Записка про ${WORD}`)
  await expect(h.locator('mark').first()).toBeVisible()
  // закрытый документ и черновик не находятся и не упоминаются
  await expect(page.locator('body')).not.toContainText(CODES.closed)
  await expect(page.locator('body')).not.toContainText(CODES.draft)
  await expect(page.locator('body')).not.toContainText(SECRET5)

  // сниппет ведёт к блоку: страница документа, блок отмечен и виден
  await h.locator('.snip', { hasText: 'Текст' }).getByRole('link').click()
  await expect(page).toHaveURL(new RegExp(`/doc/MEMO-9601#b-intro$`))
  const block = page.locator('#b-intro')
  await expect(block).toHaveAttribute('data-found', '')
  await expect(block).toBeInViewport()
  await expect(block).toBeFocused()
})

test('закрытое не находится и не раскрывается: ни фрагмент, ни блок, ни название закрытого документа', async ({ page }) => {
  for (const secret of [SECRET4, SECRET5]) {
    await search(page, secret)
    await expect(page.getByTestId('search-total')).toHaveText('Найдено документов: 0')
    await expect(page.getByTestId('search-nothing')).toBeVisible()
    await expect(page.locator('body')).not.toContainText(CODES.closed)
  }
  // открытый абзац рядом с закрытым фрагментом находится, но сниппет закрытого не содержит
  await search(page, 'открытый хвост')
  await expect(hit(page, CODES.open)).toBeVisible()
  await expect(page.locator('body')).not.toContainText(SECRET4)

  // и то же на уровне ответа сервера: в теле нет ни метки, ни закрытого документа
  for (const q of [WORD, 'открытый хвост', SECRET4, SECRET5]) {
    const res = await page.request.get(`/api/search?q=${encodeURIComponent(q)}`)
    expect(res.status()).toBe(200)
    // ответ повторяет сам запрос (его написал читатель): проверяем всё остальное
    const { query, ...body } = await res.json()
    expect(query).toBe(q)
    const raw = JSON.stringify(body)
    for (const forbidden of [SECRET4, SECRET5, CODES.closed, CODES.draft]) expect(raw, `запрос «${q}»`).not.toContain(forbidden)
  }
})

test('допуск читателя расширяет выдачу ровно до его уровня, Директорат видит и черновики', async ({ browser }) => {
  const four = await browser.newContext()
  await signUp(four, { level: 4 })
  const p4 = await four.newPage()
  await search(p4, SECRET4)
  await expect(hit(p4, CODES.open)).toBeVisible() // фрагмент уровня 4 виден уровню 4
  await search(p4, SECRET5)
  await expect(p4.getByTestId('search-total')).toHaveText('Найдено документов: 0') // блок уровня 5 — нет
  await search(p4, WORD)
  await expect(hits(p4)).toHaveCount(1) // закрытый документ уровня 5 не виден
  await four.close()

  const five = await browser.newContext()
  await signUp(five, { level: 5 })
  const p5 = await five.newPage()
  await search(p5, WORD)
  await expect(hits(p5)).toHaveCount(2) // открытая записка и документ уровня 5
  await expect(hit(p5, CODES.closed)).toBeVisible()
  await search(p5, SECRET5)
  await expect(hit(p5, CODES.open)).toBeVisible()
  await expect(hit(p5, CODES.closed)).toBeVisible()
  await five.close()

  const boss = await browser.newContext()
  await signUp(boss, { level: 6, directorate: true })
  const pb = await boss.newPage()
  await search(pb, WORD)
  await expect(hits(pb)).toHaveCount(3)
  await expect(hit(pb, CODES.draft)).toContainText('Черновик') // статус — словами, а не кодом
  await boss.close()
})

test('запрос и фильтры живут в адресе; короткие и слишком частые запросы объяснены', async ({ page }) => {
  await search(page, WORD)
  await expect(page.getByTestId('search-total')).toHaveText('Найдено документов: 1') // гость: только открытая записка
  // фильтр по типу — в адресе; выбор в списке меняет адрес и выдачу
  await page.getByLabel('Тип', { exact: true }).selectOption('memo')
  await expect(page).toHaveURL(/type=memo/)
  await expect(page.getByTestId('search-total')).toHaveText('Найдено документов: 1')
  await page.goto(`/search?q=${encodeURIComponent(WORD)}&type=order`)
  await expect(page.getByTestId('search-total')).toHaveText('Найдено документов: 0')
  await expect(page.getByText('Попробуйте сбросить фильтры')).toBeVisible()
  await page.getByRole('button', { name: 'Сбросить фильтры' }).click()
  await expect(page).not.toHaveURL(/type=/)
  await expect(page.getByTestId('search-total')).toHaveText('Найдено документов: 1')
  await page.goto(`/search?q=${encodeURIComponent(WORD)}&from=1980`)
  await expect(page.getByTestId('search-total')).toHaveText('Найдено документов: 0') // записка 1979 года

  await page.goto('/search?q=%D0%B0')
  await expect(page.getByText('слишком короткий')).toBeVisible()
  await search(page, 'и в на')
  await expect(page.getByTestId('search-total')).toContainText('слишком частых слов')
  await page.goto(`/search?q=${encodeURIComponent(WORD)}&type=nope`)
  await expect(page.getByTestId('search-bad')).toBeVisible()
})

test('поиск из каталога и по шифру документа', async ({ page }) => {
  await page.goto('/catalog')
  await page.getByRole('searchbox', { name: 'Найти в архиве' }).fill('мемо-9601')
  await page.getByRole('button', { name: 'Найти', exact: true }).click()
  await expect(page).toHaveURL(/\/search\?q=/)
  await expect(hits(page).first()).toHaveAttribute('data-code', CODES.open)
})

test('телефон 375 px и доступность: выдача с подсветкой и пустые состояния', async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 800 })
  await search(page, WORD)
  await expect(hits(page)).toHaveCount(1)
  expect(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBeLessThanOrEqual(0)

  await page.setViewportSize({ width: 1200, height: 900 })
  const axe = async (what) => {
    await settle(page)
    const r = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()
    expect(r.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(' ')).slice(0, 3).join(' | ')}`), what).toEqual([])
  }
  await axe('выдача')
  await search(page, SECRET5)
  await axe('ничего не найдено')
  await page.goto('/search')
  await axe('пустой запрос')
})
