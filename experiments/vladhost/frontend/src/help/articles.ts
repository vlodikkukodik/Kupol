// Справка: статьи лежат в markdown-файлах src/help/ru и src/help/it (одно имя файла — одна статья на обоих языках).
import { parseArticleFile, parseMarkdown, plainText, type Block } from '@/lib/markdown'
import type { Locale } from '@/i18n'

export interface Article {
  slug: string
  category: string
  title: string
  description: string
  blocks: Block[]
  /** Текст без разметки в нижнем регистре: по нему идёт поиск. */
  search: string
}

const raw: Record<Locale, Record<string, string>> = {
  ru: import.meta.glob('./ru/*.md', { query: '?raw', import: 'default', eager: true }) as Record<string, string>,
  it: import.meta.glob('./it/*.md', { query: '?raw', import: 'default', eager: true }) as Record<string, string>,
}

/** Разделы справки в порядке показа. */
export const HELP_CATEGORIES = ['start', 'sites', 'domains', 'dns', 'mail', 'data', 'apps', 'access'] as const
export type HelpCategory = (typeof HELP_CATEGORIES)[number]

const slugOf = (path: string) => path.replace(/^.*\//, '').replace(/\.md$/, '')

function build(locale: Locale): Article[] {
  return Object.entries(raw[locale])
    .map(([path, text]) => {
      const { meta, body } = parseArticleFile(text)
      const blocks = parseMarkdown(body)
      return {
        slug: slugOf(path),
        category: meta.category,
        title: meta.title,
        description: meta.description,
        blocks,
        search: `${meta.title} ${meta.description} ${plainText(blocks)}`.toLowerCase(),
      }
    })
    .sort((a, b) => a.slug.localeCompare(b.slug))
}

const cache = new Map<Locale, Article[]>()

/** Статьи на языке интерфейса. */
export function articles(locale: Locale): Article[] {
  let list = cache.get(locale)
  if (!list) cache.set(locale, (list = build(locale)))
  return list
}

/** Поиск по заголовку, описанию и тексту: все слова запроса должны встретиться. */
export function searchArticles(list: Article[], query: string): Article[] {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean)
  if (!words.length) return list
  return list.filter((a) => words.every((w) => a.search.includes(w)))
}
