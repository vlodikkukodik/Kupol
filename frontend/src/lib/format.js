/** «Через сколько повторить» словами: секунды округляются вверх до целых минут и часов. */
export function formatWait(seconds) {
  const s = Math.max(1, Math.ceil(Number(seconds) || 0))
  if (s < 60) return `${s} сек.`
  const minutes = Math.ceil(s / 60)
  if (minutes < 60) return `${minutes} мин.`
  const hours = Math.ceil(minutes / 60)
  return `${hours} ч.`
}

const MONTHS_NOMINATIVE = [
  'январь', 'февраль', 'март', 'апрель', 'май', 'июнь',
  'июль', 'август', 'сентябрь', 'октябрь', 'ноябрь', 'декабрь',
]
const MONTHS_GENITIVE = [
  'января', 'февраля', 'марта', 'апреля', 'мая', 'июня',
  'июля', 'августа', 'сентября', 'октября', 'ноября', 'декабря',
]

/**
 * Дата составления внутри вселенной; месяц и день могут быть неизвестны:
 * {year: 1979} -> «1979 г.», + month -> «март 1979 г.», + day -> «14 марта 1979 г.».
 */
export function formatComposed(composed) {
  if (!composed || !composed.year) return ''
  const { year, month, day } = composed
  if (!month) return `${year} г.`
  if (!day) return `${MONTHS_NOMINATIVE[month - 1]} ${year} г.`
  return `${day} ${MONTHS_GENITIVE[month - 1]} ${year} г.`
}

const dateFormat = new Intl.DateTimeFormat('ru-RU', { day: 'numeric', month: 'long', year: 'numeric', timeZone: 'UTC' })

/** Дата ISO -> «19 сентября 2026 г.»; для мусора — пустая строка. */
export function formatDate(iso) {
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? '' : dateFormat.format(d)
}

const dateTimeFormat = new Intl.DateTimeFormat('ru-RU', { day: 'numeric', month: 'long', year: 'numeric', hour: '2-digit', minute: '2-digit' })
const timeFormat = new Intl.DateTimeFormat('ru-RU', { hour: '2-digit', minute: '2-digit' })

/** Момент ISO (или Date) -> «19 сентября 2026 г. в 14:05» по часам читателя; для мусора — пустая строка. */
export function formatDateTime(value) {
  const d = value instanceof Date ? value : new Date(value)
  return Number.isNaN(d.getTime()) ? '' : dateTimeFormat.format(d)
}

/** Момент -> «14:05» по часам читателя. */
export function formatTime(value) {
  const d = value instanceof Date ? value : new Date(value)
  return Number.isNaN(d.getTime()) ? '' : timeFormat.format(d)
}
