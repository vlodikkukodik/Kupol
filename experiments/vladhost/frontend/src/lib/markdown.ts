// Небольшой безопасный разбор разметки статей справки. Поддерживается только то, что нужно статьям: заголовки «##» и «###», абзацы,
// маркированные и нумерованные списки, блоки кода, цитаты-заметки «>», а внутри строк — `код`, **жирный** и ссылки [текст](адрес).
// Результат — дерево данных, которое компонент выводит обычными элементами (без v-html), поэтому произвольный HTML в статью не попадёт.

export type Inline =
  | { t: 'text'; v: string }
  | { t: 'code'; v: string }
  | { t: 'strong'; v: Inline[] }
  | { t: 'link'; v: string; href: string; internal: boolean }

export type Block =
  | { t: 'h2' | 'h3'; v: Inline[] }
  | { t: 'p' | 'note'; v: Inline[] }
  | { t: 'ul' | 'ol'; items: Inline[][] }
  | { t: 'code'; v: string }

export interface ArticleMeta {
  title: string
  category: string
  description: string
}

/** Ссылка допустима, если ведёт внутрь панели («/путь») или на https/http. Всё остальное (javascript:, data:, //хост) — обычный текст. */
export function linkKind(href: string): 'internal' | 'external' | null {
  if (/^\/(?!\/)[A-Za-z0-9\-._~/?#=&%]*$/.test(href)) return 'internal'
  if (/^https?:\/\/[^\s<>"']+$/i.test(href)) return 'external'
  return null
}

const INLINE = /`([^`]+)`|\*\*([^*]+)\*\*|\[([^\]]+)\]\(([^)\s]+)\)/g

export function parseInline(src: string): Inline[] {
  const out: Inline[] = []
  let last = 0
  for (const m of src.matchAll(INLINE)) {
    const at = m.index ?? 0
    if (at > last) out.push({ t: 'text', v: src.slice(last, at) })
    if (m[1] !== undefined) out.push({ t: 'code', v: m[1] })
    else if (m[2] !== undefined) out.push({ t: 'strong', v: parseInline(m[2]) })
    else {
      const kind = linkKind(m[4]!)
      if (kind) out.push({ t: 'link', v: m[3]!, href: m[4]!, internal: kind === 'internal' })
      else out.push({ t: 'text', v: m[0] })
    }
    last = at + m[0].length
  }
  if (last < src.length) out.push({ t: 'text', v: src.slice(last) })
  return out
}

/** Заголовок статьи: блок «--- … ---» с полями title, category, description; остальное — текст. */
export function parseArticleFile(raw: string): { meta: ArticleMeta; body: string } {
  const text = raw.replace(/\r\n/g, '\n')
  const m = /^---\n([\s\S]*?)\n---\n?([\s\S]*)$/.exec(text)
  const meta: ArticleMeta = { title: '', category: '', description: '' }
  if (!m) return { meta, body: text }
  for (const line of m[1]!.split('\n')) {
    const i = line.indexOf(':')
    if (i < 0) continue
    const key = line.slice(0, i).trim()
    const value = line.slice(i + 1).trim()
    if (key === 'title' || key === 'category' || key === 'description') meta[key] = value
  }
  return { meta, body: m[2]! }
}

export function parseMarkdown(src: string): Block[] {
  const lines = src.replace(/\r\n/g, '\n').split('\n')
  const blocks: Block[] = []
  let i = 0
  while (i < lines.length) {
    const line = lines[i]!
    if (line.trim() === '') {
      i++
      continue
    }
    if (line.startsWith('```')) {
      const code: string[] = []
      i++
      while (i < lines.length && !lines[i]!.startsWith('```')) code.push(lines[i++]!)
      i++ // закрывающая строка
      blocks.push({ t: 'code', v: code.join('\n') })
      continue
    }
    const h = /^(#{2,3})\s+(.*)$/.exec(line)
    if (h) {
      blocks.push({ t: h[1]!.length === 2 ? 'h2' : 'h3', v: parseInline(h[2]!) })
      i++
      continue
    }
    if (line.startsWith('>')) {
      const parts: string[] = []
      while (i < lines.length && lines[i]!.startsWith('>')) parts.push(lines[i++]!.replace(/^>\s?/, ''))
      blocks.push({ t: 'note', v: parseInline(parts.join(' ')) })
      continue
    }
    const ul = /^[-*]\s+/.test(line)
    const ol = /^\d+\.\s+/.test(line)
    if (ul || ol) {
      const re = ul ? /^[-*]\s+(.*)$/ : /^\d+\.\s+(.*)$/
      const items: Inline[][] = []
      while (i < lines.length && re.test(lines[i]!)) items.push(parseInline(re.exec(lines[i++]!)![1]!))
      blocks.push({ t: ul ? 'ul' : 'ol', items })
      continue
    }
    const para: string[] = []
    while (i < lines.length && lines[i]!.trim() !== '' && !/^(#{2,3}\s|```|>|[-*]\s|\d+\.\s)/.test(lines[i]!)) para.push(lines[i++]!.trim())
    blocks.push({ t: 'p', v: parseInline(para.join(' ')) })
  }
  return blocks
}

const inlineText = (v: Inline[]): string => v.map((x) => (x.t === 'strong' ? inlineText(x.v) : x.v)).join('')

/** Текст блоков без разметки — для поиска. */
export function plainText(blocks: Block[]): string {
  return blocks
    .map((b) => ('items' in b ? b.items.map(inlineText).join(' ') : b.t === 'code' ? b.v : inlineText(b.v)))
    .join(' ')
}
