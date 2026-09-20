// Предпросмотр «глазами уровня N»: вспомогательная логика. Что закрыто, решает сервер; здесь только подсчёт для подписи.
import type { OutDocument } from '@/api/generated/documents'

export interface RedactionStats {
  /** Блоки, закрытые целиком (метка redacted вместо блока) */
  blocks: number
  /** Закрытые фрагменты внутри открытых блоков */
  fragments: number
}

/** Сколько в ответе сервера закрытых блоков и фрагментов. Метка закрытого не несёт ни текста, ни длины — считаем только метки. */
export function redactionStats(doc: Pick<OutDocument, 'blocks'> | null | undefined): RedactionStats {
  const stats: RedactionStats = { blocks: 0, fragments: 0 }
  for (const block of doc?.blocks ?? []) {
    if (block.type === 'redacted') {
      stats.blocks++
      continue
    }
    stats.fragments += countFragments(block.data)
  }
  return stats
}

function countFragments(value: unknown): number {
  if (Array.isArray(value)) return value.reduce<number>((n, v) => n + countFragments(v), 0)
  if (value && typeof value === 'object') {
    const obj = value as Record<string, unknown>
    if (obj.redacted === true) return 1
    return Object.values(obj).reduce<number>((n, v) => n + countFragments(v), 0)
  }
  return 0
}

/** «2 блока и 3 фрагмента»; пусто — «ничего не закрыто». */
export function describeRedactions({ blocks, fragments }: RedactionStats): string {
  const plural = (n: number, one: string, few: string, many: string) => {
    const m10 = n % 10
    const m100 = n % 100
    return m10 === 1 && m100 !== 11 ? one : m10 >= 2 && m10 <= 4 && (m100 < 12 || m100 > 14) ? few : many
  }
  const parts: string[] = []
  if (blocks) parts.push(`${blocks} ${plural(blocks, 'блок', 'блока', 'блоков')}`)
  if (fragments) parts.push(`${fragments} ${plural(fragments, 'фрагмент', 'фрагмента', 'фрагментов')}`)
  return parts.length ? parts.join(' и ') : 'ничего не закрыто'
}
