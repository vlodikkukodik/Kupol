// Пререндер главной и «О КУПОЛЕ» (этап 4): поисковик и предпросмотр ссылок получают готовый текст без выполнения JavaScript.
//
// После `vite build` берёт dist/index.html и:
//   • кладёт в него тексты главной и og:-теги (dist/index.html — это и есть главная, и запасная страница SPA для остальных адресов);
//   • пишет dist/about.html с полным текстом «О КУПОЛЕ» (справка, правила, политика) — Apache отдаёт его по /about (public/.htaccess).
// Приложение при запуске заменяет содержимое #app, так что живая страница не зависит от пререндера; пререндер только для тех, кто
// не выполняет скрипты. Текст берётся из тех же файлов, что и страницы (src/content), поэтому расхождения быть не может.
// Компонент AboutContent специально не зависит от роутера и хранилищ — иначе его нельзя было бы отрендерить здесь.

import { readFileSync, writeFileSync } from 'node:fs'
import { pathToFileURL } from 'node:url'
import { resolve } from 'node:path'

const ROOT = resolve(import.meta.dirname, '..')
const DIST = resolve(ROOT, 'dist')

export const escapeHtml = (s) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;').replace(/'/g, '&#39;')

/**
 * Подставляет в HTML страницы заголовок, описание, og:-теги и содержимое #app. Отсутствие любого из ожидаемых мест — ошибка сборки:
 * молча выкатить страницу без пререндера хуже, чем остановить сборку.
 */
export function buildPage(html, { title, description, url, body, ogType = 'website' }) {
  const need = (re, what) => {
    if (!re.test(html)) throw new Error(`prerender: в index.html нет ${what}`)
  }
  need(/<title>.*?<\/title>/s, '<title>')
  need(/<\/head>/i, '</head>')
  need(/<div id="app"><\/div>/, '<div id="app"></div>')
  const t = escapeHtml(title)
  const d = escapeHtml(description)
  const u = escapeHtml(url)
  const tags = [
    `<link rel="canonical" href="${u}" />`,
    `<meta property="og:type" content="${ogType}" />`,
    '<meta property="og:site_name" content="КУПОЛ" />',
    '<meta property="og:locale" content="ru_RU" />',
    `<meta property="og:title" content="${t}" />`,
    `<meta property="og:description" content="${d}" />`,
    `<meta property="og:url" content="${u}" />`,
    '<meta name="twitter:card" content="summary" />',
    `<meta name="twitter:title" content="${t}" />`,
    `<meta name="twitter:description" content="${d}" />`,
  ].join('\n    ')
  // функции-замены, а не строки: в тексте могут встретиться «$1» и «$&»
  return html
    .replace(/<title>.*?<\/title>/s, () => `<title>${t}</title>`)
    .replace(/<meta\s+name="description"[^>]*>\s*/i, () => `<meta name="description" content="${d}" />\n    `)
    .replace(/<\/head>/i, () => `    ${tags}\n  </head>`)
    // маркеры вокруг содержимого: og.php убирает его, когда строит предпросмотр документа на основе index.html
    .replace('<div id="app"></div>', () => `<div id="app"><!--prerender-->${body}<!--/prerender--></div>`)
}

async function main() {
  const { createServer } = await import('vite')
  const { createSSRApp } = await import('vue')
  const { renderToString } = await import('vue/server-renderer')
  const origin = (process.env.SITE_URL || process.env.VITE_SITE_URL || 'https://kupol.vladinc.ru').replace(/\/+$/, '')

  const vite = await createServer({ root: ROOT, appType: 'custom', logLevel: 'error', server: { middlewareMode: true }, optimizeDeps: { noDiscovery: true } })
  try {
    const { default: AboutContent } = await vite.ssrLoadModule('/src/components/AboutContent.vue')
    const about = await vite.ssrLoadModule('/src/content/about.ts')
    const site = await vite.ssrLoadModule('/src/content/site.ts')

    // index.html остаётся нейтральным: он же — страница SPA для всех остальных адресов и основа для og.php (предпросмотр документов)
    const template = readFileSync(resolve(DIST, 'index.html'), 'utf8')

    // «О КУПОЛЕ»: контакты автора живые (правит Директорат) — в пререндер не попадают, страница подставит их после загрузки
    const aboutBody = await renderToString(createSSRApp(AboutContent))
    const aboutTitle = 'О КУПОЛЕ — вымышленный архив КУПОЛ'
    writeFileSync(
      resolve(DIST, 'about.html'),
      buildPage(template, {
        title: aboutTitle,
        description: about.ABOUT_INTRO,
        url: `${origin}/about`,
        ogType: 'article',
        body: `<div data-prerendered class="prerender"><h1>О КУПОЛЕ</h1>${aboutBody}</div>`,
      }),
    )

    // главная: Apache отдаёт home.html по «/» (public/.htaccess)
    const homeBody =
      `<div data-prerendered class="prerender"><h1>${escapeHtml(site.HOME_NAME)}</h1><p>${escapeHtml(site.HOME_FULL_NAME)}</p>` +
      `<p>${escapeHtml(site.HOME_LEAD)}</p><p>${escapeHtml(site.SITE_DESCRIPTION)}</p>` +
      '<nav><a href="/catalog">Каталог</a> <a href="/search">Поиск</a> <a href="/about">О КУПОЛЕ</a></nav></div>'
    writeFileSync(
      resolve(DIST, 'home.html'),
      buildPage(template, { title: 'КУПОЛ — Центральный архив', description: site.SITE_DESCRIPTION, url: `${origin}/`, body: homeBody }),
    )
    console.log(`prerender: home.html и about.html (${origin})`)
  } finally {
    await vite.close()
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main().catch((err) => {
    console.error(err)
    process.exit(1)
  })
}
