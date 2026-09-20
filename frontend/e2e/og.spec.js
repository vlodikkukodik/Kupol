import { expect, test } from '@playwright/test'
import { canWrite } from './helpers/kupol.js'

// Предпросмотр ссылок в мессенджерах (этап 4): og:-теги для ботов, обычное SPA для людей, одинаковый ответ на закрытое и несуществующее.
// Правило переписывания (боты → og.php) живёт в public/.htaccess: только Apache-прогон (scripts/e2e-apache.sh) его проверяет.
test.skip(!process.env.KUPOL_E2E_APACHE, 'нужен Apache с .htaccess: запускается в scripts/e2e-apache.sh')
test.skip(!canWrite, 'нужны фикстуры документов')

const BOT = { 'User-Agent': 'TelegramBot (like TwitterBot)' }
const PERSON = { 'User-Agent': 'Mozilla/5.0 (X11; Linux x86_64) Chrome/120 Safari/537.36' }

const meta = (html, prop) => new RegExp(`<meta (?:property|name)="${prop}" content="([^"]*)"`).exec(html)?.[1]

test('бот получает og:-теги открытого документа — из открытого текста, без закрытого', async ({ request, baseURL }) => {
  const res = await request.get('/doc/O-9001', { headers: BOT })
  expect(res.status()).toBe(200)
  expect(res.headers()['content-type']).toContain('text/html')
  const html = await res.text()
  expect(meta(html, 'og:title')).toBe('О-9001 — [e2e] Открытый объект со всеми блоками — КУПОЛ')
  expect(meta(html, 'og:type')).toBe('article')
  expect(meta(html, 'og:site_name')).toBe('КУПОЛ')
  expect(meta(html, 'og:url')).toBe(`${new URL(baseURL).origin}/doc/O-9001`)
  expect(meta(html, 'og:description')).toBeTruthy()
  expect(meta(html, 'description')).toBe(meta(html, 'og:description'))
  expect(meta(html, 'twitter:card')).toBe('summary')
  expect(html).toContain(`<link rel="canonical" href="${new URL(baseURL).origin}/doc/O-9001" />`)
  expect(html).toContain('<title>О-9001 — [e2e] Открытый объект со всеми блоками — КУПОЛ</title>')
  // страница остаётся страницей приложения: скрипт и корень на месте
  expect(html).toContain('<div id="app">')
  expect(html).toMatch(/<script type="module"[^>]*src="\/assets\//)
  // закрытых слов в разметке нет вовсе
  expect(html).not.toContain('СЕКРЕТ')
  expect(res.headers()['x-robots-tag']).toContain('noindex') // документы не индексируются (только главная и «О КУПОЛЕ»)
})

test('человек получает обычное SPA без og:-тегов; шифр в любой раскладке', async ({ request }) => {
  const human = await (await request.get('/doc/O-9001', { headers: PERSON })).text()
  expect(human).not.toContain('og:title')
  expect(human).toContain('<title>КУПОЛ</title>')
  const bot = await (await request.get('/doc/o-9001', { headers: BOT })).text()
  expect(meta(bot, 'og:title')).toBe('О-9001 — [e2e] Открытый объект со всеми блоками — КУПОЛ')
})

test('закрытый, черновик и несуществующий документы: боту — тот же index.html, что и человеку; существование не раскрывается', async ({ request }) => {
  // «тот же index.html» — нейтральная страница SPA (главная отдаётся отдельным пререндеренным home.html)
  const plain = await (await request.get('/index.html', { headers: PERSON })).text()
  for (const ref of ['O-9003', 'O-9007', 'O-9004', 'O-9999', 'MEMO-1', 'мусор', 'a%20b']) {
    const res = await request.get(`/doc/${ref}`, { headers: BOT })
    expect(res.status(), ref).toBe(200)
    const html = await res.text()
    expect(html, ref).toBe(plain)
    expect(html).not.toContain('og:title')
    expect(html).not.toContain('СЕКРЕТ')
  }
})

test('в описании нет закрытого даже у документа с закрытыми фрагментами; особые знаки экранируются', async ({ request }) => {
  const html = await (await request.get('/doc/O-9001', { headers: BOT })).text()
  for (const secret of ['СЕКРЕТ-ФРАГМЕНТ-УР3', 'СЕКРЕТ-ПУНКТ-УР4', 'СЕКРЕТ-ЖУРНАЛ-УР2']) expect(html).not.toContain(secret)
  // ни одного неэкранированного значения в атрибутах: кавычка внутри content закрыла бы атрибут
  for (const m of html.matchAll(/<meta (?:property|name)="(?:og|twitter):[a-z_:]+" content="([^"]*)"/g)) expect(m[1]).not.toMatch(/[<>]/)
})

test('пререндер: главная и «О КУПОЛЕ» читаются без JavaScript; с JavaScript приложение заменяет их живой страницей', async ({ browser }) => {
  const plain = await browser.newContext({ javaScriptEnabled: false })
  const page = await plain.newPage()
  await page.goto('/about')
  await expect(page.getByRole('heading', { level: 1, name: 'О КУПОЛЕ' })).toBeVisible()
  await expect(page.getByRole('heading', { level: 2, name: 'Политика конфиденциальности' })).toBeVisible()
  await expect(page.locator('body')).toContainText('kupol_session')
  await expect(page.locator('head meta[property="og:title"]')).toHaveAttribute('content', 'О КУПОЛЕ — вымышленный архив КУПОЛ')
  await page.goto('/')
  await expect(page.getByRole('heading', { level: 1, name: 'Купол' })).toBeVisible()
  await expect(page.locator('body')).toContainText('Центральном архиве Купола')
  await plain.close()

  // с JavaScript: живая страница (шапка, подвал), прежнего текста-заготовки в документе нет
  const live = await browser.newContext()
  const p2 = await live.newPage()
  await p2.goto('/about')
  await expect(p2.getByRole('banner')).toBeVisible()
  await expect(p2.getByTestId('timeline')).toBeVisible()
  await expect(p2.locator('[data-prerendered]')).toHaveCount(0)
  await p2.goto('/')
  await expect(p2.getByRole('banner')).toBeVisible()
  await expect(p2.locator('[data-prerendered]')).toHaveCount(0)
  await live.close()
})

test('запросы к og.php напрямую и странные адреса не ломаются', async ({ request }) => {
  for (const url of ['/og.php', '/og.php?ref=', '/og.php?ref=../../etc/passwd', '/og.php?ref=' + 'a'.repeat(200)]) {
    const res = await request.get(url, { headers: BOT })
    expect(res.status(), url).toBe(200)
    expect(await res.text(), url).toContain('<div id="app">')
  }
})
