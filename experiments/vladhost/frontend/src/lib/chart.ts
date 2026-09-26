// Помощники для графиков статистики: красивые границы оси и подписи суток.

/** Верхняя граница оси: 1, 2, 2.5 или 5 × 10^n, не меньше значения. Для нуля — 1, чтобы ось не схлопывалась. */
export function niceMax(v: number): number {
  if (!Number.isFinite(v) || v <= 0) return 1
  const exp = Math.floor(Math.log10(v))
  const base = 10 ** exp
  for (const m of [1, 2, 2.5, 5, 10]) {
    if (m * base >= v) return m * base
  }
  return 10 * base
}

/** Три отметки оси: 0, середина, максимум. */
export function ticks(max: number): number[] {
  return [0, max / 2, max]
}

/** Сутки YYYY-MM-DD (UTC) → Date в полдень UTC: так подпись не «съезжает» на соседний день в часовых поясах. */
export function dayDate(day: string): Date {
  return new Date(`${day}T12:00:00Z`)
}

/** Доля в процентах с одним знаком после запятой для малых значений; при нулевом знаменателе — 0. */
export function percent(part: number, whole: number): number {
  if (whole <= 0) return 0
  const p = (part / whole) * 100
  return p >= 10 ? Math.round(p) : Math.round(p * 10) / 10
}

/** Индексы подписей оси X: первый, последний и равномерно между ними, не чаще чем через `step` столбцов. */
export function labelIndexes(n: number, maxLabels: number): number[] {
  if (n <= 0) return []
  if (n <= maxLabels) return Array.from({ length: n }, (_, i) => i)
  const out: number[] = []
  const step = (n - 1) / (maxLabels - 1)
  for (let i = 0; i < maxLabels; i++) out.push(Math.round(i * step))
  return out
}
