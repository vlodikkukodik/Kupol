// Каталог: состояние живёт в адресе (/catalog?type=object&class=3&sort=year&order=desc&page=2) —
// им можно поделиться, «назад» работает, перезагрузка ничего не теряет.
import type { LocationQuery, LocationQueryRaw } from 'vue-router'

export const CATEGORY_NAMES: Record<string, string> = {
  person: 'Человек с аномальными свойствами',
  entity: 'Существо или явление',
  place: 'Место или локация',
}

export const CONTAINMENT_NAMES: Record<string, string> = {
  contained: 'Содержится',
  lost: 'Утрачен',
  destroyed: 'Уничтожен',
  studying: 'Изучается',
}

export const SORTS = ['code', 'title', 'year', 'class', 'deviation'] as const
export type SortKey = (typeof SORTS)[number]
export const PER_PAGE = 25

// имя в адресе → имя параметра API
const FILTER_KEYS: Record<string, string> = {
  type: 'type',
  class: 'class',
  dept: 'department',
  category: 'category',
  containment: 'containment',
  from: 'year_from',
  to: 'year_to',
}

type Query = LocationQuery | LocationQueryRaw

const first = (v: unknown): string | undefined => {
  const x = Array.isArray(v) ? v[0] : v
  return typeof x === 'string' ? x : undefined
}

const sortOf = (query: Query): SortKey => {
  const s = first(query.sort)
  return (SORTS as readonly string[]).includes(s ?? '') ? (s as SortKey) : 'code'
}

/** Параметры запроса к API из query адреса; неизвестное и пустое отбрасывается. */
export function toApiParams(query: Query): URLSearchParams {
  const p = new URLSearchParams()
  for (const [key, apiKey] of Object.entries(FILTER_KEYS)) {
    const v = first(query[key])
    if (v) p.set(apiKey, v)
  }
  const sort = first(query.sort)
  if ((SORTS as readonly string[]).includes(sort ?? '')) p.set('sort', sort as string)
  if (first(query.order) === 'desc') p.set('order', 'desc')
  const page = Number.parseInt(first(query.page) ?? '', 10)
  if (page > 1) p.set('page', String(page))
  p.set('per_page', String(PER_PAGE))
  return p
}

/** Есть ли включённые фильтры (для сообщения «ничего не найдено» и кнопки сброса). */
export function hasFilters(query: Query): boolean {
  return Object.keys(FILTER_KEYS).some((k) => Boolean(first(query[k])))
}

/** Новый query при смене фильтров: пустые значения убираются, страница сбрасывается, вид и сортировка остаются. */
export function withFilters(query: Query, changes: Record<string, string | null | undefined>): LocationQueryRaw {
  const next: LocationQueryRaw = { ...query, ...changes }
  delete next.page
  for (const k of Object.keys(next)) {
    if (next[k] === '' || next[k] == null) delete next[k]
  }
  return next
}

/** Клик по заголовку столбца: тот же — меняет направление, другой — сортирует по возрастанию. */
export function nextSort(query: Query, key: SortKey): LocationQueryRaw {
  const current = sortOf(query)
  const desc = first(query.order) === 'desc'
  const next: LocationQueryRaw = { ...query }
  delete next.page
  next.sort = key
  if (current === key && !desc) next.order = 'desc'
  else delete next.order
  return next
}

/** aria-sort для заголовка столбца. */
export function ariaSort(query: Query, key: SortKey): 'none' | 'ascending' | 'descending' {
  if (sortOf(query) !== key) return 'none'
  return first(query.order) === 'desc' ? 'descending' : 'ascending'
}

/** Номера страниц для навигации: первая, последняя и окно вокруг текущей; разрывы — null. */
export function pageWindow(page: number, pages: number, radius = 2): (number | null)[] {
  if (pages <= 1) return []
  const set = new Set([1, pages])
  for (let p = page - radius; p <= page + radius; p++) if (p >= 1 && p <= pages) set.add(p)
  const sorted = [...set].sort((a, b) => a - b)
  const out: (number | null)[] = []
  sorted.forEach((p, i) => {
    const prev = sorted[i - 1]
    if (prev !== undefined) {
      const gap = p - prev - 1 // сколько страниц пропущено
      if (gap === 1) out.push(p - 1) // «…» вместо одной страницы — лишнее: покажем её саму
      else if (gap > 1) out.push(null)
    }
    out.push(p)
  })
  return out
}
