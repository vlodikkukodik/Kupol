import { expect, test } from '@playwright/test'
import { canWrite, signUp } from './helpers/kupol.js'

// Документы-фикстуры (e2e/fixtures/pack.json) загружает global-setup командой `kupol doc import` и удаляет после прогона.
// Против внешнего адреса без KUPOL_E2E_ALLOW_WRITES их нет — сценарии не идут.
test.skip(!canWrite, 'нужны фикстуры и пользователи: против внешнего адреса не запускается')

// Метки в фикстуре: число в конце — допуск, с которого текст становится виден.
const MARKERS = [
  { text: 'СЕКРЕТ-ФРАГМЕНТ-УР3', level: 3 },
  { text: 'СЕКРЕТ-ЯЧЕЙКА-УР3', level: 3 },
  { text: 'СЕКРЕТ-ПУНКТ-УР4', level: 4 },
  { text: 'СЕКРЕТ-ЖУРНАЛ-УР2', level: 2 },
  { text: 'СЕКРЕТ-БЛОК-УР4', level: 4 },
  { text: 'СЕКРЕТ-БЛОК-УР4-ВТОРОЙ', level: 4 },
  { text: 'СЕКРЕТ-БЛОК-УР6', level: 6 },
  // цель ссылки О-9003 (допуск 5): её название и примечание автора
  { text: 'СЕКРЕТ-НАЗВАНИЕ-УР5', level: 5 },
  { text: 'СЕКРЕТ-ПРИМЕЧАНИЕ-ССЫЛКИ', level: 5 },
]

// Что должен увидеть читатель с допуском L на странице О-9001.
const fragmentLevels = [3, 4, 3] // зачернённые фрагменты внутри абзаца, списка, таблицы
const plateLevels = [2, 4, 6] // закрытые блоки (два подряд блока уровня 4 сливаются в одну плашку)
const closedLinkLevels = [2, 5] // ссылки на О-9002 (допуск 2) и О-9003 (допуск 5)

const body = (page) => page.locator('article.dossier')

async function openDossier(page, ref = 'O-9001') {
  await page.goto(`/doc/${ref}`)
  await expect(body(page)).toBeVisible()
  return body(page)
}

/** Проверка страницы О-9001 «глазами» читателя с допуском level: и в ответе API, и на экране. */
async function expectDossierFor(page, level) {
  const raw = await (await page.request.get('/api/documents/O-9001')).text()
  const article = await openDossier(page)
  const shown = await article.textContent()

  for (const m of MARKERS) {
    const visible = level >= m.level
    if (visible) {
      expect(raw, `в ответе API должна быть метка ${m.text} (допуск ${level})`).toContain(m.text)
      expect(shown, `на экране должна быть метка ${m.text} (допуск ${level})`).toContain(m.text)
    } else {
      expect(raw, `в ответе API не должно быть метки ${m.text} (допуск ${level})`).not.toContain(m.text)
      expect(shown, `на экране не должно быть метки ${m.text} (допуск ${level})`).not.toContain(m.text)
    }
  }
  await expect(article.locator('.redacted')).toHaveCount(fragmentLevels.filter((l) => l > level).length)
  await expect(article.locator('.plate')).toHaveCount(plateLevels.filter((l) => l > level).length)
  await expect(article.locator('.link--closed')).toHaveCount(closedLinkLevels.filter((l) => l > level).length)
}

test.describe('гость (уровень 0)', () => {
  test('открытая часть дела читается: шапка досье и все типы блоков', async ({ page }) => {
    const article = await openDossier(page)
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('[e2e] Открытый объект со всеми блоками')
    await expect(article.getByTestId('dossier-header')).toContainText('О-9001')
    await expect(article.getByTestId('dossier-header')).toContainText('Класс опасности')
    await expect(article.getByTestId('dossier-header')).toContainText('Череповцом')
    await expect(article.getByTestId('dossier-header')).toContainText('14 марта 1979')

    const shown = await article.textContent()
    for (const text of [
      'Общие сведения', // heading
      'Открытый жирный фрагмент', // paragraph, bold
      'Первый признак', // list
      'Оформить акт', // ordered list
      'Оно не приближается', // quote
      'Совершенно секретно', // stamp
      'О порядке содержания', // memo
      'Вечерний Череповец', // clipping, газета
      'Что вы видели?', // clipping, расшифровка записи
      'Замеры', // table
      'Сноска для проверки вёрстки.', // footnote
      'Схема расположения', // appendix
      'Заключительный открытый абзац.',
    ]) {
      expect(shown, text).toContain(text)
    }
    await expect(article.locator('strong', { hasText: 'Открытый жирный фрагмент' })).toBeVisible()
    await expect(article.locator('em', { hasText: 'курсив' })).toBeVisible()
    await expect(article.locator('ol li')).toHaveCount(2)
  })

  test('закрытое заменено плашками и чёрными полосами, ни метки, ни ответа API с ними нет', async ({ page }) => {
    await expectDossierFor(page, 0)
    const article = body(page)
    // плашка называет только нужный допуск
    await expect(article.locator('.plate[data-level="2"]')).toContainText('допуск не ниже уровня 2 (Стажёр)')
    await expect(article.locator('.plate[data-level="4"]')).toHaveCount(1) // два блока подряд — одна плашка
    await expect(article.locator('.plate[data-level="6"]')).toContainText('Особый Совет')
    await expect(article.locator('.redacted[data-level="3"]')).toHaveCount(2)
    // «Данные удалены» видно только у плашек
    await expect(article.getByText('Данные удалены')).toHaveCount(3)
  })

  test('ссылки: доступный документ — ссылка, закрытые — «Засекречен» без шифра и названия', async ({ page }) => {
    const article = await openDossier(page)
    await expect(article.locator('.link--closed')).toHaveCount(2)
    await expect(article.locator('.link--closed').first()).toContainText('Засекречен')
    const shown = await article.textContent()
    expect(shown).not.toContain('О-9002')
    expect(shown).not.toContain('О-9003')
    expect(shown).not.toContain('Смежный объект')

    await article.getByRole('link', { name: /ПРИКАЗ-1901-91/ }).click()
    await expect(page).toHaveURL(/\/doc\/PRIKAZ-1901-91$/)
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('[e2e] Приказ об отчётности')
    // после перехода фокус — на названии документа
    await expect(page.locator('[data-doc-title]')).toBeFocused()
    await expect(page).toHaveTitle(/ПРИКАЗ-1901-91 — \[e2e\] Приказ об отчётности — КУПОЛ/)
  })

  test('любая запись шифра ведёт к одному адресу', async ({ page }) => {
    for (const ref of ['О-9001', 'о-9001', 'O-9001', 'o-9001', encodeURIComponent('О-9001')]) {
      await page.goto(`/doc/${ref}`)
      await expect(page.getByRole('heading', { level: 1 })).toHaveText('[e2e] Открытый объект со всеми блоками')
      await expect(page).toHaveURL(/\/doc\/O-9001$/)
    }
    await page.goto('/doc/ПРИКАЗ-1901-91')
    await expect(page).toHaveURL(/\/doc\/PRIKAZ-1901-91$/)
  })

  test('выше допуска: 404 или «Доступ запрещён» — по режиму документа; черновик и несуществующий — 404', async ({ page }) => {
    // О-9002: допуск 2, режим forbidden — гость видит, какой допуск нужен
    await page.goto('/doc/O-9002')
    await expect(page.getByTestId('access-denied')).toContainText('допуск не ниже уровня 2 (Стажёр)')
    await expect(page.getByTestId('access-denied')).toContainText('Вы не вошли в архив')
    await expect(page).toHaveTitle('Доступ запрещён — КУПОЛ')
    expect(await page.locator('body').textContent()).not.toContain('Объект допуска 2') // названия нет

    // О-9007: только Директорат
    await page.goto('/doc/O-9007')
    await expect(page.getByTestId('access-denied')).toContainText('только Директорат')
    expect(await page.locator('body').textContent()).not.toContain('СЕКРЕТ-ДИРЕКТОРАТ')

    // О-9003: допуск 5, режим not_found — существование не раскрывается
    for (const ref of ['O-9003', 'O-9004', 'O-9999', 'нет-такого-шифра']) {
      await page.goto(`/doc/${ref}`)
      await expect(page.getByRole('heading', { name: 'Дело не найдено' })).toBeVisible()
      await expect(page).toHaveTitle('Дело не найдено — КУПОЛ')
      await expect(page.getByTestId('access-denied')).toHaveCount(0)
      expect(await page.locator('body').textContent()).not.toContain('СЕКРЕТ-')
    }
  })

  test('API: коды ответов и форма ошибок, в теле нет закрытого', async ({ request }) => {
    const denied = await request.get('/api/documents/O-9002')
    expect(denied.status()).toBe(403)
    const err = (await denied.json()).error
    expect(err.code).toBe('access_denied')
    expect(err.required_level).toBe(2)
    expect(JSON.stringify(err)).not.toContain('Объект допуска 2')

    for (const ref of ['O-9003', 'O-9004', 'O-9999']) {
      const res = await request.get(`/api/documents/${ref}`)
      expect(res.status(), ref).toBe(404)
      const text = await res.text()
      expect(text).not.toContain('СЕКРЕТ-')
      expect(JSON.parse(text).error.code).toBe('not_found')
    }
    expect(JSON.parse(await (await request.get('/api/documents/O-9007')).text()).error.required_level).toBe(7)
  })
})

test.describe('уровни допуска', () => {
  // Один и тот же документ для каждого уровня: чем выше допуск, тем меньше закрыто.
  for (const level of [1, 2, 3, 4, 5, 6]) {
    test(`пользователь уровня ${level}`, async ({ browser }) => {
      const context = await browser.newContext()
      const page = await context.newPage()
      await signUp(context, { level })
      await expectDossierFor(page, level)
      await context.close()
    })
  }

  test('уровень 5 открывает дело О-9003 и видит его в ссылке; для уровня 4 — 404', async ({ browser }) => {
    const four = await browser.newContext()
    await signUp(four, { level: 4 })
    const p4 = await four.newPage()
    await p4.goto('/doc/O-9003')
    await expect(p4.getByRole('heading', { name: 'Дело не найдено' })).toBeVisible()
    await four.close()

    const five = await browser.newContext()
    await signUp(five, { level: 5 })
    const p5 = await five.newPage()
    await p5.goto('/doc/O-9003')
    await expect(p5.getByRole('heading', { level: 1 })).toHaveText('[e2e] СЕКРЕТ-НАЗВАНИЕ-УР5')
    await five.close()
  })

  test('уровень 2 открывает О-9002, а «только Директорат» остаётся закрытым и для уровня 6', async ({ browser }) => {
    const two = await browser.newContext()
    await signUp(two, { level: 2 })
    const p2 = await two.newPage()
    await p2.goto('/doc/O-9002')
    await expect(p2.getByRole('heading', { level: 1 })).toHaveText('[e2e] Объект допуска 2')
    await two.close()

    const six = await browser.newContext()
    await signUp(six, { level: 6 })
    const p6 = await six.newPage()
    await p6.goto('/doc/O-9007')
    await expect(p6.getByTestId('access-denied')).toContainText('только Директорат')
    await expect(p6.getByTestId('access-denied')).toContainText('Ваш допуск: уровень 6 (Особый Совет)')
    await six.close()
  })

  test('Директорат видит всё: без плашек, черновики и уровень 7', async ({ browser }) => {
    const context = await browser.newContext()
    const page = await context.newPage()
    await signUp(context, { level: 1, directorate: true })
    await expectDossierFor(page, 7)

    await page.goto('/doc/O-9007')
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('[e2e] СЕКРЕТ-ДИРЕКТОРАТ')
    await page.goto('/doc/O-9004')
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('[e2e] СЕКРЕТ-ЧЕРНОВИК')
    await context.close()
  })

  test('выход из аккаунта возвращает закрытое: тот же адрес снова с плашками', async ({ browser }) => {
    const context = await browser.newContext()
    const page = await context.newPage()
    await signUp(context, { level: 6 })
    await expectDossierFor(page, 6)
    await page.goto('/file')
    await page.getByRole('button', { name: 'Выйти' }).click()
    await expect(page).toHaveURL(/\/$/)
    await expectDossierFor(page, 0)
    await context.close()
  })
})
