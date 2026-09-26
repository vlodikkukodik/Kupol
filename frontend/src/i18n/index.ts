// Интернационализация интерфейса: русский (основной) и итальянский. Все тексты живут в src/i18n/messages/<язык>/*.ts;
// в шаблонах и коде — только ключи: $t('catalog.title') в шаблоне, t('catalog.title') в скрипте (import { t } from '@/i18n').
// Язык выбирает читатель (переключатель в шапке и меню), выбор запоминается; без выбора — по языку браузера (итальянский → итальянский,
// всё остальное — русский). Сервер тоже получает язык (заголовок Accept-Language) и отвечает сообщениями на нём.
import { computed, watch } from 'vue'
import { createI18n } from 'vue-i18n'
import { messages } from './messages'

export const LOCALES = ['ru', 'it'] as const
export type Locale = (typeof LOCALES)[number]
export const DEFAULT_LOCALE: Locale = 'ru'

/** Как язык записывается в BCP 47 для <html lang>, Intl и заголовка Accept-Language */
export const LOCALE_TAGS: Record<Locale, string> = { ru: 'ru-RU', it: 'it-IT' }

const STORAGE_KEY = 'kupol.locale'

export function isLocale(v: unknown): v is Locale {
  return typeof v === 'string' && (LOCALES as readonly string[]).includes(v)
}

function readStored(): Locale | null {
  try {
    const v = window.localStorage.getItem(STORAGE_KEY)
    return isLocale(v) ? v : null
  } catch {
    return null // хранилище недоступно (закрытое окно, запрет) — работаем без него
  }
}

/** Язык по списку предпочтений браузера: первый из известных нам; итальянский — только если он раньше русского. */
export function pickFromBrowser(languages: readonly string[]): Locale {
  for (const lang of languages) {
    const base = lang.toLowerCase().split('-')[0]
    if (isLocale(base)) return base
  }
  return DEFAULT_LOCALE
}

/** Язык из адреса (?lang=it): ссылка на итальянскую версию, в том числе из пререндера, открывает интерфейс по-итальянски */
function readFromUrl(): Locale | null {
  try {
    const v = new URLSearchParams(window.location.search).get('lang')
    return isLocale(v) ? v : null
  } catch {
    return null
  }
}

export function detectLocale(): Locale {
  const fromUrl = readFromUrl()
  if (fromUrl) {
    try {
      window.localStorage.setItem(STORAGE_KEY, fromUrl)
    } catch {
      /* без хранилища язык действует только на этот визит */
    }
    return fromUrl
  }
  const stored = readStored()
  if (stored) return stored
  const langs = typeof navigator !== 'undefined' ? (navigator.languages?.length ? navigator.languages : [navigator.language]) : []
  return pickFromBrowser(langs.filter(Boolean))
}

/**
 * Правило множественного числа. Русский: 1 файл, 2 файла, 5 файлов; итальянский: 1 file, 2 file (одна и много).
 * В сообщении формы через «|»: русское «один | несколько | много», итальянское «один | много». Ноль — форма «много».
 */
const pluralRules = {
  ru: (choice: number, choicesLength: number): number => {
    const n = Math.abs(choice)
    if (choicesLength < 3) return n === 1 ? 0 : 1
    const mod10 = n % 10
    const mod100 = n % 100
    if (mod10 === 1 && mod100 !== 11) return 0
    if (mod10 >= 2 && mod10 <= 4 && (mod100 < 12 || mod100 > 14)) return 1
    return 2
  },
  it: (choice: number): number => (Math.abs(choice) === 1 ? 0 : 1),
}

export const i18n = createI18n({
  legacy: false,
  locale: detectLocale(),
  fallbackLocale: DEFAULT_LOCALE,
  messages,
  pluralRules,
  // потерянный ключ виден в консоли при разработке; тест паритета языков не даёт ему попасть в сборку
  missingWarn: import.meta.env.DEV,
  fallbackWarn: import.meta.env.DEV,
})

const composer = i18n.global

/** Перевод вне компонентов (библиотеки, сторы, роутер): t('ключ', { имя: значение }) */
export const t = composer.t
/** Множественное число: tc('ключ', число) — форма выбирается по числу, само число доступно как {n} */
export const tc = (key: string, n: number, named: Record<string, unknown> = {}): string => composer.t(key, { n, ...named }, n)
/** Есть ли ключ в текущем языке (для необязательных подписей) */
export const te = composer.te

export const locale = computed<Locale>(() => (isLocale(composer.locale.value) ? composer.locale.value : DEFAULT_LOCALE))
/** Тег для Intl (ru-RU / it-IT) */
export const localeTag = computed(() => LOCALE_TAGS[locale.value])

/** Задать язык интерфейса и запомнить выбор. Остальное (lang документа, заголовки, кэш запросов) подхватывают наблюдатели. */
export function setLocale(next: Locale): void {
  composer.locale.value = next
  try {
    window.localStorage.setItem(STORAGE_KEY, next)
  } catch {
    /* выбор просто не запомнится */
  }
}

/** Слушатели смены языка, которым нужен именно момент смены (например, сбросить кэш ответов сервера) */
export function onLocaleChange(fn: (next: Locale) => void): () => void {
  return watch(locale, (next) => fn(next), { flush: 'sync' })
}

// <html lang> всегда совпадает с языком интерфейса: от него зависят скринридеры, переносы и подбор шрифта
if (typeof document !== 'undefined') {
  document.documentElement.lang = locale.value
  watch(locale, (next) => {
    document.documentElement.lang = next
  })
}
