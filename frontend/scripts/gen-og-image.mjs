// Картинки для превью ссылок (og:image / twitter:image) и apple-touch-icon: рисуются один раз этим скриптом
// (npm run gen:og-image) и коммитятся как обычные статические файлы в public/ — как favicon.svg. Файлы не собираются
// заново при каждом деплое: это готовые картинки бренда, а не производные от контента.
//
// Рисует HTML/SVG теми же красками и шрифтами, что и сам сайт (см. src/styles/tokens.css, UiSeal.vue), и снимает
// с него скриншот headless-браузером (Chromium уже используется для сквозных тестов — отдельного инструмента не нужно).
// Шрифты подключены из локальных файлов (@fontsource): сеть при генерации не нужна, результат воспроизводим.

import { chromium } from '@playwright/test'
import { mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { resolve, join } from 'node:path'
import { pathToFileURL } from 'node:url'

const ROOT = resolve(import.meta.dirname, '..')
const PUBLIC = resolve(ROOT, 'public')
const FONTS = resolve(ROOT, 'node_modules/@fontsource')
const font = (pkg, file) => pathToFileURL(resolve(FONTS, pkg, 'files', file)).href

const FACES = `
  @font-face { font-family: 'Oswald'; font-weight: 700; src: url('${font('oswald', 'oswald-cyrillic-700-normal.woff2')}') format('woff2'); unicode-range: U+0400-04FF; }
  @font-face { font-family: 'Oswald'; font-weight: 700; src: url('${font('oswald', 'oswald-latin-700-normal.woff2')}') format('woff2'); }
  @font-face { font-family: 'PT Sans'; font-weight: 400; src: url('${font('pt-sans', 'pt-sans-cyrillic-400-normal.woff2')}') format('woff2'); unicode-range: U+0400-04FF; }
  @font-face { font-family: 'PT Sans'; font-weight: 400; src: url('${font('pt-sans', 'pt-sans-latin-400-normal.woff2')}') format('woff2'); }
`

// Печать КУПОЛ — та же разметка, что в src/ui/UiSeal.vue (круг, надпись по окружности, купол, год основания).
const seal = (ringText) => `
  <svg class="seal" viewBox="0 0 200 200" focusable="false">
    <defs><path id="ring" d="M 100,100 m -76,0 a 76,76 0 1,1 152,0 a 76,76 0 1,1 -152,0" /></defs>
    <circle cx="100" cy="100" r="96" class="line" fill="none" stroke-width="3" />
    <circle cx="100" cy="100" r="90" class="line" fill="none" stroke-width="1" />
    <circle cx="100" cy="100" r="58" class="line" fill="none" stroke-width="2" />
    <text class="ring-text" font-size="13"><textPath href="#ring" startOffset="0" textLength="470" lengthAdjust="spacing">${ringText}</textPath></text>
    <path d="M 62,112 A 38,38 0 0 1 138,112 Z" class="fill" />
    <line x1="56" y1="112" x2="144" y2="112" class="line" stroke-width="3" />
    <line x1="100" y1="74" x2="100" y2="62" class="line" stroke-width="3" />
    <circle cx="100" cy="60" r="3.5" class="fill" />
    <text x="100" y="140" text-anchor="middle" class="year" font-size="20">1974</text>
  </svg>
`

const CARD_CSS = `
  ${FACES}
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body {
    width: 1200px; height: 630px; overflow: hidden; position: relative;
    background: radial-gradient(120% 140% at 20% 15%, #1b2421 0%, #0e1412 60%, #0a0e0c 100%);
    font-family: 'PT Sans', sans-serif;
  }
  .card {
    position: absolute; inset: 48px;
    background: #fdfbf5;
    border: 2px solid #101213;
    box-shadow: 0 24px 60px rgb(0 0 0 / 0.45);
    display: flex; align-items: center; gap: 56px; padding: 0 72px;
  }
  .seal { width: 240px; height: 240px; color: #8f1c17; flex-shrink: 0; }
  .seal .line { stroke: currentColor; }
  .seal .fill { fill: currentColor; }
  .seal .ring-text, .seal .year { fill: currentColor; font-family: 'Oswald', sans-serif; font-weight: 700; letter-spacing: 0.05em; }
  h1 {
    font-family: 'Oswald', sans-serif; font-weight: 700; text-transform: uppercase; letter-spacing: 0.06em;
    font-size: 92px; line-height: 1; color: #191c1e; margin-bottom: 22px;
  }
  p { font-size: 27px; line-height: 1.4; color: #52585b; max-width: 620px; }
  .stamp {
    position: absolute; right: 56px; bottom: 40px;
    font-family: 'Oswald', sans-serif; font-weight: 700; text-transform: uppercase; letter-spacing: 0.08em;
    font-size: 20px; color: #a4231d; border: 3px solid #a4231d; padding: 6px 16px; border-radius: 3px;
    transform: rotate(-4deg); opacity: 0.85;
  }
`

const CARD_HTML = (ringText, title, subtitle, stampText) => `<!doctype html><html><head><meta charset="utf-8" /><style>${CARD_CSS}</style></head>
<body>
  <div class="card">
    ${seal(ringText)}
    <div><h1>${title}</h1><p>${subtitle}</p></div>
  </div>
  <div class="stamp">${stampText}</div>
</body></html>`

const ICON_HTML = `<!doctype html><html><head><meta charset="utf-8" /><style>
  ${FACES}
  * { margin: 0; padding: 0; }
  body { width: 180px; height: 180px; background: #fdfbf5; display: flex; align-items: center; justify-content: center; }
  .seal { width: 156px; height: 156px; color: #8f1c17; }
  .seal .line { stroke: currentColor; } .seal .fill { fill: currentColor; }
  .seal .ring-text, .seal .year { fill: currentColor; font-family: 'Oswald', sans-serif; font-weight: 700; letter-spacing: 0.05em; }
</style></head><body>${seal('КОМИТЕТ УПРАВЛЕНИЯ ПАРАНОРМАЛЬНЫМИ ОБЪЕКТАМИ И ЛОКАЦИЯМИ')}</body></html>`

async function shoot(browser, html, file, width, height) {
  const dir = mkdtempSync(join(tmpdir(), 'kupol-og-'))
  const path = join(dir, 'page.html')
  writeFileSync(path, html)
  const page = await browser.newPage({ viewport: { width, height } })
  await page.goto(pathToFileURL(path).href)
  await page.waitForTimeout(50) // дать шрифтам примениться после goto
  await page.screenshot({ path: resolve(PUBLIC, file) })
  await page.close()
  rmSync(dir, { recursive: true, force: true })
}

async function main() {
  const browser = await chromium.launch()
  try {
    await shoot(
      browser,
      CARD_HTML(
        'КОМИТЕТ УПРАВЛЕНИЯ ПАРАНОРМАЛЬНЫМИ ОБЪЕКТАМИ И ЛОКАЦИЯМИ',
        'КУПОЛ',
        'Центральный архив. Вымышленный архив Комитета Управления Паранормальными Объектами и Локациями.',
        'Форма КУПОЛ-1',
      ),
      'og-image.png',
      1200,
      630,
    )
    await shoot(
      browser,
      CARD_HTML(
        'КОМИТЕТ УПРАВЛЕНИЯ ПАРАНОРМАЛЬНЫМИ ОБЪЕКТАМИ И ЛОКАЦИЯМИ',
        'KUPOL',
        "Archivio centrale. L'archivio immaginario del Comitato per la Gestione degli Oggetti e dei Luoghi Paranormali.",
        'Modulo KUPOL-1',
      ),
      'og-image-it.png',
      1200,
      630,
    )
    await shoot(browser, ICON_HTML, 'apple-touch-icon.png', 180, 180)
    console.log('og-image.png, og-image-it.png, apple-touch-icon.png -> public/')
  } finally {
    await browser.close()
  }
}

main().catch((err) => {
  console.error(err)
  process.exit(1)
})
