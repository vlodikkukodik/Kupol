// @vitest-environment node
import { afterEach, describe, expect, it } from 'vitest'
import { createSSRApp } from 'vue'
import { renderToString } from 'vue/server-renderer'
import AboutContent from '@/components/AboutContent.vue'
import { aboutSections, levelRows, privacyItems } from '@/content/about'
import { i18n, t } from '@/i18n'
import { PRERENDER_LANGS, buildPage, escapeHtml } from '../scripts/prerender.mjs'

const TEMPLATE = `<!doctype html>
<html lang="ru">
  <head>
    <meta charset="UTF-8" />
    <title>КУПОЛ</title>
    <meta name="description" content="старое описание" />
  </head>
  <body>
    <div id="app"></div>
    <script type="module" src="/assets/index.js"></script>
  </body>
</html>`

const meta = { title: 'Страница', description: 'Описание', url: 'https://x.example/page', body: '<p>Текст</p>' }

describe('buildPage', () => {
  it('подставляет заголовок, описание, og:-теги и содержимое #app; остальное не трогает', () => {
    const out = buildPage(TEMPLATE, meta)
    expect(out).toContain('<title>Страница</title>')
    expect(out.match(/name="description"/g)).toHaveLength(1)
    expect(out).not.toContain('старое описание')
    expect(out).toContain('<meta property="og:title" content="Страница" />')
    expect(out).toContain('<meta property="og:url" content="https://x.example/page" />')
    expect(out).toContain('<link rel="canonical" href="https://x.example/page" />')
    expect(out).toContain('<meta name="twitter:card" content="summary" />')
    expect(out).toContain('<meta property="og:locale" content="ru_RU" />')
    expect(out).toContain('<html lang="ru">')
    expect(out).toContain('<div id="app"><!--prerender--><p>Текст</p><!--/prerender--></div>')
    expect(out).toContain('<script type="module" src="/assets/index.js"></script>')
    expect(out.indexOf('og:title')).toBeLessThan(out.indexOf('</head>'))
  })

  it('итальянская страница: lang, og:locale и ссылки hreflang на обе версии', () => {
    const out = buildPage(TEMPLATE, {
      ...meta,
      lang: 'it',
      ogLocale: 'it_IT',
      siteName: 'KUPOL',
      alternates: [
        { lang: 'ru', url: 'https://x.example/about' },
        { lang: 'it', url: 'https://x.example/about?lang=it' },
      ],
    })
    expect(out).toContain('<html lang="it">')
    expect(out).toContain('<meta property="og:locale" content="it_IT" />')
    expect(out).toContain('<meta property="og:site_name" content="KUPOL" />')
    expect(out).toContain('<link rel="alternate" hreflang="ru" href="https://x.example/about" />')
    expect(out).toContain('<link rel="alternate" hreflang="it" href="https://x.example/about?lang=it" />')
  })

  it('экранирует значения и не принимает «$1», «$&» за ссылки на группы замены', () => {
    const out = buildPage(TEMPLATE, { ...meta, title: 'А "б" <в> & $1 $& \\0', description: 'Описание "с кавычкой" $&' })
    expect(out).toContain('<title>А &quot;б&quot; &lt;в&gt; &amp; $1 $&amp; \\0</title>')
    expect(out).toContain('content="Описание &quot;с кавычкой&quot; $&amp;"')
    expect(out).not.toMatch(/content="[^"]*[<>][^"]*"/)
  })

  it('нет ожидаемого места в шаблоне — ошибка сборки, а не молчаливая страница без пререндера', () => {
    expect(() => buildPage(TEMPLATE.replace('<div id="app"></div>', '<div id="root"></div>'), meta)).toThrow(/#app|id="app"/)
    expect(() => buildPage(TEMPLATE.replace(/<title>.*<\/title>/, ''), meta)).toThrow(/<title>/)
    expect(() => buildPage(TEMPLATE.replace('</head>', ''), meta)).toThrow(/<\/head>/)
    expect(() => buildPage(TEMPLATE.replace('<html lang="ru">', '<html>'), meta)).toThrow(/html lang/)
  })

  it('escapeHtml', () => {
    expect(escapeHtml(`<a href="x">'&'</a>`)).toBe('&lt;a href=&quot;x&quot;&gt;&#39;&amp;&#39;&lt;/a&gt;')
  })

  it('языки пререндера: русский без суффикса, итальянский — home.it.html и ?lang=it', () => {
    expect(PRERENDER_LANGS.map((l) => [l.code, l.suffix, l.query])).toEqual([
      ['ru', '', ''],
      ['it', '.it', '?lang=it'],
    ])
  })
})

describe('AboutContent на сервере', () => {
  afterEach(() => {
    i18n.global.locale.value = 'ru'
  })

  for (const lang of ['ru', 'it'] as const) {
    it(`[${lang}] рендерится без браузера, роутера и хранилищ: весь текст страницы есть в HTML`, async () => {
      i18n.global.locale.value = lang
      const html = await renderToString(createSSRApp(AboutContent, { contact: 'автор@example.org' }).use(i18n))
      const plain = (s: string) => escapeHtml(s) // Vue экранирует текст теми же пятью знаками
      expect(html).toContain(plain(t('about.intro')))
      for (const s of aboutSections()) {
        expect(html).toContain(`id="${s.id}"`)
        expect(html).toContain(plain(s.title))
        for (const p of s.paragraphs) expect(html).toContain(plain(p))
      }
      for (const r of levelRows()) expect(html).toContain(plain(r.name))
      for (const p of privacyItems()) expect(html).toContain(plain(p.term))
      expect(html).toContain('автор@example.org')
      expect(html).toContain('href="#privacy"')
      // ни один ключ не «протёк» на страницу вместо текста
      expect(html).not.toMatch(/about\.(sections|levels|privacy|author)\./)
    })
  }

  it('итальянская страница действительно итальянская: в ней нет кириллицы, кроме введённого контакта', async () => {
    i18n.global.locale.value = 'it'
    const html = (await renderToString(createSSRApp(AboutContent).use(i18n))).replace(/<!--[\s\S]*?-->/g, '') // без комментариев разработчика
    expect(html).not.toMatch(/[А-Яа-яЁё]/)
  })

  it('без контакта раздела контактов нет', async () => {
    const html = await renderToString(createSSRApp(AboutContent).use(i18n))
    expect(html).not.toContain('author-contact')
  })
})
