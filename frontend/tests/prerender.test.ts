// @vitest-environment node
import { describe, expect, it } from 'vitest'
import { createSSRApp } from 'vue'
import { renderToString } from 'vue/server-renderer'
import AboutContent from '@/components/AboutContent.vue'
import { ABOUT_INTRO, ABOUT_SECTIONS, LEVEL_ROWS, PRIVACY_ITEMS } from '@/content/about'
import { HOME_LEAD } from '@/content/site'
import { buildPage, escapeHtml } from '../scripts/prerender.mjs'

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
    expect(out).toContain('<div id="app"><!--prerender--><p>Текст</p><!--/prerender--></div>')
    expect(out).toContain('<script type="module" src="/assets/index.js"></script>')
    expect(out.indexOf('og:title')).toBeLessThan(out.indexOf('</head>'))
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
  })

  it('escapeHtml', () => {
    expect(escapeHtml(`<a href="x">'&'</a>`)).toBe('&lt;a href=&quot;x&quot;&gt;&#39;&amp;&#39;&lt;/a&gt;')
  })
})

describe('AboutContent на сервере', () => {
  it('рендерится без браузера, роутера и хранилищ: весь текст страницы есть в HTML', async () => {
    const html = await renderToString(createSSRApp(AboutContent, { contact: 'автор@example.org' }))
    expect(html).toContain(ABOUT_INTRO)
    for (const s of ABOUT_SECTIONS) {
      expect(html).toContain(`id="${s.id}"`)
      expect(html).toContain(s.title)
      for (const p of s.paragraphs) expect(html).toContain(escapeHtml(p).replace(/&#39;/g, "'").replace(/&quot;/g, '"'))
    }
    for (const r of LEVEL_ROWS) expect(html).toContain(r.name)
    for (const p of PRIVACY_ITEMS) expect(html).toContain(p.term)
    expect(html).toContain('автор@example.org')
    expect(html).toContain('href="#privacy"')
  })

  it('без контакта раздела контактов нет; текст главной задан один раз', async () => {
    const html = await renderToString(createSSRApp(AboutContent))
    expect(html).not.toContain('author-contact')
    expect(HOME_LEAD.length).toBeGreaterThan(40)
  })
})
