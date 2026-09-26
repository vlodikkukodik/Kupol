// Пререндер главной и «О КУПОЛЕ» (этап 4): поисковик и предпросмотр ссылок получают готовый текст без выполнения JavaScript.
//
// После `vite build` берёт dist/index.html и пишет по странице на каждый язык интерфейса (русский — основной, итальянский):
//   • home.html / home.it.html — тексты главной и og:-теги (dist/index.html остаётся нейтральным: это запасная страница SPA для остальных адресов);
//   • about.html / about.it.html — полный текст «О КУПОЛЕ» (справка, правила, политика).
// Apache отдаёт русские страницы по «/» и /about, итальянские — с ?lang=it (public/.htaccess); в <head> — ссылки hreflang друг на друга.
// Приложение при запуске заменяет содержимое #app, так что живая страница не зависит от пререндера; пререндер только для тех, кто
// не выполняет скрипты. Текст берётся из каталогов языков (src/i18n/messages), поэтому расхождения быть не может.
// Компонент AboutContent специально не зависит от роутера и хранилищ — иначе его нельзя было бы отрендерить здесь.
// og:image/apple-touch-icon — не отсюда: это готовые картинки бренда в public/, см. scripts/gen-og-image.mjs.

import { readFileSync, writeFileSync } from 'node:fs'
import { pathToFileURL } from 'node:url'
import { resolve } from 'node:path'

const ROOT = resolve(import.meta.dirname, '..')
const DIST = resolve(ROOT, 'dist')

/** Языки пререндера: тег для <html lang> и og:locale, суффикс имени файла и параметр адреса у неосновных языков, картинка превью. */
export const PRERENDER_LANGS = [
  { code: 'ru', ogLocale: 'ru_RU', suffix: '', query: '', ogImage: 'og-image.png' },
  { code: 'it', ogLocale: 'it_IT', suffix: '.it', query: '?lang=it', ogImage: 'og-image-it.png' },
]

export const escapeHtml = (s) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;').replace(/'/g, '&#39;')

/**
 * Подставляет в HTML страницы заголовок, описание, og:-теги (в т.ч. картинку превью), структурированные данные (JSON-LD) и
 * содержимое #app. Отсутствие любого из ожидаемых мест — ошибка сборки: молча выкатить страницу без пререндера хуже, чем
 * остановить сборку.
 * lang — язык страницы (<html lang>, og:locale); alternates — те же страницы на других языках [{ lang, url }] (hreflang);
 * image — картинка превью {url, width, height}; jsonLd — объект(ы) для <script type="application/ld+json">.
 */
export function buildPage(html, { title, description, url, body, ogType = 'website', lang = 'ru', ogLocale = 'ru_RU', siteName = 'КУПОЛ', alternates = [], image, jsonLd }) {
  const need = (re, what) => {
    if (!re.test(html)) throw new Error(`prerender: в index.html нет ${what}`)
  }
  need(/<title>.*?<\/title>/s, '<title>')
  need(/<\/head>/i, '</head>')
  need(/<div id="app"><\/div>/, '<div id="app"></div>')
  need(/<html lang="[^"]*"/, '<html lang="…">')
  const t = escapeHtml(title)
  const d = escapeHtml(description)
  const u = escapeHtml(url)
  const tags = [
    `<link rel="canonical" href="${u}" />`,
    ...alternates.map((a) => `<link rel="alternate" hreflang="${escapeHtml(a.lang)}" href="${escapeHtml(a.url)}" />`),
    `<meta property="og:type" content="${ogType}" />`,
    `<meta property="og:site_name" content="${escapeHtml(siteName)}" />`,
    `<meta property="og:locale" content="${escapeHtml(ogLocale)}" />`,
    `<meta property="og:title" content="${t}" />`,
    `<meta property="og:description" content="${d}" />`,
    `<meta property="og:url" content="${u}" />`,
    ...(image
      ? [
          `<meta property="og:image" content="${escapeHtml(image.url)}" />`,
          `<meta property="og:image:width" content="${image.width}" />`,
          `<meta property="og:image:height" content="${image.height}" />`,
          '<meta name="twitter:card" content="summary_large_image" />',
          `<meta name="twitter:image" content="${escapeHtml(image.url)}" />`,
        ]
      : ['<meta name="twitter:card" content="summary" />']),
    `<meta name="twitter:title" content="${t}" />`,
    `<meta name="twitter:description" content="${d}" />`,
    ...(jsonLd ? [`<script type="application/ld+json">${JSON.stringify(jsonLd)}</script>`] : []),
  ].join('\n    ')
  // функции-замены, а не строки: в тексте могут встретиться «$1» и «$&»
  return html
    .replace(/<html lang="[^"]*"/, () => `<html lang="${escapeHtml(lang)}"`)
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
    const { i18n } = await vite.ssrLoadModule('/src/i18n/index.ts')
    const t = i18n.global.t

    // index.html остаётся нейтральным: он же — страница SPA для всех остальных адресов и основа для og.php (предпросмотр документов)
    const template = readFileSync(resolve(DIST, 'index.html'), 'utf8')
    const written = []

    for (const lang of PRERENDER_LANGS) {
      i18n.global.locale.value = lang.code
      const alternatesFor = (path) => PRERENDER_LANGS.map((l) => ({ lang: l.code, url: `${origin}${path}${l.query}` }))
      const image = { url: `${origin}/${lang.ogImage}`, width: 1200, height: 630 }
      const common = { lang: lang.code, ogLocale: lang.ogLocale, siteName: t('title.base'), image }

      // «О КУПОЛЕ»: контакты автора живые (правит Директорат) — в пререндер не попадают, страница подставит их после загрузки
      const aboutBody = await renderToString(createSSRApp(AboutContent).use(i18n))
      const aboutUrl = `${origin}/about${lang.query}`
      writeFileSync(
        resolve(DIST, `about${lang.suffix}.html`),
        buildPage(template, {
          ...common,
          title: t('about.pageTitle'),
          description: t('about.intro'),
          url: aboutUrl,
          ogType: 'article',
          alternates: alternatesFor('/about'),
          body: `<div data-prerendered class="prerender"><h1>${escapeHtml(t('about.title'))}</h1>${aboutBody}</div>`,
          jsonLd: { '@context': 'https://schema.org', '@type': 'Article', headline: t('about.title'), description: t('about.intro'), url: aboutUrl, inLanguage: lang.code, isPartOf: { '@type': 'WebSite', name: t('title.base'), url: `${origin}/` } },
        }),
      )

      // главная: Apache отдаёт home.html по «/» (public/.htaccess)
      const homeBody =
        `<div data-prerendered class="prerender"><h1>${escapeHtml(t('home.name'))}</h1><p>${escapeHtml(t('home.fullName'))}</p>` +
        `<p>${escapeHtml(t('home.lead'))}</p><p>${escapeHtml(t('home.description'))}</p>` +
        `<nav><a href="/catalog">${escapeHtml(t('home.navCatalog'))}</a> <a href="/search">${escapeHtml(t('home.navSearch'))}</a> <a href="/about${lang.query}">${escapeHtml(t('home.navAbout'))}</a></nav></div>`
      const homeUrl = `${origin}/${lang.query}`
      writeFileSync(
        resolve(DIST, `home${lang.suffix}.html`),
        buildPage(template, {
          ...common,
          title: t('title.home'),
          description: t('home.description'),
          url: homeUrl,
          alternates: alternatesFor('/'),
          body: homeBody,
          jsonLd: { '@context': 'https://schema.org', '@type': 'WebSite', name: t('home.name'), alternateName: t('title.base'), description: t('home.description'), url: `${origin}/`, inLanguage: PRERENDER_LANGS.map((l) => l.code) },
        }),
      )
      written.push(`home${lang.suffix}.html`, `about${lang.suffix}.html`)
    }

    // sitemap.xml собирается здесь же (не статикой в public/), чтобы <lastmod> был датой самой сборки, а не забытой
    // рукописной датой; на каждый адрес — сам он и ссылки hreflang на языковые варианты (см. PRERENDER_LANGS).
    const today = new Date().toISOString().slice(0, 10)
    const urlBlock = (path) =>
      PRERENDER_LANGS.map(
        (l) =>
          `  <url>\n    <loc>${escapeHtml(`${origin}${path}${l.query}`)}</loc>\n    <lastmod>${today}</lastmod>\n` +
          PRERENDER_LANGS.map((a) => `    <xhtml:link rel="alternate" hreflang="${a.code}" href="${escapeHtml(`${origin}${path}${a.query}`)}" />`).join('\n') +
          `\n  </url>`,
      ).join('\n')
    writeFileSync(
      resolve(DIST, 'sitemap.xml'),
      `<?xml version="1.0" encoding="UTF-8"?>\n<!-- Индексируются только главная и «О КУПОЛЕ» (обе версии языка); остальное закрыто заголовком X-Robots-Tag (public/.htaccess). -->\n` +
        `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9" xmlns:xhtml="http://www.w3.org/1999/xhtml">\n${urlBlock('/')}\n${urlBlock('/about')}\n</urlset>\n`,
    )
    written.push('sitemap.xml')

    console.log(`prerender: ${written.join(', ')} (${origin})`)
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
