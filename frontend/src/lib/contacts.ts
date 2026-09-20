// Контакты автора — обычный текст, который вводит Директорат. Ссылками становятся только явные адреса: https://…, http://… и почта.
// Ничего другого (javascript:, data: и т.п.) ссылкой не станет — разбор пропускает только эти три вида.

export interface ContactPart {
  text: string
  /** Есть только у ссылок */
  href?: string
}

// адрес до пробела; хвостовая пунктуация («…example.org.», «(…)») в ссылку не входит
const TOKEN = /(https?:\/\/[^\s<>"']+|[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)+)/g
const TRAILING = /[.,;:!?)\]»”]+$/

/** Разбирает строку на куски текста и ссылки. */
export function linkifyContact(line: string): ContactPart[] {
  const parts: ContactPart[] = []
  let last = 0
  for (const m of line.matchAll(TOKEN)) {
    const start = m.index ?? 0
    let token = m[0]
    const tail = TRAILING.exec(token)?.[0] ?? ''
    if (tail) token = token.slice(0, token.length - tail.length)
    if (start > last) parts.push({ text: line.slice(last, start) })
    const isUrl = /^https?:\/\//i.test(token)
    if (isUrl) {
      try {
        const u = new URL(token)
        if (u.protocol !== 'http:' && u.protocol !== 'https:') throw new Error('схема')
        parts.push({ text: token, href: u.href })
      } catch {
        parts.push({ text: token })
      }
    } else {
      parts.push({ text: token, href: `mailto:${token}` })
    }
    last = start + token.length
  }
  if (last < line.length) parts.push({ text: line.slice(last) })
  return parts
}
