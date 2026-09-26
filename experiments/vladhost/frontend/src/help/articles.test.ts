import { describe, expect, it } from 'vitest'
import { linkKind, parseArticleFile, parseInline, parseMarkdown, plainText, type Block, type Inline } from '@/lib/markdown'
import { articles, HELP_CATEGORIES, searchArticles } from './articles'

const hasCyrillic = (s: string) => /\p{Script=Cyrillic}/u.test(s)

describe('разбор разметки статей', () => {
  it('понимает строчные элементы', () => {
    expect(parseInline('a `код` и **жирный** [тут](/sites) и [там](https://example.com)')).toEqual([
      { t: 'text', v: 'a ' },
      { t: 'code', v: 'код' },
      { t: 'text', v: ' и ' },
      { t: 'strong', v: [{ t: 'text', v: 'жирный' }] },
      { t: 'text', v: ' ' },
      { t: 'link', v: 'тут', href: '/sites', internal: true },
      { t: 'text', v: ' и ' },
      { t: 'link', v: 'там', href: 'https://example.com', internal: false },
    ])
  })

  it('опасные ссылки остаются текстом, а HTML не разбирается', () => {
    for (const bad of ['javascript:alert(1)', 'data:text/html,x', '//evil.example', 'ftp://x.example', '/a b']) {
      expect(linkKind(bad), bad).toBeNull()
    }
    const out = parseInline('[клик](javascript:alert(1)) <b>x</b> <script>alert(1)</script>')
    expect(out.every((n: Inline) => n.t === 'text')).toBe(true)
    expect(plainText([{ t: 'p', v: out }])).toContain('<script>')
  })

  it('делит текст на блоки', () => {
    const blocks = parseMarkdown(
      ['## Заголовок', '', 'Первая строка', 'вторая строка.', '', '- один', '- два', '', '1. раз', '2. два', '', '> заметка', '> продолжение', '', '```', 'node server.js', '```', '', '### Малый'].join('\n'),
    )
    expect(blocks.map((b: Block) => b.t)).toEqual(['h2', 'p', 'ul', 'ol', 'note', 'code', 'h3'])
    expect(blocks[1]).toEqual({ t: 'p', v: [{ t: 'text', v: 'Первая строка вторая строка.' }] })
    expect((blocks[2] as { items: Inline[][] }).items).toHaveLength(2)
    expect(blocks[5]).toEqual({ t: 'code', v: 'node server.js' })
  })

  it('читает заголовок статьи', () => {
    const { meta, body } = parseArticleFile('---\ntitle: Тема: с двоеточием\ncategory: mail\ndescription: Кратко\n---\n## Раздел\n')
    expect(meta).toEqual({ title: 'Тема: с двоеточием', category: 'mail', description: 'Кратко' })
    expect(body).toBe('## Раздел\n')
    expect(parseArticleFile('без заголовка').meta.title).toBe('')
  })
})

// Внутренние адреса панели, на которые статьи вправе ссылаться.
const ROUTES = ['/sites', '/dns', '/mail', '/databases', '/ssh', '/cron', '/settings', '/support', '/help']

describe('статьи справки', () => {
  const ru = articles('ru')
  const it_ = articles('it')

  it('есть, и набор на обоих языках одинаковый', () => {
    expect(ru.length).toBeGreaterThanOrEqual(10)
    expect(it_.map((a) => [a.slug, a.category])).toEqual(ru.map((a) => [a.slug, a.category]))
  })

  it('у каждой статьи есть заголовок, описание, известный раздел и текст', () => {
    for (const a of [...ru, ...it_]) {
      expect(a.title.length, a.slug).toBeGreaterThan(3)
      expect(a.description.length, a.slug).toBeGreaterThan(10)
      expect(HELP_CATEGORIES as readonly string[], `${a.slug}: раздел ${a.category}`).toContain(a.category)
      expect(a.blocks.length, a.slug).toBeGreaterThan(3)
      expect(a.blocks[0]!.t, `${a.slug}: статья начинается с заголовка раздела`).toBe('h2')
    }
  })

  it('русские тексты русские, итальянские без кириллицы', () => {
    for (const a of ru) expect(hasCyrillic(a.title + a.description), a.slug).toBe(true)
    for (const a of it_) expect(hasCyrillic(a.title + a.description + a.search), `it/${a.slug}`).toBe(false)
  })

  it('ссылки ведут на существующие страницы, а сырого HTML в текстах нет', () => {
    for (const a of [...ru, ...it_]) {
      const walk = (nodes: Inline[]) => {
        for (const n of nodes) {
          if (n.t === 'link') {
            if (n.internal) expect(ROUTES, `${a.slug}: ${n.href}`).toContain(n.href)
          } else if (n.t === 'strong') walk(n.v)
        }
      }
      for (const b of a.blocks) {
        if ('items' in b) b.items.forEach(walk)
        else if (b.t !== 'code') walk(b.v)
      }
      expect(a.search, a.slug).not.toMatch(/<\/?[a-z][^>]*>/i)
    }
  })

  it('у каждого раздела есть хотя бы одна статья', () => {
    for (const c of HELP_CATEGORIES) expect(ru.some((a) => a.category === c), c).toBe(true)
  })

  it('поиск требует все слова и не различает регистр', () => {
    expect(searchArticles(ru, '').length).toBe(ru.length)
    const hits = searchArticles(ru, 'СЕРТИФИКАТ HTTPS')
    expect(hits.some((a) => a.slug.includes('https'))).toBe(true)
    expect(searchArticles(ru, 'такого слова нет нигде qwertyzxc')).toEqual([])
    expect(searchArticles(it_, 'certificato').some((a) => a.slug.includes('https'))).toBe(true)
  })
})
