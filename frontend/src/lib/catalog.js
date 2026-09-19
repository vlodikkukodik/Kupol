// Каталог: состояние живёт в адресе (/catalog?type=object&class=3&sort=year&order=desc&page=2) —
// им можно поделиться, «назад» работает, перезагрузка ничего не теряет.

export const CATEGORY_NAMES = {
  person: 'Человек с аномальными свойствами',
  entity: 'Существо или явление',
  place: 'Место или локация',
}

export const CONTAINMENT_NAMES = {
  contained: 'Содержится',
  lost: 'Утрачен',
  destroyed: 'Уничтожен',
  studying: 'Изучается',
}

export const SORTS = ['code', 'title', 'year', 'class', 'deviation']
export const PER_PAGE = 25

// имя в адресе -> имя параметра API
const FILTER_KEYS = {
  type: 'type',
  class: 'class',
  dept: 'department',
  category: 'category',
  containment: 'containment',
  from: 'year_from',
  to: 'year_to',
}

const first = (v) => (Array.isArray(v) ? v[0] : v)

/** Параметры запроса к API из query адреса; неизвестное и пустое отбрасывается. */
export function toApiParams(query) {
  const p = new URLSearchParams()
  for (const [key, apiKey] of Object.entries(FILTER_KEYS)) {
    const v = first(query[key])
    if (v) p.set(apiKey, v)
  }
  const sort = first(query.sort)
  if (SORTS.includes(sort)) p.set('sort', sort)
  if (first(query.order) === 'desc') p.set('order', 'desc')
  const page = Number.parseInt(first(query.page), 10)
  if (page > 1) p.set('page', String(page))
  p.set('per_page', String(PER_PAGE))
  return p
}

/** Есть ли включённые фильтры (для сообщения «ничего не найдено» и кнопки сброса). */
export function hasFilters(query) {
  return Object.keys(FILTER_KEYS).some((k) => Boolean(first(query[k])))
}

/** Новый query при смене фильтров: пустые значения убираются, страница сбрасывается, вид и сортировка остаются. */
export function withFilters(query, changes) {
  const next = { ...query, ...changes }
  delete next.page
  for (const k of Object.keys(next)) {
    if (next[k] === '' || next[k] == null) delete next[k]
  }
  return next
}

/** Клик по заголовку столбца: тот же — меняет направление, другой — сортирует по возрастанию. */
export function nextSort(query, key) {
  const current = SORTS.includes(first(query.sort)) ? first(query.sort) : 'code'
  const desc = first(query.order) === 'desc'
  const next = { ...query }
  delete next.page
  next.sort = key
  if (current === key && !desc) next.order = 'desc'
  else delete next.order
  return next
}

/** aria-sort для заголовка столбца. */
export function ariaSort(query, key) {
  const current = SORTS.includes(first(query.sort)) ? first(query.sort) : 'code'
  if (current !== key) return 'none'
  return first(query.order) === 'desc' ? 'descending' : 'ascending'
}

/** Номера страниц для навигации: первая, последняя и окно вокруг текущей; разрывы — null. */
export function pageWindow(page, pages, radius = 2) {
  if (pages <= 1) return []
  const set = new Set([1, pages])
  for (let p = page - radius; p <= page + radius; p++) if (p >= 1 && p <= pages) set.add(p)
  const sorted = [...set].sort((a, b) => a - b)
  const out = []
  sorted.forEach((p, i) => {
    if (i > 0) {
      const gap = p - sorted[i - 1] - 1 // сколько страниц пропущено
      if (gap === 1) out.push(p - 1) // «…» вместо одной страницы — лишнее: покажем её саму
      else if (gap > 1) out.push(null)
    }
    out.push(p)
  })
  return out
}
