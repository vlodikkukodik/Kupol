import type { Composed } from '@/api/generated/documents'
import { localeTag, t } from '@/i18n'

/** «Через сколько повторить» словами: секунды округляются вверх до целых минут и часов. */
export function formatWait(seconds: number): string {
  const s = Math.max(1, Math.ceil(Number(seconds) || 0))
  if (s < 60) return t('time.seconds', { n: s })
  const minutes = Math.ceil(s / 60)
  if (minutes < 60) return t('time.minutes', { n: minutes })
  const hours = Math.ceil(minutes / 60)
  return t('time.hours', { n: hours })
}

// Форматтеры Intl создаются под текущий язык; кэш по тегу языка, чтобы не строить их на каждую дату
const cache = new Map<string, Intl.DateTimeFormat>()
function formatter(tag: string, options: Intl.DateTimeFormatOptions): Intl.DateTimeFormat {
  const key = `${tag}|${JSON.stringify(options)}`
  let f = cache.get(key)
  if (!f) {
    f = new Intl.DateTimeFormat(tag, options)
    cache.set(key, f)
  }
  return f
}

/** Названия месяцев 1–12 в именительном падеже на текущем языке (для списков выбора): «январь» / «gennaio». */
export function monthNames(): string[] {
  const f = formatter(localeTag.value, { month: 'long', timeZone: 'UTC' })
  return Array.from({ length: 12 }, (_, i) => f.format(new Date(Date.UTC(2001, i, 15))))
}

/**
 * Дата составления внутри вселенной; месяц и день могут быть неизвестны:
 * {year: 1979} → «1979 г.», + month → «март 1979 г.», + day → «14 марта 1979 г.» (по-итальянски «1979», «marzo 1979», «14 marzo 1979»).
 * Считается в UTC: дата вымышленная и не зависит от часового пояса читателя.
 */
export function formatComposed(composed: Composed | null | undefined): string {
  if (!composed || !composed.year) return ''
  const { year, month, day } = composed
  if (!month) return t('time.year', { year })
  const d = new Date(0)
  d.setUTCFullYear(year, month - 1, day || 1) // год, месяц и день разом: 29 февраля високосного года не «переползает» в март
  if (!day) return formatter(localeTag.value, { month: 'long', year: 'numeric', timeZone: 'UTC' }).format(d)
  return formatter(localeTag.value, { day: 'numeric', month: 'long', year: 'numeric', timeZone: 'UTC' }).format(d)
}

const toDate = (v: string | Date): Date => (v instanceof Date ? v : new Date(v))

/** Дата ISO → «19 сентября 2026 г.»; для мусора — пустая строка. */
export function formatDate(iso: string | Date): string {
  const d = toDate(iso)
  return Number.isNaN(d.getTime()) ? '' : formatter(localeTag.value, { day: 'numeric', month: 'long', year: 'numeric', timeZone: 'UTC' }).format(d)
}

/** Момент → «19 сентября 2026 г. в 14:05» по часам читателя; для мусора — пустая строка. */
export function formatDateTime(value: string | Date): string {
  const d = toDate(value)
  return Number.isNaN(d.getTime()) ? '' : formatter(localeTag.value, { day: 'numeric', month: 'long', year: 'numeric', hour: '2-digit', minute: '2-digit' }).format(d)
}

/** Момент → «14:05» по часам читателя. */
export function formatTime(value: string | Date): string {
  const d = toDate(value)
  return Number.isNaN(d.getTime()) ? '' : formatter(localeTag.value, { hour: '2-digit', minute: '2-digit' }).format(d)
}
